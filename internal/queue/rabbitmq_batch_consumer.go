package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"

	"github.com/hzbay/chain-bridge/internal/blockchain"
)

// MessageWrapper wraps amqp.Delivery with parsed job data
type MessageWrapper struct {
	Delivery   amqp.Delivery
	Job        BatchJob
	ReceivedAt time.Time
}

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
	workerCount int,
) *RabbitMQBatchConsumer {
	return &RabbitMQBatchConsumer{
		client:              client,
		db:                  db,
		batchOptimizer:      optimizer,
		cpopCallers:         cpopCallers,
		confirmationWatcher: confirmationWatcher,
		batchProcessor:      batchProcessor,

		// Chain-specific configuration
		chainID:    chainID,
		queueNames: queueNames,

		pendingMessages: make(map[BatchGroup][]*MessageWrapper),
		stopChan:        make(chan struct{}),

		// Configuration optimized for single-chain processing
		maxBatchSize:  30,               // Maximum batch size
		maxWaitTime:   15 * time.Second, // Maximum wait time
		consumerCount: workerCount,      // Configurable workers per chain
	}
}

// RabbitMQBatchConsumer now supports chain-specific processing
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

	// Configuration
	maxBatchSize  int
	maxWaitTime   time.Duration
	consumerCount int

	// Metrics
	processedCount int64
	errorCount     int64
	startedAt      time.Time
}

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

// handleChainMessage handles a single message for this chain
func (c *RabbitMQBatchConsumer) handleChainMessage(delivery amqp.Delivery) {
	// Parse message to BatchJob
	job, err := c.parseMessage(delivery.Body)
	if err != nil {
		log.Error().
			Int64("chain_id", c.chainID).
			Err(err).
			Msg("Failed to parse chain message")
		c.errorCount++
		delivery.Nack(false, false) // Don't requeue invalid messages
		return
	}

	// Validate that the job is for this chain
	if job.GetChainID() != c.chainID {
		log.Error().
			Int64("expected_chain_id", c.chainID).
			Int64("actual_chain_id", job.GetChainID()).
			Str("job_id", job.GetID()).
			Msg("Job chain ID mismatch - this should not happen")
		c.errorCount++
		delivery.Nack(false, false)
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

// parseMessage parses a message body to BatchJob interface
func (c *RabbitMQBatchConsumer) parseMessage(body []byte) (BatchJob, error) {
	var rawMessage map[string]interface{}
	if err := json.Unmarshal(body, &rawMessage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	// Determine job type from message content
	jobType, ok := rawMessage["job_type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid job_type field")
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

	case JobTypeNotification:
		var job NotificationJob
		if err := json.Unmarshal(body, &job); err != nil {
			return nil, fmt.Errorf("failed to unmarshal notification job: %w", err)
		}
		return job, nil

	default:
		return nil, fmt.Errorf("unsupported job type: %s", jobType)
	}
}

// GetQueueStats returns queue statistics for this chain
func (c *RabbitMQBatchConsumer) GetQueueStats() map[string]QueueStats {
	stats := make(map[string]QueueStats)

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

		stats[queueName] = QueueStats{
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

	// Process all remaining batches
	for group, messages := range allMessages {
		if len(messages) > 0 {
			log.Info().
				Int64("chain_id", c.chainID).
				Int("token_id", group.TokenID).
				Str("job_type", string(group.JobType)).
				Int("remaining_messages", len(messages)).
				Msg("Processing remaining messages batch")

			c.processBatch(ctx, messages, group)
			c.processedCount += int64(len(messages))
		}
	}

	log.Info().
		Int64("chain_id", c.chainID).
		Msg("Finished processing remaining messages")
}

// ackAllMessages acknowledges all messages in a batch
func (c *RabbitMQBatchConsumer) ackAllMessages(messages []*MessageWrapper) {
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
	for _, msgWrapper := range messages {
		if err := msgWrapper.Delivery.Nack(false, true); err != nil { // Requeue for retry
			log.Error().
				Int64("chain_id", c.chainID).
				Err(err).
				Msg("Failed to NACK message")
		}
	}
}
