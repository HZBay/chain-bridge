package queue

import (
	"context"
	"database/sql"

	"github.com/hzbay/chain-bridge/internal/config"
	"github.com/rs/zerolog/log"
)

// RabbitMQProcessor wraps RabbitMQ client for batch processing
type RabbitMQProcessor struct {
	client         *RabbitMQClient
	db             *sql.DB
	batchOptimizer *BatchOptimizer

	// Consumer management
	consumerManager *ConsumerManager
	stopChan        chan struct{}

	config config.Server
}

// NewRabbitMQProcessor creates a new RabbitMQ processor
func NewRabbitMQProcessor(client *RabbitMQClient, db *sql.DB, optimizer *BatchOptimizer, config config.Server) *RabbitMQProcessor {
	rp := &RabbitMQProcessor{
		client:         client,
		db:             db,
		batchOptimizer: optimizer,
		stopChan:       make(chan struct{}),
		config:         config,
	}
	rp.consumerManager = NewConsumerManager(client, db, optimizer, rp, config)
	return rp
}

// PublishTransfer publishes a transfer job to the appropriate chain-specific queue
func (r *RabbitMQProcessor) PublishTransfer(ctx context.Context, job TransferJob) error {
	queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())

	log.Debug().
		Str("queue", queueName).
		Str("job_id", job.GetID()).
		Int64("chain_id", job.GetChainID()).
		Int("token_id", job.GetTokenID()).
		Msg("Publishing transfer job to chain-specific queue")

	return r.client.PublishMessage(ctx, queueName, job)
}

// PublishAssetAdjust publishes an asset adjustment job to the appropriate chain-specific queue
func (r *RabbitMQProcessor) PublishAssetAdjust(ctx context.Context, job AssetAdjustJob) error {
	queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())

	log.Debug().
		Str("queue", queueName).
		Str("job_id", job.GetID()).
		Int64("chain_id", job.GetChainID()).
		Int("token_id", job.GetTokenID()).
		Msg("Publishing asset adjust job to chain-specific queue")

	return r.client.PublishMessage(ctx, queueName, job)
}

// PublishNotification publishes a notification job to the appropriate chain-specific queue
func (r *RabbitMQProcessor) PublishNotification(ctx context.Context, job NotificationJob) error {
	queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())

	log.Debug().
		Str("queue", queueName).
		Str("job_id", job.GetID()).
		Int64("chain_id", job.GetChainID()).
		Int("token_id", job.GetTokenID()).
		Msg("Publishing notification job to chain-specific queue")

	return r.client.PublishMessage(ctx, queueName, job)
}

// PublishHealthCheck publishes a health check job to the appropriate queue
func (r *RabbitMQProcessor) PublishHealthCheck(ctx context.Context, job HealthCheckJob) error {
	queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())

	log.Debug().
		Str("queue", queueName).
		Str("job_id", job.GetID()).
		Int64("chain_id", job.GetChainID()).
		Int("token_id", job.GetTokenID()).
		Msg("Publishing health check job to queue")

	return r.client.PublishMessage(ctx, queueName, job)
}

// PublishNFTMint publishes an NFT mint job to the appropriate chain-specific queue
func (r *RabbitMQProcessor) PublishNFTMint(ctx context.Context, job NFTMintJob) error {
	queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())

	log.Debug().
		Str("queue", queueName).
		Str("job_id", job.GetID()).
		Int64("chain_id", job.GetChainID()).
		Str("collection_id", job.CollectionID).
		Str("token_id", job.TokenID).
		Msg("Publishing NFT mint job to chain-specific queue")

	return r.client.PublishMessage(ctx, queueName, job)
}

// PublishNFTBurn publishes an NFT burn job to the appropriate chain-specific queue
func (r *RabbitMQProcessor) PublishNFTBurn(ctx context.Context, job NFTBurnJob) error {
	queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())

	log.Debug().
		Str("queue", queueName).
		Str("job_id", job.GetID()).
		Int64("chain_id", job.GetChainID()).
		Str("collection_id", job.CollectionID).
		Str("token_id", job.TokenID).
		Msg("Publishing NFT burn job to chain-specific queue")

	return r.client.PublishMessage(ctx, queueName, job)
}

// PublishNFTTransfer publishes an NFT transfer job to the appropriate chain-specific queue
func (r *RabbitMQProcessor) PublishNFTTransfer(ctx context.Context, job NFTTransferJob) error {
	queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())

	log.Debug().
		Str("queue", queueName).
		Str("job_id", job.GetID()).
		Int64("chain_id", job.GetChainID()).
		Str("collection_id", job.CollectionID).
		Str("token_id", job.TokenID).
		Msg("Publishing NFT transfer job to chain-specific queue")

	return r.client.PublishMessage(ctx, queueName, job)
}

// StartBatchConsumer starts the consumer manager which handles per-chain consumers
func (r *RabbitMQProcessor) StartBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Starting RabbitMQ batch consumer with per-chain queue management")

	if r.consumerManager == nil {
		log.Error().Msg("Consumer manager not initialized - dependencies not set")
		return ErrConsumerManagerNotInitialized
	}

	// Start the consumer manager, which will handle all per-chain consumers
	return r.consumerManager.Start(ctx)
}

// StopBatchConsumer stops the consumer manager and all per-chain consumers
func (r *RabbitMQProcessor) StopBatchConsumer(ctx context.Context) error {
	log.Info().Msg("Graceful shutdown initiated for RabbitMQ batch consumer")

	// Signal stop
	close(r.stopChan)

	// Stop the consumer manager
	if r.consumerManager != nil {
		return r.consumerManager.Stop(ctx)
	}

	return nil
}

// GetQueueStats returns queue statistics from all per-chain consumers
func (r *RabbitMQProcessor) GetQueueStats() map[string]Stats {
	if r.consumerManager != nil {
		return r.consumerManager.GetQueueStats()
	}
	return make(map[string]Stats)
}

// IsHealthy checks if the RabbitMQ client and consumer manager are healthy
func (r *RabbitMQProcessor) IsHealthy() bool {
	clientHealthy := r.client != nil && r.client.IsHealthy()
	consumerHealthy := r.consumerManager == nil || r.consumerManager.IsHealthy()

	return clientHealthy && consumerHealthy
}

// Close closes the processor and all its consumers
func (r *RabbitMQProcessor) Close() error {
	log.Info().Msg("Closing RabbitMQ processor")

	var lastErr error

	// Stop consumer manager
	if r.consumerManager != nil {
		if err := r.consumerManager.Stop(context.Background()); err != nil {
			log.Error().Err(err).Msg("Failed to stop consumer manager")
			lastErr = err
		}
	}

	// Close client
	if r.client != nil {
		if err := r.client.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close RabbitMQ client")
			lastErr = err
		}
	}

	return lastErr
}
