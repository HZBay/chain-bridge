package transfer

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericlagergren/decimal"
	"github.com/google/uuid"
	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/hzbay/chain-bridge/internal/queue"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/rs/zerolog/log"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	sqltypes "github.com/volatiletech/sqlboiler/v4/types"
)

// Service defines the transfer service interface
type Service interface {
	TransferAssets(ctx context.Context, req *types.TransferRequest) (*types.TransferResponse, *types.BatchInfo, error)
	GetUserTransactions(ctx context.Context, userID string, params GetTransactionsParams) (*types.TransactionHistoryResponse, *types.TransactionSummary, error)
}

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
}

// NewService creates a new transfer service
func NewService(db *sql.DB, batchProcessor queue.BatchProcessor, batchOptimizer *queue.BatchOptimizer) Service {
	return &service{
		db:             db,
		batchProcessor: batchProcessor,
		batchOptimizer: batchOptimizer,
	}
}

// TransferAssets handles user-to-user asset transfers
func (s *service) TransferAssets(ctx context.Context, req *types.TransferRequest) (*types.TransferResponse, *types.BatchInfo, error) {
	log.Info().
		Str("from_user_id", *req.FromUserID).
		Str("to_user_id", *req.ToUserID).
		Str("amount", *req.Amount).
		Int64("chain_id", *req.ChainID).
		Str("token_symbol", *req.TokenSymbol).
		Msg("Processing transfer request")

	// 1. Validate request (basic validation, detailed validation in handler)
	if err := s.validateTransferRequest(req); err != nil {
		return nil, nil, fmt.Errorf("invalid transfer request: %w", err)
	}

	// 2. Generate transaction IDs
	outgoingTxID := uuid.New()
	incomingTxID := uuid.New()
	operationID := uuid.New()

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
		TokenID:           s.getTokenIDBySymbol(*req.TokenSymbol), // TODO: implement token lookup
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
		TokenID:           s.getTokenIDBySymbol(*req.TokenSymbol),
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
	defer tx.Rollback()

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
		TokenID:       s.getTokenIDBySymbol(*req.TokenSymbol),
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
		CurrentBatchSize:   int64(s.getCurrentBatchSize(*req.ChainID, s.getTokenIDBySymbol(*req.TokenSymbol))),
		OptimalBatchSize:   25, // TODO: get from configuration
		ExpectedEfficiency: "74-76%",
	}

	log.Info().
		Str("operation_id", operationID.String()).
		Str("status", "recorded").
		Msg("Transfer assets request processed successfully")

	return response, batchInfo, nil
}

// GetUserTransactions retrieves user transaction history
func (s *service) GetUserTransactions(ctx context.Context, userID string, params GetTransactionsParams) (*types.TransactionHistoryResponse, *types.TransactionSummary, error) {
	// TODO: Implement transaction history retrieval
	// This would query the transactions table with filters and pagination
	log.Info().
		Str("user_id", userID).
		Msg("Getting user transactions (implementation pending)")

	// Return empty response for now
	response := &types.TransactionHistoryResponse{
		UserID:       &userID,
		TotalCount:   func() *int64 { i := int64(0); return &i }(),
		Page:         func() *int64 { i := int64(params.Page); return &i }(),
		Limit:        func() *int64 { i := int64(params.Limit); return &i }(),
		Transactions: []*types.TransactionInfo{},
	}

	summary := &types.TransactionSummary{
		TotalIncoming: "0",
		TotalOutgoing: "0",
		NetChange:     "0",
		GasSavedTotal: "0",
	}

	return response, summary, nil
}

// Helper methods

func (s *service) validateTransferRequest(req *types.TransferRequest) error {
	// Basic validation - detailed validation should be in API handler
	if req.FromUserID == nil || *req.FromUserID == "" {
		return fmt.Errorf("from_user_id is required")
	}
	if req.ToUserID == nil || *req.ToUserID == "" {
		return fmt.Errorf("to_user_id is required")
	}
	if *req.FromUserID == *req.ToUserID {
		return fmt.Errorf("cannot transfer to self")
	}
	if req.Amount == nil || *req.Amount == "" {
		return fmt.Errorf("amount is required")
	}
	if req.ChainID == nil {
		return fmt.Errorf("chain_id is required")
	}
	if req.TokenSymbol == nil || *req.TokenSymbol == "" {
		return fmt.Errorf("token_symbol is required")
	}

	return nil
}

func (s *service) getTokenIDBySymbol(symbol string) int {
	// TODO: Implement token lookup from supported_tokens table
	// For now, return a default value
	return 1
}

func (s *service) getCurrentBatchSize(chainID int64, tokenID int) int32 {
	if s.batchOptimizer != nil {
		return int32(s.batchOptimizer.GetOptimalBatchSize(chainID, tokenID))
	}
	// Fallback to default value
	return 25
}
