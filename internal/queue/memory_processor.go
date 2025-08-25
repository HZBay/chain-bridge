package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// MemoryProcessor implements BatchProcessor using in-memory queues (fallback)
type MemoryProcessor struct {
	queues map[string]*memoryQueue
	mutex  sync.RWMutex

	// Processing settings
	batchSize   int
	maxWaitTime time.Duration

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
		queues:      make(map[string]*memoryQueue),
		stats:       make(map[string]*queueStats),
		batchSize:   25, // Default optimal batch size
		maxWaitTime: 15 * time.Second,
		stopChan:    make(chan struct{}),
	}
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
		go m.processBatch(queue)
	} else if queue.timer == nil {
		// Start timer for batch processing
		queue.timer = time.AfterFunc(m.maxWaitTime, func() {
			m.processBatch(queue)
		})
	}

	return nil
}

// shouldProcessBatch determines if a batch should be processed immediately
func (m *MemoryProcessor) shouldProcessBatch(queue *memoryQueue) bool {
	// Process if queue size reaches batch size
	if len(queue.jobs) >= m.batchSize {
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

// processBatch processes a batch of jobs from the queue
func (m *MemoryProcessor) processBatch(queue *memoryQueue) {
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

	// Take jobs for batch processing
	batchJobs := make([]BatchJob, len(queue.jobs))
	copy(batchJobs, queue.jobs)
	queue.jobs = queue.jobs[:0] // Clear the queue

	queue.mutex.Unlock()

	// Update stats
	m.updateProcessingCount(queue.queueName, len(batchJobs))
	m.updatePendingCount(queue.queueName, 0)

	log.Info().
		Str("queue", queue.queueName).
		Int("batch_size", len(batchJobs)).
		Msg("Processing memory batch")

	// Process the batch (simulate processing)
	startTime := time.Now()
	success := m.simulateProcessing(batchJobs)
	processingTime := time.Since(startTime)

	// Update stats after processing
	queue.mutex.Lock()
	queue.processing = false
	queue.lastBatch = time.Now()
	queue.mutex.Unlock()

	if success {
		m.updateCompletedCount(queue.queueName, int64(len(batchJobs)), processingTime)
		log.Info().
			Str("queue", queue.queueName).
			Int("batch_size", len(batchJobs)).
			Dur("processing_time", processingTime).
			Msg("Memory batch processed successfully")
	} else {
		m.updateFailedCount(queue.queueName, int64(len(batchJobs)))
		log.Error().
			Str("queue", queue.queueName).
			Int("batch_size", len(batchJobs)).
			Msg("Memory batch processing failed")
	}

	m.updateProcessingCount(queue.queueName, 0)
}

// simulateProcessing simulates batch processing (placeholder implementation)
func (m *MemoryProcessor) simulateProcessing(jobs []BatchJob) bool {
	// Simulate processing time based on batch size
	processingTime := time.Duration(len(jobs)) * 100 * time.Millisecond
	time.Sleep(processingTime)

	// Simulate 95% success rate
	return time.Now().UnixNano()%100 < 95
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

// StopBatchConsumer stops the batch consumer
func (m *MemoryProcessor) StopBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Memory processor consumer stopped")
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
