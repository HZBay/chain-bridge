package queue

import (
	"fmt"

	"github.com/hzbay/chain-bridge/internal/config"
	"github.com/rs/zerolog/log"
)

// NewBatchProcessor creates a new RabbitMQ-based BatchProcessor
func NewBatchProcessor(cfg config.Server) (BatchProcessor, error) {
	// RabbitMQ is now required - no fallback to memory processor
	if !cfg.RabbitMQ.Enabled {
		log.Error().Msg("RabbitMQ is disabled but required for batch processing")
		return nil, fmt.Errorf("RabbitMQ is required for batch processing but is disabled in configuration")
	}

	// Try to create RabbitMQ client
	rabbitmqClient, err := NewRabbitMQClient(cfg.RabbitMQ)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create RabbitMQ client")
		return nil, fmt.Errorf("failed to create RabbitMQ client: %w", err)
	}

	// Create simplified hybrid processor (RabbitMQ only)
	hybridProcessor := NewHybridBatchProcessor(rabbitmqClient, cfg.RabbitMQ.BatchStrategy)

	log.Info().
		Bool("rabbitmq_enabled", cfg.RabbitMQ.BatchStrategy.EnableRabbitMQ).
		Msg("RabbitMQ batch processor created")

	return hybridProcessor, nil
}

// MustNewBatchProcessor creates a new BatchProcessor and panics on error
func MustNewBatchProcessor(cfg config.Server) BatchProcessor {
	processor, err := NewBatchProcessor(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create batch processor")
	}
	return processor
}
