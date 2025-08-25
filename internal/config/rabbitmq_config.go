package config

import (
	"fmt"
	"time"

	"github.com/hzbay/chain-bridge/internal/util"
)

// RabbitMQConfig contains configuration for RabbitMQ message queue
type RabbitMQConfig struct {
	Enabled  bool   `envconfig:"ENABLED"`
	Host     string `envconfig:"HOST"`
	Port     int    `envconfig:"PORT"`
	Username string `envconfig:"USERNAME"`
	Password string `envconfig:"PASSWORD" json:"-"` // sensitive field
	VHost    string `envconfig:"VHOST"`

	// Connection settings
	MaxConnections int           `envconfig:"MAX_CONNECTIONS"`
	Heartbeat      time.Duration `envconfig:"HEARTBEAT"`

	// Queue settings
	QueuePrefix   string `envconfig:"QUEUE_PREFIX"`
	DurableQueues bool   `envconfig:"DURABLE_QUEUES"`

	// Batch processing settings
	BatchStrategy BatchProcessingStrategy
}

// BatchProcessingStrategy defines the strategy for batch processing
type BatchProcessingStrategy struct {
	EnableRabbitMQ     bool `envconfig:"ENABLE_RABBITMQ"`
	RabbitMQPercentage int  `envconfig:"RABBITMQ_PERCENTAGE"` // 0-100, for gradual rollout
	FallbackToMemory   bool `envconfig:"FALLBACK_TO_MEMORY"`
	MonitoringEnabled  bool `envconfig:"MONITORING_ENABLED"`
}

// LoadRabbitMQConfig loads RabbitMQ configuration from environment variables
func LoadRabbitMQConfig() RabbitMQConfig {
	return RabbitMQConfig{
		Enabled:        util.GetEnvAsBool("RABBITMQ_ENABLED", false), // Default: disabled
		Host:           util.GetEnv("RABBITMQ_HOST", "localhost"),
		Port:           util.GetEnvAsInt("RABBITMQ_PORT", 5672),
		Username:       util.GetEnv("RABBITMQ_USERNAME", "guest"),
		Password:       util.GetEnv("RABBITMQ_PASSWORD", "guest"),
		VHost:          util.GetEnv("RABBITMQ_VHOST", "/"),
		MaxConnections: util.GetEnvAsInt("RABBITMQ_MAX_CONNECTIONS", 10),
		Heartbeat:      time.Second * time.Duration(util.GetEnvAsInt("RABBITMQ_HEARTBEAT_SECONDS", 60)),
		QueuePrefix:    util.GetEnv("RABBITMQ_QUEUE_PREFIX", "chain-bridge"),
		DurableQueues:  util.GetEnvAsBool("RABBITMQ_DURABLE_QUEUES", true),
		BatchStrategy: BatchProcessingStrategy{
			EnableRabbitMQ:     util.GetEnvAsBool("BATCH_ENABLE_RABBITMQ", false),
			RabbitMQPercentage: util.GetEnvAsInt("BATCH_RABBITMQ_PERCENTAGE", 0), // Start with 0%
			FallbackToMemory:   util.GetEnvAsBool("BATCH_FALLBACK_TO_MEMORY", true),
			MonitoringEnabled:  util.GetEnvAsBool("BATCH_MONITORING_ENABLED", true),
		},
	}
}

// GetConnectionURL returns the RabbitMQ connection URL
func (r RabbitMQConfig) GetConnectionURL() string {
	return fmt.Sprintf("amqp://%s:%s@%s:%d%s",
		r.Username, r.Password, r.Host, r.Port, r.VHost)
}

// GetQueueName generates queue name based on operation and chain/token
func (r RabbitMQConfig) GetQueueName(operation string, chainID int64, tokenID int) string {
	return fmt.Sprintf("%s.%s.%d.%d", r.QueuePrefix, operation, chainID, tokenID)
}

// IsHealthy checks if RabbitMQ configuration is valid
func (r RabbitMQConfig) IsHealthy() error {
	if !r.Enabled {
		return fmt.Errorf("RabbitMQ is disabled")
	}

	if r.Host == "" {
		return fmt.Errorf("RabbitMQ host is required")
	}

	if r.Port <= 0 || r.Port > 65535 {
		return fmt.Errorf("RabbitMQ port must be between 1 and 65535")
	}

	return nil
}
