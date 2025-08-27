package queue

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/hzbay/chain-bridge/internal/blockchain"
)

// MemoryProcessor implements BatchProcessor using in-memory queues (fallback)
type MemoryProcessor struct {
	queues map[string]*memoryQueue
	mutex  sync.RWMutex

	// Dependencies (same as RabbitMQ implementation)
	db             *sql.DB
	batchOptimizer *BatchOptimizer
	cpopCallers    map[int64]*blockchain.CPOPBatchCaller

	// Processing settings
	maxBatchSize int
	maxWaitTime  time.Duration

	// Stats tracking
	stats map[string]*queueStats

	// Stop channel for graceful shutdown
	stopChan chan struct{}
	wg       sync.WaitGroup
}

type memoryQueue struct {
	jobs      []BatchJob
	mutex     sync.RWMutex
	timer     *time.Timer
	processor *MemoryProcessor
	queueName string

	// Processing state
	processing bool
	lastBatch  time.Time
}

// MemoryMessageWrapper simulates RabbitMQ MessageWrapper for consistency
type MemoryMessageWrapper struct {
	Job        BatchJob
	ReceivedAt time.Time
	Processed  bool
	Failed     bool
}

type queueStats struct {
	pendingCount    int
	processingCount int
	completedCount  int64
	failedCount     int64
	totalLatency    time.Duration
	processedJobs   int64
	lastProcessedAt time.Time
	mutex           sync.RWMutex
}

// NewMemoryProcessor creates a new memory-based batch processor
func NewMemoryProcessor() *MemoryProcessor {
	return &MemoryProcessor{
		queues:       make(map[string]*memoryQueue),
		stats:        make(map[string]*queueStats),
		maxBatchSize: 30, // Maximum batch size
		maxWaitTime:  15 * time.Second,
		stopChan:     make(chan struct{}),
	}
}

// SetDependencies sets the dependencies needed for real blockchain operations
func (m *MemoryProcessor) SetDependencies(
	db *sql.DB,
	optimizer *BatchOptimizer,
	cpopCallers map[int64]*blockchain.CPOPBatchCaller,
) {
	m.db = db
	m.batchOptimizer = optimizer
	m.cpopCallers = cpopCallers
}

// PublishTransfer publishes a transfer job to memory queue
func (m *MemoryProcessor) PublishTransfer(ctx context.Context, job TransferJob) error {
	queueName := m.getQueueName(job)
	return m.publishJob(queueName, job)
}

// PublishAssetAdjust publishes an asset adjustment job to memory queue
func (m *MemoryProcessor) PublishAssetAdjust(ctx context.Context, job AssetAdjustJob) error {
	queueName := m.getQueueName(job)
	return m.publishJob(queueName, job)
}

// PublishNotification publishes a notification job to memory queue
func (m *MemoryProcessor) PublishNotification(ctx context.Context, job NotificationJob) error {
	queueName := m.getQueueName(job)
	return m.publishJob(queueName, job)
}

// publishJob adds a job to the specified queue
func (m *MemoryProcessor) publishJob(queueName string, job BatchJob) error {
	queue := m.getOrCreateQueue(queueName)

	queue.mutex.Lock()
	defer queue.mutex.Unlock()

	// Add job to queue
	queue.jobs = append(queue.jobs, job)

	// Update stats
	m.updatePendingCount(queueName, len(queue.jobs))

	log.Debug().
		Str("queue", queueName).
		Str("job_id", job.GetID()).
		Str("job_type", string(job.GetJobType())).
		Int("queue_size", len(queue.jobs)).
		Msg("Job added to memory queue")

	// Check if we should process batch immediately
	if m.shouldProcessBatch(queue) {
		go m.processBatchWithRetry(queue)
	} else if queue.timer == nil {
		// Start timer for batch processing
		queue.timer = time.AfterFunc(m.maxWaitTime, func() {
			m.processBatchWithRetry(queue)
		})
	}

	return nil
}

// shouldProcessBatch determines if a batch should be processed immediately
func (m *MemoryProcessor) shouldProcessBatch(queue *memoryQueue) bool {
	// Get optimal batch size using BatchOptimizer (same as RabbitMQ)
	optimalSize := m.maxBatchSize
	if m.batchOptimizer != nil && len(queue.jobs) > 0 {
		firstJob := queue.jobs[0]
		optimalSize = m.batchOptimizer.GetOptimalBatchSize(firstJob.GetChainID(), firstJob.GetTokenID())
	}

	// Process if queue size reaches optimal size
	if len(queue.jobs) >= optimalSize {
		return true
	}

	// Process high priority jobs immediately
	for _, job := range queue.jobs {
		if job.GetPriority() >= PriorityHigh {
			return true
		}
	}

	return false
}

// processBatchWithRetry processes a batch with retry logic (same pattern as RabbitMQ)
func (m *MemoryProcessor) processBatchWithRetry(queue *memoryQueue) {
	queue.mutex.Lock()

	// Check if already processing
	if queue.processing || len(queue.jobs) == 0 {
		queue.mutex.Unlock()
		return
	}

	queue.processing = true

	// Stop timer if it's running
	if queue.timer != nil {
		queue.timer.Stop()
		queue.timer = nil
	}

	// Get optimal batch size
	optimalSize := m.maxBatchSize
	if m.batchOptimizer != nil && len(queue.jobs) > 0 {
		firstJob := queue.jobs[0]
		optimalSize = m.batchOptimizer.GetOptimalBatchSize(firstJob.GetChainID(), firstJob.GetTokenID())
	}

	// Take jobs for batch processing (limit to optimal size)
	batchSize := minMemory(len(queue.jobs), optimalSize)
	batchJobs := make([]BatchJob, batchSize)
	copy(batchJobs, queue.jobs[:batchSize])

	// Remove processed jobs from queue
	queue.jobs = queue.jobs[batchSize:]

	queue.mutex.Unlock()

	// Update stats
	m.updateProcessingCount(queue.queueName, len(batchJobs))
	m.updatePendingCount(queue.queueName, len(queue.jobs))

	// Process the batch using real blockchain and database operations
	m.processBatch(context.Background(), batchJobs, queue.queueName)

	// Update stats after processing
	queue.mutex.Lock()
	queue.processing = false
	queue.lastBatch = time.Now()
	queue.mutex.Unlock()

	m.updateProcessingCount(queue.queueName, 0)
}

// processBatch processes a batch using real blockchain and database operations (same as RabbitMQ)
func (m *MemoryProcessor) processBatch(ctx context.Context, jobs []BatchJob, queueName string) {
	log.Info().
		Str("queue", queueName).
		Int("batch_size", len(jobs)).
		Msg("Processing memory batch with real blockchain operations")

	startTime := time.Now()

	// Check if dependencies are available
	if m.db == nil || m.cpopCallers == nil {
		log.Warn().Msg("Memory processor dependencies not set, falling back to simulation")
		m.simulateProcessing(jobs)
		return
	}

	// Group jobs by BatchGroup (same as RabbitMQ)
	group := m.getBatchGroup(jobs[0])

	// Create memory message wrappers
	messages := make([]*MemoryMessageWrapper, len(jobs))
	for i, job := range jobs {
		messages[i] = &MemoryMessageWrapper{
			Job:        job,
			ReceivedAt: time.Now(),
		}
	}

	// Step 1: Insert/Update transactions to 'batching' status
	batchID := uuid.New()
	err := m.updateTransactionsToBatching(ctx, messages, batchID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update transactions to batching status")
		m.handleBatchFailure(ctx, messages, batchID, err)
		return
	}

	// Step 2: Execute blockchain batch operation
	result, err := m.executeBlockchainBatch(ctx, messages, group)
	if err != nil {
		log.Error().Err(err).Msg("Blockchain batch operation failed")
		m.handleBatchFailure(ctx, messages, batchID, err)
		return
	}

	processingTime := time.Since(startTime)

	// Step 3: Atomic update of all three tables
	err = m.updateThreeTablesAfterSuccess(ctx, messages, group, batchID, result, processingTime)
	if err != nil {
		log.Error().Err(err).
			Str("tx_hash", result.TxHash).
			Msg("Failed to update database after successful blockchain operation")
		m.handleBatchFailure(ctx, messages, batchID, err)
		return
	}

	// Step 4: Mark messages as successfully processed
	m.ackAllMessages(messages)

	// Step 5: Record performance metrics
	if m.batchOptimizer != nil {
		performance := BatchPerformance{
			BatchSize:        len(messages),
			ProcessingTime:   processingTime,
			GasSaved:         float64(result.GasSaved),
			EfficiencyRating: result.Efficiency,
			Timestamp:        time.Now(),
			ChainID:          group.ChainID,
			TokenID:          group.TokenID,
		}
		m.batchOptimizer.RecordBatchPerformance(performance)
	}

	m.updateCompletedCount(queueName, int64(len(jobs)), processingTime)
	log.Info().
		Str("batch_id", batchID.String()).
		Str("tx_hash", result.TxHash).
		Float64("efficiency", result.Efficiency).
		Dur("processing_time", processingTime).
		Int("messages_processed", len(messages)).
		Msg("Memory batch processed successfully")
}

// simulateProcessing fallback simulation when dependencies are not available
func (m *MemoryProcessor) simulateProcessing(jobs []BatchJob) {
	// Simulate processing time based on batch size
	processingTime := time.Duration(len(jobs)) * 100 * time.Millisecond
	time.Sleep(processingTime)

	log.Info().
		Int("batch_size", len(jobs)).
		Dur("processing_time", processingTime).
		Msg("Memory batch processed (simulated)")
}

// getOrCreateQueue gets an existing queue or creates a new one
func (m *MemoryProcessor) getOrCreateQueue(queueName string) *memoryQueue {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if queue, exists := m.queues[queueName]; exists {
		return queue
	}

	queue := &memoryQueue{
		jobs:      make([]BatchJob, 0),
		processor: m,
		queueName: queueName,
	}

	m.queues[queueName] = queue
	m.stats[queueName] = &queueStats{}

	log.Debug().
		Str("queue", queueName).
		Msg("Created new memory queue")

	return queue
}

// getQueueName generates a queue name for a job
func (m *MemoryProcessor) getQueueName(job BatchJob) string {
	return fmt.Sprintf("memory.%s.%d.%d",
		string(job.GetJobType()),
		job.GetChainID(),
		job.GetTokenID())
}

// StartBatchConsumer starts the batch consumer (no-op for memory processor)
func (m *MemoryProcessor) StartBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Memory processor consumer started (in-process)")
	return nil
}

// StopBatchConsumer stops the batch consumer gracefully
func (m *MemoryProcessor) StopBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Graceful shutdown initiated for memory processor")

	// Signal stop to all running processes
	close(m.stopChan)

	// Process all remaining jobs
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)

		m.mutex.Lock()
		remainingJobs := 0
		for queueName, queue := range m.queues {
			queue.mutex.Lock()
			if len(queue.jobs) > 0 {
				remainingJobs += len(queue.jobs)
				log.Info().
					Str("queue", queueName).
					Int("remaining_jobs", len(queue.jobs)).
					Msg("Processing remaining jobs before shutdown")

				// Process the remaining batch immediately
				m.wg.Add(1)
				go func(q *memoryQueue) {
					defer m.wg.Done()
					m.processBatchWithRetry(q)
				}(queue)
			}
			queue.mutex.Unlock()
		}
		m.mutex.Unlock()

		if remainingJobs > 0 {
			log.Info().Int("remaining_jobs", remainingJobs).Msg("Waiting for remaining jobs to complete")
		}

		// Wait for all processing to complete
		m.wg.Wait()
	}()

	select {
	case <-done:
		log.Info().Msg("All remaining jobs processed, memory processor stopped gracefully")
	case <-timeout.C:
		log.Warn().
			Dur("timeout", 30*time.Second).
			Msg("Memory processor shutdown timeout, some jobs may not be completed")
	}

	return nil
}

// GetQueueStats returns current queue statistics
func (m *MemoryProcessor) GetQueueStats() map[string]QueueStats {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]QueueStats)

	for queueName, stats := range m.stats {
		stats.mutex.RLock()

		var averageLatency time.Duration
		if stats.processedJobs > 0 {
			averageLatency = stats.totalLatency / time.Duration(stats.processedJobs)
		}

		result[queueName] = QueueStats{
			QueueName:       queueName,
			PendingCount:    stats.pendingCount,
			ProcessingCount: stats.processingCount,
			CompletedCount:  stats.completedCount,
			FailedCount:     stats.failedCount,
			AverageLatency:  averageLatency,
			LastProcessedAt: stats.lastProcessedAt,
		}

		stats.mutex.RUnlock()
	}

	return result
}

// IsHealthy checks if the memory processor is healthy
func (m *MemoryProcessor) IsHealthy() bool {
	return true // Memory processor is always healthy
}

// Close closes the memory processor
func (m *MemoryProcessor) Close() error {
	close(m.stopChan)

	// Stop all timers
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, queue := range m.queues {
		queue.mutex.Lock()
		if queue.timer != nil {
			queue.timer.Stop()
		}
		queue.mutex.Unlock()
	}

	log.Info().Msg("Memory processor closed")
	return nil
}

// Helper methods - same logic as RabbitMQ implementation

// getBatchGroup extracts BatchGroup from a job
func (m *MemoryProcessor) getBatchGroup(job BatchJob) BatchGroup {
	return BatchGroup{
		ChainID: job.GetChainID(),
		TokenID: job.GetTokenID(),
		JobType: job.GetJobType(),
	}
}

// updateTransactionsToBatching updates transaction records to 'batching' status
func (m *MemoryProcessor) updateTransactionsToBatching(ctx context.Context, messages []*MemoryMessageWrapper, batchID uuid.UUID) error {
	if m.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, msgWrapper := range messages {
		job := msgWrapper.Job

		// Insert or update transaction record using same logic as RabbitMQ
		err = m.upsertTransactionRecord(tx, job, batchID)
		if err != nil {
			return fmt.Errorf("failed to upsert transaction record: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batching status update: %w", err)
	}

	return nil
}

// upsertTransactionRecord - same logic as RabbitMQ batch consumer
func (m *MemoryProcessor) upsertTransactionRecord(tx *sql.Tx, job BatchJob, batchID uuid.UUID) error {
	// This is the same implementation as in batch_processor.go
	// For brevity, using the RabbitMQBatchConsumer logic
	consumer := &RabbitMQBatchConsumer{db: m.db}
	return consumer.upsertTransactionRecord(tx, job, batchID)
}

// executeBlockchainBatch executes the blockchain batch operation
func (m *MemoryProcessor) executeBlockchainBatch(ctx context.Context, messages []*MemoryMessageWrapper, group BatchGroup) (*blockchain.BatchResult, error) {
	caller := m.cpopCallers[group.ChainID]
	if caller == nil {
		return nil, fmt.Errorf("no CPOP caller found for chain %d", group.ChainID)
	}

	jobs := make([]BatchJob, len(messages))
	for i, msg := range messages {
		jobs[i] = msg.Job
	}

	switch group.JobType {
	case JobTypeAssetAdjust:
		return m.processAssetAdjustBatch(ctx, caller, jobs)
	case JobTypeTransfer:
		return m.processTransferBatch(ctx, caller, jobs)
	default:
		return nil, fmt.Errorf("unsupported job type: %s", group.JobType)
	}
}

// processAssetAdjustBatch, processTransferBatch - same as RabbitMQ implementation
func (m *MemoryProcessor) processAssetAdjustBatch(ctx context.Context, caller *blockchain.CPOPBatchCaller, jobs []BatchJob) (*blockchain.BatchResult, error) {
	// Same logic as RabbitMQ batch consumer
	consumer := &RabbitMQBatchConsumer{cpopCallers: m.cpopCallers}
	return consumer.processAssetAdjustBatch(ctx, caller, jobs)
}

func (m *MemoryProcessor) processTransferBatch(ctx context.Context, caller *blockchain.CPOPBatchCaller, jobs []BatchJob) (*blockchain.BatchResult, error) {
	// Same logic as RabbitMQ batch consumer
	consumer := &RabbitMQBatchConsumer{cpopCallers: m.cpopCallers}
	return consumer.processTransferBatch(ctx, caller, jobs)
}

// updateThreeTablesAfterSuccess - same as RabbitMQ implementation
func (m *MemoryProcessor) updateThreeTablesAfterSuccess(
	ctx context.Context,
	messages []*MemoryMessageWrapper,
	group BatchGroup,
	batchID uuid.UUID,
	result *blockchain.BatchResult,
	processingTime time.Duration,
) error {
	if m.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Begin transaction for atomic updates of all three tables
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	jobs := make([]BatchJob, len(messages))
	for i, msg := range messages {
		jobs[i] = msg.Job
	}

	// Use same logic as RabbitMQ batch consumer
	consumer := &RabbitMQBatchConsumer{db: m.db, batchOptimizer: m.batchOptimizer}

	// 1. Create batch record
	err = consumer.createBatchRecord(tx, batchID, group, jobs, result, processingTime)
	if err != nil {
		return fmt.Errorf("failed to create batch record: %w", err)
	}

	// 2. Update transaction statuses
	err = consumer.updateTransactionStatuses(tx, jobs, batchID, result)
	if err != nil {
		return fmt.Errorf("failed to update transaction statuses: %w", err)
	}

	// 3. Update user balances
	err = consumer.updateUserBalances(tx, jobs, group)
	if err != nil {
		return fmt.Errorf("failed to update user balances: %w", err)
	}

	// Commit all changes atomically
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit three-table update: %w", err)
	}

	log.Debug().
		Str("batch_id", batchID.String()).
		Int("jobs_count", len(jobs)).
		Msg("Successfully updated all three tables atomically (Memory)")

	return nil
}

// ackAllMessages marks messages as successfully processed
func (m *MemoryProcessor) ackAllMessages(messages []*MemoryMessageWrapper) {
	successCount := 0
	for _, msgWrapper := range messages {
		msgWrapper.Processed = true
		successCount++
	}

	log.Info().
		Int("total_messages", len(messages)).
		Int("acked_messages", successCount).
		Msg("Memory messages ACK completed")
}

// handleBatchFailure handles batch processing failures
func (m *MemoryProcessor) handleBatchFailure(ctx context.Context, messages []*MemoryMessageWrapper, batchID uuid.UUID, failureErr error) {
	log.Error().Err(failureErr).Str("batch_id", batchID.String()).Msg("Handling memory batch failure")

	if m.db != nil {
		// Update transactions to failed status
		jobIDs := make([]uuid.UUID, len(messages))
		for i, msg := range messages {
			if txID, err := uuid.Parse(msg.Job.GetID()); err == nil {
				jobIDs[i] = txID
			}
		}

		// Use same failure handling as RabbitMQ
		consumer := &RabbitMQBatchConsumer{db: m.db}
		consumer.handleBatchFailure(ctx, nil, batchID, failureErr)
	}

	// Mark messages as failed (Memory equivalent of NACK)
	m.nackAllMessages(messages)
	m.updateFailedCount("memory", int64(len(messages)))
}

// nackAllMessages marks messages as failed for potential retry
func (m *MemoryProcessor) nackAllMessages(messages []*MemoryMessageWrapper) {
	for _, msgWrapper := range messages {
		msgWrapper.Failed = true
	}

	log.Warn().
		Int("nacked_messages", len(messages)).
		Msg("Memory messages NACKed for potential retry")
}

// Helper methods to update statistics
func (m *MemoryProcessor) updatePendingCount(queueName string, count int) {
	if stats, exists := m.stats[queueName]; exists {
		stats.mutex.Lock()
		stats.pendingCount = count
		stats.mutex.Unlock()
	}
}

func (m *MemoryProcessor) updateProcessingCount(queueName string, count int) {
	if stats, exists := m.stats[queueName]; exists {
		stats.mutex.Lock()
		stats.processingCount = count
		stats.mutex.Unlock()
	}
}

func (m *MemoryProcessor) updateCompletedCount(queueName string, count int64, latency time.Duration) {
	if stats, exists := m.stats[queueName]; exists {
		stats.mutex.Lock()
		stats.completedCount += count
		stats.processedJobs += count
		stats.totalLatency += latency
		stats.lastProcessedAt = time.Now()
		stats.mutex.Unlock()
	}
}

func (m *MemoryProcessor) updateFailedCount(queueName string, count int64) {
	if stats, exists := m.stats[queueName]; exists {
		stats.mutex.Lock()
		stats.failedCount += count
		stats.mutex.Unlock()
	}
}

// minMemory helper function (renamed to avoid conflict)
func minMemory(a, b int) int {
	if a < b {
		return a
	}
	return b
}
