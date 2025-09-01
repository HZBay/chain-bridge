package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// QueueMonitor provides monitoring and metrics for queue operations
type QueueMonitor struct {
	processor BatchProcessor
	metrics   *QueueMetrics
	mutex     sync.RWMutex
}

// QueueMetrics holds queue operation statistics
type QueueMetrics struct {
	TotalJobsPublished    int64            `json:"total_jobs_published"`
	TotalJobsProcessed    int64            `json:"total_jobs_processed"`
	TotalJobsFailed       int64            `json:"total_jobs_failed"`
	AverageProcessingTime float64          `json:"average_processing_time_ms"`
	QueueDepth            int64            `json:"queue_depth"`
	LastUpdated           time.Time        `json:"last_updated"`
	ProcessorType         string           `json:"processor_type"`
	ConnectionStatus      string           `json:"connection_status"`
	AlertCounts           map[string]int64 `json:"alert_counts,omitempty"`
	LastAlertTime         time.Time        `json:"last_alert_time,omitempty"`
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
		JobType:      JobTypeTransfer,
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
				m.sendAlert(ctx, AlertTypeHealthCheckFailure, fmt.Sprintf("Queue health check failed: %v", err))
			}
		}
	}
}

// AlertType defines the type of monitoring alerts
type AlertType string

const (
	AlertTypeHealthCheckFailure AlertType = "health_check_failure"
	AlertTypeQueueBacklog       AlertType = "queue_backlog"
	AlertTypeHighErrorRate      AlertType = "high_error_rate"
	AlertTypeProcessingDelay    AlertType = "processing_delay"
)

// Alert represents a monitoring alert
type Alert struct {
	Type        AlertType `json:"type"`
	Message     string    `json:"message"`
	Severity    string    `json:"severity"`
	Timestamp   time.Time `json:"timestamp"`
	QueueName   string    `json:"queue_name,omitempty"`
	MetricValue float64   `json:"metric_value,omitempty"`
}

// sendAlert sends an alert to the monitoring system
func (m *QueueMonitor) sendAlert(ctx context.Context, alertType AlertType, message string) {
	alert := Alert{
		Type:      alertType,
		Message:   message,
		Severity:  m.getAlertSeverity(alertType),
		Timestamp: time.Now(),
	}

	// Log the alert
	log.Warn().
		Str("alert_type", string(alertType)).
		Str("severity", alert.Severity).
		Str("message", message).
		Time("timestamp", alert.Timestamp).
		Msg("Queue monitoring alert triggered")

	// In a production environment, you would:
	// 1. Send to external monitoring systems (Prometheus, Grafana, etc.)
	// 2. Send notifications (email, Slack, PagerDuty)
	// 3. Store in metrics database
	// 4. Trigger automated remediation if applicable

	// For now, we'll just increment internal counters and log
	m.incrementAlertCounter(alertType)
}

// getAlertSeverity returns the severity level for an alert type
func (m *QueueMonitor) getAlertSeverity(alertType AlertType) string {
	switch alertType {
	case AlertTypeHealthCheckFailure:
		return "critical"
	case AlertTypeHighErrorRate:
		return "high"
	case AlertTypeQueueBacklog:
		return "medium"
	case AlertTypeProcessingDelay:
		return "low"
	default:
		return "low"
	}
}

// incrementAlertCounter increments the alert counter for metrics
func (m *QueueMonitor) incrementAlertCounter(alertType AlertType) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Initialize alert counters if not exist
	if m.metrics.AlertCounts == nil {
		m.metrics.AlertCounts = make(map[string]int64)
	}

	m.metrics.AlertCounts[string(alertType)]++
	m.metrics.LastAlertTime = time.Now()
}

// CheckQueueBacklog monitors queue depth and triggers alerts if needed
func (m *QueueMonitor) CheckQueueBacklog(ctx context.Context) {
	const maxQueueDepth = 1000 // configurable threshold

	stats := m.processor.GetQueueStats()
	for queueName, queueStats := range stats {
		if queueStats.PendingCount > maxQueueDepth {
			message := fmt.Sprintf("Queue %s has %d pending items (threshold: %d)",
				queueName, queueStats.PendingCount, maxQueueDepth)

			log.Warn().
				Str("queue_name", queueName).
				Int("pending_count", queueStats.PendingCount).
				Int("threshold", maxQueueDepth).
				Msg("Queue backlog alert")

			m.sendAlert(ctx, AlertTypeQueueBacklog, message)
		}
	}
}

// CheckProcessingDelay monitors processing latency and triggers alerts if needed
func (m *QueueMonitor) CheckProcessingDelay(ctx context.Context) {
	const maxLatency = 5 * time.Minute // configurable threshold

	stats := m.processor.GetQueueStats()
	for queueName, queueStats := range stats {
		if queueStats.AverageLatency > maxLatency {
			message := fmt.Sprintf("Queue %s average latency is %v (threshold: %v)",
				queueName, queueStats.AverageLatency, maxLatency)

			log.Warn().
				Str("queue_name", queueName).
				Dur("avg_latency", queueStats.AverageLatency).
				Dur("threshold", maxLatency).
				Msg("Processing delay alert")

			m.sendAlert(ctx, AlertTypeProcessingDelay, message)
		}
	}
}
