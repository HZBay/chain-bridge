package events

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/hzbay/chain-bridge/internal/queue"
)

// PaymentEventService is the main service that manages payment event listening
type PaymentEventService struct {
	db             *sql.DB
	rabbitmqClient *queue.RabbitMQClient
	eventManager   *PaymentEventManager

	// Service state
	started bool
	mutex   sync.RWMutex
}

// NewPaymentEventService creates a new payment event service
func NewPaymentEventService(db *sql.DB, rabbitmqClient *queue.RabbitMQClient) *PaymentEventService {
	eventManager := NewPaymentEventManager(rabbitmqClient)

	return &PaymentEventService{
		db:             db,
		rabbitmqClient: rabbitmqClient,
		eventManager:   eventManager,
		started:        false,
	}
}

// Start initializes and starts the payment event service
func (s *PaymentEventService) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.started {
		return fmt.Errorf("payment event service is already started")
	}

	log.Info().Msg("Starting payment event service")

	// Load configuration from database
	config, err := LoadConfigFromDatabase(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to load payment configuration from database: %w", err)
	}

	// Check if any chains are configured for payment listening
	if len(config.Chains) == 0 {
		log.Info().Msg("No chains configured with payment contracts, payment event service will not start listeners")
		s.started = true
		return nil
	}

	// Load configuration into event manager
	if err := s.eventManager.LoadConfig(config); err != nil {
		return fmt.Errorf("failed to load payment configuration: %w", err)
	}

	// Start event manager
	if err := s.eventManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start payment event manager: %w", err)
	}

	s.started = true

	log.Info().
		Int("configured_chains", len(config.Chains)).
		Msg("Payment event service started successfully")

	return nil
}

// Stop stops the payment event service gracefully
func (s *PaymentEventService) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.started {
		return nil // Already stopped or never started
	}

	log.Info().Msg("Stopping payment event service")

	// Stop event manager
	if err := s.eventManager.Stop(); err != nil {
		log.Error().Err(err).Msg("Error stopping payment event manager")
		return err
	}

	s.started = false

	log.Info().Msg("Payment event service stopped successfully")
	return nil
}

// IsStarted returns whether the service is currently started
func (s *PaymentEventService) IsStarted() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.started
}

// GetStats returns statistics from the payment event service
func (s *PaymentEventService) GetStats() map[string]interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if !s.started || s.eventManager == nil {
		return map[string]interface{}{
			"started": false,
			"message": "Payment event service not started",
		}
	}

	stats := s.eventManager.GetStats()
	stats["started"] = s.started

	return stats
}

// IsHealthy returns whether the service is healthy
func (s *PaymentEventService) IsHealthy() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if !s.started {
		return true // Service is healthy if not expected to be running
	}

	return s.eventManager.IsHealthy()
}

// ReloadConfig reloads the payment configuration from database
func (s *PaymentEventService) ReloadConfig(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.started {
		return fmt.Errorf("service is not started")
	}

	log.Info().Msg("Reloading payment event service configuration")

	// Stop current listeners
	if err := s.eventManager.Stop(); err != nil {
		log.Error().Err(err).Msg("Error stopping event manager during reload")
	}

	// Create new event manager
	s.eventManager = NewPaymentEventManager(s.rabbitmqClient)

	// Load fresh configuration from database
	config, err := LoadConfigFromDatabase(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to reload payment configuration from database: %w", err)
	}

	// Load configuration into new event manager
	if err := s.eventManager.LoadConfig(config); err != nil {
		return fmt.Errorf("failed to load reloaded payment configuration: %w", err)
	}

	// Start new event manager
	if err := s.eventManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start reloaded payment event manager: %w", err)
	}

	log.Info().
		Int("configured_chains", len(config.Chains)).
		Msg("Payment event service configuration reloaded successfully")

	return nil
}

// GetListener returns a specific payment event listener by chain ID
func (s *PaymentEventService) GetListener(chainID int64) (*PaymentEventListener, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if !s.started || s.eventManager == nil {
		return nil, false
	}

	return s.eventManager.GetListener(chainID)
}

// GlobalStats contains global statistics for all payment event listeners
type GlobalStats struct {
	TotalListeners       int64
	HealthyListeners     int64
	TotalEventsProcessed int64
	TotalErrors          int64
}

// GetGlobalStats returns global statistics for all payment event listeners
func (s *PaymentEventService) GetGlobalStats() GlobalStats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if !s.started || s.eventManager == nil {
		return GlobalStats{}
	}

	return s.eventManager.GetGlobalStats()
}

// GetAllListenerStats returns statistics for all individual listeners
func (s *PaymentEventService) GetAllListenerStats() map[string]interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if !s.started || s.eventManager == nil {
		return make(map[string]interface{})
	}

	return s.eventManager.GetAllListenerStats()
}
