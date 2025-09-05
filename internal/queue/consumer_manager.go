package queue

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/hzbay/chain-bridge/internal/blockchain"
	"github.com/hzbay/chain-bridge/internal/config"
	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/rs/zerolog/log"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

// ConsumerManager manages per-chain RabbitMQ batch consumers
type ConsumerManager struct {
	client              *RabbitMQClient
	db                  *sql.DB
	batchOptimizer      *BatchOptimizer
	callers             map[int64]*blockchain.BatchCaller // Changed from cpopCallers
	confirmationWatcher *TxConfirmationWatcher
	batchProcessor      BatchProcessor
	config              config.Server

	// Per-chain consumers management
	consumers      map[int64]*ChainBatchConsumer // chainID -> consumer
	consumersMutex sync.RWMutex

	// Control channels
	stopChan chan struct{}
	workerWg sync.WaitGroup

	// Configuration
	refreshInterval time.Duration
	workersPerChain int
}

// ChainBatchConsumer represents a consumer for a specific chain
type ChainBatchConsumer struct {
	ChainID        int64
	ChainName      string
	Consumer       *RabbitMQBatchConsumer
	QueueNames     []string
	IsActive       bool
	StartedAt      time.Time
	ProcessedCount int64
	ErrorCount     int64
}

// NewConsumerManager creates a new consumer manager
func NewConsumerManager(
	client *RabbitMQClient,
	db *sql.DB,
	optimizer *BatchOptimizer,
	batchProcessor BatchProcessor,
	config config.Server,
) *ConsumerManager {
	return &ConsumerManager{
		client:              client,
		db:                  db,
		batchOptimizer:      optimizer,
		batchProcessor:      batchProcessor,
		consumers:           make(map[int64]*ChainBatchConsumer),
		confirmationWatcher: NewTxConfirmationWatcher(),
		config:              config,
		stopChan:            make(chan struct{}),
		refreshInterval:     5 * time.Minute, // Check for new chains every 5 minutes
		workersPerChain:     1,               // Default 3 workers per chain
	}
}

// Start starts the consumer manager and all per-chain consumers
func (cm *ConsumerManager) Start(ctx context.Context) error {
	log.Info().Msg("Starting Consumer Manager")

	// Initial setup of consumers for all enabled chains
	if err := cm.setupConsumersForEnabledChains(ctx); err != nil {
		return fmt.Errorf("failed to setup initial consumers: %w", err)
	}

	// Start periodic refresh to handle new chains
	cm.workerWg.Add(1)
	go cm.runPeriodicRefresh(ctx)

	log.Info().
		Int("active_chains", len(cm.consumers)).
		Dur("refresh_interval", cm.refreshInterval).
		Int("workers_per_chain", cm.workersPerChain).
		Msg("Consumer Manager started successfully")

	return nil
}

// Stop stops all consumers and the consumer manager
func (cm *ConsumerManager) Stop(ctx context.Context) error {
	log.Info().Msg("Stopping Consumer Manager")

	// Signal stop
	close(cm.stopChan)

	// Stop all chain consumers
	cm.consumersMutex.Lock()
	var stopWg sync.WaitGroup
	for chainID, chainConsumer := range cm.consumers {
		stopWg.Add(1)
		go func(cid int64, consumer *ChainBatchConsumer) {
			defer stopWg.Done()
			if err := consumer.Consumer.Stop(ctx); err != nil {
				log.Error().
					Int64("chain_id", cid).
					Err(err).
					Msg("Failed to stop chain consumer")
			}
		}(chainID, chainConsumer)
	}
	cm.consumersMutex.Unlock()

	// Wait for all consumers to stop
	done := make(chan struct{})
	go func() {
		stopWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info().Msg("All chain consumers stopped")
	case <-time.After(30 * time.Second):
		log.Warn().Msg("Timeout waiting for chain consumers to stop")
	}

	// Wait for manager workers to finish
	cm.workerWg.Wait()

	log.Info().Msg("Consumer Manager stopped")
	return nil
}

// setupConsumersForEnabledChains initializes RabbitMQ batch consumers for all enabled blockchain chains.
// This function performs the following operations:
// 1. Retrieves all enabled chains from the database
// 2. Creates CPOP (Custom Payment Operation Protocol) callers for blockchain interaction
// 3. Sets up dedicated consumers for each chain to process transactions
// 4. Initializes transaction confirmation watcher for monitoring blockchain confirmations
func (cm *ConsumerManager) setupConsumersForEnabledChains(ctx context.Context) error {
	log.Info().Msg("Setting up consumers for enabled chains")

	// Step 1: Retrieve all enabled chains from database
	chains, err := cm.getEnabledChains(ctx)
	if err != nil {
		return fmt.Errorf("failed to get enabled chains: %w", err)
	}

	log.Info().Int("chains_count", len(chains)).Msg("Found enabled chains")

	// Early return if no chains are enabled
	if len(chains) == 0 {
		log.Warn().Msg("No enabled chains found, skipping consumer setup")
		return nil
	}

	// Step 2: Thread-safe initialization of consumer and caller maps
	cm.consumersMutex.Lock()
	defer cm.consumersMutex.Unlock()

	// Initialize unified callers map for blockchain interactions
	cm.callers = make(map[int64]*blockchain.BatchCaller)

	// Step 3: Process each enabled chain and create necessary components
	successfulChains := 0
	for _, chain := range chains {
		// Skip if consumer already exists for this chain
		if _, exists := cm.consumers[chain.ChainID]; exists {
			log.Debug().
				Int64("chain_id", chain.ChainID).
				Str("chain_name", chain.Name).
				Msg("Consumer already exists for chain, skipping")
			continue
		}

		// Step 3a: Validate token contract address
		if !common.IsHexAddress(chain.ToeknAddress.String) {
			log.Warn().
				Int64("chain_id", chain.ChainID).
				Str("chain_name", chain.Name).
				Str("invalid_address", chain.ToeknAddress.String).
				Msg("Invalid token contract address, skipping chain")
			continue
		}

		// Step 3b: Parse deployment private key for blockchain transactions
		privateKey, err := blockchain.GetDeploymentPrivateKeyFromString(cm.config.Blockchain.UnifiedDeploymentPrivateKey)
		if err != nil {
			log.Error().
				Int64("chain_id", chain.ChainID).
				Str("chain_name", chain.Name).
				Err(err).
				Msg("Failed to parse deployment private key, skipping chain")
			continue
		}

		// Step 3c: Create transaction authorization for this specific chain
		auth, err := bind.NewKeyedTransactorWithChainID(privateKey, big.NewInt(chain.ChainID))
		if err != nil {
			log.Error().
				Int64("chain_id", chain.ChainID).
				Str("chain_name", chain.Name).
				Err(err).
				Msg("Failed to create transaction authorization, skipping chain")
			continue
		}

		// Step 3d: Initialize unified batch caller for blockchain operations
		// Get NFT contract address if available
		nftContractAddr := common.Address{} // Default to zero address
		if chain.OfficialNFTContractAddress.Valid && chain.OfficialNFTContractAddress.String != "" {
			if common.IsHexAddress(chain.OfficialNFTContractAddress.String) {
				nftContractAddr = common.HexToAddress(chain.OfficialNFTContractAddress.String)
				log.Info().
					Int64("chain_id", chain.ChainID).
					Str("nft_contract_addr", nftContractAddr.Hex()).
					Msg("NFT contract address found for chain")
			} else {
				log.Warn().
					Int64("chain_id", chain.ChainID).
					Str("invalid_nft_address", chain.OfficialNFTContractAddress.String).
					Msg("Invalid NFT contract address format, NFT operations will be disabled")
			}
		}

		caller, err := blockchain.NewBatchCaller(
			chain.RPCURL,
			common.HexToAddress(chain.ToeknAddress.String), // CPOP token address
			nftContractAddr, // NFT contract address
			auth,
			chain.ChainID,
		)
		if err != nil {
			log.Error().
				Int64("chain_id", chain.ChainID).
				Str("chain_name", chain.Name).
				Str("rpc_url", chain.RPCURL).
				Err(err).
				Msg("Failed to create unified batch caller, skipping chain")
			continue
		}

		// Step 3e: Store the caller for this chain
		cm.callers[chain.ChainID] = caller

		// Step 3f: Create and start the consumer for this chain
		if err := cm.createChainConsumer(ctx, chain); err != nil {
			log.Error().
				Int64("chain_id", chain.ChainID).
				Str("chain_name", chain.Name).
				Err(err).
				Msg("Failed to create consumer for chain, skipping")
			// Remove the caller since consumer creation failed
			delete(cm.callers, chain.ChainID)
			continue
		}

		successfulChains++
		log.Info().
			Int64("chain_id", chain.ChainID).
			Str("chain_name", chain.Name).
			Msg("Successfully set up consumer for chain")
	}

	// Step 5: Log setup summary
	log.Info().
		Int("total_chains", len(chains)).
		Int("successful_chains", successfulChains).
		Int("failed_chains", len(chains)-successfulChains).
		Int("active_consumers", len(cm.consumers)).
		Msg("Consumer setup completed")

	// Return error if no chains were successfully set up
	if successfulChains == 0 && len(chains) > 0 {
		return fmt.Errorf("failed to set up consumers for any of the %d enabled chains", len(chains))
	}

	return nil
}

// createChainConsumer creates a consumer for a specific chain
func (cm *ConsumerManager) createChainConsumer(ctx context.Context, chain *models.Chain) error {
	log.Info().
		Int64("chain_id", chain.ChainID).
		Str("chain_name", chain.Name).
		Msg("Creating consumer for chain")

	// Get all tokens for this chain to determine queues
	tokens, err := cm.getEnabledTokensForChain(ctx, chain.ChainID)
	if err != nil {
		return fmt.Errorf("failed to get tokens for chain %d: %w", chain.ChainID, err)
	}

	// Generate queue names for this chain
	queueNames := cm.generateQueueNamesForChain(chain.ChainID, tokens)

	// Create new batch consumer for this chain
	consumer := NewRabbitMQBatchConsumerForChain(
		cm.client,
		cm.db,
		cm.batchOptimizer,
		cm.callers,
		cm.confirmationWatcher,
		cm.batchProcessor,
		chain.ChainID,
		queueNames,
		cm.workersPerChain,
	)

	// Start the consumer
	if err := consumer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start consumer for chain %d: %w", chain.ChainID, err)
	}

	// Store the chain consumer
	chainConsumer := &ChainBatchConsumer{
		ChainID:        chain.ChainID,
		ChainName:      chain.Name,
		Consumer:       consumer,
		QueueNames:     queueNames,
		IsActive:       true,
		StartedAt:      time.Now(),
		ProcessedCount: 0,
		ErrorCount:     0,
	}

	cm.consumers[chain.ChainID] = chainConsumer

	log.Info().
		Int64("chain_id", chain.ChainID).
		Str("chain_name", chain.Name).
		Strs("queues", queueNames).
		Int("workers", cm.workersPerChain).
		Msg("Chain consumer created and started")

	return nil
}

// generateQueueNamesForChain generates all queue names for a specific chain
func (cm *ConsumerManager) generateQueueNamesForChain(chainID int64, tokens []*models.SupportedToken) []string {
	var queueNames []string

	// Job types that need queues per chain/token
	jobTypes := []JobType{JobTypeTransfer, JobTypeAssetAdjust}

	// Add queues for token-based operations
	for _, jobType := range jobTypes {
		for _, token := range tokens {
			queueName := cm.client.GetQueueName(jobType, chainID, token.ID)
			queueNames = append(queueNames, queueName)
		}
	}

	// Add queues for NFT operations (these don't need token iteration)
	nftJobTypes := []JobType{JobTypeNFTMint, JobTypeNFTBurn, JobTypeNFTTransfer}
	for _, jobType := range nftJobTypes {
		queueName := cm.client.GetQueueName(jobType, chainID, 0) // Use token_id 0 for NFT queues
		queueNames = append(queueNames, queueName)
	}

	// Note: Notification queues are not consumed by this service
	// They are only published to for other systems to consume

	return queueNames
}

// runPeriodicRefresh periodically checks for new enabled chains and creates consumers
func (cm *ConsumerManager) runPeriodicRefresh(ctx context.Context) {
	defer cm.workerWg.Done()

	ticker := time.NewTicker(cm.refreshInterval)
	defer ticker.Stop()

	log.Info().Dur("interval", cm.refreshInterval).Msg("Starting periodic chain refresh")

	for {
		select {
		case <-ctx.Done():
			return
		case <-cm.stopChan:
			return
		case <-ticker.C:
			if err := cm.refreshChains(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to refresh chains")
			}
		}
	}
}

// refreshChains checks for new or disabled chains and updates consumers accordingly
func (cm *ConsumerManager) refreshChains(ctx context.Context) error {
	log.Debug().Msg("Refreshing chains configuration")

	chains, err := cm.getEnabledChains(ctx)
	if err != nil {
		return fmt.Errorf("failed to get enabled chains: %w", err)
	}

	enabledChains := make(map[int64]*models.Chain)
	for _, chain := range chains {
		enabledChains[chain.ChainID] = chain
	}

	cm.consumersMutex.Lock()
	defer cm.consumersMutex.Unlock()

	// Check for new chains
	for chainID, chain := range enabledChains {
		if _, exists := cm.consumers[chainID]; !exists {
			log.Info().
				Int64("chain_id", chainID).
				Str("chain_name", chain.Name).
				Msg("New enabled chain detected, creating consumer")

			if err := cm.createChainConsumer(ctx, chain); err != nil {
				log.Error().
					Int64("chain_id", chainID).
					Err(err).
					Msg("Failed to create consumer for new chain")
			}
		}
	}

	// Check for disabled chains
	for chainID, chainConsumer := range cm.consumers {
		if _, exists := enabledChains[chainID]; !exists {
			log.Info().
				Int64("chain_id", chainID).
				Str("chain_name", chainConsumer.ChainName).
				Msg("Chain disabled, stopping consumer")

			// Stop the consumer in background
			go func(consumer *ChainBatchConsumer) {
				if err := consumer.Consumer.Stop(ctx); err != nil {
					log.Error().
						Int64("chain_id", consumer.ChainID).
						Err(err).
						Msg("Failed to stop consumer for disabled chain")
				}
			}(chainConsumer)

			// Remove from active consumers
			delete(cm.consumers, chainID)
		}
	}

	return nil
}

// getEnabledChains retrieves all enabled chains from the database
func (cm *ConsumerManager) getEnabledChains(ctx context.Context) ([]*models.Chain, error) {
	chains, err := models.Chains(
		models.ChainWhere.IsEnabled.EQ(null.BoolFrom(true)),
		qm.OrderBy(models.ChainColumns.ChainID),
	).All(ctx, cm.db)

	if err != nil {
		return nil, fmt.Errorf("failed to query enabled chains: %w", err)
	}

	return chains, nil
}

// getEnabledTokensForChain retrieves all enabled tokens for a specific chain
func (cm *ConsumerManager) getEnabledTokensForChain(ctx context.Context, chainID int64) ([]*models.SupportedToken, error) {
	tokens, err := models.SupportedTokens(
		models.SupportedTokenWhere.ChainID.EQ(chainID),
		models.SupportedTokenWhere.IsEnabled.EQ(null.BoolFrom(true)),
		qm.OrderBy(models.SupportedTokenColumns.ID),
	).All(ctx, cm.db)

	if err != nil {
		return nil, fmt.Errorf("failed to query enabled tokens for chain %d: %w", chainID, err)
	}

	return tokens, nil
}

// GetQueueStats returns queue statistics from all chain consumers
func (cm *ConsumerManager) GetQueueStats() map[string]Stats {
	cm.consumersMutex.RLock()
	defer cm.consumersMutex.RUnlock()

	allStats := make(map[string]Stats)

	for chainID, chainConsumer := range cm.consumers {
		chainStats := chainConsumer.Consumer.GetQueueStats()
		for queueName, stats := range chainStats {
			// Prefix with chain ID for clarity
			prefixedName := fmt.Sprintf("chain_%d.%s", chainID, queueName)
			allStats[prefixedName] = stats
		}
	}

	return allStats
}

// IsHealthy checks if all chain consumers are healthy
func (cm *ConsumerManager) IsHealthy() bool {
	cm.consumersMutex.RLock()
	defer cm.consumersMutex.RUnlock()

	for chainID, chainConsumer := range cm.consumers {
		if !chainConsumer.Consumer.IsHealthy() {
			log.Warn().
				Int64("chain_id", chainID).
				Str("chain_name", chainConsumer.ChainName).
				Msg("Chain consumer is unhealthy")
			return false
		}
	}

	return true
}

// GetConsumerInfo returns information about all active consumers
func (cm *ConsumerManager) GetConsumerInfo() map[int64]*ChainBatchConsumer {
	cm.consumersMutex.RLock()
	defer cm.consumersMutex.RUnlock()

	result := make(map[int64]*ChainBatchConsumer)
	for chainID, consumer := range cm.consumers {
		// Create a copy to avoid data races
		result[chainID] = &ChainBatchConsumer{
			ChainID:        consumer.ChainID,
			ChainName:      consumer.ChainName,
			QueueNames:     append([]string{}, consumer.QueueNames...),
			IsActive:       consumer.IsActive,
			StartedAt:      consumer.StartedAt,
			ProcessedCount: consumer.ProcessedCount,
			ErrorCount:     consumer.ErrorCount,
		}
	}

	return result
}
