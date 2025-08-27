package queue

import (
	"context"
	"database/sql"
	"math/rand"
	"sync"
	"time"

	"github.com/hzbay/chain-bridge/internal/blockchain"
	"github.com/hzbay/chain-bridge/internal/config"
	"github.com/rs/zerolog/log"
)

// HybridBatchProcessor combines RabbitMQ and memory processors for gradual rollout
type HybridBatchProcessor struct {
	rabbitmqProcessor *RabbitMQProcessor
	memoryProcessor   *MemoryProcessor
	config            config.BatchProcessingStrategy

	// Metrics for comparison
	rabbitmqStats *ProcessorMetrics
	memoryStats   *ProcessorMetrics
}

// RabbitMQProcessor wraps RabbitMQ client for batch processing
type RabbitMQProcessor struct {
	client          *RabbitMQClient
	db              *sql.DB
	batchOptimizer  *BatchOptimizer
	cpopCallers     map[int64]*blockchain.CPOPBatchCaller // chainID -> caller
	stopChan        chan struct{}
	processingJobs  map[string][]BatchJob // queueName -> jobs
	processingMutex sync.RWMutex

	// New batch consumer
	batchConsumer *RabbitMQBatchConsumer
}

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

// NewHybridBatchProcessor creates a new hybrid batch processor
func NewHybridBatchProcessor(rabbitmqClient *RabbitMQClient, strategy config.BatchProcessingStrategy) *HybridBatchProcessor {
	processor := &HybridBatchProcessor{
		rabbitmqProcessor: &RabbitMQProcessor{client: rabbitmqClient},
		memoryProcessor:   NewMemoryProcessor(),
		config:            strategy,
		rabbitmqStats:     &ProcessorMetrics{},
		memoryStats:       &ProcessorMetrics{},
	}

	log.Info().
		Bool("rabbitmq_enabled", strategy.EnableRabbitMQ).
		Int("rabbitmq_percentage", strategy.RabbitMQPercentage).
		Bool("fallback_enabled", strategy.FallbackToMemory).
		Msg("Hybrid batch processor initialized")

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

// selectProcessor selects which processor to use based on configuration
func (h *HybridBatchProcessor) selectProcessor() (BatchProcessor, string) {
	// If RabbitMQ is disabled, use memory processor
	if !h.config.EnableRabbitMQ {
		return h.memoryProcessor, "memory"
	}

	// Check if RabbitMQ is healthy
	if !h.rabbitmqProcessor.IsHealthy() {
		if h.config.FallbackToMemory {
			log.Warn().Msg("RabbitMQ unhealthy, falling back to memory processor")
			return h.memoryProcessor, "memory_fallback"
		}
		log.Error().Msg("RabbitMQ unhealthy and fallback disabled")
		return h.rabbitmqProcessor, "rabbitmq_unhealthy"
	}

	// Gradual rollout: use percentage to determine processor
	if h.config.RabbitMQPercentage >= 100 {
		return h.rabbitmqProcessor, "rabbitmq"
	}

	if h.config.RabbitMQPercentage <= 0 {
		return h.memoryProcessor, "memory"
	}

	// Random selection based on percentage
	if rand.Intn(100) < h.config.RabbitMQPercentage {
		return h.rabbitmqProcessor, "rabbitmq"
	}

	return h.memoryProcessor, "memory"
}

// updateMetrics updates processor performance metrics
func (h *HybridBatchProcessor) updateMetrics(processorType string, success bool, latency time.Duration) {
	var metrics *ProcessorMetrics

	if processorType == "rabbitmq" || processorType == "rabbitmq_unhealthy" {
		metrics = h.rabbitmqStats
	} else {
		metrics = h.memoryStats
	}

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

// StartBatchConsumer starts batch consumers for both processors
func (h *HybridBatchProcessor) StartBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Starting hybrid batch consumer")

	// Start RabbitMQ consumer if enabled
	if h.config.EnableRabbitMQ && h.rabbitmqProcessor.IsHealthy() {
		if err := h.rabbitmqProcessor.StartBatchConsumer(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to start RabbitMQ batch consumer")
			if !h.config.FallbackToMemory {
				return err
			}
		}
	}

	// Set dependencies for memory processor using RabbitMQ processor's dependencies
	if h.rabbitmqProcessor != nil {
		h.memoryProcessor.SetDependencies(
			h.rabbitmqProcessor.db,
			h.rabbitmqProcessor.batchOptimizer,
			h.rabbitmqProcessor.cpopCallers,
		)
		log.Debug().Msg("Memory processor dependencies set from RabbitMQ processor")
	}

	// Always start memory processor consumer (as fallback)
	if err := h.memoryProcessor.StartBatchConsumer(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to start memory batch consumer")
		return err
	}

	return nil
}

// StopBatchConsumer stops batch consumers for both processors
func (h *HybridBatchProcessor) StopBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Stopping hybrid batch consumer")

	// Stop RabbitMQ consumer
	if h.rabbitmqProcessor != nil {
		if err := h.rabbitmqProcessor.StopBatchConsumer(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to stop RabbitMQ batch consumer")
		}
	}

	// Stop memory processor consumer
	if h.memoryProcessor != nil {
		if err := h.memoryProcessor.StopBatchConsumer(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to stop memory batch consumer")
		}
	}

	return nil
}

// GetQueueStats returns queue statistics from both processors
func (h *HybridBatchProcessor) GetQueueStats() map[string]QueueStats {
	result := make(map[string]QueueStats)

	// Get RabbitMQ stats
	if h.rabbitmqProcessor != nil {
		rabbitStats := h.rabbitmqProcessor.GetQueueStats()
		for name, stats := range rabbitStats {
			result["rabbitmq."+name] = stats
		}
	}

	// Get memory processor stats
	if h.memoryProcessor != nil {
		memoryStats := h.memoryProcessor.GetQueueStats()
		for name, stats := range memoryStats {
			result["memory."+name] = stats
		}
	}

	return result
}

// IsHealthy checks if at least one processor is healthy
func (h *HybridBatchProcessor) IsHealthy() bool {
	rabbitmqHealthy := h.rabbitmqProcessor != nil && h.rabbitmqProcessor.IsHealthy()
	memoryHealthy := h.memoryProcessor.IsHealthy()

	// If RabbitMQ is enabled, check its health
	if h.config.EnableRabbitMQ {
		if rabbitmqHealthy {
			return true
		}
		// If RabbitMQ is unhealthy but fallback is enabled, check memory processor
		if h.config.FallbackToMemory && memoryHealthy {
			return true
		}
		return false
	}

	// If RabbitMQ is disabled, only check memory processor
	return memoryHealthy
}

// Close closes both processors
func (h *HybridBatchProcessor) Close() error {
	var lastErr error

	if h.rabbitmqProcessor != nil {
		if err := h.rabbitmqProcessor.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close RabbitMQ processor")
			lastErr = err
		}
	}

	if h.memoryProcessor != nil {
		if err := h.memoryProcessor.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close memory processor")
			lastErr = err
		}
	}

	return lastErr
}

// RabbitMQProcessor implementation
func (r *RabbitMQProcessor) PublishTransfer(ctx context.Context, job TransferJob) error {
	queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())
	return r.client.PublishMessage(ctx, queueName, job)
}

func (r *RabbitMQProcessor) PublishAssetAdjust(ctx context.Context, job AssetAdjustJob) error {
	queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())
	return r.client.PublishMessage(ctx, queueName, job)
}

func (r *RabbitMQProcessor) PublishNotification(ctx context.Context, job NotificationJob) error {
	queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())
	return r.client.PublishMessage(ctx, queueName, job)
}

func (r *RabbitMQProcessor) StartBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Starting RabbitMQ batch consumer with CPOP integration")

	// Initialize the new batch consumer if not already created
	if r.batchConsumer == nil {
		r.batchConsumer = NewRabbitMQBatchConsumer(
			r.client,
			r.db,
			r.batchOptimizer,
			r.cpopCallers,
		)
	}

	// Start the new batch consumer
	return r.batchConsumer.Start(ctx)
}

func (r *RabbitMQProcessor) StopBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Graceful shutdown initiated for RabbitMQ batch consumer")

	// Stop the new batch consumer
	if r.batchConsumer != nil {
		return r.batchConsumer.Stop(ctx)
	}

	return nil
}

func (r *RabbitMQProcessor) GetQueueStats() map[string]QueueStats {
	// Delegated to the new batch consumer
	if r.batchConsumer != nil {
		return r.batchConsumer.GetQueueStats()
	}
	return make(map[string]QueueStats)
}

func (r *RabbitMQProcessor) IsHealthy() bool {
	return r.client != nil && r.client.IsHealthy()
}

func (r *RabbitMQProcessor) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// setBatchDependencies sets the batch processing dependencies
func (r *RabbitMQProcessor) setBatchDependencies(db *sql.DB, optimizer *BatchOptimizer, cpopCallers map[int64]*blockchain.CPOPBatchCaller) {
	r.db = db
	r.batchOptimizer = optimizer
	r.cpopCallers = cpopCallers
}
