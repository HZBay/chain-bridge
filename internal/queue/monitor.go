package queue

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog/log"
)

// QueueMonitor provides monitoring and metrics for queue operations
type QueueMonitor struct {
	processor BatchProcessor
	metrics   *QueueMetrics
}

// QueueMetrics holds queue operation statistics
type QueueMetrics struct {
	TotalJobsPublished    int64     `json:"total_jobs_published"`
	TotalJobsProcessed    int64     `json:"total_jobs_processed"`
	TotalJobsFailed       int64     `json:"total_jobs_failed"`
	AverageProcessingTime float64   `json:"average_processing_time_ms"`
	QueueDepth            int64     `json:"queue_depth"`
	LastUpdated           time.Time `json:"last_updated"`
	ProcessorType         string    `json:"processor_type"`
	ConnectionStatus      string    `json:"connection_status"`
}

// NewQueueMonitor creates a new queue monitor
func NewQueueMonitor(processor BatchProcessor) *QueueMonitor {
	return &QueueMonitor{
		processor: processor,
		metrics: &QueueMetrics{
			LastUpdated:      time.Now(),
			ProcessorType:    "unknown",
			ConnectionStatus: "unknown",
		},
	}
}

// GetMetrics returns current queue metrics
func (m *QueueMonitor) GetMetrics() *QueueMetrics {
	// Update metrics from processor
	stats := m.processor.GetQueueStats()

	// Aggregate stats from all queues
	var totalDepth int64
	var totalProcessed int64
	var totalFailed int64
	connectionStatus := "healthy"

	for queueName, stat := range stats {
		totalDepth += int64(stat.PendingCount + stat.ProcessingCount)
		totalProcessed += stat.CompletedCount
		totalFailed += stat.FailedCount

		// Determine queue health based on metrics
		if stat.FailedCount > stat.CompletedCount/10 { // More than 10% failure rate
			connectionStatus = "degraded"
			log.Warn().
				Str("queue", queueName).
				Int64("failed", stat.FailedCount).
				Int64("completed", stat.CompletedCount).
				Msg("Queue showing high failure rate")
		}
	}

	m.metrics.QueueDepth = totalDepth
	m.metrics.TotalJobsProcessed = totalProcessed
	m.metrics.TotalJobsFailed = totalFailed
	m.metrics.ConnectionStatus = connectionStatus
	m.metrics.LastUpdated = time.Now()

	return m.metrics
}

// GetDetailedStats returns detailed statistics for all queues
func (m *QueueMonitor) GetDetailedStats() map[string]QueueStats {
	return m.processor.GetQueueStats()
}

// HealthCheck performs a health check on the queue system
func (m *QueueMonitor) HealthCheck(ctx context.Context) error {
	// Create a test job to verify queue is working
	testJob := TransferJob{
		ID:           "health-check-" + time.Now().Format("20060102150405"),
		ChainID:      1, // Test chain
		TokenID:      1, // Test token
		FromUserID:   "health-check-user",
		ToUserID:     "health-check-user",
		Amount:       "0.000001", // Minimal amount
		BusinessType: "health_check",
		ReasonType:   "system_health_check",
		Priority:     PriorityLow,
		CreatedAt:    time.Now(),
	}

	// Try to publish test job
	if err := m.processor.PublishTransfer(ctx, testJob); err != nil {
		log.Error().Err(err).Msg("Queue health check failed - unable to publish test job")
		return err
	}

	log.Info().Msg("Queue health check passed")
	return nil
}

// ExportMetrics exports metrics in JSON format for external monitoring systems
func (m *QueueMonitor) ExportMetrics() ([]byte, error) {
	metrics := m.GetMetrics()
	detailedStats := m.GetDetailedStats()

	export := map[string]interface{}{
		"summary":        metrics,
		"detailed_stats": detailedStats,
		"export_time":    time.Now(),
	}

	return json.MarshalIndent(export, "", "  ")
}

// StartPeriodicHealthCheck starts a background goroutine that performs periodic health checks
func (m *QueueMonitor) StartPeriodicHealthCheck(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Info().
		Dur("interval", interval).
		Msg("Starting periodic queue health checks")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping periodic health checks")
			return
		case <-ticker.C:
			if err := m.HealthCheck(ctx); err != nil {
				log.Error().Err(err).Msg("Periodic health check failed")
				// TODO: Send alert to monitoring system
			}
		}
	}
}
