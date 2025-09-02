package queue

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// BatchJob represents a job that can be batched
type BatchJob interface {
	GetID() string
	GetChainID() int64
	GetTokenID() int
	GetJobType() JobType
	GetPriority() Priority
	GetCreatedAt() time.Time
}

// JobType defines the type of batch job
type JobType string

const (
	JobTypeTransfer     JobType = "transfer"
	JobTypeAssetAdjust  JobType = "asset_adjust"
	JobTypeNotification JobType = "notification"
	JobTypeHealthCheck  JobType = "health_check"
)

// Priority defines job priority levels
type Priority int

const (
	PriorityLow    Priority = 1
	PriorityNormal Priority = 5
	PriorityHigh   Priority = 10
)

// TransferJob represents a transfer operation to be batched
type TransferJob struct {
	ID            string    `json:"id"`
	JobType       JobType   `json:"job_type"`
	TransactionID uuid.UUID `json:"transaction_id"`
	ChainID       int64     `json:"chain_id"`
	TokenID       int       `json:"token_id"`
	FromUserID    string    `json:"from_user_id"`
	ToUserID      string    `json:"to_user_id"`
	Amount        string    `json:"amount"`
	BusinessType  string    `json:"business_type"`
	ReasonType    string    `json:"reason_type"`
	ReasonDetail  string    `json:"reason_detail,omitempty"`
	Priority      Priority  `json:"priority"`
	CreatedAt     time.Time `json:"created_at"`
}

func (t TransferJob) GetID() string           { return t.ID }
func (t TransferJob) GetChainID() int64       { return t.ChainID }
func (t TransferJob) GetTokenID() int         { return t.TokenID }
func (t TransferJob) GetJobType() JobType     { return t.JobType }
func (t TransferJob) GetPriority() Priority   { return t.Priority }
func (t TransferJob) GetCreatedAt() time.Time { return t.CreatedAt }

// AssetAdjustJob represents an asset adjustment operation (mint/burn)
type AssetAdjustJob struct {
	ID             string    `json:"id"`
	JobType        JobType   `json:"job_type"`
	TransactionID  uuid.UUID `json:"transaction_id"`
	ChainID        int64     `json:"chain_id"`
	TokenID        int       `json:"token_id"`
	UserID         string    `json:"user_id"`
	Amount         string    `json:"amount"`
	AdjustmentType string    `json:"adjustment_type"` // "mint" or "burn"
	BusinessType   string    `json:"business_type"`
	ReasonType     string    `json:"reason_type"`
	ReasonDetail   string    `json:"reason_detail,omitempty"`
	Priority       Priority  `json:"priority"`
	CreatedAt      time.Time `json:"created_at"`
}

func (a AssetAdjustJob) GetID() string           { return a.ID }
func (a AssetAdjustJob) GetChainID() int64       { return a.ChainID }
func (a AssetAdjustJob) GetTokenID() int         { return a.TokenID }
func (a AssetAdjustJob) GetJobType() JobType     { return a.JobType }
func (a AssetAdjustJob) GetPriority() Priority   { return a.Priority }
func (a AssetAdjustJob) GetCreatedAt() time.Time { return a.CreatedAt }

// NotificationJob represents a notification to be sent
type NotificationJob struct {
	ID        string                 `json:"id"`
	JobType   JobType                `json:"job_type"`
	UserID    string                 `json:"user_id,omitempty"`
	EventType string                 `json:"event_type"`
	Data      map[string]interface{} `json:"data"`
	Priority  Priority               `json:"priority"`
	CreatedAt time.Time              `json:"created_at"`
}

func (n NotificationJob) GetID() string           { return n.ID }
func (n NotificationJob) GetChainID() int64       { return 0 } // Notifications are not chain-specific
func (n NotificationJob) GetTokenID() int         { return 0 } // Notifications are not token-specific
func (n NotificationJob) GetJobType() JobType     { return n.JobType }
func (n NotificationJob) GetPriority() Priority   { return n.Priority }
func (n NotificationJob) GetCreatedAt() time.Time { return n.CreatedAt }

// HealthCheckJob represents a health check operation for queue monitoring
type HealthCheckJob struct {
	ID           string    `json:"id"`
	JobType      JobType   `json:"job_type"`
	ChainID      int64     `json:"chain_id"`
	TokenID      int       `json:"token_id"`
	FromUserID   string    `json:"from_user_id"`
	ToUserID     string    `json:"to_user_id"`
	Amount       string    `json:"amount"`
	BusinessType string    `json:"business_type"`
	ReasonType   string    `json:"reason_type"`
	Priority     Priority  `json:"priority"`
	CreatedAt    time.Time `json:"created_at"`
}

func (h HealthCheckJob) GetID() string           { return h.ID }
func (h HealthCheckJob) GetChainID() int64       { return h.ChainID }
func (h HealthCheckJob) GetTokenID() int         { return h.TokenID }
func (h HealthCheckJob) GetJobType() JobType     { return h.JobType }
func (h HealthCheckJob) GetPriority() Priority   { return h.Priority }
func (h HealthCheckJob) GetCreatedAt() time.Time { return h.CreatedAt }

// Stats provides statistics about queue performance
type Stats struct {
	QueueName       string        `json:"queue_name"`
	PendingCount    int           `json:"pending_count"`
	ProcessingCount int           `json:"processing_count"`
	CompletedCount  int64         `json:"completed_count"`
	FailedCount     int64         `json:"failed_count"`
	AverageLatency  time.Duration `json:"average_latency"`
	LastProcessedAt time.Time     `json:"last_processed_at"`
}

// BatchProcessor defines the interface for batch processing
type BatchProcessor interface {
	// Publishing jobs
	PublishTransfer(ctx context.Context, job TransferJob) error
	PublishAssetAdjust(ctx context.Context, job AssetAdjustJob) error
	PublishNotification(ctx context.Context, job NotificationJob) error
	PublishHealthCheck(ctx context.Context, job HealthCheckJob) error

	// Consumer management
	StartBatchConsumer(ctx context.Context) error
	StopBatchConsumer(ctx context.Context) error

	// Monitoring
	GetQueueStats() map[string]Stats
	IsHealthy() bool

	// Close resources
	Close() error
}
