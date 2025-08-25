package queue

import (
	"fmt"

	"github.com/hzbay/chain-bridge/internal/config"
	"github.com/rs/zerolog/log"
)

// NewBatchProcessor creates a new BatchProcessor based on configuration
func NewBatchProcessor(cfg config.Server) (BatchProcessor, error) {
	// If RabbitMQ is disabled, use memory processor only
	if !cfg.RabbitMQ.Enabled {
		log.Info().Msg("RabbitMQ disabled, using memory processor")
		return NewMemoryProcessor(), nil
	}

	// Try to create RabbitMQ client
	rabbitmqClient, err := NewRabbitMQClient(cfg.RabbitMQ)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create RabbitMQ client")

		// Fallback to memory processor if allowed
		if cfg.RabbitMQ.BatchStrategy.FallbackToMemory {
			log.Warn().Msg("Falling back to memory processor")
			return NewMemoryProcessor(), nil
		}

		return nil, fmt.Errorf("failed to create RabbitMQ client and fallback disabled: %w", err)
	}

	// Create hybrid processor for gradual rollout
	hybridProcessor := NewHybridBatchProcessor(rabbitmqClient, cfg.RabbitMQ.BatchStrategy)

	log.Info().
		Bool("rabbitmq_enabled", cfg.RabbitMQ.BatchStrategy.EnableRabbitMQ).
		Int("rabbitmq_percentage", cfg.RabbitMQ.BatchStrategy.RabbitMQPercentage).
		Msg("Hybrid batch processor created")

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
