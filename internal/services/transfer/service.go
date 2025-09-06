package transfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
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
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	sqltypes "github.com/volatiletech/sqlboiler/v4/types"
)

// Service defines the transfer service interface
type Service interface {
	BatchTransferAssets(ctx context.Context, req *types.TransferRequest) (*types.TransferResponse, *types.BatchInfo, error)
	GetUserTransactions(ctx context.Context, userID string, params GetTransactionsParams) (*types.TransactionHistoryResponse, error)
}

// TODO: 这里使用make swagger生成的结构体，不要自己定义
// GetTransactionsParams contains parameters for querying user transactions
type GetTransactionsParams struct {
	ChainID     *int64
	TokenSymbol *string
	TxType      *string
	Status      *string
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

// Removed: TransferAssets method - now using unified batch format only

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
	}

	// Build separate query mods for counting vs selection
	countQueryMods := []qm.QueryMod{
		models.TransactionWhere.UserID.EQ(userID),
	}

	// Apply filters to both query sets
	if params.ChainID != nil {
		queryMods = append(queryMods, models.TransactionWhere.ChainID.EQ(*params.ChainID))
		countQueryMods = append(countQueryMods, models.TransactionWhere.ChainID.EQ(*params.ChainID))
	}
	if params.TokenSymbol != nil {
		// Join with supported_tokens to filter by symbol
		queryMods = append(queryMods, qm.InnerJoin("supported_tokens st ON st.id = transactions.token_id"))
		queryMods = append(queryMods, qm.Where("st.symbol = ?", *params.TokenSymbol))
		// For count query, use subquery to avoid GROUP BY issues
		countQueryMods = append(countQueryMods, qm.Where("transactions.token_id IN (SELECT id FROM supported_tokens WHERE symbol = ?)", *params.TokenSymbol))
	}
	if params.TxType != nil {
		queryMods = append(queryMods, models.TransactionWhere.TXType.EQ(*params.TxType))
		countQueryMods = append(countQueryMods, models.TransactionWhere.TXType.EQ(*params.TxType))
	}
	if params.Status != nil {
		queryMods = append(queryMods, models.TransactionWhere.Status.EQ(null.StringFrom(*params.Status)))
		countQueryMods = append(countQueryMods, models.TransactionWhere.Status.EQ(null.StringFrom(*params.Status)))
	}
	if params.StartDate != nil {
		queryMods = append(queryMods, qm.Where("transactions.created_at >= ?", *params.StartDate))
		countQueryMods = append(countQueryMods, qm.Where("transactions.created_at >= ?", *params.StartDate))
	}
	if params.EndDate != nil {
		queryMods = append(queryMods, qm.Where("transactions.created_at <= ?", *params.EndDate))
		countQueryMods = append(countQueryMods, qm.Where("transactions.created_at <= ?", *params.EndDate))
	}

	// Get total count using clean count query without JOINs
	totalCount, err := models.Transactions(countQueryMods...).Count(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("failed to count transactions: %w", err)
	}

	// Add ORDER BY for main query only
	queryMods = append(queryMods, qm.OrderBy(models.TransactionColumns.CreatedAt+" DESC"))

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
		// Parse amount as decimal for calculations - safely handle null decimals
		var amount float64
		if tx.Amount.Big != nil {
			amount, _ = tx.Amount.Float64()
		}

		if amount > 0 {
			totalIncoming += amount
		} else {
			totalOutgoing += -amount // Convert negative to positive
		}

		// Calculate gas saved if available - safely handle null decimals
		if tx.GasSavedPercentage.Big != nil {
			if val, ok := tx.GasSavedPercentage.Float64(); ok {
				// Mock calculation - in real implementation would depend on tx value
				gasSaved := amount * val / 100 * 0.001 // rough estimate
				totalGasSaved += gasSaved
			}
		}

		// Build transaction info - safely handle null decimals
		var amountStr string
		if tx.Amount.Big != nil {
			amountStr = tx.Amount.String()
		} else {
			amountStr = "0.0"
		}
		// Set required fields with defaults if needed
		status := "pending" // Default status
		if tx.Status.Valid {
			status = tx.Status.String
		}

		// Get token symbol - this should always be available from the relationship
		tokenSymbol := "UNKNOWN" // Default token symbol
		if tx.R != nil && tx.R.Token != nil {
			tokenSymbol = tx.R.Token.Symbol
		}

		txInfo := &types.TransactionInfo{
			TxID:         &tx.TXID,
			ChainID:      &tx.ChainID,
			Amount:       &amountStr,
			TxType:       &tx.TXType,
			BusinessType: &tx.BusinessType,
			Status:       &status,
			TokenSymbol:  &tokenSymbol,
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
		if tx.GasSavedPercentage.Big != nil {
			if val, ok := tx.GasSavedPercentage.Float64(); ok {
				txInfo.GasSavedPercentage = float32(val)
			}
		}
		if tx.ConfirmedAt.Valid {
			confirmedAt := strfmt.DateTime(tx.ConfirmedAt.Time)
			txInfo.ConfirmedAt = confirmedAt
		}

		// Add chain name if loaded
		if tx.R != nil && tx.R.Chain != nil {
			txInfo.ChainName = tx.R.Chain.Name
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

// getTokenIDBySymbolWithValidation validates token exists and returns ID or validation error
func (s *service) getTokenIDBySymbolWithValidation(chainID int64, symbol string) (int, error) {
	cacheKey := fmt.Sprintf("%d:%s", chainID, symbol)

	// Check cache first
	s.cacheMutex.RLock()
	if tokenID, exists := s.tokenCache[cacheKey]; exists {
		s.cacheMutex.RUnlock()
		return tokenID, nil
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
		if errors.Is(err, sql.ErrNoRows) {
			// Return structured validation error
			return 0, fmt.Errorf("validation_error:token_not_found:%d:%s", chainID, symbol)
		}
		return 0, fmt.Errorf("database error while looking up token: %w", err)
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

	return tokenID, nil
}

// validateChainExists checks if a chain exists and is enabled
func (s *service) validateChainExists(chainID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := models.Chains(
		models.ChainWhere.ChainID.EQ(chainID),
		models.ChainWhere.IsEnabled.EQ(null.BoolFrom(true)),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("validation_error:chain_not_found:%d", chainID)
		}
		return fmt.Errorf("database error while validating chain: %w", err)
	}

	return nil
}

// validateUserAccountExists checks if a user has an account on the specified chain
func (s *service) validateUserAccountExists(ctx context.Context, userID string, chainID int64) error {
	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := models.UserAccounts(
		models.UserAccountWhere.UserID.EQ(userID),
		models.UserAccountWhere.ChainID.EQ(chainID),
	).One(ctxTimeout, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("validation_error:user_account_not_found:%s:%d", userID, chainID)
		}
		return fmt.Errorf("database error while validating user account: %w", err)
	}

	return nil
}

func (s *service) getCurrentBatchSize(chainID int64, tokenID int) int32 {
	if s.batchOptimizer != nil {
		optimalSize := s.batchOptimizer.GetOptimalBatchSize(chainID, tokenID)
		if optimalSize <= 0 {
			return 25 // Default fallback
		}
		if optimalSize <= math.MaxInt32 {
			return int32(optimalSize)
		}
		return math.MaxInt32
	}
	// Fallback to optimal batch size from chain config
	optimalSize := s.getOptimalBatchSize(chainID, tokenID)
	if optimalSize <= 0 {
		return 25 // Default fallback
	}
	if optimalSize <= math.MaxInt32 {
		return int32(optimalSize)
	}
	return math.MaxInt32
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
// Removed: buildExistingTransferResponse method - replaced with buildExistingBatchTransferResponse

// buildExistingBatchTransferResponse builds a TransferResponse for an existing operation
func (s *service) buildExistingBatchTransferResponse(ctx context.Context, operationID string) (*types.TransferResponse, *types.BatchInfo, error) {
	// Get all transactions for this operation_id
	transactions, err := models.Transactions(
		models.TransactionWhere.OperationID.EQ(null.StringFrom(operationID)),
		models.TransactionWhere.TXType.EQ("transfer"),
		models.TransactionWhere.BusinessType.EQ("transfer"),
		qm.OrderBy(models.TransactionColumns.CreatedAt),
	).All(ctx, s.db)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query existing transactions: %w", err)
	}

	if len(transactions) == 0 {
		return nil, nil, fmt.Errorf("no transactions found for operation_id: %s", operationID)
	}

	// Group transactions by transfer operation (each operation has 2 records: outgoing + incoming)
	transferOpsMap := make(map[string][]*models.Transaction)
	for _, tx := range transactions {
		// Use a combination of from_user + to_user + amount to group transfer operations
		// For outgoing: use from_user_id as key, for incoming: use to_user_id as key
		var groupKey string
		if tx.TransferDirection.String == "outgoing" {
			// For outgoing tx: key = from_user_id:to_user_id:amount
			groupKey = fmt.Sprintf("%s:%s:%s", tx.UserID, tx.RelatedUserID.String, tx.Amount.Big.String())
		} else {
			// For incoming tx: key = to_user_id:from_user_id:amount (reversed to match outgoing)
			groupKey = fmt.Sprintf("%s:%s:%s", tx.RelatedUserID.String, tx.UserID, tx.Amount.Big.String())
		}
		transferOpsMap[groupKey] = append(transferOpsMap[groupKey], tx)
	}

	// Build transfer operation results
	transferOperations := make([]*types.TransferOperationResult, 0, len(transferOpsMap))
	for _, txGroup := range transferOpsMap {
		if len(txGroup) != 2 {
			continue // Skip incomplete transfer operations
		}

		// Find outgoing and incoming transactions
		var outgoingTx, incomingTx *models.Transaction
		for _, tx := range txGroup {
			if tx.TransferDirection.String == "outgoing" {
				outgoingTx = tx
			} else if tx.TransferDirection.String == "incoming" {
				incomingTx = tx
			}
		}

		if outgoingTx == nil || incomingTx == nil {
			continue // Skip incomplete pairs
		}

		// Get token symbol from the first available transaction
		tokenSymbol := "UNKNOWN"
		if token, err := models.SupportedTokens(
			models.SupportedTokenWhere.ID.EQ(outgoingTx.TokenID),
		).One(ctx, s.db); err == nil {
			tokenSymbol = token.Symbol
		}

		// Extract memo from reason_detail
		memo := ""
		if outgoingTx.ReasonDetail.Valid {
			memo = outgoingTx.ReasonDetail.String
		}

		// Build transfer records
		transferRecords := []*types.TransferRecord{
			{
				Amount:            outgoingTx.Amount.Big.String(),
				TxID:              outgoingTx.TXID,
				TransferDirection: "outgoing",
				UserID:            outgoingTx.UserID,
			},
			{
				Amount:            incomingTx.Amount.Big.String(),
				TxID:              incomingTx.TXID,
				TransferDirection: "incoming",
				UserID:            incomingTx.UserID,
			},
		}

		// Build transfer operation result
		amountString := incomingTx.Amount.Big.String()
		transferOperation := &types.TransferOperationResult{
			FromUserID:      &outgoingTx.UserID,
			ToUserID:        &incomingTx.UserID,
			ChainID:         &outgoingTx.ChainID,
			TokenSymbol:     &tokenSymbol,
			Amount:          &amountString, // Use positive amount
			Memo:            memo,
			Status:          &outgoingTx.Status.String,
			TransferRecords: transferRecords,
		}

		transferOperations = append(transferOperations, transferOperation)
	}

	// Count processed transfers
	processedCount := int64(len(transferOperations))

	// Get status from first transaction
	status := "recorded"
	if len(transactions) > 0 && transactions[0].Status.Valid {
		status = transactions[0].Status.String
	}

	// Build batch info using first transaction's info
	batchInfo := &types.BatchInfo{
		WillBeBatched:      true,
		BatchType:          "batchTransferFrom",
		CurrentBatchSize:   int64(s.getCurrentBatchSize(transactions[0].ChainID, transactions[0].TokenID)),
		OptimalBatchSize:   int64(s.getOptimalBatchSize(transactions[0].ChainID, transactions[0].TokenID)),
		ExpectedEfficiency: "74-76%",
	}

	// Build response
	response := &types.TransferResponse{
		OperationID:        &operationID,
		ProcessedCount:     &processedCount,
		Status:             &status,
		TransferOperations: transferOperations,
		BatchInfo:          batchInfo,
	}

	return response, batchInfo, nil
}

// BatchTransferAssets handles batch user-to-user asset transfers
func (s *service) BatchTransferAssets(ctx context.Context, req *types.TransferRequest) (*types.TransferResponse, *types.BatchInfo, error) {
	log.Info().
		Str("operation_id", *req.OperationID).
		Int("transfer_count", len(req.Transfers)).
		Msg("Processing batch transfer request")

	// Validate operation_id format - it should be a valid UUID
	mainOperationID, err := uuid.Parse(*req.OperationID)
	if err != nil {
		return nil, nil, fmt.Errorf("validation_error:operation_id:Must be a valid UUID format (e.g., 550e8400-e29b-41d4-a716-446655440000)")
	}

	// 1. Idempotency check - check if OperationID already exists
	existingTx, err := models.Transactions(
		models.TransactionWhere.OperationID.EQ(null.StringFrom(mainOperationID.String())),
		models.TransactionWhere.TXType.EQ("transfer"),
		models.TransactionWhere.BusinessType.EQ("transfer"),
	).One(ctx, s.db)

	if err == nil && existingTx != nil {
		// OperationID already exists, return existing result
		log.Info().
			Str("operation_id", *req.OperationID).
			Str("existing_tx_id", existingTx.TXID).
			Msg("Batch transfer operation already processed, returning existing result")

		// Build existing transfer operations for response
		existingResponse, batchInfo, err := s.buildExistingBatchTransferResponse(ctx, mainOperationID.String())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build existing batch transfer response: %w", err)
		}

		return existingResponse, batchInfo, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		// Database query error (not "no rows found")
		return nil, nil, fmt.Errorf("failed to check operation idempotency: %w", err)
	}

	// OperationID doesn't exist, proceed with normal processing
	log.Debug().Str("operation_id", *req.OperationID).Msg("New batch transfer operation, proceeding with processing")

	// Validate all transfers in the batch and check balances
	var validTransfers []int
	var insufficientBalanceTransfers []int

	for i, transfer := range req.Transfers {
		// Validate amount format
		if _, err := strconv.ParseFloat(*transfer.Amount, 64); err != nil {
			return nil, nil, fmt.Errorf("invalid amount format in transfer %d: %s", i, *transfer.Amount)
		}

		// Validate chain exists
		if err := s.validateChainExists(*transfer.ChainID); err != nil {
			return nil, nil, fmt.Errorf("transfer %d: %w", i, err)
		}

		// Validate token exists
		if _, err := s.getTokenIDBySymbolWithValidation(*transfer.ChainID, *transfer.TokenSymbol); err != nil {
			return nil, nil, fmt.Errorf("transfer %d: %w", i, err)
		}

		// Validate both users have accounts on the specified chain
		if err := s.validateUserAccountExists(ctx, *transfer.FromUserID, *transfer.ChainID); err != nil {
			return nil, nil, fmt.Errorf("validation_error:from_user_account_not_found:%s:%d", *transfer.FromUserID, *transfer.ChainID)
		}

		if err := s.validateUserAccountExists(ctx, *transfer.ToUserID, *transfer.ChainID); err != nil {
			return nil, nil, fmt.Errorf("validation_error:to_user_account_not_found:%s:%d", *transfer.ToUserID, *transfer.ChainID)
		}

		// Check if user has sufficient balance
		hasBalance, err := s.checkUserBalance(ctx, *transfer.FromUserID, *transfer.ChainID, *transfer.TokenSymbol, *transfer.Amount)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to check balance for transfer %d: %w", i, err)
		}

		if hasBalance {
			validTransfers = append(validTransfers, i)
		} else {
			insufficientBalanceTransfers = append(insufficientBalanceTransfers, i)
		}
	}

	// Begin database transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Warn().Err(err).Msg("Failed to rollback transaction")
		}
	}()

	// Process each transfer operation - separate valid and insufficient balance transfers
	var allTransactions []*models.Transaction
	var validTransferResults []*types.TransferOperationResult
	var insufficientBalanceResults []*types.TransferOperationResult

	for i, transfer := range req.Transfers {
		// Get token ID for this transfer
		tokenID, err := s.getTokenIDBySymbolWithValidation(*transfer.ChainID, *transfer.TokenSymbol)
		if err != nil {
			return nil, nil, fmt.Errorf("transfer %d: %w", i, err)
		}

		// Generate transaction IDs for this transfer
		outgoingTxID := uuid.New()
		incomingTxID := uuid.New()

		// Determine status based on balance check
		var status string
		isValidTransfer := false
		for _, validIdx := range validTransfers {
			if validIdx == i {
				isValidTransfer = true
				break
			}
		}

		if isValidTransfer {
			status = "pending" // Will be sent to queue
		} else {
			status = "failed"
		}

		// Create outgoing transaction record (debit from sender)
		outgoingTx := &models.Transaction{
			TXID:              outgoingTxID.String(),
			OperationID:       null.StringFrom(mainOperationID.String()),
			UserID:            *transfer.FromUserID,
			ChainID:           *transfer.ChainID,
			TXType:            "transfer",
			BusinessType:      "transfer",
			RelatedUserID:     null.StringFrom(*transfer.ToUserID),
			TransferDirection: null.StringFrom("outgoing"),
			TokenID:           tokenID,
			Amount: sqltypes.NewDecimal(func() *decimal.Big {
				d, _ := decimal.New(0, 0).SetString(fmt.Sprintf("-%s", *transfer.Amount))
				return d
			}()), // Negative for outgoing
			Status:           null.StringFrom(status),
			IsBatchOperation: null.BoolFrom(true),
			ReasonType:       "user_transfer",
			ReasonDetail:     null.StringFromPtr(&transfer.Memo),
		}

		// Create incoming transaction record (credit to recipient)
		incomingTx := &models.Transaction{
			TXID:              incomingTxID.String(),
			OperationID:       null.StringFrom(mainOperationID.String()),
			UserID:            *transfer.ToUserID,
			ChainID:           *transfer.ChainID,
			TXType:            "transfer",
			BusinessType:      "transfer",
			RelatedUserID:     null.StringFrom(*transfer.FromUserID),
			TransferDirection: null.StringFrom("incoming"),
			TokenID:           tokenID,
			Amount: sqltypes.NewDecimal(func() *decimal.Big {
				d, _ := decimal.New(0, 0).SetString(*transfer.Amount)
				return d
			}()), // Positive for incoming
			Status:           null.StringFrom(status),
			IsBatchOperation: null.BoolFrom(true),
			ReasonType:       "user_transfer",
			ReasonDetail:     null.StringFromPtr(&transfer.Memo),
		}

		allTransactions = append(allTransactions, outgoingTx, incomingTx)

		// Build transfer records for this operation
		transferRecords := []*types.TransferRecord{
			{
				Amount:            fmt.Sprintf("-%s", *transfer.Amount),
				TxID:              outgoingTx.TXID,
				TransferDirection: "outgoing",
				UserID:            *transfer.FromUserID,
			},
			{
				Amount:            *transfer.Amount,
				TxID:              incomingTx.TXID,
				TransferDirection: "incoming",
				UserID:            *transfer.ToUserID,
			},
		}

		// Build transfer operation result
		transferResult := &types.TransferOperationResult{
			FromUserID:      transfer.FromUserID,
			ToUserID:        transfer.ToUserID,
			ChainID:         transfer.ChainID,
			TokenSymbol:     transfer.TokenSymbol,
			Amount:          transfer.Amount,
			Memo:            transfer.Memo,
			Status:          &status,
			TransferRecords: transferRecords,
		}

		if isValidTransfer {
			validTransferResults = append(validTransferResults, transferResult)
		} else {
			insufficientBalanceResults = append(insufficientBalanceResults, transferResult)
		}
	}

	// Insert all transaction records using raw SQL
	insertQuery := `
		INSERT INTO transactions (
			tx_id, operation_id, user_id, chain_id, tx_type, business_type, 
			related_user_id, transfer_direction, token_id, amount, status, 
			is_batch_operation, reason_type, reason_detail, failure_reason, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW())
	`

	for _, transaction := range allTransactions {
		// Determine failure reason for insufficient balance transactions
		var failureReason *string
		if transaction.Status.String == "failed" {
			reason := "insufficient_balance"
			failureReason = &reason
		}

		_, err = tx.ExecContext(ctx, insertQuery,
			transaction.TXID,
			transaction.OperationID.Ptr(),
			transaction.UserID,
			transaction.ChainID,
			transaction.TXType,
			transaction.BusinessType,
			transaction.RelatedUserID.Ptr(),
			transaction.TransferDirection.Ptr(),
			transaction.TokenID,
			transaction.Amount.Big.String(),
			transaction.Status.Ptr(),
			transaction.IsBatchOperation.Ptr(),
			transaction.ReasonType,
			transaction.ReasonDetail.Ptr(),
			failureReason,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to insert transaction %s: %w", transaction.TXID, err)
		}
	}

	// Only create transfer jobs for valid transfers (those with sufficient balance)
	for _, validIdx := range validTransfers {
		transfer := req.Transfers[validIdx]

		// Get token ID again (already validated above)
		tokenID, _ := s.getTokenIDBySymbolWithValidation(*transfer.ChainID, *transfer.TokenSymbol)

		// Create transfer job
		transferJob := queue.TransferJob{
			ID:            uuid.New().String(),
			JobType:       queue.JobTypeTransfer,
			TransactionID: uuid.MustParse(allTransactions[validIdx*2].TXID), // Use outgoing transaction ID
			OperationID:   mainOperationID,
			ChainID:       *transfer.ChainID,
			TokenID:       tokenID,
			FromUserID:    *transfer.FromUserID,
			ToUserID:      *transfer.ToUserID,
			Amount:        *transfer.Amount,
			BusinessType:  "transfer",
			ReasonType:    "user_transfer",
			ReasonDetail:  transfer.Memo,
			Priority:      queue.PriorityNormal,
			CreatedAt:     time.Now(),
		}

		// Publish to queue
		if err := s.batchProcessor.PublishTransfer(ctx, transferJob); err != nil {
			// If publishing fails, we need to mark the transactions as failed
			log.Error().Err(err).Str("operation_id", mainOperationID.String()).Int("transfer_index", validIdx).Msg("Failed to publish transfer job to queue")

			// Mark the corresponding transactions as failed due to queue publish failure
			updateQuery := `UPDATE transactions SET status = 'failed', failure_reason = 'queue_publish_failed' WHERE tx_id IN ($1, $2)`
			outgoingTxID := allTransactions[validIdx*2].TXID
			incomingTxID := allTransactions[validIdx*2+1].TXID
			_, updateErr := tx.ExecContext(ctx, updateQuery, outgoingTxID, incomingTxID)
			if updateErr != nil {
				log.Error().Err(updateErr).Msg("Failed to update transaction status after queue publish failure")
			}

			// Send notification about queue publish failure
			if txID, parseErr := uuid.Parse(outgoingTxID); parseErr == nil {
				s.sendTransactionStatusNotification(ctx, txID, "failed", *transfer.FromUserID, map[string]interface{}{
					"old_status":     "pending",
					"failure_reason": "queue_publish_failed",
					"error_message":  err.Error(),
					"chain_id":       *transfer.ChainID,
					"token_symbol":   *transfer.TokenSymbol,
					"amount":         *transfer.Amount,
				})
			}

			return nil, nil, fmt.Errorf("failed to publish transfer job %d to queue: %w", validIdx, err)
		}
	}

	// Send notifications for insufficient balance transfers
	for _, insufficientIdx := range insufficientBalanceTransfers {
		transfer := req.Transfers[insufficientIdx]
		outgoingTxID := allTransactions[insufficientIdx*2].TXID

		// Send notification about insufficient balance
		if txID, parseErr := uuid.Parse(outgoingTxID); parseErr == nil {
			s.sendTransactionStatusNotification(ctx, txID, "failed", *transfer.FromUserID, map[string]interface{}{
				"old_status":       "pending",
				"failure_reason":   "insufficient_balance",
				"chain_id":         *transfer.ChainID,
				"token_symbol":     *transfer.TokenSymbol,
				"amount":           *transfer.Amount,
				"requested_amount": *transfer.Amount,
			})
		}
		log.Info().Str("user_id", *transfer.FromUserID).Str("tx_id", outgoingTxID).Msg("Transfer failed due to insufficient balance - notification sent")
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Info().
		Str("operation_id", *req.OperationID).
		Int("transfer_count", len(req.Transfers)).
		Int("total_tx_records", len(allTransactions)).
		Msg("Batch transfer request processed successfully")

	// Build response including both valid and failed transfers
	processedCount := int64(len(req.Transfers))
	validCount := int64(len(validTransfers))

	// Determine overall status
	var overallStatus string
	if validCount == processedCount {
		overallStatus = "recorded"
	} else if validCount == 0 {
		overallStatus = "failed"
	} else {
		overallStatus = "partial_success"
	}

	// Combine all transfer operation results (already built above)
	allTransferOperations := make([]*types.TransferOperationResult, 0, len(req.Transfers))
	allTransferOperations = append(allTransferOperations, validTransferResults...)
	allTransferOperations = append(allTransferOperations, insufficientBalanceResults...)

	// Build batch info using the first transfer's chain/token info
	var chainID int64
	var tokenID int
	if len(req.Transfers) > 0 {
		chainID = *req.Transfers[0].ChainID
		tokenID, _ = s.getTokenIDBySymbolWithValidation(chainID, *req.Transfers[0].TokenSymbol)
	}

	batchInfo := &types.BatchInfo{
		WillBeBatched:      true,
		BatchType:          "batchTransferFrom",
		CurrentBatchSize:   int64(s.getCurrentBatchSize(chainID, tokenID)),
		OptimalBatchSize:   int64(s.getOptimalBatchSize(chainID, tokenID)),
		ExpectedEfficiency: "74-76%",
	}

	response := &types.TransferResponse{
		OperationID:        req.OperationID,
		ProcessedCount:     &processedCount,
		Status:             &overallStatus,
		TransferOperations: allTransferOperations,
		BatchInfo:          batchInfo,
	}

	return response, batchInfo, nil
}

// checkUserBalance checks if user has sufficient balance for the transfer
func (s *service) checkUserBalance(ctx context.Context, userID string, chainID int64, tokenSymbol string, amount string) (bool, error) {
	// Get token ID
	tokenID, err := s.getTokenIDBySymbolWithValidation(chainID, tokenSymbol)
	if err != nil {
		return false, err
	}

	// Parse requested amount
	requestedAmount := decimal.New(0, 0)
	_, ok := requestedAmount.SetString(amount)
	if !ok {
		return false, fmt.Errorf("invalid amount format: %s", amount)
	}

	// Query user balance
	query := `
		SELECT confirmed_balance 
		FROM user_balances 
		WHERE user_id = $1 AND chain_id = $2 AND token_id = $3
	`

	var balanceStr string
	err = s.db.QueryRowContext(ctx, query, userID, chainID, tokenID).Scan(&balanceStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No balance record means 0 balance
			return false, nil
		}
		return false, fmt.Errorf("failed to query user balance: %w", err)
	}

	// Parse balance
	balance := decimal.New(0, 0)
	_, ok = balance.SetString(balanceStr)
	if !ok {
		return false, fmt.Errorf("failed to parse balance: %s", balanceStr)
	}

	// Check if balance is sufficient
	return balance.Cmp(requestedAmount) >= 0, nil
}

// sendTransactionStatusNotification sends notification about transaction status changes
func (s *service) sendTransactionStatusNotification(ctx context.Context, txID uuid.UUID, status, userID string, extraData map[string]interface{}) {
	if s.batchProcessor == nil {
		return // No notification processor available, skip silently
	}

	// Get transaction record to get operation_id
	tx, err := models.Transactions(
		models.TransactionWhere.TXID.EQ(txID.String()),
	).One(ctx, s.db)
	if err != nil {
		log.Error().Err(err).
			Str("tx_id", txID.String()).
			Msg("Failed to get transaction for notification")
		return
	}

	// Build notification data following the established patterns from the notification guide
	notificationData := map[string]interface{}{
		"type":           "transaction_status_changed",
		"transaction_id": txID.String(),
		"user_id":        userID,
		"new_status":     status,
		"timestamp":      time.Now().Unix(), // Use Unix timestamp for consistency
	}

	// Add old_status if provided in extraData
	if oldStatus, exists := extraData["old_status"]; exists {
		notificationData["old_status"] = oldStatus
	}

	// Add standard transaction fields
	if chainID, exists := extraData["chain_id"]; exists {
		notificationData["chain_id"] = chainID
	}
	if amount, exists := extraData["amount"]; exists {
		notificationData["amount"] = amount
	}
	if tokenSymbol, exists := extraData["token_symbol"]; exists {
		notificationData["token_symbol"] = tokenSymbol
	}
	if txHash, exists := extraData["transaction_hash"]; exists {
		notificationData["transaction_hash"] = txHash
	}
	if failureReason, exists := extraData["failure_reason"]; exists {
		notificationData["failure_reason"] = failureReason
	}
	if errorMessage, exists := extraData["error_message"]; exists {
		notificationData["error_message"] = errorMessage
	}
	if requestedAmount, exists := extraData["requested_amount"]; exists {
		notificationData["requested_amount"] = requestedAmount
	}

	// Add user_id to notification data
	notificationData["user_id"] = userID

	// Get operation_id from transaction record
	var operationID uuid.UUID
	if tx.OperationID.Valid {
		operationID = uuid.MustParse(tx.OperationID.String)
	}

	notification := queue.NotificationJob{
		ID:          uuid.New().String(),
		JobType:     queue.JobTypeNotification,
		OperationID: operationID,
		EventType:   "transaction_status_changed", // Keep this for routing purposes
		Data:        notificationData,
		Priority:    queue.PriorityHigh, // Use high priority for failure notifications
		CreatedAt:   time.Now(),
	}

	if err := s.batchProcessor.PublishNotification(ctx, notification); err != nil {
		log.Warn().Err(err).
			Str("transaction_id", txID.String()).
			Str("user_id", userID).
			Str("status", status).
			Msg("Failed to send transaction status notification")
	}
}
