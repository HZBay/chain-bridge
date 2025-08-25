package assets

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

// Service defines the assets service interface
type Service interface {
	AdjustAssets(ctx context.Context, req *types.AssetAdjustRequest) (*types.AssetAdjustResponse, *types.BatchInfo, error)
}

// service implements the assets service
type service struct {
	db             *sql.DB
	batchProcessor queue.BatchProcessor
	batchOptimizer *queue.BatchOptimizer
}

// NewService creates a new assets service
func NewService(db *sql.DB, batchProcessor queue.BatchProcessor, batchOptimizer *queue.BatchOptimizer) Service {
	return &service{
		db:             db,
		batchProcessor: batchProcessor,
		batchOptimizer: batchOptimizer,
	}
}

// AdjustAssets handles batch asset adjustments (mint/burn operations)
func (s *service) AdjustAssets(ctx context.Context, req *types.AssetAdjustRequest) (*types.AssetAdjustResponse, *types.BatchInfo, error) {
	log.Info().
		Str("operation_id", *req.OperationID).
		Int("adjustment_count", len(req.Adjustments)).
		Msg("Processing asset adjustment request")

	// 1. Validate request
	if err := s.validateAdjustRequest(req); err != nil {
		return nil, nil, fmt.Errorf("invalid adjust request: %w", err)
	}

	// 2. Process each adjustment
	var adjustJobs []queue.AssetAdjustJob
	var transactionRecords []*models.Transaction

	mainOperationID := uuid.MustParse(*req.OperationID)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, adjustment := range req.Adjustments {
		// Generate unique transaction ID for this adjustment
		txID := uuid.New()

		// Determine adjustment type based on amount sign
		adjustmentType := "mint"
		amount := *adjustment.Amount
		if len(amount) > 0 && amount[0] == '-' {
			adjustmentType = "burn"
		}

		// Create transaction record
		transaction := &models.Transaction{
			TXID:         txID.String(),
			OperationID:  null.StringFrom(mainOperationID.String()),
			UserID:       *adjustment.UserID,
			ChainID:      *adjustment.ChainID,
			TXType:       "asset_adjust",
			BusinessType: *adjustment.BusinessType,
			TokenID:      s.getTokenIDBySymbol(*adjustment.TokenSymbol),
			Amount: sqltypes.NewDecimal(func() *decimal.Big {
				d, _ := decimal.New(0, 0).SetString(amount)
				return d
			}()),
			Status:           null.StringFrom("pending"),
			IsBatchOperation: null.BoolFrom(true),
			ReasonType:       *adjustment.ReasonType,
			ReasonDetail:     null.StringFromPtr(&adjustment.ReasonDetail),
		}

		// Insert transaction record
		if err := transaction.Insert(ctx, tx, boil.Infer()); err != nil {
			return nil, nil, fmt.Errorf("failed to insert transaction for user %s: %w", *adjustment.UserID, err)
		}

		transactionRecords = append(transactionRecords, transaction)

		// Create asset adjust job
		adjustJob := queue.AssetAdjustJob{
			ID:             txID.String(),
			TransactionID:  txID,
			ChainID:        *adjustment.ChainID,
			TokenID:        s.getTokenIDBySymbol(*adjustment.TokenSymbol),
			UserID:         *adjustment.UserID,
			Amount:         amount,
			AdjustmentType: adjustmentType,
			BusinessType:   *adjustment.BusinessType,
			ReasonType:     *adjustment.ReasonType,
			ReasonDetail:   adjustment.ReasonDetail,
			Priority:       s.determinePriority(*adjustment.BusinessType),
			CreatedAt:      time.Now(),
		}

		adjustJobs = append(adjustJobs, adjustJob)
	}

	// Commit transaction records
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit transactions: %w", err)
	}

	// 3. Publish all jobs to batch processor
	for _, job := range adjustJobs {
		if err := s.batchProcessor.PublishAssetAdjust(ctx, job); err != nil {
			log.Error().Err(err).
				Str("job_id", job.ID).
				Str("user_id", job.UserID).
				Msg("Failed to publish asset adjust job, but transaction is recorded")

			// Note: We don't return error here because transactions are already recorded
			// The batch processor failure doesn't mean the adjustment failed
		}
	}

	// 4. Build response
	processedCount := int64(len(req.Adjustments))
	status := "recorded"

	response := &types.AssetAdjustResponse{
		OperationID:    req.OperationID,
		ProcessedCount: &processedCount,
		Status:         &status,
	}

	// 5. Build batch info
	var chainID int64
	var tokenID int
	if len(req.Adjustments) > 0 {
		chainID = *req.Adjustments[0].ChainID
		tokenID = s.getTokenIDBySymbol(*req.Adjustments[0].TokenSymbol)
	}

	batchInfo := &types.BatchInfo{
		WillBeBatched:      true,
		BatchType:          "batchAdjustAssets",
		CurrentBatchSize:   int64(s.getCurrentBatchSize(chainID, tokenID)),
		OptimalBatchSize:   25, // TODO: get from configuration
		ExpectedEfficiency: "74-76%",
	}

	log.Info().
		Str("operation_id", *req.OperationID).
		Int64("processed_count", processedCount).
		Str("status", status).
		Msg("Asset adjustment request processed successfully")

	return response, batchInfo, nil
}

// Helper methods

func (s *service) validateAdjustRequest(req *types.AssetAdjustRequest) error {
	if req.OperationID == nil || *req.OperationID == "" {
		return fmt.Errorf("operation_id is required")
	}

	if len(req.Adjustments) == 0 {
		return fmt.Errorf("at least one adjustment is required")
	}

	if len(req.Adjustments) > 100 {
		return fmt.Errorf("too many adjustments, maximum 100 allowed")
	}

	// Validate each adjustment
	for i, adjustment := range req.Adjustments {
		if adjustment.UserID == nil || *adjustment.UserID == "" {
			return fmt.Errorf("adjustment[%d]: user_id is required", i)
		}
		if adjustment.Amount == nil || *adjustment.Amount == "" {
			return fmt.Errorf("adjustment[%d]: amount is required", i)
		}
		if adjustment.ChainID == nil {
			return fmt.Errorf("adjustment[%d]: chain_id is required", i)
		}
		if adjustment.TokenSymbol == nil || *adjustment.TokenSymbol == "" {
			return fmt.Errorf("adjustment[%d]: token_symbol is required", i)
		}
		if adjustment.BusinessType == nil || *adjustment.BusinessType == "" {
			return fmt.Errorf("adjustment[%d]: business_type is required", i)
		}
		if adjustment.ReasonType == nil || *adjustment.ReasonType == "" {
			return fmt.Errorf("adjustment[%d]: reason_type is required", i)
		}

		// Validate business type enum
		validBusinessTypes := []string{"reward", "gas_fee", "consumption", "refund"}
		isValidBusinessType := false
		for _, validType := range validBusinessTypes {
			if *adjustment.BusinessType == validType {
				isValidBusinessType = true
				break
			}
		}
		if !isValidBusinessType {
			return fmt.Errorf("adjustment[%d]: invalid business_type '%s'", i, *adjustment.BusinessType)
		}

		// Validate amount format (should be a valid decimal)
		if _, ok := decimal.New(0, 0).SetString(*adjustment.Amount); !ok {
			return fmt.Errorf("adjustment[%d]: invalid amount format '%s'", i, *adjustment.Amount)
		}
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

func (s *service) determinePriority(businessType string) queue.Priority {
	switch businessType {
	case "gas_fee":
		return queue.PriorityHigh // Gas fees need quick processing
	case "reward":
		return queue.PriorityNormal // Rewards are standard priority
	case "consumption":
		return queue.PriorityNormal // Consumption is standard priority
	case "refund":
		return queue.PriorityHigh // Refunds need quick processing
	default:
		return queue.PriorityNormal
	}
}
