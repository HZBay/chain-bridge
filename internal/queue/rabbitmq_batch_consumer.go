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

// RabbitMQBatchConsumer handles batch processing of RabbitMQ messages
type RabbitMQBatchConsumer struct {
	client         *RabbitMQClient
	db             *sql.DB
	batchOptimizer *BatchOptimizer
	cpopCallers    map[int64]*blockchain.CPOPBatchCaller

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
}

// MessageWrapper wraps amqp.Delivery with parsed job data
type MessageWrapper struct {
	Delivery   amqp.Delivery
	Job        BatchJob
	ReceivedAt time.Time
}

// Note: BatchGroup is defined in hybrid_processor.go

// NewRabbitMQBatchConsumer creates a new batch consumer
func NewRabbitMQBatchConsumer(
	client *RabbitMQClient,
	db *sql.DB,
	optimizer *BatchOptimizer,
	cpopCallers map[int64]*blockchain.CPOPBatchCaller,
) *RabbitMQBatchConsumer {
	return &RabbitMQBatchConsumer{
		client:          client,
		db:              db,
		batchOptimizer:  optimizer,
		cpopCallers:     cpopCallers,
		pendingMessages: make(map[BatchGroup][]*MessageWrapper),
		stopChan:        make(chan struct{}),
		maxBatchSize:    30,               // Maximum batch size
		maxWaitTime:     15 * time.Second, // Maximum wait time
		consumerCount:   3,                // Number of consumer workers
	}
}

// Start starts the batch consumer
func (c *RabbitMQBatchConsumer) Start(ctx context.Context) error {
	log.Info().Msg("Starting RabbitMQ batch consumer")

	// Start message aggregator
	c.workerWg.Add(1)
	go c.runMessageAggregator(ctx)

	// Start consumer workers
	for i := 0; i < c.consumerCount; i++ {
		c.workerWg.Add(1)
		go c.runConsumerWorker(ctx, i)
	}

	log.Info().
		Int("consumer_workers", c.consumerCount).
		Int("max_batch_size", c.maxBatchSize).
		Dur("max_wait_time", c.maxWaitTime).
		Msg("RabbitMQ batch consumer started")

	return nil
}

// Stop stops the batch consumer gracefully
func (c *RabbitMQBatchConsumer) Stop(ctx context.Context) error {
	log.Info().Msg("Stopping RabbitMQ batch consumer")

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
		log.Info().Msg("RabbitMQ batch consumer stopped gracefully")
	case <-time.After(30 * time.Second):
		log.Warn().Msg("RabbitMQ batch consumer shutdown timeout")
	}

	return nil
}

// runConsumerWorker runs a single consumer worker
func (c *RabbitMQBatchConsumer) runConsumerWorker(ctx context.Context, workerID int) {
	defer c.workerWg.Done()

	log.Info().Int("worker_id", workerID).Msg("Starting consumer worker")

	// Setup RabbitMQ consumer
	queueNames := c.getQueueNames()

	for _, queueName := range queueNames {
		go c.consumeFromQueue(ctx, queueName, workerID)
	}

	// Wait for stop signal
	<-c.stopChan
	log.Info().Int("worker_id", workerID).Msg("Consumer worker stopped")
}

// consumeFromQueue consumes messages from a specific queue
func (c *RabbitMQBatchConsumer) consumeFromQueue(ctx context.Context, queueName string, workerID int) {
	ch, err := c.client.connection.Channel()
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("Failed to open channel")
		return
	}
	defer ch.Close()

	// Set QoS to limit prefetch count
	err = ch.Qos(10, 0, false)
	if err != nil {
		log.Error().Err(err).Msg("Failed to set QoS")
		return
	}

	// Start consuming
	msgs, err := ch.Consume(
		queueName,                          // queue
		fmt.Sprintf("worker-%d", workerID), // consumer
		false,                              // auto-ack = false (manual ACK)
		false,                              // exclusive
		false,                              // no-local
		false,                              // no-wait
		nil,                                // args
	)
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("Failed to start consuming")
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			c.handleMessage(msg)
		}
	}
}

// handleMessage handles a single message
func (c *RabbitMQBatchConsumer) handleMessage(delivery amqp.Delivery) {
	// Parse message to BatchJob
	job, err := c.parseMessage(delivery.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse message")
		delivery.Nack(false, false) // Don't requeue invalid messages
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

// addToAggregationBuffer adds message to aggregation buffer
func (c *RabbitMQBatchConsumer) addToAggregationBuffer(msgWrapper *MessageWrapper) {
	group := BatchGroup{
		ChainID: msgWrapper.Job.GetChainID(),
		TokenID: msgWrapper.Job.GetTokenID(),
		JobType: msgWrapper.Job.GetJobType(),
	}

	c.messagesMutex.Lock()
	defer c.messagesMutex.Unlock()

	c.pendingMessages[group] = append(c.pendingMessages[group], msgWrapper)

	log.Debug().
		Int64("chain_id", group.ChainID).
		Int("token_id", group.TokenID).
		Str("job_type", string(group.JobType)).
		Int("group_size", len(c.pendingMessages[group])).
		Msg("Message added to aggregation buffer")
}

// runMessageAggregator runs the message aggregation and batch processing loop
func (c *RabbitMQBatchConsumer) runMessageAggregator(ctx context.Context) {
	defer c.workerWg.Done()

	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	log.Info().Msg("Starting message aggregator")

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.processAggregatedMessages(ctx)
		}
	}
}

// processAggregatedMessages processes aggregated messages for batch processing
func (c *RabbitMQBatchConsumer) processAggregatedMessages(ctx context.Context) {
	c.messagesMutex.Lock()
	defer c.messagesMutex.Unlock()

	for group, messages := range c.pendingMessages {
		if len(messages) == 0 {
			continue
		}

		// Get optimal batch size for this group
		optimalSize := c.maxBatchSize
		if c.batchOptimizer != nil {
			optimalSize = c.batchOptimizer.GetOptimalBatchSize(group.ChainID, group.TokenID)
		}

		// Check if we should process this batch
		shouldProcess := len(messages) >= optimalSize || c.hasOldMessages(messages, c.maxWaitTime)

		if shouldProcess {
			// Take messages for processing
			batchSize := minBatch(len(messages), optimalSize)
			batchMessages := make([]*MessageWrapper, batchSize)
			copy(batchMessages, messages[:batchSize])

			// Remove processed messages from buffer
			c.pendingMessages[group] = messages[batchSize:]
			if len(c.pendingMessages[group]) == 0 {
				delete(c.pendingMessages, group)
			}

			// Process batch asynchronously
			go c.processBatch(ctx, batchMessages, group)
		}
	}
}

// hasOldMessages checks if any message is older than maxAge
func (c *RabbitMQBatchConsumer) hasOldMessages(messages []*MessageWrapper, maxAge time.Duration) bool {
	cutoff := time.Now().Add(-maxAge)
	for _, msg := range messages {
		if msg.ReceivedAt.Before(cutoff) {
			return true
		}
	}
	return false
}

// parseMessage parses amqp message body to BatchJob
func (c *RabbitMQBatchConsumer) parseMessage(body []byte) (BatchJob, error) {
	var rawMsg map[string]interface{}
	if err := json.Unmarshal(body, &rawMsg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	// Determine job type
	jobType, ok := rawMsg["job_type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid job_type")
	}

	switch JobType(jobType) {
	case JobTypeAssetAdjust:
		var job AssetAdjustJob
		if err := json.Unmarshal(body, &job); err != nil {
			return nil, fmt.Errorf("failed to unmarshal AssetAdjustJob: %w", err)
		}
		return job, nil

	case JobTypeTransfer:
		var job TransferJob
		if err := json.Unmarshal(body, &job); err != nil {
			return nil, fmt.Errorf("failed to unmarshal TransferJob: %w", err)
		}
		return job, nil

	default:
		return nil, fmt.Errorf("unsupported job type: %s", jobType)
	}
}

// getQueueNames returns list of queue names to consume from
func (c *RabbitMQBatchConsumer) getQueueNames() []string {
	return []string{
		"transfer_jobs",
		"asset_adjust_jobs",
		"notification_jobs",
	}
}

// processRemainingMessages processes any remaining messages before shutdown
func (c *RabbitMQBatchConsumer) processRemainingMessages(ctx context.Context) {
	log.Info().Msg("Processing remaining messages before shutdown")

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

	// Process all remaining messages
	var wg sync.WaitGroup
	for group, messages := range allMessages {
		if len(messages) > 0 {
			wg.Add(1)
			go func(g BatchGroup, msgs []*MessageWrapper) {
				defer wg.Done()
				c.processBatch(ctx, msgs, g)
			}(group, messages)
		}
	}

	// Wait for all remaining batches to complete
	wg.Wait()
	log.Info().Msg("Finished processing remaining messages")
}

// GetQueueStats returns current queue statistics  
func (c *RabbitMQBatchConsumer) GetQueueStats() map[string]QueueStats {
	c.messagesMutex.RLock()
	defer c.messagesMutex.RUnlock()
	
	result := make(map[string]QueueStats)
	
	// Create stats for each active queue group
	for group, messages := range c.pendingMessages {
		queueName := fmt.Sprintf("rabbitmq.%s.%d.%d", string(group.JobType), group.ChainID, group.TokenID)
		
		result[queueName] = QueueStats{
			QueueName:       queueName,
			PendingCount:    len(messages),
			ProcessingCount: 0, // Not tracked in this implementation
			CompletedCount:  0, // Would need persistent tracking
			FailedCount:     0, // Would need persistent tracking
			AverageLatency:  0, // Would need latency tracking
			LastProcessedAt: time.Now(),
		}
	}
	
	return result
}

// minBatch helper function for batch size calculations
func minBatch(a, b int) int {
	if a < b {
		return a
	}
	return b
}
