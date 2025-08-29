package transfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ericlagergren/decimal"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/hzbay/chain-bridge/internal/queue"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/rs/zerolog/log"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	sqltypes "github.com/volatiletech/sqlboiler/v4/types"
)

// Service defines the transfer service interface
type Service interface {
	TransferAssets(ctx context.Context, req *types.TransferRequest) (*types.TransferResponse, *types.BatchInfo, error)
	GetUserTransactions(ctx context.Context, userID string, params GetTransactionsParams) (*types.TransactionHistoryResponse, error)
}

// TODO: 这里使用make swagger生成的结构体，不要自己定义
// GetTransactionsParams contains parameters for querying user transactions
type GetTransactionsParams struct {
	ChainID     *int64
	TokenSymbol *string
	TxType      *string
	Page        int
	Limit       int
	StartDate   *string
	EndDate     *string
}

// service implements the transfer service
type service struct {
	db             *sql.DB
	batchProcessor queue.BatchProcessor
	batchOptimizer *queue.BatchOptimizer

	// Token caching
	tokenCache map[string]int // "chainID:symbol" -> tokenID
	cacheMutex sync.RWMutex
}

// NewService creates a new transfer service
func NewService(db *sql.DB, batchProcessor queue.BatchProcessor, batchOptimizer *queue.BatchOptimizer) Service {
	return &service{
		db:             db,
		batchProcessor: batchProcessor,
		batchOptimizer: batchOptimizer,
		tokenCache:     make(map[string]int),
	}
}

// TransferAssets handles user-to-user asset transfers
func (s *service) TransferAssets(ctx context.Context, req *types.TransferRequest) (*types.TransferResponse, *types.BatchInfo, error) {
	log.Info().
		Str("operation_id", *req.OperationID).
		Str("from_user_id", *req.FromUserID).
		Str("to_user_id", *req.ToUserID).
		Str("amount", *req.Amount).
		Int64("chain_id", *req.ChainID).
		Str("token_symbol", *req.TokenSymbol).
		Msg("Processing transfer request")

	// Request validation is handled at handler layer

	// 1. 幂等性检查 - 检查 OperationID 是否已经存在
	mainOperationID := uuid.MustParse(*req.OperationID)
	existingTx, err := models.Transactions(
		models.TransactionWhere.OperationID.EQ(null.StringFrom(mainOperationID.String())),
		models.TransactionWhere.TXType.EQ("transfer"),
		models.TransactionWhere.BusinessType.EQ("transfer"),
	).One(ctx, s.db)

	if err == nil && existingTx != nil {
		// OperationID 已存在，返回已有结果
		log.Info().
			Str("operation_id", *req.OperationID).
			Str("existing_tx_id", existingTx.TXID).
			Msg("Transfer operation already processed, returning existing result")

		existingResponse, batchInfo, err := s.buildExistingTransferResponse(ctx, mainOperationID.String())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build existing transfer response: %w", err)
		}

		return existingResponse, batchInfo, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		// 数据库查询错误（非记录不存在）
		return nil, nil, fmt.Errorf("failed to check operation idempotency: %w", err)
	}

	// OperationID 不存在，继续正常处理
	log.Debug().Str("operation_id", *req.OperationID).Msg("New transfer operation, proceeding with processing")

	// 2. Generate transaction IDs
	outgoingTxID := uuid.New()
	incomingTxID := uuid.New()
	operationID := mainOperationID

	// 3. Create outgoing transaction record (debit from sender)
	outgoingTx := &models.Transaction{
		TXID:              outgoingTxID.String(),
		OperationID:       null.StringFrom(operationID.String()),
		UserID:            *req.FromUserID,
		ChainID:           *req.ChainID,
		TXType:            "transfer",
		BusinessType:      "transfer",
		RelatedUserID:     null.StringFrom(*req.ToUserID),
		TransferDirection: null.StringFrom("outgoing"),
		TokenID:           s.getTokenIDBySymbol(*req.ChainID, *req.TokenSymbol),
		Amount: sqltypes.NewDecimal(func() *decimal.Big {
			d, _ := decimal.New(0, 0).SetString(fmt.Sprintf("-%s", *req.Amount))
			return d
		}()), // Negative for outgoing
		Status:           null.StringFrom("pending"),
		IsBatchOperation: null.BoolFrom(true),
		ReasonType:       "user_transfer",
		ReasonDetail:     null.StringFromPtr(&req.Memo),
	}

	// 4. Create incoming transaction record (credit to recipient)
	incomingTx := &models.Transaction{
		TXID:              incomingTxID.String(),
		OperationID:       null.StringFrom(operationID.String()),
		UserID:            *req.ToUserID,
		ChainID:           *req.ChainID,
		TXType:            "transfer",
		BusinessType:      "transfer",
		RelatedUserID:     null.StringFrom(*req.FromUserID),
		TransferDirection: null.StringFrom("incoming"),
		TokenID:           s.getTokenIDBySymbol(*req.ChainID, *req.TokenSymbol),
		Amount: sqltypes.NewDecimal(func() *decimal.Big {
			d, _ := decimal.New(0, 0).SetString(*req.Amount)
			return d
		}()), // Positive for incoming
		Status:           null.StringFrom("pending"),
		IsBatchOperation: null.BoolFrom(true),
		ReasonType:       "user_transfer",
		ReasonDetail:     null.StringFromPtr(&req.Memo),
	}

	// 5. Insert both transaction records in a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Warn().Err(err).Msg("Failed to rollback transaction")
		}
	}()

	if err := outgoingTx.Insert(ctx, tx, boil.Infer()); err != nil {
		return nil, nil, fmt.Errorf("failed to insert outgoing transaction: %w", err)
	}

	if err := incomingTx.Insert(ctx, tx, boil.Infer()); err != nil {
		return nil, nil, fmt.Errorf("failed to insert incoming transaction: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 6. Create transfer job and publish to queue
	transferJob := queue.TransferJob{
		ID:            operationID.String(),
		TransactionID: operationID, // Use operation ID as the main transaction ID
		ChainID:       *req.ChainID,
		TokenID:       s.getTokenIDBySymbol(*req.ChainID, *req.TokenSymbol),
		FromUserID:    *req.FromUserID,
		ToUserID:      *req.ToUserID,
		Amount:        *req.Amount,
		BusinessType:  "transfer",
		ReasonType:    "user_transfer",
		ReasonDetail:  req.Memo,
		Priority:      queue.PriorityNormal,
		CreatedAt:     time.Now(),
	}

	// 7. Publish to batch processor
	if err := s.batchProcessor.PublishTransfer(ctx, transferJob); err != nil {
		log.Error().Err(err).
			Str("operation_id", operationID.String()).
			Msg("Failed to publish transfer job, but transactions are recorded")

		// Note: We don't return error here because transactions are already recorded
		// The batch processor failure doesn't mean the transfer failed
		// TODO: Consider implementing a retry mechanism or dead letter queue
	}

	// 8. Build response (API format unchanged)
	transferRecords := []*types.TransferRecord{
		{
			Amount:            fmt.Sprintf("-%s", *req.Amount),
			TxID:              outgoingTxID.String(),
			TransferDirection: "outgoing",
			UserID:            *req.FromUserID,
		},
		{
			Amount:            *req.Amount,
			TxID:              incomingTxID.String(),
			TransferDirection: "incoming",
			UserID:            *req.ToUserID,
		},
	}

	response := &types.TransferResponse{
		Amount:          req.Amount,
		ChainID:         req.ChainID,
		FromUserID:      req.FromUserID,
		ToUserID:        req.ToUserID,
		OperationID:     func() *string { s := operationID.String(); return &s }(),
		Status:          func() *string { s := "recorded"; return &s }(), // Status: recorded -> batching -> submitted -> confirmed
		TokenSymbol:     req.TokenSymbol,
		TransferRecords: transferRecords,
	}

	// 9. Build batch info for client
	batchInfo := &types.BatchInfo{
		WillBeBatched:      true,
		BatchType:          "batchTransferFrom",
		CurrentBatchSize:   int64(s.getCurrentBatchSize(*req.ChainID, s.getTokenIDBySymbol(*req.ChainID, *req.TokenSymbol))),
		OptimalBatchSize:   int64(s.getOptimalBatchSize(*req.ChainID, s.getTokenIDBySymbol(*req.ChainID, *req.TokenSymbol))),
		ExpectedEfficiency: "74-76%",
	}

	log.Info().
		Str("operation_id", operationID.String()).
		Str("status", "recorded").
		Msg("Transfer assets request processed successfully")

	return response, batchInfo, nil
}

// GetUserTransactions retrieves user transaction history
func (s *service) GetUserTransactions(ctx context.Context, userID string, params GetTransactionsParams) (*types.TransactionHistoryResponse, error) {
	log.Info().
		Str("user_id", userID).
		Int("page", params.Page).
		Int("limit", params.Limit).
		Msg("Getting user transactions")

	// Build query conditions
	queryMods := []qm.QueryMod{
		models.TransactionWhere.UserID.EQ(userID),
		qm.Load(models.TransactionRels.Token),
		qm.Load(models.TransactionRels.Chain),
		qm.OrderBy(models.TransactionColumns.CreatedAt + " DESC"),
	}

	// Apply filters
	if params.ChainID != nil {
		queryMods = append(queryMods, models.TransactionWhere.ChainID.EQ(*params.ChainID))
	}
	if params.TokenSymbol != nil {
		// Join with supported_tokens to filter by symbol
		queryMods = append(queryMods, qm.InnerJoin("supported_tokens st ON st.id = transactions.token_id"))
		queryMods = append(queryMods, qm.Where("st.symbol = ?", *params.TokenSymbol))
	}
	if params.TxType != nil {
		queryMods = append(queryMods, models.TransactionWhere.TXType.EQ(*params.TxType))
	}
	if params.StartDate != nil {
		queryMods = append(queryMods, qm.Where("transactions.created_at >= ?", *params.StartDate))
	}
	if params.EndDate != nil {
		queryMods = append(queryMods, qm.Where("transactions.created_at <= ?", *params.EndDate))
	}

	// Get total count (without pagination)
	totalCount, err := models.Transactions(queryMods...).Count(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("failed to count transactions: %w", err)
	}

	// Apply pagination
	offset := (params.Page - 1) * params.Limit
	queryMods = append(queryMods, qm.Offset(offset), qm.Limit(params.Limit))

	// Execute query
	transactions, err := models.Transactions(queryMods...).All(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}

	// Convert to API types
	transactionInfos := make([]*types.TransactionInfo, len(transactions))
	var totalIncoming, totalOutgoing, totalGasSaved float64

	for i, tx := range transactions {
		// Parse amount as decimal for calculations
		amount, _ := tx.Amount.Float64()

		if amount > 0 {
			totalIncoming += amount
		} else {
			totalOutgoing += -amount // Convert negative to positive
		}

		// Calculate gas saved if available
		if val, ok := tx.GasSavedPercentage.Float64(); ok {
			// Mock calculation - in real implementation would depend on tx value
			gasSaved := amount * val / 100 * 0.001 // rough estimate
			totalGasSaved += gasSaved
		}

		// Build transaction info
		amountStr := tx.Amount.String()
		txInfo := &types.TransactionInfo{
			TxID:    &tx.TXID,
			ChainID: &tx.ChainID,
			Amount:  &amountStr,
			TxType:  &tx.TXType,
		}

		if tx.Status.Valid {
			txInfo.Status = &tx.Status.String
		}
		if tx.ReasonType != "" {
			txInfo.ReasonType = tx.ReasonType
		}
		if tx.CreatedAt.Valid {
			createdAt := strfmt.DateTime(tx.CreatedAt.Time)
			txInfo.CreatedAt = createdAt
		}

		// Add optional fields
		if tx.OperationID.Valid {
			txInfo.OperationID = tx.OperationID.String
		}
		if tx.TXHash.Valid {
			txInfo.TxHash = tx.TXHash.String
		}
		if tx.RelatedUserID.Valid {
			txInfo.RelatedUserID = tx.RelatedUserID.String
		}
		if tx.TransferDirection.Valid {
			txInfo.TransferDirection = tx.TransferDirection.String
		}
		if tx.ReasonDetail.Valid {
			txInfo.ReasonDetail = tx.ReasonDetail.String
		}
		if tx.BatchID.Valid {
			txInfo.BatchID = tx.BatchID.String
		}
		if tx.IsBatchOperation.Valid {
			txInfo.IsBatchOperation = &tx.IsBatchOperation.Bool
		}
		if val, ok := tx.GasSavedPercentage.Float64(); ok {
			txInfo.GasSavedPercentage = float32(val)
		}
		if tx.ConfirmedAt.Valid {
			confirmedAt := strfmt.DateTime(tx.ConfirmedAt.Time)
			txInfo.ConfirmedAt = confirmedAt
		}

		// Add chain name if loaded
		if tx.R != nil && tx.R.Chain != nil {
			txInfo.ChainName = tx.R.Chain.Name
		}

		// Add token symbol if loaded
		if tx.R != nil && tx.R.Token != nil {
			tokenSymbol := tx.R.Token.Symbol
			txInfo.TokenSymbol = &tokenSymbol
		}

		transactionInfos[i] = txInfo
	}

	// Build response
	totalCountInt64 := int64(totalCount)
	pageInt64 := int64(params.Page)
	limitInt64 := int64(params.Limit)

	response := &types.TransactionHistoryResponse{
		UserID:       &userID,
		TotalCount:   &totalCountInt64,
		Page:         &pageInt64,
		Limit:        &limitInt64,
		Transactions: transactionInfos,
	}

	// Calculate net change for logging
	netChange := totalIncoming - totalOutgoing

	log.Info().
		Str("user_id", userID).
		Int64("total_count", totalCountInt64).
		Int("returned_count", len(transactions)).
		Float64("total_incoming", totalIncoming).
		Float64("total_outgoing", totalOutgoing).
		Float64("net_change", netChange).
		Msg("User transactions retrieved successfully")

	return response, nil
}

// Helper methods

func (s *service) getTokenIDBySymbol(chainID int64, symbol string) int {
	cacheKey := fmt.Sprintf("%d:%s", chainID, symbol)

	// Check cache first
	s.cacheMutex.RLock()
	if tokenID, exists := s.tokenCache[cacheKey]; exists {
		s.cacheMutex.RUnlock()
		return tokenID
	}
	s.cacheMutex.RUnlock()

	// Query database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := models.SupportedTokens(
		models.SupportedTokenWhere.ChainID.EQ(chainID),
		models.SupportedTokenWhere.Symbol.EQ(symbol),
		models.SupportedTokenWhere.IsEnabled.EQ(null.BoolFrom(true)),
	).One(ctx, s.db)

	if err != nil {
		log.Warn().
			Int64("chain_id", chainID).
			Str("symbol", symbol).
			Err(err).
			Msg("Failed to find token, using default")
		return 1 // Fallback to default
	}

	tokenID := token.ID

	// Update cache
	s.cacheMutex.Lock()
	s.tokenCache[cacheKey] = tokenID
	s.cacheMutex.Unlock()

	log.Debug().
		Int64("chain_id", chainID).
		Str("symbol", symbol).
		Int("token_id", tokenID).
		Msg("Token found and cached")

	return tokenID
}

func (s *service) getCurrentBatchSize(chainID int64, tokenID int) int32 {
	if s.batchOptimizer != nil {
		return int32(s.batchOptimizer.GetOptimalBatchSize(chainID, tokenID))
	}
	// Fallback to optimal batch size from chain config
	return int32(s.getOptimalBatchSize(chainID, tokenID))
}

func (s *service) getOptimalBatchSize(chainID int64, tokenID int) int {
	// Try to get from batch optimizer first
	if s.batchOptimizer != nil {
		optimized := s.batchOptimizer.GetOptimalBatchSize(chainID, tokenID)
		if optimized > 0 {
			return optimized
		}
	}

	// Fallback to chain configuration
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	chain, err := models.Chains(
		models.ChainWhere.ChainID.EQ(chainID),
		models.ChainWhere.IsEnabled.EQ(null.BoolFrom(true)),
	).One(ctx, s.db)

	if err != nil {
		log.Warn().
			Int64("chain_id", chainID).
			Err(err).
			Msg("Failed to get chain config, using default batch size")
		return 25 // Default fallback
	}

	// Return optimal batch size from chain config, or default if not set
	if chain.OptimalBatchSize.Valid && chain.OptimalBatchSize.Int > 0 {
		return chain.OptimalBatchSize.Int
	}

	return 25 // Default fallback
}

// buildExistingTransferResponse builds response from existing transfer transactions
func (s *service) buildExistingTransferResponse(ctx context.Context, operationID string) (*types.TransferResponse, *types.BatchInfo, error) {
	// Query all transactions for this operation ID
	transactions, err := models.Transactions(
		models.TransactionWhere.OperationID.EQ(null.StringFrom(operationID)),
		models.TransactionWhere.TXType.EQ("transfer"),
		models.TransactionWhere.BusinessType.EQ("transfer"),
		qm.OrderBy(models.TransactionColumns.CreatedAt+" ASC"),
	).All(ctx, s.db)

	if err != nil {
		return nil, nil, fmt.Errorf("failed to query existing transactions: %w", err)
	}

	if len(transactions) == 0 {
		return nil, nil, fmt.Errorf("no transactions found for operation_id: %s", operationID)
	}

	// Build response from existing transactions
	var fromUserID, toUserID, amount, tokenSymbol string
	var chainID int64
	transferRecords := make([]*types.TransferRecord, 0, len(transactions))

	for _, tx := range transactions {
		amountStr := tx.Amount.String()

		transferRecord := &types.TransferRecord{
			Amount: amountStr,
			TxID:   tx.TXID,
			UserID: tx.UserID,
		}

		if tx.TransferDirection.Valid {
			transferRecord.TransferDirection = tx.TransferDirection.String

			// Extract transfer details from the transactions
			if tx.TransferDirection.String == "outgoing" {
				fromUserID = tx.UserID
				if tx.RelatedUserID.Valid {
					toUserID = tx.RelatedUserID.String
				}
				// Convert negative amount back to positive for API response
				if amountDecimal := tx.Amount.Big; amountDecimal != nil {
					absAmount := new(decimal.Big).Abs(amountDecimal)
					amount = absAmount.String()
				}
			} else if tx.TransferDirection.String == "incoming" {
				toUserID = tx.UserID
				if tx.RelatedUserID.Valid {
					fromUserID = tx.RelatedUserID.String
				}
				amount = amountStr
			}
		}

		chainID = tx.ChainID

		// Get token symbol from token_id (cached)
		if tokenSymbol == "" {
			ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			token, err := models.SupportedTokens(
				models.SupportedTokenWhere.ID.EQ(tx.TokenID),
			).One(ctx2, s.db)

			if err == nil {
				tokenSymbol = token.Symbol
			}
		}

		transferRecords = append(transferRecords, transferRecord)
	}

	// Get current status from the most recent transaction
	status := "recorded" // default
	if len(transactions) > 0 && transactions[0].Status.Valid {
		status = transactions[0].Status.String
	}

	response := &types.TransferResponse{
		Amount:          &amount,
		ChainID:         &chainID,
		FromUserID:      &fromUserID,
		ToUserID:        &toUserID,
		OperationID:     &operationID,
		Status:          &status,
		TokenSymbol:     &tokenSymbol,
		TransferRecords: transferRecords,
	}

	// Build batch info using the first transaction's chain/token info
	var tokenID int
	if len(transactions) > 0 {
		tokenID = transactions[0].TokenID
	}

	batchInfo := &types.BatchInfo{
		WillBeBatched:      true,
		BatchType:          "batchTransferFrom",
		CurrentBatchSize:   int64(s.getCurrentBatchSize(chainID, tokenID)),
		OptimalBatchSize:   int64(s.getOptimalBatchSize(chainID, tokenID)),
		ExpectedEfficiency: "74-76%",
	}

	return response, batchInfo, nil
}
