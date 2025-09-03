package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hzbay/chain-bridge/internal/config"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)

// RabbitMQClient handles RabbitMQ connections and operations
type RabbitMQClient struct {
	config     config.RabbitMQConfig
	connection *amqp.Connection
	channel    *amqp.Channel
	queues     map[string]amqp.Queue
	mutex      sync.RWMutex

	// Connection management
	reconnectCount int
	lastReconnect  time.Time

	// Health status
	healthy bool

	// Stop channel for graceful shutdown
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewRabbitMQClient creates a new RabbitMQ client
func NewRabbitMQClient(cfg config.RabbitMQConfig) (*RabbitMQClient, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("RabbitMQ is disabled")
	}

	client := &RabbitMQClient{
		config:   cfg,
		queues:   make(map[string]amqp.Queue),
		stopChan: make(chan struct{}),
		healthy:  false,
	}

	if err := client.connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Start connection monitoring
	client.wg.Add(1)
	go client.monitorConnection()

	return client, nil
}

// connect establishes connection to RabbitMQ
func (c *RabbitMQClient) connect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Close existing connections
	c.closeConnections()

	// Create new connection
	var err error
	c.connection, err = amqp.Dial(c.config.GetConnectionURL())
	if err != nil {
		c.healthy = false
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Create channel
	c.channel, err = c.connection.Channel()
	if err != nil {
		c.connection.Close()
		c.healthy = false
		return fmt.Errorf("failed to create channel: %w", err)
	}

	// Configure channel settings
	err = c.channel.Qos(
		10,    // prefetch count
		0,     // prefetch size
		false, // global
	)
	if err != nil {
		c.closeConnections()
		c.healthy = false
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	c.healthy = true
	c.lastReconnect = time.Now()

	log.Info().
		Str("host", c.config.Host).
		Int("port", c.config.Port).
		Msg("Connected to RabbitMQ")

	return nil
}

// closeConnections closes the channel and connection
func (c *RabbitMQClient) closeConnections() {
	if c.channel != nil && !c.channel.IsClosed() {
		c.channel.Close()
	}
	if c.connection != nil && !c.connection.IsClosed() {
		c.connection.Close()
	}
	c.healthy = false
}

// monitorConnection monitors the connection and reconnects if needed
func (c *RabbitMQClient) monitorConnection() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			if !c.IsHealthy() {
				log.Warn().Msg("RabbitMQ connection unhealthy, attempting reconnect")
				if err := c.connect(); err != nil {
					c.reconnectCount++
					log.Error().
						Err(err).
						Int("attempt", c.reconnectCount).
						Msg("Failed to reconnect to RabbitMQ")
				} else {
					c.reconnectCount = 0
					log.Info().Msg("Successfully reconnected to RabbitMQ")
				}
			}
		}
	}
}

// DeclareQueue declares a queue with appropriate settings
func (c *RabbitMQClient) DeclareQueue(queueName string) (amqp.Queue, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.healthy {
		return amqp.Queue{}, fmt.Errorf("RabbitMQ client is not healthy")
	}

	// Check if queue already declared
	if queue, exists := c.queues[queueName]; exists {
		return queue, nil
	}

	// Declare queue
	queue, err := c.channel.QueueDeclare(
		queueName,              // name
		c.config.DurableQueues, // durable
		false,                  // delete when unused
		false,                  // exclusive
		false,                  // no-wait
		amqp.Table{
			"x-message-ttl": int32(24 * 60 * 60 * 1000), // 24 hours TTL
		}, // arguments
	)
	if err != nil {
		return amqp.Queue{}, fmt.Errorf("failed to declare queue %s: %w", queueName, err)
	}

	c.queues[queueName] = queue

	log.Debug().
		Str("queue", queueName).
		Bool("durable", c.config.DurableQueues).
		Msg("Queue declared")

	return queue, nil
}

// PublishMessage publishes a message to the specified queue
func (c *RabbitMQClient) PublishMessage(ctx context.Context, queueName string, message interface{}) error {
	if !c.healthy {
		return fmt.Errorf("RabbitMQ client is not healthy")
	}

	// Ensure queue is declared
	_, err := c.DeclareQueue(queueName)
	if err != nil {
		return err
	}

	// Marshal message to JSON
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Publish message
	err = c.channel.PublishWithContext(
		ctx,
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // Make message persistent
			Timestamp:    time.Now(),
			Body:         body,
		},
	)
	if err != nil {
		c.healthy = false // Mark as unhealthy on publish failure
		return fmt.Errorf("failed to publish message to queue %s: %w", queueName, err)
	}

	log.Debug().
		Str("queue", queueName).
		Int("size", len(body)).
		Msg("Message published")

	return nil
}

// ConsumeMessages consumes messages from the specified queue
func (c *RabbitMQClient) ConsumeMessages(queueName string, _ func([]byte) error) (<-chan amqp.Delivery, error) {
	if !c.healthy {
		return nil, fmt.Errorf("RabbitMQ client is not healthy")
	}

	// Ensure queue is declared
	_, err := c.DeclareQueue(queueName)
	if err != nil {
		return nil, err
	}

	// Start consuming
	messages, err := c.channel.Consume(
		queueName, // queue
		"",        // consumer
		false,     // auto-ack (we'll ack manually)
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	if err != nil {
		return nil, fmt.Errorf("failed to consume from queue %s: %w", queueName, err)
	}

	log.Info().
		Str("queue", queueName).
		Msg("Started consuming messages")

	return messages, nil
}

// GetQueueInfo returns information about a queue
func (c *RabbitMQClient) GetQueueInfo(queueName string) (int, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.healthy {
		return 0, fmt.Errorf("RabbitMQ client is not healthy")
	}

	// Use QueueDeclare with Passive: true instead of deprecated QueueInspect
	queue, err := c.channel.QueueDeclarePassive(
		queueName, // name
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		return 0, fmt.Errorf("failed to inspect queue %s: %w", queueName, err)
	}

	return queue.Messages, nil
}

// GetQueueName generates a queue name based on job type and identifiers
func (c *RabbitMQClient) GetQueueName(jobType JobType, chainID int64, tokenID int) string {
	return c.config.GetQueueName(string(jobType), chainID, tokenID)
}

// IsHealthy checks if the RabbitMQ connection is healthy
func (c *RabbitMQClient) IsHealthy() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.healthy {
		return false
	}

	if c.connection == nil || c.connection.IsClosed() {
		c.healthy = false
		return false
	}

	if c.channel == nil || c.channel.IsClosed() {
		c.healthy = false
		return false
	}

	return true
}

// Ping tests the connection to RabbitMQ
func (c *RabbitMQClient) Ping() error {
	if !c.IsHealthy() {
		return fmt.Errorf("RabbitMQ client is not healthy")
	}

	// Try to declare a temporary queue as a ping test
	tempQueueName := fmt.Sprintf("%s.ping.%d", c.config.QueuePrefix, time.Now().Unix())
	_, err := c.channel.QueueDeclare(
		tempQueueName, // name
		false,         // durable
		true,          // delete when unused
		true,          // exclusive
		false,         // no-wait
		nil,           // arguments
	)
	if err != nil {
		c.healthy = false
		return fmt.Errorf("ping failed: %w", err)
	}

	// Clean up the temporary queue
	_, err = c.channel.QueueDelete(tempQueueName, false, false, false)
	if err != nil {
		log.Warn().Err(err).Str("queue", tempQueueName).Msg("Failed to delete ping queue")
	}

	return nil
}

// Close closes the RabbitMQ connection and stops monitoring
func (c *RabbitMQClient) Close() error {
	log.Info().Msg("Closing RabbitMQ client")

	// Check if already closed
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.healthy && c.connection == nil {
		// Already closed
		log.Debug().Msg("RabbitMQ client already closed")
		return nil
	}

	// Stop monitoring (only if not already stopped)
	select {
	case <-c.stopChan:
		// Channel already closed
	default:
		close(c.stopChan)
	}
	c.wg.Wait()

	// Close connections
	c.closeConnections()

	log.Info().Msg("RabbitMQ client closed")
	return nil
}
