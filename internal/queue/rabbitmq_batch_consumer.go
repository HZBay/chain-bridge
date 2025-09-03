// Package queue provides RabbitMQ batch consumer implementation for chain-specific message processing.
// This file is organized into logical sections for better maintainability:
//
// 1. TYPES AND STRUCTS - Data structures and type definitions
// 2. CONSTRUCTOR AND CONFIGURATION - Creation and configuration management
// 3. LIFECYCLE MANAGEMENT - Start/Stop operations
// 4. CONSUMER WORKERS - Message consumption from queues
// 5. MESSAGE HANDLING - Individual message processing and parsing
// 6. MESSAGE AGGREGATION - Batching and aggregation logic
// 7. MESSAGE ACKNOWLEDGMENT - ACK/NACK operations
// 8. STATUS AND MONITORING - Health checks and statistics
// 9. SHUTDOWN PROCESSING - Graceful shutdown handling
// 10. SHUTDOWN BATCH PROCESSING - Special processing during shutdown
package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"

	"github.com/hzbay/chain-bridge/internal/blockchain"
	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

// ============================================================================
// TYPES AND STRUCTS
// ============================================================================

// MessageWrapper wraps amqp.Delivery with parsed job data
type MessageWrapper struct {
	Delivery   amqp.Delivery
	Job        BatchJob
	ReceivedAt time.Time
}

// RabbitMQBatchConsumer supports chain-specific processing
type RabbitMQBatchConsumer struct {
	client              *RabbitMQClient
	db                  *sql.DB
	batchOptimizer      *BatchOptimizer
	cpopCallers         map[int64]*blockchain.CPOPBatchCaller
	confirmationWatcher *TxConfirmationWatcher
	batchProcessor      BatchProcessor // Added for notification publishing

	// Chain-specific fields
	chainID    int64
	queueNames []string

	// Message aggregation
	pendingMessages map[BatchGroup][]*MessageWrapper
	messagesMutex   sync.RWMutex

	// Control channels
	stopChan chan struct{}
	workerWg sync.WaitGroup

	// Batch Configuration (loaded from database)
	maxBatchSize     int           // Working batch size for processing
	minBatchSize     int           // Minimum batch size allowed
	optimalBatchSize int           // Optimal batch size from database
	maxWaitTime      time.Duration // Maximum wait time before processing batch
	consumerCount    int           // Number of consumer workers for this chain

	// Metrics
	processedCount int64
	errorCount     int64
	startedAt      time.Time
}

// ============================================================================
// CONSTRUCTOR AND CONFIGURATION
// ============================================================================

// NewRabbitMQBatchConsumerForChain creates a new batch consumer specifically for a single chain
func NewRabbitMQBatchConsumerForChain(
	client *RabbitMQClient,
	db *sql.DB,
	optimizer *BatchOptimizer,
	cpopCallers map[int64]*blockchain.CPOPBatchCaller,
	confirmationWatcher *TxConfirmationWatcher,
	batchProcessor BatchProcessor,
	chainID int64,
	queueNames []string,
	workerCount int, // This will be used as fallback if database value is not available
) *RabbitMQBatchConsumer {
	// Create consumer first
	consumer := &RabbitMQBatchConsumer{
		client:              client,
		db:                  db,
		batchOptimizer:      optimizer,
		cpopCallers:         cpopCallers,
		confirmationWatcher: confirmationWatcher,
		batchProcessor:      batchProcessor,
		chainID:             chainID,
		queueNames:          queueNames,
		pendingMessages:     make(map[BatchGroup][]*MessageWrapper),
		stopChan:            make(chan struct{}),
	}

	// Load batch configuration from database using member function
	err := consumer.loadChainBatchConfig()
	if err != nil {
		log.Warn().Err(err).
			Int64("chain_id", chainID).
			Msg("Failed to load batch config from database, using defaults")
		// Set fallback defaults if database load fails
		consumer.maxBatchSize = 25
		consumer.minBatchSize = 10
		consumer.optimalBatchSize = 25
		consumer.maxWaitTime = 15 * time.Second
		consumer.consumerCount = max(1, workerCount) // Use provided workerCount as fallback
	}

	// Ensure consumer count is at least 1
	if consumer.consumerCount <= 0 {
		consumer.consumerCount = max(1, workerCount) // Use provided workerCount as fallback
	}

	// Ensure batch size is reasonable
	if consumer.maxBatchSize <= 0 {
		consumer.maxBatchSize = 25 // Default fallback
	}

	log.Info().
		Int64("chain_id", chainID).
		Int("max_batch_size", consumer.maxBatchSize).
		Int("min_batch_size", consumer.minBatchSize).
		Int("optimal_batch_size", consumer.optimalBatchSize).
		Dur("max_wait_time", consumer.maxWaitTime).
		Int("consumer_count", consumer.consumerCount).
		Msg("Creating batch consumer with database configuration")

	return consumer
}

// loadChainBatchConfig loads batch configuration from database for this consumer's chain
func (c *RabbitMQBatchConsumer) loadChainBatchConfig() error {
	// Query the chain configuration from database
	chain, err := models.Chains(
		qm.Where("chain_id = ?", c.chainID),
	).One(context.Background(), c.db)
	if err != nil {
		return fmt.Errorf("failed to load chain %d configuration: %w", c.chainID, err)
	}

	// Set default values first
	c.maxBatchSize = 25              // Default working batch size
	c.minBatchSize = 10              // Default minimum batch size
	c.optimalBatchSize = 25          // Default optimal batch size
	c.maxWaitTime = 15 * time.Second // Default 15 seconds
	c.consumerCount = 1              // Default 1 consumer

	// Override with database values if they exist
	if chain.OptimalBatchSize.Valid {
		c.optimalBatchSize = chain.OptimalBatchSize.Int
		c.maxBatchSize = chain.OptimalBatchSize.Int // Use optimal as working size
	}
	if chain.MinBatchSize.Valid {
		c.minBatchSize = chain.MinBatchSize.Int
	}
	if chain.MaxWaitTimeMS.Valid {
		c.maxWaitTime = time.Duration(chain.MaxWaitTimeMS.Int) * time.Millisecond
	}
	if chain.ConsumerCount.Valid {
		c.consumerCount = chain.ConsumerCount.Int
	}

	log.Debug().
		Int64("chain_id", c.chainID).
		Int("max_batch_size", c.maxBatchSize).
		Int("min_batch_size", c.minBatchSize).
		Int("optimal_batch_size", c.optimalBatchSize).
		Dur("max_wait_time", c.maxWaitTime).
		Int("consumer_count", c.consumerCount).
		Msg("Loaded batch configuration from database")

	return nil
}

// ReloadConfig reloads batch configuration from database
func (c *RabbitMQBatchConsumer) ReloadConfig() error {
	// Store old values for logging
	oldMaxBatch := c.maxBatchSize
	oldMinBatch := c.minBatchSize
	oldOptimalBatch := c.optimalBatchSize
	oldWaitTime := c.maxWaitTime
	oldConsumerCount := c.consumerCount

	// Reload configuration from database
	err := c.loadChainBatchConfig()
	if err != nil {
		log.Warn().Err(err).
			Int64("chain_id", c.chainID).
			Msg("Failed to reload batch config from database")
		return err
	}

	log.Info().
		Int64("chain_id", c.chainID).
		Int("old_max_batch", oldMaxBatch).
		Int("new_max_batch", c.maxBatchSize).
		Int("old_min_batch", oldMinBatch).
		Int("new_min_batch", c.minBatchSize).
		Int("old_optimal_batch", oldOptimalBatch).
		Int("new_optimal_batch", c.optimalBatchSize).
		Dur("old_wait_time", oldWaitTime).
		Dur("new_wait_time", c.maxWaitTime).
		Int("old_consumer_count", oldConsumerCount).
		Int("new_consumer_count", c.consumerCount).
		Msg("Batch configuration reloaded from database")

	return nil
}

// GetBatchConfig returns current batch configuration as a map for external access
func (c *RabbitMQBatchConsumer) GetBatchConfig() map[string]interface{} {
	return map[string]interface{}{
		"max_batch_size":     c.maxBatchSize,
		"min_batch_size":     c.minBatchSize,
		"optimal_batch_size": c.optimalBatchSize,
		"max_wait_time_ms":   int(c.maxWaitTime.Milliseconds()),
		"consumer_count":     c.consumerCount,
		"chain_id":           c.chainID,
	}
}

// ApplyOptimizerRecommendations applies recommendations from the batch optimizer
func (c *RabbitMQBatchConsumer) ApplyOptimizerRecommendations() {
	if c.batchOptimizer == nil {
		return
	}

	// Get optimization recommendations for all tokens on this chain
	// Using token_id = 1 as a representative token for chain-level optimization
	recommendation := c.batchOptimizer.GetOptimizationRecommendation(c.chainID, 1)

	if recommendation == nil {
		return
	}

	// Apply batch size recommendation if it's within database constraints
	if recommendation.RecommendedSize != c.maxBatchSize &&
		recommendation.RecommendedSize >= c.minBatchSize &&
		recommendation.ExpectedImprovement > 5.0 { // Only apply if improvement > 5%

		old := c.maxBatchSize
		c.maxBatchSize = recommendation.RecommendedSize

		log.Info().
			Int64("chain_id", c.chainID).
			Int("old_batch_size", old).
			Int("new_batch_size", c.maxBatchSize).
			Float64("expected_improvement", recommendation.ExpectedImprovement).
			Float64("confidence", recommendation.Confidence).
			Str("reason", recommendation.Reason).
			Msg("Applied optimizer batch size recommendation")
	}

	// Apply wait time recommendation if significantly different
	if recommendation.RecommendedWaitTime > 0 {
		newWaitTime := time.Duration(recommendation.RecommendedWaitTime) * time.Millisecond
		if abs(int(newWaitTime.Milliseconds())-int(c.maxWaitTime.Milliseconds())) > 2000 { // >2s difference
			old := c.maxWaitTime
			c.maxWaitTime = newWaitTime

			log.Info().
				Int64("chain_id", c.chainID).
				Dur("old_wait_time", old).
				Dur("new_wait_time", c.maxWaitTime).
				Str("chain_characteristics", recommendation.ChainCharacteristics).
				Msg("Applied optimizer wait time recommendation")
		}
	}

	// Log consumer count recommendation (cannot dynamically change running consumers)
	if recommendation.RecommendedConsumerCount != c.consumerCount {
		log.Info().
			Int64("chain_id", c.chainID).
			Int("current_consumers", c.consumerCount).
			Int("recommended_consumers", recommendation.RecommendedConsumerCount).
			Str("chain_characteristics", recommendation.ChainCharacteristics).
			Msg("Consumer count recommendation (requires restart to apply)")
	}
}

// abs returns absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// runOptimizationWorker periodically applies optimizer recommendations
func (c *RabbitMQBatchConsumer) runOptimizationWorker(ctx context.Context) {
	defer c.workerWg.Done()

	// Apply optimization every 10 minutes
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	log.Info().
		Int64("chain_id", c.chainID).
		Msg("Starting optimization worker for chain")

	// Apply initial optimization after 2 minutes (allow time for data collection)
	initialDelay := time.After(2 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case <-initialDelay:
			c.ApplyOptimizerRecommendations()
			initialDelay = nil // Only fire once
		case <-ticker.C:
			c.ApplyOptimizerRecommendations()
		}
	}
}

// ============================================================================
// LIFECYCLE MANAGEMENT
// ============================================================================

// Start starts the chain-specific batch consumer
func (c *RabbitMQBatchConsumer) Start(ctx context.Context) error {
	log.Info().
		Int64("chain_id", c.chainID).
		Strs("queues", c.queueNames).
		Int("workers", c.consumerCount).
		Msg("Starting chain-specific batch consumer")

	c.startedAt = time.Now()

	// Start message aggregator
	c.workerWg.Add(1)
	go c.runMessageAggregator(ctx)

	// Start optimization application worker if optimizer is available
	if c.batchOptimizer != nil {
		c.workerWg.Add(1)
		go c.runOptimizationWorker(ctx)
	}

	// Start consumer workers for this chain's queues
	for i := 0; i < c.consumerCount; i++ {
		c.workerWg.Add(1)
		go c.runChainConsumerWorker(ctx, i)
	}

	log.Info().
		Int64("chain_id", c.chainID).
		Int("consumer_workers", c.consumerCount).
		Int("max_batch_size", c.maxBatchSize).
		Dur("max_wait_time", c.maxWaitTime).
		Strs("target_queues", c.queueNames).
		Msg("Chain-specific batch consumer started")

	return nil
}

// Stop stops the chain-specific batch consumer gracefully
func (c *RabbitMQBatchConsumer) Stop(ctx context.Context) error {
	log.Info().Int64("chain_id", c.chainID).Msg("Stopping chain-specific batch consumer")

	// Signal stop
	close(c.stopChan)

	// Process remaining messages
	c.processRemainingMessages(ctx)

	// Wait for workers to finish
	done := make(chan struct{})
	go func() {
		c.workerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info().
			Int64("chain_id", c.chainID).
			Int64("processed", c.processedCount).
			Int64("errors", c.errorCount).
			Msg("Chain-specific batch consumer stopped gracefully")
	case <-time.After(30 * time.Second):
		log.Warn().
			Int64("chain_id", c.chainID).
			Msg("Chain-specific batch consumer shutdown timeout")
	}

	return nil
}

// ============================================================================
// CONSUMER WORKERS
// ============================================================================

// runChainConsumerWorker runs a consumer worker that only processes this chain's queues
func (c *RabbitMQBatchConsumer) runChainConsumerWorker(ctx context.Context, workerID int) {
	defer c.workerWg.Done()

	log.Info().
		Int64("chain_id", c.chainID).
		Int("worker_id", workerID).
		Msg("Starting chain consumer worker")

	// Each worker consumes from all queues for this chain
	for _, queueName := range c.queueNames {
		c.workerWg.Add(1)
		go c.consumeFromChainQueue(ctx, queueName, workerID)
	}

	// Wait for stop signal
	<-c.stopChan
	log.Info().
		Int64("chain_id", c.chainID).
		Int("worker_id", workerID).
		Msg("Chain consumer worker stopped")
}

// consumeFromChainQueue consumes messages from a specific queue for this chain
func (c *RabbitMQBatchConsumer) consumeFromChainQueue(ctx context.Context, queueName string, workerID int) {
	defer c.workerWg.Done()

	log.Info().
		Int64("chain_id", c.chainID).
		Str("queue", queueName).
		Int("worker_id", workerID).
		Msg("Starting to consume from chain queue")

	ch, err := c.client.connection.Channel()
	if err != nil {
		log.Error().
			Int64("chain_id", c.chainID).
			Str("queue", queueName).
			Err(err).
			Msg("Failed to open channel for chain queue")
		return
	}
	defer ch.Close()

	// Ensure queue is declared before consuming
	_, err = c.client.DeclareQueue(queueName)
	if err != nil {
		log.Error().
			Int64("chain_id", c.chainID).
			Str("queue", queueName).
			Err(err).
			Msg("Failed to declare queue before consuming")
		return
	}

	// Set QoS to limit prefetch count
	err = ch.Qos(10, 0, false)
	if err != nil {
		log.Error().
			Int64("chain_id", c.chainID).
			Str("queue", queueName).
			Err(err).
			Msg("Failed to set QoS for chain queue")
		return
	}

	// Start consuming
	msgs, err := ch.Consume(
		queueName, // queue
		fmt.Sprintf("chain_%d_worker_%d", c.chainID, workerID), // consumer
		false, // auto-ack = false (manual ACK)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		log.Error().
			Int64("chain_id", c.chainID).
			Str("queue", queueName).
			Err(err).
			Msg("Failed to start consuming from chain queue")
		return
	}

	log.Info().
		Int64("chain_id", c.chainID).
		Str("queue", queueName).
		Int("worker_id", workerID).
		Msg("Successfully started consuming from chain queue")

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case msg, ok := <-msgs:
			if !ok {
				log.Warn().
					Int64("chain_id", c.chainID).
					Str("queue", queueName).
					Msg("Chain queue message channel closed")
				return
			}
			c.handleChainMessage(msg)
		}
	}
}

// ============================================================================
// MESSAGE HANDLING
// ============================================================================

// handleChainMessage handles a single message for this chain
func (c *RabbitMQBatchConsumer) handleChainMessage(delivery amqp.Delivery) {
	// Parse message to BatchJob
	job, err := c.parseMessage(delivery.Body)
	if err != nil {
		log.Error().
			Int64("chain_id", c.chainID).
			Str("message_body", string(delivery.Body)).
			Err(err).
			Msg("Failed to parse chain message")
		c.errorCount++
		c.safeNackMessage(delivery, false, false) // Don't requeue invalid messages
		return
	}

	// Validate that the job is for this chain
	if job.GetChainID() != c.chainID {
		log.Error().
			Int64("expected_chain_id", c.chainID).
			Int64("actual_chain_id", job.GetChainID()).
			Str("job_id", job.GetID()).
			Str("job_type", string(job.GetJobType())).
			Msg("Job chain ID mismatch - this should not happen")
		c.errorCount++
		c.safeNackMessage(delivery, false, false)
		return
	}

	// Create message wrapper
	msgWrapper := &MessageWrapper{
		Delivery:   delivery,
		Job:        job,
		ReceivedAt: time.Now(),
	}

	// Add to aggregation buffer
	c.addToAggregationBuffer(msgWrapper)
}

// parseMessage parses a message body to BatchJob interface
func (c *RabbitMQBatchConsumer) parseMessage(body []byte) (BatchJob, error) {
	var rawMessage map[string]interface{}
	if err := json.Unmarshal(body, &rawMessage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	// Log raw message for debugging
	log.Debug().
		Int64("chain_id", c.chainID).
		Interface("raw_message", rawMessage).
		Msg("Parsing message")

	// Determine job type from message content
	jobType, ok := rawMessage["job_type"].(string)
	if !ok {
		// Try alternative field names for backward compatibility
		if jType, exists := rawMessage["type"]; exists {
			if jTypeStr, isStr := jType.(string); isStr {
				jobType = jTypeStr
				ok = true
			}
		}
	}

	if !ok {
		return nil, fmt.Errorf("missing or invalid job_type field. Available fields: %v", getMapKeys(rawMessage))
	}

	switch JobType(jobType) {
	case JobTypeTransfer:
		var job TransferJob
		if err := json.Unmarshal(body, &job); err != nil {
			return nil, fmt.Errorf("failed to unmarshal transfer job: %w", err)
		}
		return job, nil

	case JobTypeAssetAdjust:
		var job AssetAdjustJob
		if err := json.Unmarshal(body, &job); err != nil {
			return nil, fmt.Errorf("failed to unmarshal asset adjust job: %w", err)
		}
		return job, nil

	// NFT batch operations
	case JobTypeNFTMint:
		var job NFTMintJob
		if err := json.Unmarshal(body, &job); err != nil {
			return nil, fmt.Errorf("failed to unmarshal NFT mint job: %w", err)
		}
		return job, nil

	case JobTypeNFTBurn:
		var job NFTBurnJob
		if err := json.Unmarshal(body, &job); err != nil {
			return nil, fmt.Errorf("failed to unmarshal NFT burn job: %w", err)
		}
		return job, nil

	case JobTypeNFTTransfer:
		var job NFTTransferJob
		if err := json.Unmarshal(body, &job); err != nil {
			return nil, fmt.Errorf("failed to unmarshal NFT transfer job: %w", err)
		}
		return job, nil

	default:
		return nil, fmt.Errorf("unsupported job type for chain consumer: %s", jobType)
	}
}

// getMapKeys returns all keys from a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ============================================================================
// MESSAGE AGGREGATION
// ============================================================================

// addToAggregationBuffer adds message to aggregation buffer (optimized for single chain)
func (c *RabbitMQBatchConsumer) addToAggregationBuffer(msgWrapper *MessageWrapper) {
	group := BatchGroup{
		ChainID: msgWrapper.Job.GetChainID(), // Should always be c.chainID
		TokenID: msgWrapper.Job.GetTokenID(),
		JobType: msgWrapper.Job.GetJobType(),
	}

	c.messagesMutex.Lock()
	defer c.messagesMutex.Unlock()

	c.pendingMessages[group] = append(c.pendingMessages[group], msgWrapper)

	log.Debug().
		Int64("chain_id", c.chainID).
		Int("token_id", group.TokenID).
		Str("job_type", string(group.JobType)).
		Int("group_size", len(c.pendingMessages[group])).
		Msg("Message added to chain aggregation buffer")
}

// runMessageAggregator runs the message aggregation and batch processing loop for this chain
func (c *RabbitMQBatchConsumer) runMessageAggregator(ctx context.Context) {
	defer c.workerWg.Done()

	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	log.Info().
		Int64("chain_id", c.chainID).
		Msg("Starting chain message aggregator")

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.processChainAggregatedMessages(ctx)
		}
	}
}

// processChainAggregatedMessages processes aggregated messages for this chain
func (c *RabbitMQBatchConsumer) processChainAggregatedMessages(ctx context.Context) {
	c.messagesMutex.Lock()
	defer c.messagesMutex.Unlock()

	for group, messages := range c.pendingMessages {
		// Validate that group belongs to this chain
		if group.ChainID != c.chainID {
			log.Error().
				Int64("expected_chain_id", c.chainID).
				Int64("actual_chain_id", group.ChainID).
				Msg("Group chain ID mismatch in aggregated messages")
			continue
		}

		shouldProcess := len(messages) >= c.maxBatchSize
		if !shouldProcess && len(messages) > 0 {
			// Check if oldest message has exceeded wait time
			oldestMessage := messages[0]
			shouldProcess = time.Since(oldestMessage.ReceivedAt) >= c.maxWaitTime
		}

		if shouldProcess && len(messages) > 0 {
			log.Info().
				Int64("chain_id", c.chainID).
				Int("token_id", group.TokenID).
				Str("job_type", string(group.JobType)).
				Int("batch_size", len(messages)).
				Msg("Processing chain batch")

			// Process the batch
			go func(batchGroup BatchGroup, batchMessages []*MessageWrapper) {
				c.processBatch(ctx, batchMessages, batchGroup)
				c.processedCount += int64(len(batchMessages))
			}(group, messages)

			// Clear processed messages
			delete(c.pendingMessages, group)
		}
	}
}

// ============================================================================
// MESSAGE ACKNOWLEDGMENT
// ============================================================================

// ackAllMessages acknowledges all messages in a batch
func (c *RabbitMQBatchConsumer) ackAllMessages(messages []*MessageWrapper) {
	// Check if client is healthy before attempting to ACK
	if c.client == nil || !c.client.IsHealthy() {
		log.Warn().
			Int64("chain_id", c.chainID).
			Int("message_count", len(messages)).
			Msg("Skipping ACK - RabbitMQ client not healthy")
		return
	}

	for _, msgWrapper := range messages {
		if err := msgWrapper.Delivery.Ack(false); err != nil {
			log.Error().
				Int64("chain_id", c.chainID).
				Err(err).
				Msg("Failed to ACK message")
		}
	}
}

// nackAllMessages negatively acknowledges all messages in a batch
func (c *RabbitMQBatchConsumer) nackAllMessages(messages []*MessageWrapper) {
	// Check if client is healthy before attempting to NACK
	if c.client == nil || !c.client.IsHealthy() {
		log.Warn().
			Int64("chain_id", c.chainID).
			Int("message_count", len(messages)).
			Msg("Skipping NACK - RabbitMQ client not healthy")
		return
	}

	for _, msgWrapper := range messages {
		if err := msgWrapper.Delivery.Nack(false, true); err != nil { // Requeue for retry
			log.Error().
				Int64("chain_id", c.chainID).
				Err(err).
				Msg("Failed to NACK message")
		}
	}
}

// safeNackMessage safely negative acknowledges a single message
func (c *RabbitMQBatchConsumer) safeNackMessage(delivery amqp.Delivery, multiple, requeue bool) {
	if c.client == nil || !c.client.IsHealthy() {
		log.Warn().
			Int64("chain_id", c.chainID).
			Msg("Skipping NACK - RabbitMQ client not healthy")
		return
	}

	if err := delivery.Nack(multiple, requeue); err != nil {
		log.Error().
			Int64("chain_id", c.chainID).
			Err(err).
			Msg("Failed to NACK message")
	}
}

// ============================================================================
// STATUS AND MONITORING
// ============================================================================
// GetQueueStats returns queue statistics for this chain
func (c *RabbitMQBatchConsumer) GetQueueStats() map[string]Stats {
	stats := make(map[string]Stats)

	for _, queueName := range c.queueNames {
		messageCount, err := c.client.GetQueueInfo(queueName)
		if err != nil {
			log.Warn().
				Int64("chain_id", c.chainID).
				Str("queue", queueName).
				Err(err).
				Msg("Failed to get queue info")
			continue
		}

		stats[queueName] = Stats{
			QueueName:       queueName,
			PendingCount:    messageCount,
			ProcessingCount: 0,
			CompletedCount:  c.processedCount,
			FailedCount:     c.errorCount,
			AverageLatency:  0,
			LastProcessedAt: time.Now(),
		}
	}

	return stats
}

// IsHealthy checks if the chain consumer is healthy
func (c *RabbitMQBatchConsumer) IsHealthy() bool {
	return c.client != nil && c.client.IsHealthy()
}

// ============================================================================
// SHUTDOWN PROCESSING
// ============================================================================

// processRemainingMessages processes any remaining messages before shutdown
func (c *RabbitMQBatchConsumer) processRemainingMessages(ctx context.Context) {
	log.Info().
		Int64("chain_id", c.chainID).
		Msg("Processing remaining messages before shutdown")

	c.messagesMutex.Lock()
	allMessages := make(map[BatchGroup][]*MessageWrapper)
	for group, messages := range c.pendingMessages {
		if len(messages) > 0 {
			allMessages[group] = make([]*MessageWrapper, len(messages))
			copy(allMessages[group], messages)
		}
	}
	// Clear the buffer
	c.pendingMessages = make(map[BatchGroup][]*MessageWrapper)
	c.messagesMutex.Unlock()

	// Process all remaining batches with shutdown context
	for group, messages := range allMessages {
		if len(messages) > 0 {
			log.Info().
				Int64("chain_id", c.chainID).
				Int("token_id", group.TokenID).
				Str("job_type", string(group.JobType)).
				Int("remaining_messages", len(messages)).
				Msg("Processing remaining messages batch")

			c.processBatchDuringShutdown(ctx, messages, group)
			c.processedCount += int64(len(messages))
		}
	}

	log.Info().
		Int64("chain_id", c.chainID).
		Msg("Finished processing remaining messages")
}

// ============================================================================
// SHUTDOWN BATCH PROCESSING
// ============================================================================

// processBatchDuringShutdown processes a batch during shutdown without NACKing on failure
func (c *RabbitMQBatchConsumer) processBatchDuringShutdown(ctx context.Context, messages []*MessageWrapper, group BatchGroup) {
	log.Info().
		Int64("chain_id", group.ChainID).
		Int("token_id", group.TokenID).
		Str("job_type", string(group.JobType)).
		Int("batch_size", len(messages)).
		Msg("Processing shutdown batch")

	startTime := time.Now()

	// Step 0: Pre-validate user balances and separate valid/invalid operations
	validMessages, invalidMessages, err := c.validateAndSeparateByBalance(ctx, messages)
	if err != nil {
		log.Error().Err(err).Msg("Failed to validate user balances during shutdown")
		// During shutdown, don't NACK - just log the failure
		return
	}

	// Handle invalid messages (insufficient balance) during shutdown
	if len(invalidMessages) > 0 {
		log.Warn().
			Int("invalid_count", len(invalidMessages)).
			Int("valid_count", len(validMessages)).
			Msg("Some operations have insufficient balance during shutdown")

		// Process invalid messages but don't attempt NACK during shutdown
		c.handleInsufficientBalanceMessagesDuringShutdown(ctx, invalidMessages)
	}

	// If no valid messages, exit early
	if len(validMessages) == 0 {
		log.Info().Msg("No valid operations to process during shutdown")
		return
	}

	// Update log to reflect actual processing count
	log.Info().
		Int("original_batch_size", len(messages)).
		Int("valid_operations", len(validMessages)).
		Int("invalid_operations", len(invalidMessages)).
		Msg("Proceeding with valid operations during shutdown")

	// Step 1: Insert/Update transactions to 'batching' status (only valid messages)
	batchID := uuid.New()
	err = c.updateTransactionsToBatching(ctx, validMessages, batchID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update transactions to batching status during shutdown")
		// During shutdown, don't NACK - just log the failure
		return
	}

	// Step 2: Execute blockchain batch operation (only valid messages)
	result, err := c.executeBlockchainBatch(ctx, validMessages, group)
	if err != nil {
		log.Error().Err(err).Msg("Blockchain batch operation failed during shutdown")
		c.handleBatchFailureDuringShutdown(ctx, validMessages, batchID, err)
		return
	}

	// Step 2.5: Update to 'submitted' status after successful blockchain submission
	err = c.updateBatchToSubmitted(ctx, batchID, result)
	if err != nil {
		log.Error().Err(err).
			Str("tx_hash", result.TxHash).
			Msg("Failed to update batch to submitted status during shutdown")
		// NOTE: Blockchain operation succeeded but status update failed
		// The batch will be picked up by the confirmation monitor
	}

	processingTime := time.Since(startTime)

	// During shutdown, we don't wait for confirmations
	// Complete the batch immediately and let the confirmation watcher handle it
	log.Info().
		Str("tx_hash", result.TxHash).
		Msg("Transaction submitted during shutdown, background watcher will handle completion")

	// Complete the batch without waiting for confirmation
	err = c.completeSuccessfulBatch(ctx, messages, group, batchID, result, processingTime)
	if err != nil {
		log.Error().Err(err).
			Str("tx_hash", result.TxHash).
			Msg("Failed to complete successful batch during shutdown")
		// During shutdown, don't NACK - just log the failure
		return
	}

	// During shutdown, we don't ACK/NACK messages to avoid connection errors
	// The messages will be redelivered when the service restarts
	log.Info().
		Str("batch_id", batchID.String()).
		Str("tx_hash", result.TxHash).
		Float64("efficiency", result.Efficiency).
		Dur("processing_time", processingTime).
		Int("messages_processed", len(messages)).
		Msg("Batch processed successfully during shutdown")
}

// handleBatchFailureDuringShutdown handles batch failures during shutdown without NACKing
func (c *RabbitMQBatchConsumer) handleBatchFailureDuringShutdown(ctx context.Context, messages []*MessageWrapper, batchID uuid.UUID, failureErr error) {
	log.Error().Err(failureErr).Str("batch_id", batchID.String()).Msg("Handling batch failure during shutdown")

	if c.db != nil {
		tx, err := c.db.BeginTx(ctx, nil)
		if err != nil {
			log.Error().Err(err).Msg("Failed to begin transaction for batch failure handling during shutdown")
			// During shutdown, don't NACK - just log the failure
			return
		}
		defer func() {
			if err := tx.Rollback(); err != nil {
				log.Debug().Err(err).Msg("Transaction rollback error (expected if committed)")
			}
		}()

		// Update batch status to failed
		batchQuery := `UPDATE batches SET status = 'failed' WHERE batch_id = $1`
		_, err = tx.Exec(batchQuery, batchID.String())
		if err != nil {
			log.Error().Err(err).Msg("Failed to update batch to failed status during shutdown")
		}

		// Update transactions to failed status
		jobIDs := make([]string, len(messages))
		for i, msg := range messages {
			jobIDs[i] = msg.Job.GetID()
		}

		txQuery := `UPDATE transactions SET status = 'failed' WHERE tx_id = ANY($1)`
		_, err = tx.Exec(txQuery, pq.Array(jobIDs))
		if err != nil {
			log.Error().Err(err).Msg("Failed to update failed transaction statuses during shutdown")
		} else {
			// Send transaction status notifications for failed transactions
			for _, msg := range messages {
				var userID string
				switch job := msg.Job.(type) {
				case TransferJob:
					userID = job.FromUserID // Notify sender about failed transaction
				case AssetAdjustJob:
					userID = job.UserID
				}

				if userID != "" {
					if txID, err := uuid.Parse(msg.Job.GetID()); err == nil {
						extraData := map[string]interface{}{
							"failure_reason": failureErr.Error(),
							"batch_id":       batchID.String(),
							"chain_id":       c.chainID,
						}
						c.sendTransactionStatusNotification(ctx, txID, "failed", userID, extraData)
					}
				}
			}
		}

		// Unfreeze balances for failed operations
		jobs := make([]BatchJob, len(messages))
		for i, msg := range messages {
			jobs[i] = msg.Job
		}

		for _, job := range jobs {
			err = c.unfreezeUserBalance(tx, job)
			if err != nil {
				log.Error().Err(err).Msg("Failed to unfreeze user balance during shutdown")
			}
		}

		if err = tx.Commit(); err != nil {
			log.Error().Err(err).Msg("Failed to commit batch failure handling during shutdown")
		} else {
			// Send batch status notification after successful commit
			c.sendBatchStatusNotification(ctx, batchID, "failed", map[string]interface{}{
				"failure_reason":    failureErr.Error(),
				"transaction_count": len(messages),
			})

			log.Info().Str("batch_id", batchID.String()).Msg("Successfully handled batch failure during shutdown")
		}
	}

	// During shutdown, don't NACK messages to avoid connection errors
	log.Info().Str("batch_id", batchID.String()).Msg("Batch failed during shutdown - messages will be redelivered on restart")
}

// handleInsufficientBalanceMessagesDuringShutdown handles insufficient balance messages during shutdown
func (c *RabbitMQBatchConsumer) handleInsufficientBalanceMessagesDuringShutdown(ctx context.Context, messages []*MessageWrapper) {
	// Similar to handleInsufficientBalanceMessages but without NACK
	log.Info().Int("count", len(messages)).Msg("Handling insufficient balance messages during shutdown")

	// Process notifications but don't ACK/NACK
	for _, msg := range messages {
		var userID string
		switch job := msg.Job.(type) {
		case TransferJob:
			userID = job.FromUserID
		case AssetAdjustJob:
			userID = job.UserID
		}

		if userID != "" {
			if txID, err := uuid.Parse(msg.Job.GetID()); err == nil {
				extraData := map[string]interface{}{
					"failure_reason": "insufficient balance",
					"chain_id":       c.chainID,
				}
				c.sendTransactionStatusNotification(ctx, txID, "failed", userID, extraData)
			}
		}
	}

	log.Info().Int("count", len(messages)).Msg("Processed insufficient balance messages during shutdown")
}
