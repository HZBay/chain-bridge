package events

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog/log"

	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/hzbay/chain-bridge/internal/queue"
	"github.com/volatiletech/null/v8"
)

// PaymentEventManager manages multiple payment event listeners across different chains
type PaymentEventManager struct {
	listeners      map[int64]*PaymentEventListener
	rabbitmqClient *queue.RabbitMQClient
	mutex          sync.RWMutex
	stopChan       chan struct{}
	wg             sync.WaitGroup
}

// PaymentChainConfig represents configuration for a specific chain's payment listener
type PaymentChainConfig struct {
	ChainID            int64  `json:"chain_id" yaml:"chain_id"`
	RPCEndpoint        string `json:"rpc_endpoint" yaml:"rpc_endpoint"`
	PaymentAddress     string `json:"payment_address" yaml:"payment_address"`
	StartBlock         uint64 `json:"start_block" yaml:"start_block"`
	ConfirmationBlocks uint64 `json:"confirmation_blocks" yaml:"confirmation_blocks"`
	PollInterval       string `json:"poll_interval" yaml:"poll_interval"` // e.g., "10s"
	Enabled            bool   `json:"enabled" yaml:"enabled"`
}

// PaymentManagerConfig contains configuration for all payment listeners
type PaymentManagerConfig struct {
	Chains []PaymentChainConfig `json:"chains" yaml:"chains"`
}

// NewPaymentEventManager creates a new payment event manager
func NewPaymentEventManager(rabbitmqClient *queue.RabbitMQClient) *PaymentEventManager {
	return &PaymentEventManager{
		listeners:      make(map[int64]*PaymentEventListener),
		rabbitmqClient: rabbitmqClient,
		stopChan:       make(chan struct{}),
	}
}

// LoadConfig loads payment listener configuration and starts listeners
func (m *PaymentEventManager) LoadConfig(cfg PaymentManagerConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	log.Info().Int("chain_count", len(cfg.Chains)).Msg("Loading payment event listener configuration")

	for _, chainCfg := range cfg.Chains {
		if !chainCfg.Enabled {
			log.Info().Int64("chain_id", chainCfg.ChainID).Msg("Payment listener disabled for chain")
			continue
		}

		// Validate configuration
		if err := m.validateChainConfig(chainCfg); err != nil {
			log.Error().
				Err(err).
				Int64("chain_id", chainCfg.ChainID).
				Msg("Invalid chain configuration, skipping")
			continue
		}

		// Parse poll interval
		pollInterval, err := time.ParseDuration(chainCfg.PollInterval)
		if err != nil {
			log.Error().
				Err(err).
				Int64("chain_id", chainCfg.ChainID).
				Str("poll_interval", chainCfg.PollInterval).
				Msg("Invalid poll interval, using default 10s")
			pollInterval = 10 * time.Second
		}

		// Create listener configuration
		listenerConfig := PaymentListenerConfig{
			ChainID:            chainCfg.ChainID,
			RPCEndpoint:        chainCfg.RPCEndpoint,
			PaymentAddress:     common.HexToAddress(chainCfg.PaymentAddress),
			StartBlock:         chainCfg.StartBlock,
			ConfirmationBlocks: chainCfg.ConfirmationBlocks,
			PollInterval:       pollInterval,
		}

		// Create and add listener
		if err := m.addListener(chainCfg.ChainID, listenerConfig); err != nil {
			log.Error().
				Err(err).
				Int64("chain_id", chainCfg.ChainID).
				Msg("Failed to add payment listener for chain")
			continue
		}

		log.Info().
			Int64("chain_id", chainCfg.ChainID).
			Str("payment_address", chainCfg.PaymentAddress).
			Str("rpc_endpoint", chainCfg.RPCEndpoint).
			Msg("Payment listener configured for chain")
	}

	return nil
}

// validateChainConfig validates chain configuration
func (m *PaymentEventManager) validateChainConfig(cfg PaymentChainConfig) error {
	if cfg.ChainID <= 0 {
		return fmt.Errorf("invalid chain ID: %d", cfg.ChainID)
	}

	if cfg.RPCEndpoint == "" {
		return fmt.Errorf("RPC endpoint is required")
	}

	if cfg.PaymentAddress == "" {
		return fmt.Errorf("payment address is required")
	}

	if !common.IsHexAddress(cfg.PaymentAddress) {
		return fmt.Errorf("invalid payment address: %s", cfg.PaymentAddress)
	}

	if cfg.ConfirmationBlocks == 0 {
		return fmt.Errorf("confirmation blocks must be > 0")
	}

	if cfg.PollInterval == "" {
		return fmt.Errorf("poll interval is required")
	}

	return nil
}

// addListener creates and adds a payment listener for a specific chain
func (m *PaymentEventManager) addListener(chainID int64, config PaymentListenerConfig) error {
	// Check if listener already exists
	if _, exists := m.listeners[chainID]; exists {
		return fmt.Errorf("listener for chain %d already exists", chainID)
	}

	// Create new listener
	listener, err := NewPaymentEventListener(config, m.rabbitmqClient)
	if err != nil {
		return fmt.Errorf("failed to create payment listener: %w", err)
	}

	// Add to listeners map
	m.listeners[chainID] = listener

	return nil
}

// Start starts all configured payment listeners
func (m *PaymentEventManager) Start(ctx context.Context) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	log.Info().Int("listener_count", len(m.listeners)).Msg("Starting payment event listeners")

	for chainID, listener := range m.listeners {
		m.wg.Add(1)
		go func(cid int64, l *PaymentEventListener) {
			defer m.wg.Done()

			if err := l.Start(ctx); err != nil {
				log.Error().
					Err(err).
					Int64("chain_id", cid).
					Msg("Failed to start payment listener")
			}
		}(chainID, listener)

		log.Info().Int64("chain_id", chainID).Msg("Payment listener started")
	}

	// Start health monitoring
	m.wg.Add(1)
	go m.monitorHealth(ctx)

	return nil
}

// Stop stops all payment listeners gracefully
func (m *PaymentEventManager) Stop() error {
	log.Info().Msg("Stopping payment event manager")

	// Signal stop to all goroutines
	close(m.stopChan)

	// Stop all listeners
	m.mutex.RLock()
	for chainID, listener := range m.listeners {
		log.Info().Int64("chain_id", chainID).Msg("Stopping payment listener")
		if err := listener.Stop(); err != nil {
			log.Error().
				Err(err).
				Int64("chain_id", chainID).
				Msg("Error stopping payment listener")
		}
	}
	m.mutex.RUnlock()

	// Wait for all goroutines to complete
	m.wg.Wait()

	log.Info().Msg("Payment event manager stopped")
	return nil
}

// monitorHealth periodically checks the health of all listeners
func (m *PaymentEventManager) monitorHealth(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.checkListenerHealth()
		}
	}
}

// checkListenerHealth checks the health of all listeners
func (m *PaymentEventManager) checkListenerHealth() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for chainID, listener := range m.listeners {
		stats := listener.GetStats()
		isHealthy := listener.IsHealthy()

		logEvent := log.Info()
		if !isHealthy {
			logEvent = log.Warn()
		}

		logEvent.
			Int64("chain_id", chainID).
			Bool("healthy", isHealthy).
			Interface("stats", stats).
			Msg("Payment listener health check")
	}
}

// GetStats returns statistics for all listeners
func (m *PaymentEventManager) GetStats() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	stats := make(map[string]interface{})
	listenerStats := make(map[string]interface{})

	totalEvents := uint64(0)
	totalErrors := uint64(0)
	healthyCount := 0

	for chainID, listener := range m.listeners {
		chainStats := listener.GetStats()
		listenerStats[fmt.Sprintf("chain_%d", chainID)] = chainStats

		if events, ok := chainStats["total_events"].(uint64); ok {
			totalEvents += events
		}
		if errors, ok := chainStats["total_errors"].(uint64); ok {
			totalErrors += errors
		}
		if listener.IsHealthy() {
			healthyCount++
		}
	}

	stats["listeners"] = listenerStats
	stats["total_listeners"] = len(m.listeners)
	stats["healthy_listeners"] = healthyCount
	stats["total_events_processed"] = totalEvents
	stats["total_errors"] = totalErrors

	return stats
}

// GetListener returns a specific listener by chain ID
func (m *PaymentEventManager) GetListener(chainID int64) (*PaymentEventListener, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	listener, exists := m.listeners[chainID]
	return listener, exists
}

// IsHealthy returns whether all listeners are healthy
func (m *PaymentEventManager) IsHealthy() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for _, listener := range m.listeners {
		if !listener.IsHealthy() {
			return false
		}
	}

	return len(m.listeners) > 0 // At least one listener should be running
}

// GetGlobalStats returns global statistics in the format expected by the API
func (m *PaymentEventManager) GetGlobalStats() GlobalStats {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	totalEvents := int64(0)
	totalErrors := int64(0)
	healthyCount := int64(0)
	totalListeners := int64(len(m.listeners))

	for _, listener := range m.listeners {
		stats := listener.GetStats()
		if events, ok := stats["total_events"].(uint64); ok {
			if events <= math.MaxInt64 {
				totalEvents += int64(events)
			}
		}
		if errors, ok := stats["total_errors"].(uint64); ok {
			if errors <= math.MaxInt64 {
				totalErrors += int64(errors)
			}
		}
		if listener.IsHealthy() {
			healthyCount++
		}
	}

	return GlobalStats{
		TotalListeners:       totalListeners,
		HealthyListeners:     healthyCount,
		TotalEventsProcessed: totalEvents,
		TotalErrors:          totalErrors,
	}
}

// GetAllListenerStats returns statistics for all listeners in API format
func (m *PaymentEventManager) GetAllListenerStats() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]interface{})

	for chainID, listener := range m.listeners {
		stats := listener.GetStats()
		chainKey := fmt.Sprintf("chain_%d", chainID)

		// Convert stats to API format
		apiStats := map[string]interface{}{
			"chain_id":             chainID,
			"payment_address":      stats["payment_address"],
			"last_processed_block": stats["last_processed_block"],
			"total_events":         stats["total_events"],
			"total_errors":         stats["total_errors"],
		}

		// Add status
		if listener.IsHealthy() {
			apiStats["status"] = "running"
		} else {
			apiStats["status"] = "error"
		}

		// Add optional fields if available
		if lastEventTime, ok := stats["last_event_time"].(time.Time); ok && !lastEventTime.IsZero() {
			apiStats["last_event_time"] = lastEventTime.Format(time.RFC3339)
		}

		// Add processing latency (mock value for now)
		apiStats["processing_latency_ms"] = 150

		result[chainKey] = apiStats
	}

	return result
}

// LoadConfigFromDatabase loads payment configuration from chains table in database
func LoadConfigFromDatabase(ctx context.Context, db *sql.DB) (PaymentManagerConfig, error) {
	paymentCfg := PaymentManagerConfig{
		Chains: []PaymentChainConfig{},
	}

	// Query all enabled chains with payment contract addresses
	chains, err := models.Chains(
		models.ChainWhere.IsEnabled.EQ(null.BoolFrom(true)),
		models.ChainWhere.PaymentContractAddress.IsNotNull(),
	).All(ctx, db)
	if err != nil {
		return paymentCfg, fmt.Errorf("failed to query chains: %w", err)
	}

	log.Info().Int("chain_count", len(chains)).Msg("Loading payment configuration from database")

	for _, chain := range chains {
		// Skip chains without payment contract address
		if !chain.PaymentContractAddress.Valid || chain.PaymentContractAddress.String == "" {
			log.Debug().Int64("chain_id", chain.ChainID).Msg("Chain has no payment contract address, skipping")
			continue
		}

		// Create payment chain configuration
		paymentChainCfg := PaymentChainConfig{
			ChainID:            chain.ChainID,
			RPCEndpoint:        chain.RPCURL,
			PaymentAddress:     chain.PaymentContractAddress.String,
			StartBlock:         0,                                    // Start from latest block for new listeners
			ConfirmationBlocks: func() uint64 {
				val := chain.ConfirmationBlocks.Int
				if val < 0 {
					return 0
				}
				// Use safe conversion avoiding direct cast
				return uint64(val)
			}(), // Default confirmation blocks (could be made configurable)
			PollInterval:       "10s",                                // Default poll interval (could be made configurable)
			Enabled:            chain.IsEnabled.Bool,
		}

		paymentCfg.Chains = append(paymentCfg.Chains, paymentChainCfg)

		log.Debug().
			Int64("chain_id", chain.ChainID).
			Str("name", chain.Name).
			Str("payment_address", chain.PaymentContractAddress.String).
			Str("rpc_endpoint", chain.RPCURL).
			Msg("Added chain to payment configuration")
	}

	return paymentCfg, nil
}
