package queue

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hzbay/chain-bridge/internal/config"
	"github.com/rs/zerolog/log"
)

// HybridBatchProcessor wraps RabbitMQ processor with configuration management
type HybridBatchProcessor struct {
	rabbitmqProcessor *RabbitMQProcessor
	config            config.Server

	// Metrics for monitoring
	rabbitmqStats *ProcessorMetrics
}

// Note: RabbitMQProcessor is now implemented in rabbitmq_processor.go

// BatchGroup represents a group of jobs for batch processing
type BatchGroup struct {
	ChainID int64
	TokenID int
	JobType JobType
}

// ProcessorMetrics tracks performance metrics for comparison
type ProcessorMetrics struct {
	TotalJobs      int64         `json:"total_jobs"`
	SuccessJobs    int64         `json:"success_jobs"`
	FailedJobs     int64         `json:"failed_jobs"`
	AverageLatency time.Duration `json:"average_latency"`
	LastUsed       time.Time     `json:"last_used"`
}

// NewHybridBatchProcessor creates a new RabbitMQ-based batch processor
func NewHybridBatchProcessor(rabbitmqClient *RabbitMQClient, db *sql.DB, optimizer *BatchOptimizer, config config.Server) *HybridBatchProcessor {
	processor := &HybridBatchProcessor{
		rabbitmqProcessor: NewRabbitMQProcessor(rabbitmqClient, db, optimizer, config),
		config:            config,
		rabbitmqStats:     &ProcessorMetrics{},
	}

	log.Info().
		Bool("rabbitmq_enabled", config.RabbitMQ.BatchStrategy.EnableRabbitMQ).
		Msg("RabbitMQ batch processor initialized")

	return processor
}

// PublishTransfer publishes a transfer job using the selected processor
func (h *HybridBatchProcessor) PublishTransfer(ctx context.Context, job TransferJob) error {
	processor, processorType := h.selectProcessor()

	startTime := time.Now()
	err := processor.PublishTransfer(ctx, job)
	latency := time.Since(startTime)

	// Update metrics
	h.updateMetrics(processorType, err == nil, latency)

	log.Debug().
		Str("processor", processorType).
		Str("job_id", job.ID).
		Err(err).
		Dur("latency", latency).
		Msg("Transfer job published")

	return err
}

// PublishAssetAdjust publishes an asset adjustment job using the selected processor
func (h *HybridBatchProcessor) PublishAssetAdjust(ctx context.Context, job AssetAdjustJob) error {
	processor, processorType := h.selectProcessor()

	startTime := time.Now()
	err := processor.PublishAssetAdjust(ctx, job)
	latency := time.Since(startTime)

	// Update metrics
	h.updateMetrics(processorType, err == nil, latency)

	log.Debug().
		Str("processor", processorType).
		Str("job_id", job.ID).
		Err(err).
		Dur("latency", latency).
		Msg("Asset adjust job published")

	return err
}

// PublishNotification publishes a notification job using the selected processor
func (h *HybridBatchProcessor) PublishNotification(ctx context.Context, job NotificationJob) error {
	processor, processorType := h.selectProcessor()

	startTime := time.Now()
	err := processor.PublishNotification(ctx, job)
	latency := time.Since(startTime)

	// Update metrics
	h.updateMetrics(processorType, err == nil, latency)

	log.Debug().
		Str("processor", processorType).
		Str("job_id", job.ID).
		Err(err).
		Dur("latency", latency).
		Msg("Notification job published")

	return err
}

// PublishHealthCheck publishes a health check job using the selected processor
func (h *HybridBatchProcessor) PublishHealthCheck(ctx context.Context, job HealthCheckJob) error {
	processor, processorType := h.selectProcessor()

	startTime := time.Now()
	err := processor.PublishHealthCheck(ctx, job)
	latency := time.Since(startTime)

	// Update metrics
	h.updateMetrics(processorType, err == nil, latency)

	log.Debug().
		Str("processor", processorType).
		Str("job_id", job.ID).
		Err(err).
		Dur("latency", latency).
		Msg("Health check job published")

	return err
}

// PublishNFTMint publishes an NFT mint job
func (h *HybridBatchProcessor) PublishNFTMint(ctx context.Context, job NFTMintJob) error {
	processor, processorType := h.selectProcessor()

	startTime := time.Now()
	err := processor.PublishNFTMint(ctx, job)
	latency := time.Since(startTime)

	// Update metrics
	h.updateMetrics(processorType, err == nil, latency)

	log.Debug().
		Str("processor", processorType).
		Str("job_id", job.ID).
		Str("collection_id", job.CollectionID).
		Str("token_id", job.TokenID).
		Err(err).
		Dur("latency", latency).
		Msg("NFT mint job published")

	return err
}

// PublishNFTBurn publishes an NFT burn job
func (h *HybridBatchProcessor) PublishNFTBurn(ctx context.Context, job NFTBurnJob) error {
	processor, processorType := h.selectProcessor()

	startTime := time.Now()
	err := processor.PublishNFTBurn(ctx, job)
	latency := time.Since(startTime)

	// Update metrics
	h.updateMetrics(processorType, err == nil, latency)

	log.Debug().
		Str("processor", processorType).
		Str("job_id", job.ID).
		Str("collection_id", job.CollectionID).
		Str("token_id", job.TokenID).
		Err(err).
		Dur("latency", latency).
		Msg("NFT burn job published")

	return err
}

// PublishNFTTransfer publishes an NFT transfer job
func (h *HybridBatchProcessor) PublishNFTTransfer(ctx context.Context, job NFTTransferJob) error {
	processor, processorType := h.selectProcessor()

	startTime := time.Now()
	err := processor.PublishNFTTransfer(ctx, job)
	latency := time.Since(startTime)

	// Update metrics
	h.updateMetrics(processorType, err == nil, latency)

	log.Debug().
		Str("processor", processorType).
		Str("job_id", job.ID).
		Str("collection_id", job.CollectionID).
		Str("token_id", job.TokenID).
		Err(err).
		Dur("latency", latency).
		Msg("NFT transfer job published")

	return err
}

// selectProcessor returns the RabbitMQ processor (simplified implementation)
func (h *HybridBatchProcessor) selectProcessor() (BatchProcessor, string) {
	// Always use RabbitMQ processor - simplified approach
	if !h.config.RabbitMQ.BatchStrategy.EnableRabbitMQ {
		log.Warn().Msg("RabbitMQ is disabled but no fallback available")
		return h.rabbitmqProcessor, "rabbitmq_disabled"
	}

	// Check if RabbitMQ is healthy
	if !h.rabbitmqProcessor.IsHealthy() {
		log.Warn().Msg("RabbitMQ is unhealthy but no fallback available")
		return h.rabbitmqProcessor, "rabbitmq_unhealthy"
	}

	return h.rabbitmqProcessor, "rabbitmq"
}

// updateMetrics updates RabbitMQ processor performance metrics
func (h *HybridBatchProcessor) updateMetrics(_ string, success bool, latency time.Duration) {
	// Only track RabbitMQ metrics now
	metrics := h.rabbitmqStats

	metrics.TotalJobs++
	if success {
		metrics.SuccessJobs++
	} else {
		metrics.FailedJobs++
	}
	metrics.LastUsed = time.Now()

	// Update average latency (simple moving average)
	if metrics.AverageLatency == 0 {
		metrics.AverageLatency = latency
	} else {
		metrics.AverageLatency = (metrics.AverageLatency + latency) / 2
	}
}

// StartBatchConsumer starts the RabbitMQ batch consumer
func (h *HybridBatchProcessor) StartBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Starting RabbitMQ batch consumer")

	// Start RabbitMQ consumer
	if h.config.RabbitMQ.BatchStrategy.EnableRabbitMQ {
		if err := h.rabbitmqProcessor.StartBatchConsumer(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to start RabbitMQ batch consumer")
			return err
		}
	} else {
		log.Warn().Msg("RabbitMQ is disabled in configuration")
		return fmt.Errorf("RabbitMQ is disabled and no fallback processor available")
	}

	return nil
}

// StopBatchConsumer stops the RabbitMQ batch consumer
func (h *HybridBatchProcessor) StopBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Stopping RabbitMQ batch consumer")

	// Stop RabbitMQ consumer
	if h.rabbitmqProcessor != nil {
		if err := h.rabbitmqProcessor.StopBatchConsumer(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to stop RabbitMQ batch consumer")
			return err
		}
	}

	return nil
}

// GetQueueStats returns RabbitMQ queue statistics
func (h *HybridBatchProcessor) GetQueueStats() map[string]Stats {
	result := make(map[string]Stats)

	// Get RabbitMQ stats
	if h.rabbitmqProcessor != nil {
		rabbitStats := h.rabbitmqProcessor.GetQueueStats()
		for name, stats := range rabbitStats {
			result["rabbitmq."+name] = stats
		}
	}

	return result
}

// IsHealthy checks if RabbitMQ processor is healthy
func (h *HybridBatchProcessor) IsHealthy() bool {
	return h.rabbitmqProcessor != nil && h.rabbitmqProcessor.IsHealthy()
}

// Close closes the RabbitMQ processor
func (h *HybridBatchProcessor) Close() error {
	if h.rabbitmqProcessor != nil {
		if err := h.rabbitmqProcessor.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close RabbitMQ processor")
			return err
		}
	}

	return nil
}
