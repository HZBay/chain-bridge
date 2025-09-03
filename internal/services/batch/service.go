package batch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/hzbay/chain-bridge/internal/queue"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/rs/zerolog/log"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

// Service defines the batch service interface
type Service interface {
	GetBatchStatus(ctx context.Context, batchID string) (*types.BatchStatusResponse, error)
}

// service implements the batch service
type service struct {
	db             *sql.DB
	batchProcessor queue.BatchProcessor
	queueMonitor   *queue.Monitor
}

// NewService creates a new batch service
func NewService(db *sql.DB, batchProcessor queue.BatchProcessor, queueMonitor *queue.Monitor) Service {
	return &service{
		db:             db,
		batchProcessor: batchProcessor,
		queueMonitor:   queueMonitor,
	}
}

// GetBatchStatus retrieves the status and details of a specific batch operation
func (s *service) GetBatchStatus(ctx context.Context, batchID string) (*types.BatchStatusResponse, error) {
	log.Info().Str("batch_id", batchID).Msg("Getting batch status")

	// 1. Query batch from database
	batch, err := models.Batches(
		models.BatchWhere.BatchID.EQ(batchID),
		qm.Load(models.BatchRels.Chain),
		qm.Load(models.BatchRels.Token),
	).One(ctx, s.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("batch %s not found", batchID)
		}
		return nil, fmt.Errorf("failed to query batch: %w", err)
	}

	// 2. Get current queue metrics if monitor is available
	var currentQueueDepth int64
	if s.queueMonitor != nil {
		queueStats := s.queueMonitor.GetDetailedStats()
		// Check for any queue related to this batch's chain and token
		for _, stats := range queueStats {
			if stats.PendingCount > 0 {
				currentQueueDepth += int64(stats.PendingCount)
			}
		}
	}

	// 3. Build batch metrics with all available information
	batchMetrics := &types.BatchMetrics{
		OperationCount: int64(batch.OperationCount),
	}

	if batch.BatchStrategy.Valid {
		batchMetrics.BatchStrategy = batch.BatchStrategy.String
	}

	if val, ok := batch.ActualEfficiency.Float64(); ok {
		batchMetrics.ActualEfficiency = float32(val)
	}

	// 4. Build gas analysis with comprehensive gas information
	gasAnalysis := &types.GasAnalysis{}
	if val, ok := batch.GasSavedPercentage.Float64(); ok {
		gasAnalysis.GasSavedPercentage = float32(val)
	}
	if val, ok := batch.GasSavedUsd.Float64(); ok {
		gasAnalysis.GasSavedUsd = float32(val)
	}

	// Add actual gas used and gas saved information from new fields
	if batch.ActualGasUsed.Valid {
		log.Debug().Int64("actual_gas_used", batch.ActualGasUsed.Int64).Msg("Actual gas used available")
	}
	if batch.GasSaved.Valid {
		log.Debug().Int64("gas_saved", batch.GasSaved.Int64).Msg("Gas saved available")
	}

	// 5. Build CPOP info
	cpopInfo := &types.CPOPInfo{
		MasterAggregatorUsed: batch.MasterAggregatorUsed.Bool,
	}
	if batch.CpopOperationType.Valid {
		cpopInfo.CpopOperationType = batch.CpopOperationType.String
	}

	// 6. Build comprehensive response with all available data
	response := &types.BatchStatusResponse{
		BatchID:      &batchID,
		BatchMetrics: batchMetrics,
		GasAnalysis:  gasAnalysis,
		CpopInfo:     cpopInfo,
	}

	if batch.Status.Valid {
		response.Status = &batch.Status.String
	}

	// 7. Query related transactions for additional context
	transactionCount, err := models.Transactions(
		models.TransactionWhere.BatchID.EQ(null.StringFrom(batchID)),
	).Count(ctx, s.db)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to count related transactions")
	} else {
		log.Debug().Int64("related_transactions", transactionCount).Msg("Related transactions found")
	}

	// Add additional batch information logging for monitoring
	if batch.TXHash.Valid {
		log.Debug().Str("tx_hash", batch.TXHash.String).Msg("Transaction hash available")
	}
	if batch.CreatedAt.Valid {
		log.Debug().Time("created_at", batch.CreatedAt.Time).Msg("Batch creation time")
	}
	if batch.SubmittedAt.Valid {
		log.Debug().Time("submitted_at", batch.SubmittedAt.Time).Msg("Batch submission time")
	}
	if batch.ConfirmedAt.Valid {
		log.Debug().Time("confirmed_at", batch.ConfirmedAt.Time).Msg("Batch confirmation time")
	}
	if batch.Confirmations.Valid {
		log.Debug().Int("confirmations", batch.Confirmations.Int).Msg("Confirmation count")
	}
	if batch.RetryCount.Valid {
		log.Debug().Int("retry_count", batch.RetryCount.Int).Msg("Retry attempts")
	}
	if batch.FailureReason.Valid {
		log.Warn().Str("failure_reason", batch.FailureReason.String).Msg("Batch failure reason")
	}

	// Enhanced logging with comprehensive batch information
	logEvent := log.Info().
		Str("batch_id", batchID).
		Int64("chain_id", batch.ChainID).
		Int("token_id", batch.TokenID).
		Str("batch_type", batch.BatchType).
		Int("operation_count", batch.OperationCount).
		Int("optimal_batch_size", batch.OptimalBatchSize)

	if batch.Status.Valid {
		logEvent = logEvent.Str("status", batch.Status.String)
	}

	if val, ok := batch.ActualEfficiency.Float64(); ok {
		logEvent = logEvent.Float64("actual_efficiency", val)
	}

	if batch.BatchStrategy.Valid {
		logEvent = logEvent.Str("batch_strategy", batch.BatchStrategy.String)
	}

	if batch.NetworkCondition.Valid {
		logEvent = logEvent.Str("network_condition", batch.NetworkCondition.String)
	}

	if batch.ActualGasUsed.Valid {
		logEvent = logEvent.Int64("actual_gas_used", batch.ActualGasUsed.Int64)
	}

	if batch.GasSaved.Valid {
		logEvent = logEvent.Int64("gas_saved", batch.GasSaved.Int64)
	}

	if val, ok := batch.GasSavedUsd.Float64(); ok {
		logEvent = logEvent.Float64("gas_saved_usd", val)
	}

	if batch.TXHash.Valid {
		logEvent = logEvent.Str("tx_hash", batch.TXHash.String)
	}

	if currentQueueDepth > 0 {
		logEvent = logEvent.Int64("current_queue_depth", currentQueueDepth)
	}

	if transactionCount > 0 {
		logEvent = logEvent.Int64("related_transactions", transactionCount)
	}

	logEvent.Msg("Batch status retrieved successfully with comprehensive details")

	return response, nil
}
