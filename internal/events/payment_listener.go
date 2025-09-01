package events

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog/log"

	cpop "github.com/HzBay/account-abstraction/cpop-abis"
	"github.com/hzbay/chain-bridge/internal/queue"
)

// PaymentEventMessage represents a payment event message for RabbitMQ
type PaymentEventMessage struct {
	EventType   string `json:"event_type"`   // "payment_made"
	ChainID     int64  `json:"chain_id"`     // 链ID
	OrderID     string `json:"order_id"`     // 订单ID
	Payer       string `json:"payer"`        // 付款人地址
	Token       string `json:"token"`        // 代币合约地址
	Amount      string `json:"amount"`       // 支付金额
	Timestamp   int64  `json:"timestamp"`    // 支付时间戳
	BlockNumber uint64 `json:"block_number"` // 区块高度
	TxHash      string `json:"tx_hash"`      // 交易哈希
}

// PaymentListenerConfig contains configuration for payment event listener
type PaymentListenerConfig struct {
	ChainID            int64          `json:"chain_id"`
	RPCEndpoint        string         `json:"rpc_endpoint"`
	PaymentAddress     common.Address `json:"payment_address"`
	StartBlock         uint64         `json:"start_block"`
	ConfirmationBlocks uint64         `json:"confirmation_blocks"`
	PollInterval       time.Duration  `json:"poll_interval"`
}

// PaymentEventListener listens for PaymentMade events from Payment contract
type PaymentEventListener struct {
	config         PaymentListenerConfig
	client         *ethclient.Client
	payment        *cpop.Payment
	rabbitmqClient *queue.RabbitMQClient

	// Event processing
	eventChan    chan *cpop.PaymentPaymentMade
	processingWg sync.WaitGroup
	stopChan     chan struct{}

	// State management
	lastProcessedBlock uint64
	mutex              sync.RWMutex

	// Monitoring
	eventCount    uint64
	errorCount    uint64
	lastEventTime time.Time
}

// NewPaymentEventListener creates a new payment event listener
func NewPaymentEventListener(config PaymentListenerConfig, rabbitmqClient *queue.RabbitMQClient) (*PaymentEventListener, error) {
	// Connect to Ethereum client
	client, err := ethclient.Dial(config.RPCEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}

	// Verify chain ID
	chainID, err := client.ChainID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	if chainID.Int64() != config.ChainID {
		return nil, fmt.Errorf("chain ID mismatch: expected %d, got %d", config.ChainID, chainID.Int64())
	}

	// Initialize Payment contract
	payment, err := cpop.NewPayment(config.PaymentAddress, client)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Payment contract: %w", err)
	}

	// Verify contract by calling a view function
	_, err = payment.Owner(&bind.CallOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to verify Payment contract: %w", err)
	}

	listener := &PaymentEventListener{
		config:             config,
		client:             client,
		payment:            payment,
		rabbitmqClient:     rabbitmqClient,
		eventChan:          make(chan *cpop.PaymentPaymentMade, 1000), // Buffered channel
		stopChan:           make(chan struct{}),
		lastProcessedBlock: config.StartBlock,
	}

	return listener, nil
}

// Start starts the payment event listener
func (l *PaymentEventListener) Start(ctx context.Context) error {
	log.Info().
		Int64("chain_id", l.config.ChainID).
		Str("payment_address", l.config.PaymentAddress.Hex()).
		Uint64("start_block", l.config.StartBlock).
		Msg("Starting payment event listener")

	// Start event processor goroutine
	l.processingWg.Add(1)
	go l.processEvents(ctx)

	// Start historical event sync if needed
	if l.config.StartBlock > 0 {
		l.processingWg.Add(1)
		go l.syncHistoricalEvents(ctx)
	}

	// Start real-time event listener
	l.processingWg.Add(1)
	go l.listenForNewEvents(ctx)

	return nil
}

// Stop stops the payment event listener gracefully
func (l *PaymentEventListener) Stop() error {
	log.Info().Msg("Stopping payment event listener")

	close(l.stopChan)
	l.processingWg.Wait()

	close(l.eventChan)

	if l.client != nil {
		l.client.Close()
	}

	log.Info().
		Uint64("total_events", l.eventCount).
		Uint64("total_errors", l.errorCount).
		Msg("Payment event listener stopped")

	return nil
}

// syncHistoricalEvents syncs historical events from start block to current block
func (l *PaymentEventListener) syncHistoricalEvents(ctx context.Context) {
	defer l.processingWg.Done()

	log.Info().Uint64("from_block", l.config.StartBlock).Msg("Starting historical event sync")

	// Get current block
	currentBlock, err := l.client.BlockNumber(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get current block number")
		return
	}

	// Apply confirmation blocks buffer
	if currentBlock > l.config.ConfirmationBlocks {
		currentBlock -= l.config.ConfirmationBlocks
	}

	// Batch size for historical sync (stay under RPC limits)
	const batchSize = 500
	fromBlock := l.config.StartBlock

	for fromBlock <= currentBlock {
		select {
		case <-ctx.Done():
			return
		case <-l.stopChan:
			return
		default:
		}

		toBlock := fromBlock + batchSize - 1
		if toBlock > currentBlock {
			toBlock = currentBlock
		}

		log.Debug().
			Uint64("from_block", fromBlock).
			Uint64("to_block", toBlock).
			Msg("Syncing historical events batch")

		err := l.fetchEventsInRange(ctx, fromBlock, toBlock)
		if err != nil {
			log.Error().
				Err(err).
				Uint64("from_block", fromBlock).
				Uint64("to_block", toBlock).
				Msg("Failed to fetch historical events")

			// Exponential backoff
			select {
			case <-time.After(time.Second * 5):
				continue
			case <-ctx.Done():
				return
			case <-l.stopChan:
				return
			}
		}

		l.mutex.Lock()
		l.lastProcessedBlock = toBlock
		l.mutex.Unlock()

		fromBlock = toBlock + 1

		// Small delay to avoid overwhelming the RPC
		time.Sleep(time.Millisecond * 100)
	}

	log.Info().Uint64("last_block", currentBlock).Msg("Historical event sync completed")
}

// listenForNewEvents listens for new events in real-time
func (l *PaymentEventListener) listenForNewEvents(ctx context.Context) {
	defer l.processingWg.Done()

	log.Info().Msg("Starting real-time event listener")

	ticker := time.NewTicker(l.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-l.stopChan:
			return
		case <-ticker.C:
			if err := l.pollForNewEvents(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to poll for new events")
				l.errorCount++
			}
		}
	}
}

// pollForNewEvents polls for new events since last processed block
func (l *PaymentEventListener) pollForNewEvents(ctx context.Context) error {
	currentBlock, err := l.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current block number: %w", err)
	}

	// Apply confirmation blocks buffer
	if currentBlock > l.config.ConfirmationBlocks {
		currentBlock -= l.config.ConfirmationBlocks
	}

	l.mutex.RLock()
	fromBlock := l.lastProcessedBlock + 1
	l.mutex.RUnlock()

	// Initialize lastProcessedBlock to recent block if it's 0
	if l.lastProcessedBlock == 0 {
		// Start from recent blocks instead of genesis
		if currentBlock > 1000 {
			fromBlock = currentBlock - 1000
		} else {
			fromBlock = 1
		}
		l.mutex.Lock()
		l.lastProcessedBlock = fromBlock - 1
		l.mutex.Unlock()
	}

	if fromBlock > currentBlock {
		return nil // No new blocks to process
	}

	// Split large ranges into batches to avoid RPC limits (max 500 blocks)
	const maxBlockRange = 500

	for batchStart := fromBlock; batchStart <= currentBlock; batchStart += maxBlockRange {
		batchEnd := batchStart + maxBlockRange - 1
		if batchEnd > currentBlock {
			batchEnd = currentBlock
		}

		log.Debug().
			Uint64("from_block", batchStart).
			Uint64("to_block", batchEnd).
			Msg("Polling for new events")

		err = l.fetchEventsInRange(ctx, batchStart, batchEnd)
		if err != nil {
			return fmt.Errorf("failed to fetch events in range: %w", err)
		}

		l.mutex.Lock()
		l.lastProcessedBlock = batchEnd
		l.mutex.Unlock()

		// Brief pause between batches to avoid overwhelming RPC
		if batchEnd < currentBlock {
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			case <-l.stopChan:
				return nil
			}
		}
	}

	return nil
}

// fetchEventsInRange fetches PaymentMade events in the specified block range
func (l *PaymentEventListener) fetchEventsInRange(ctx context.Context, fromBlock, toBlock uint64) error {
	// Create filter for PaymentMade events
	iterator, err := l.payment.FilterPaymentMade(
		&bind.FilterOpts{
			Context: ctx,
			Start:   fromBlock,
			End:     &toBlock,
		},
		nil, // orderId filter (nil = all)
		nil, // payer filter (nil = all)
		nil, // token filter (nil = all)
	)
	if err != nil {
		return fmt.Errorf("failed to create PaymentMade filter: %w", err)
	}
	defer iterator.Close()

	// Process all events in the range
	eventCount := 0
	for iterator.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.stopChan:
			return nil
		default:
		}

		event := iterator.Event
		if event == nil {
			continue
		}

		// Send event to processing channel
		select {
		case l.eventChan <- event:
			eventCount++
		default:
			log.Warn().Msg("Event channel is full, dropping event")
		}
	}

	if iterator.Error() != nil {
		return fmt.Errorf("event iterator error: %w", iterator.Error())
	}

	if eventCount > 0 {
		log.Info().
			Int("event_count", eventCount).
			Uint64("from_block", fromBlock).
			Uint64("to_block", toBlock).
			Msg("Fetched PaymentMade events")
	}

	return nil
}

// processEvents processes events from the event channel
func (l *PaymentEventListener) processEvents(ctx context.Context) {
	defer l.processingWg.Done()

	log.Info().Msg("Starting event processor")

	for {
		select {
		case <-ctx.Done():
			return
		case <-l.stopChan:
			return
		case event, ok := <-l.eventChan:
			if !ok {
				return // Channel closed
			}

			if err := l.processPaymentEvent(ctx, event); err != nil {
				log.Error().
					Err(err).
					Str("tx_hash", event.Raw.TxHash.Hex()).
					Uint64("block_number", event.Raw.BlockNumber).
					Msg("Failed to process payment event")
				l.errorCount++
			} else {
				l.eventCount++
				l.lastEventTime = time.Now()
			}
		}
	}
}

// processPaymentEvent processes a single PaymentMade event
func (l *PaymentEventListener) processPaymentEvent(ctx context.Context, event *cpop.PaymentPaymentMade) error {
	// Create payment event message
	message := &PaymentEventMessage{
		EventType:   "payment_made",
		ChainID:     l.config.ChainID,
		OrderID:     event.OrderId.String(),
		Payer:       event.Payer.Hex(),
		Token:       event.Token.Hex(),
		Amount:      event.Amount.String(),
		Timestamp:   event.Timestamp.Int64(),
		BlockNumber: event.Raw.BlockNumber,
		TxHash:      event.Raw.TxHash.Hex(),
	}

	// Generate queue name based on chain ID
	queueName := fmt.Sprintf("payment_events.%d", l.config.ChainID)

	// Publish to RabbitMQ
	err := l.rabbitmqClient.PublishMessage(ctx, queueName, message)
	if err != nil {
		return fmt.Errorf("failed to publish payment event message: %w", err)
	}

	log.Info().
		Str("order_id", message.OrderID).
		Str("payer", message.Payer).
		Str("token", message.Token).
		Str("amount", message.Amount).
		Str("tx_hash", message.TxHash).
		Uint64("block_number", message.BlockNumber).
		Msg("Payment event processed and published")

	return nil
}

// GetStats returns listener statistics
func (l *PaymentEventListener) GetStats() map[string]interface{} {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	return map[string]interface{}{
		"chain_id":               l.config.ChainID,
		"payment_address":        l.config.PaymentAddress.Hex(),
		"last_processed_block":   l.lastProcessedBlock,
		"total_events":           l.eventCount,
		"total_errors":           l.errorCount,
		"last_event_time":        l.lastEventTime,
		"event_channel_length":   len(l.eventChan),
		"event_channel_capacity": cap(l.eventChan),
	}
}

// IsHealthy returns whether the listener is healthy
func (l *PaymentEventListener) IsHealthy() bool {
	// Consider healthy if we've processed events recently or just started
	return l.errorCount < 10 && (l.eventCount > 0 || time.Since(l.lastEventTime) < time.Hour)
}
