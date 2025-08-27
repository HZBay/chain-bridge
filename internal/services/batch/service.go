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
	queueMonitor   *queue.QueueMonitor
}

// NewService creates a new batch service
func NewService(db *sql.DB, batchProcessor queue.BatchProcessor, queueMonitor *queue.QueueMonitor) Service {
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

	// TODO: 实行下面注释未实现的部分
	// 2. Get current queue metrics if monitor is available (for future use)
	// var currentQueueDepth int64 = 0
	// if s.queueMonitor != nil {
	// 	queueStats := s.queueMonitor.GetDetailedStats()
	// 	if transferStats, exists := queueStats["transfer"]; exists {
	// 		currentQueueDepth = int64(transferStats.PendingCount)
	// 	}
	// }

	// 3. Build batch metrics
	batchMetrics := &types.BatchMetrics{
		OperationCount: int64(batch.OperationCount),
	}

	if batch.BatchStrategy.Valid {
		batchMetrics.BatchStrategy = batch.BatchStrategy.String
	}

	if val, ok := batch.ActualEfficiency.Float64(); ok {
		batchMetrics.ActualEfficiency = float32(val)
	}

	// 4. Build gas analysis
	gasAnalysis := &types.GasAnalysis{}
	if val, ok := batch.GasSavedPercentage.Float64(); ok {
		gasAnalysis.GasSavedPercentage = float32(val)
	}
	if val, ok := batch.GasSavedUsd.Float64(); ok {
		gasAnalysis.GasSavedUsd = float32(val)
	}

	// 5. Build CPOP info
	cpopInfo := &types.CPOPInfo{
		MasterAggregatorUsed: batch.MasterAggregatorUsed.Bool,
	}
	if batch.CpopOperationType.Valid {
		cpopInfo.CpopOperationType = batch.CpopOperationType.String
	}

	// 6. Build response
	response := &types.BatchStatusResponse{
		BatchID:      &batchID,
		BatchMetrics: batchMetrics,
		GasAnalysis:  gasAnalysis,
		CpopInfo:     cpopInfo,
	}

	if batch.Status.Valid {
		response.Status = &batch.Status.String
	}

	// TODO: 实现未实现的部分
	// Note: QueueInfo not available in BatchStatusResponse type
	// Queue monitoring would be added here if needed in the response

	logEvent := log.Info().Str("batch_id", batchID)

	if batch.Status.Valid {
		logEvent = logEvent.Str("status", batch.Status.String)
	}

	logEvent = logEvent.Int("operation_count", batch.OperationCount)

	if val, ok := batch.ActualEfficiency.Float64(); ok {
		logEvent = logEvent.Float64("actual_efficiency", val)
	}

	if val, ok := batch.GasSavedUsd.Float64(); ok {
		logEvent = logEvent.Float64("gas_saved_usd", val)
	}

	logEvent.Msg("Batch status retrieved successfully")

	return response, nil
}
