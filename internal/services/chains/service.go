package chains

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/hzbay/chain-bridge/internal/blockchain"
	"github.com/hzbay/chain-bridge/internal/config"
	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/rs/zerolog/log"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

// Service defines the chains service interface
type Service interface {
	GetChainConfig(ctx context.Context, chainID int64) (*ChainConfig, error)
	GetAllEnabledChains(ctx context.Context) ([]*ChainConfig, error)
	GetBatchConfig(ctx context.Context, chainID int64) (*BatchConfig, error)
	UpdateBatchConfig(ctx context.Context, chainID int64, config *BatchConfig) error
	IsChainEnabled(ctx context.Context, chainID int64) (bool, error)
	RefreshCache(ctx context.Context) error

	// CRUD operations for chain management
	CreateChain(ctx context.Context, request *CreateChainRequest) (*ChainConfig, error)
	UpdateChain(ctx context.Context, chainID int64, request *UpdateChainRequest) error
	DeleteChain(ctx context.Context, chainID int64) error
	ToggleChainStatus(ctx context.Context, chainID int64, enabled bool) error

	// 为其他服务提供 CPOP 配置
	GetCPOPConfigs(ctx context.Context) (map[int64]blockchain.CPOPConfig, error)
	GetCPOPConfig(ctx context.Context, chainID int64) (*blockchain.CPOPConfig, error)
	GetValidatedChains(ctx context.Context) (map[int64]*ChainConfig, error)
}

// ChainConfig represents chain configuration with typed fields
type ChainConfig struct {
	ChainID                 int64       `json:"chain_id"`
	Name                    string      `json:"name"`
	ShortName               string      `json:"short_name"`
	RPCURL                  string      `json:"rpc_url"`
	ExplorerURL             string      `json:"explorer_url,omitempty"`
	EntryPointAddress       string      `json:"entry_point_address,omitempty"`
	CpopTokenAddress        string      `json:"cpop_token_address,omitempty"`
	MasterAggregatorAddress string      `json:"master_aggregator_address,omitempty"`
	AccountManagerAddress   string      `json:"account_manager_address,omitempty"`
	BatchConfig             BatchConfig `json:"batch_config"`
	IsEnabled               bool        `json:"is_enabled"`
	CreatedAt               time.Time   `json:"created_at"`
}

// BatchConfig represents batch processing configuration for a chain
type BatchConfig struct {
	OptimalBatchSize int `json:"optimal_batch_size"`
	MaxBatchSize     int `json:"max_batch_size"`
	MinBatchSize     int `json:"min_batch_size"`
}

// DefaultBatchConfig provides default batch configuration
var DefaultBatchConfig = BatchConfig{
	OptimalBatchSize: 25,
	MaxBatchSize:     40,
	MinBatchSize:     10,
}

// CreateChainRequest represents a request to create a new chain
type CreateChainRequest struct {
	ChainID                 int64   `json:"chain_id"`
	Name                    string  `json:"name"`
	ShortName               string  `json:"short_name"`
	RPCURL                  string  `json:"rpc_url"`
	ExplorerURL             *string `json:"explorer_url,omitempty"`
	EntryPointAddress       *string `json:"entry_point_address,omitempty"`
	CpopTokenAddress        *string `json:"cpop_token_address,omitempty"`
	MasterAggregatorAddress *string `json:"master_aggregator_address,omitempty"`
	AccountManagerAddress   *string `json:"account_manager_address,omitempty"`
	OptimalBatchSize        *int    `json:"optimal_batch_size,omitempty"`
	MaxBatchSize            *int    `json:"max_batch_size,omitempty"`
	MinBatchSize            *int    `json:"min_batch_size,omitempty"`
	IsEnabled               *bool   `json:"is_enabled,omitempty"`
}

// UpdateChainRequest represents a request to update chain configuration
type UpdateChainRequest struct {
	Name                    *string `json:"name,omitempty"`
	ShortName               *string `json:"short_name,omitempty"`
	RPCURL                  *string `json:"rpc_url,omitempty"`
	ExplorerURL             *string `json:"explorer_url,omitempty"`
	EntryPointAddress       *string `json:"entry_point_address,omitempty"`
	CpopTokenAddress        *string `json:"cpop_token_address,omitempty"`
	MasterAggregatorAddress *string `json:"master_aggregator_address,omitempty"`
	AccountManagerAddress   *string `json:"account_manager_address,omitempty"`
	OptimalBatchSize        *int    `json:"optimal_batch_size,omitempty"`
	MaxBatchSize            *int    `json:"max_batch_size,omitempty"`
	MinBatchSize            *int    `json:"min_batch_size,omitempty"`
	IsEnabled               *bool   `json:"is_enabled,omitempty"`
}

// service implements the chains service
type service struct {
	db               *sql.DB
	cache            map[int64]*ChainConfig
	mutex            sync.RWMutex
	lastCacheUpdate  time.Time
	cacheTimeout     time.Duration
	blockchainConfig config.BlockchainConfig
}

// NewService creates a new chains service
func NewService(db *sql.DB, blockchainConfig config.BlockchainConfig) Service {
	return &service{
		db:               db,
		cache:            make(map[int64]*ChainConfig),
		cacheTimeout:     5 * time.Minute, // Cache for 5 minutes
		blockchainConfig: blockchainConfig,
	}
}

// GetChainConfig retrieves chain configuration by chain ID
func (s *service) GetChainConfig(ctx context.Context, chainID int64) (*ChainConfig, error) {
	// Check cache first
	if config := s.getCachedConfig(chainID); config != nil {
		return config, nil
	}

	// Fetch from database
	chain, err := models.Chains(
		qm.Where("chain_id = ?", chainID),
	).One(ctx, s.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("chain %d not found", chainID)
		}
		return nil, fmt.Errorf("failed to fetch chain config: %w", err)
	}

	config := s.convertToChainConfig(chain)

	// Update cache
	s.setCachedConfig(chainID, config)

	return config, nil
}

// GetAllEnabledChains retrieves all enabled chains
func (s *service) GetAllEnabledChains(ctx context.Context) ([]*ChainConfig, error) {
	chains, err := models.Chains(
		qm.Where("is_enabled = ?", true),
		qm.OrderBy("chain_id"),
	).All(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch enabled chains: %w", err)
	}

	var configs []*ChainConfig
	for _, chain := range chains {
		config := s.convertToChainConfig(chain)
		configs = append(configs, config)

		// Update cache
		s.setCachedConfig(config.ChainID, config)
	}

	return configs, nil
}

// GetBatchConfig retrieves batch configuration for a specific chain
func (s *service) GetBatchConfig(ctx context.Context, chainID int64) (*BatchConfig, error) {
	config, err := s.GetChainConfig(ctx, chainID)
	if err != nil {
		return nil, err
	}

	return &config.BatchConfig, nil
}

// UpdateBatchConfig updates batch configuration for a specific chain
func (s *service) UpdateBatchConfig(ctx context.Context, chainID int64, config *BatchConfig) error {

	// Update database
	chain, err := models.Chains(
		qm.Where("chain_id = ?", chainID),
	).One(ctx, s.db)
	if err != nil {
		return fmt.Errorf("chain %d not found: %w", chainID, err)
	}

	// Update batch configuration fields
	chain.OptimalBatchSize.SetValid(config.OptimalBatchSize)
	chain.MaxBatchSize.SetValid(config.MaxBatchSize)
	chain.MinBatchSize.SetValid(config.MinBatchSize)

	// Save to database
	if _, err := chain.Update(ctx, s.db, boil.Infer()); err != nil {
		return fmt.Errorf("failed to update batch config: %w", err)
	}

	// Invalidate cache for this chain
	s.invalidateCache(chainID)

	log.Info().
		Int64("chain_id", chainID).
		Int("optimal_batch_size", config.OptimalBatchSize).
		Int("max_batch_size", config.MaxBatchSize).
		Int("min_batch_size", config.MinBatchSize).
		Msg("Updated batch configuration for chain")

	return nil
}

// IsChainEnabled checks if a chain is enabled
func (s *service) IsChainEnabled(ctx context.Context, chainID int64) (bool, error) {
	config, err := s.GetChainConfig(ctx, chainID)
	if err != nil {
		return false, err
	}
	return config.IsEnabled, nil
}

// RefreshCache force refreshes the cache
func (s *service) RefreshCache(ctx context.Context) error {
	// Fetch chains from database without holding lock
	chains, err := models.Chains(
		qm.Where("is_enabled = ?", true),
		qm.OrderBy("chain_id"),
	).All(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to fetch enabled chains: %w", err)
	}

	// Convert all chains to configs
	var configs []*ChainConfig
	for _, chain := range chains {
		config := s.convertToChainConfig(chain)
		configs = append(configs, config)
	}

	// Now acquire lock and update cache atomically
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Clear cache
	s.cache = make(map[int64]*ChainConfig)

	// Populate cache with new data
	for _, config := range configs {
		s.cache[config.ChainID] = config
	}
	s.lastCacheUpdate = time.Now()

	log.Info().Msg("Chain configuration cache refreshed")
	return nil
}

// Helper methods

func (s *service) getCachedConfig(chainID int64) *ChainConfig {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Check if cache is expired
	if time.Since(s.lastCacheUpdate) > s.cacheTimeout {
		return nil
	}

	return s.cache[chainID]
}

func (s *service) setCachedConfig(chainID int64, config *ChainConfig) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.cache[chainID] = config
	s.lastCacheUpdate = time.Now()
}

func (s *service) invalidateCache(chainID int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.cache, chainID)
}

func (s *service) convertToChainConfig(chain *models.Chain) *ChainConfig {
	config := &ChainConfig{
		ChainID:   chain.ChainID,
		Name:      chain.Name,
		ShortName: chain.ShortName,
		RPCURL:    chain.RPCURL,
		IsEnabled: chain.IsEnabled.Bool, // null.Bool defaults to false if invalid
	}

	// Handle nullable fields
	if chain.ExplorerURL.Valid {
		config.ExplorerURL = chain.ExplorerURL.String
	}
	if chain.EntryPointAddress.Valid {
		config.EntryPointAddress = chain.EntryPointAddress.String
	}
	if chain.CpopTokenAddress.Valid {
		config.CpopTokenAddress = chain.CpopTokenAddress.String
	}
	if chain.MasterAggregatorAddress.Valid {
		config.MasterAggregatorAddress = chain.MasterAggregatorAddress.String
	}
	if chain.AccountManagerAddress.Valid {
		config.AccountManagerAddress = chain.AccountManagerAddress.String
	}
	if chain.CreatedAt.Valid {
		config.CreatedAt = chain.CreatedAt.Time
	}

	// Handle batch configuration with defaults
	config.BatchConfig = BatchConfig{
		OptimalBatchSize: DefaultBatchConfig.OptimalBatchSize,
		MaxBatchSize:     DefaultBatchConfig.MaxBatchSize,
		MinBatchSize:     DefaultBatchConfig.MinBatchSize,
	}

	if chain.OptimalBatchSize.Valid {
		config.BatchConfig.OptimalBatchSize = chain.OptimalBatchSize.Int
	}
	if chain.MaxBatchSize.Valid {
		config.BatchConfig.MaxBatchSize = chain.MaxBatchSize.Int
	}
	if chain.MinBatchSize.Valid {
		config.BatchConfig.MinBatchSize = chain.MinBatchSize.Int
	}

	return config
}

// GetCPOPConfigs 获取所有启用链的 CPOP 配置
func (s *service) GetCPOPConfigs(ctx context.Context) (map[int64]blockchain.CPOPConfig, error) {
	chains, err := s.GetAllEnabledChains(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled chains: %w", err)
	}

	configs := make(map[int64]blockchain.CPOPConfig)
	for _, chain := range chains {
		if err := s.validateChainForCPOP(chain); err != nil {
			log.Warn().
				Int64("chain_id", chain.ChainID).
				Str("chain_name", chain.Name).
				Err(err).
				Msg("Skipping chain due to incomplete configuration")
			continue
		}

		config := blockchain.CPOPConfig{
			RPCEndpoint:           chain.RPCURL,
			AccountManagerAddress: common.HexToAddress(chain.AccountManagerAddress),
			EntryPointAddress:     common.HexToAddress(chain.EntryPointAddress),
			GasPriceFactor:        s.blockchainConfig.DefaultGasPriceFactor,
			DefaultGasLimit:       s.blockchainConfig.DefaultGasLimit,
		}
		configs[chain.ChainID] = config
	}

	return configs, nil
}

// GetCPOPConfig 获取单个链的 CPOP 配置
func (s *service) GetCPOPConfig(ctx context.Context, chainID int64) (*blockchain.CPOPConfig, error) {
	chain, err := s.GetChainConfig(ctx, chainID)
	if err != nil {
		return nil, err
	}

	if !chain.IsEnabled {
		return nil, fmt.Errorf("chain %d is disabled", chainID)
	}

	if err := s.validateChainForCPOP(chain); err != nil {
		return nil, fmt.Errorf("chain %d configuration invalid: %w", chainID, err)
	}

	config := &blockchain.CPOPConfig{
		RPCEndpoint:           chain.RPCURL,
		AccountManagerAddress: common.HexToAddress(chain.AccountManagerAddress),
		EntryPointAddress:     common.HexToAddress(chain.EntryPointAddress),
		GasPriceFactor:        s.blockchainConfig.DefaultGasPriceFactor,
		DefaultGasLimit:       s.blockchainConfig.DefaultGasLimit,
	}

	return config, nil
}

// GetValidatedChains 获取所有验证过的链配置
func (s *service) GetValidatedChains(ctx context.Context) (map[int64]*ChainConfig, error) {
	allChains, err := s.GetAllEnabledChains(ctx)
	if err != nil {
		return nil, err
	}

	validChains := make(map[int64]*ChainConfig)
	for _, chain := range allChains {
		if err := s.validateChainForCPOP(chain); err == nil {
			validChains[chain.ChainID] = chain
		}
	}

	return validChains, nil
}

// validateChainForCPOP 验证链配置是否满足 CPOP 要求
func (s *service) validateChainForCPOP(chain *ChainConfig) error {
	if chain.RPCURL == "" {
		return fmt.Errorf("RPC URL is required")
	}
	if chain.AccountManagerAddress == "" {
		return fmt.Errorf("Account Manager Address is required")
	}
	if chain.EntryPointAddress == "" {
		return fmt.Errorf("Entry Point Address is required")
	}
	return nil
}

// CreateChain creates a new chain configuration
func (s *service) CreateChain(ctx context.Context, request *CreateChainRequest) (*ChainConfig, error) {
	// Check if chain already exists
	exists, err := models.Chains(
		qm.Where("chain_id = ?", request.ChainID),
	).Exists(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("failed to check chain existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("chain with ID %d already exists", request.ChainID)
	}

	// Create new chain model
	chain := &models.Chain{
		ChainID:   request.ChainID,
		Name:      request.Name,
		ShortName: request.ShortName,
		RPCURL:    request.RPCURL,
		IsEnabled: null.BoolFrom(true), // Default to enabled
	}

	// Set optional fields
	if request.ExplorerURL != nil {
		chain.ExplorerURL = null.StringFrom(*request.ExplorerURL)
	}
	if request.EntryPointAddress != nil {
		chain.EntryPointAddress = null.StringFrom(*request.EntryPointAddress)
	}
	if request.CpopTokenAddress != nil {
		chain.CpopTokenAddress = null.StringFrom(*request.CpopTokenAddress)
	}
	if request.MasterAggregatorAddress != nil {
		chain.MasterAggregatorAddress = null.StringFrom(*request.MasterAggregatorAddress)
	}
	if request.AccountManagerAddress != nil {
		chain.AccountManagerAddress = null.StringFrom(*request.AccountManagerAddress)
	}
	if request.OptimalBatchSize != nil {
		chain.OptimalBatchSize = null.IntFrom(*request.OptimalBatchSize)
	} else {
		chain.OptimalBatchSize = null.IntFrom(DefaultBatchConfig.OptimalBatchSize)
	}
	if request.MaxBatchSize != nil {
		chain.MaxBatchSize = null.IntFrom(*request.MaxBatchSize)
	} else {
		chain.MaxBatchSize = null.IntFrom(DefaultBatchConfig.MaxBatchSize)
	}
	if request.MinBatchSize != nil {
		chain.MinBatchSize = null.IntFrom(*request.MinBatchSize)
	} else {
		chain.MinBatchSize = null.IntFrom(DefaultBatchConfig.MinBatchSize)
	}
	if request.IsEnabled != nil {
		chain.IsEnabled = null.BoolFrom(*request.IsEnabled)
	}

	// Insert into database
	if err := chain.Insert(ctx, s.db, boil.Infer()); err != nil {
		return nil, fmt.Errorf("failed to create chain: %w", err)
	}

	log.Info().
		Int64("chain_id", request.ChainID).
		Str("name", request.Name).
		Str("short_name", request.ShortName).
		Msg("Chain created successfully")

	// Convert to ChainConfig and cache
	config := s.convertToChainConfig(chain)
	s.setCachedConfig(config.ChainID, config)

	return config, nil
}

// UpdateChain updates an existing chain configuration
func (s *service) UpdateChain(ctx context.Context, chainID int64, request *UpdateChainRequest) error {
	// Find existing chain
	chain, err := models.Chains(
		qm.Where("chain_id = ?", chainID),
	).One(ctx, s.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("chain %d not found", chainID)
		}
		return fmt.Errorf("failed to fetch chain: %w", err)
	}

	// Update fields if provided
	if request.Name != nil {
		chain.Name = *request.Name
	}
	if request.ShortName != nil {
		chain.ShortName = *request.ShortName
	}
	if request.RPCURL != nil {
		chain.RPCURL = *request.RPCURL
	}
	if request.ExplorerURL != nil {
		chain.ExplorerURL = null.StringFrom(*request.ExplorerURL)
	}
	if request.EntryPointAddress != nil {
		chain.EntryPointAddress = null.StringFrom(*request.EntryPointAddress)
	}
	if request.CpopTokenAddress != nil {
		chain.CpopTokenAddress = null.StringFrom(*request.CpopTokenAddress)
	}
	if request.MasterAggregatorAddress != nil {
		chain.MasterAggregatorAddress = null.StringFrom(*request.MasterAggregatorAddress)
	}
	if request.AccountManagerAddress != nil {
		chain.AccountManagerAddress = null.StringFrom(*request.AccountManagerAddress)
	}
	if request.OptimalBatchSize != nil {
		chain.OptimalBatchSize = null.IntFrom(*request.OptimalBatchSize)
	}
	if request.MaxBatchSize != nil {
		chain.MaxBatchSize = null.IntFrom(*request.MaxBatchSize)
	}
	if request.MinBatchSize != nil {
		chain.MinBatchSize = null.IntFrom(*request.MinBatchSize)
	}
	if request.IsEnabled != nil {
		chain.IsEnabled = null.BoolFrom(*request.IsEnabled)
	}

	// Update in database
	if _, err := chain.Update(ctx, s.db, boil.Infer()); err != nil {
		return fmt.Errorf("failed to update chain: %w", err)
	}

	// Invalidate cache
	s.invalidateCache(chainID)

	log.Info().
		Int64("chain_id", chainID).
		Msg("Chain configuration updated successfully")

	return nil
}

// DeleteChain removes a chain configuration
func (s *service) DeleteChain(ctx context.Context, chainID int64) error {
	// Check if chain exists
	chain, err := models.Chains(
		qm.Where("chain_id = ?", chainID),
	).One(ctx, s.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("chain %d not found", chainID)
		}
		return fmt.Errorf("failed to fetch chain: %w", err)
	}

	// Check for chain usage before allowing deletion
	if err := s.validateChainCanBeDeleted(ctx, chainID); err != nil {
		return err
	}

	// Delete from database
	if _, err := chain.Delete(ctx, s.db); err != nil {
		return fmt.Errorf("failed to delete chain: %w", err)
	}

	// Remove from cache
	s.invalidateCache(chainID)

	log.Info().
		Int64("chain_id", chainID).
		Str("name", chain.Name).
		Msg("Chain deleted successfully")

	return nil
}

// ToggleChainStatus enables or disables a chain
func (s *service) ToggleChainStatus(ctx context.Context, chainID int64, enabled bool) error {
	// Find existing chain
	chain, err := models.Chains(
		qm.Where("chain_id = ?", chainID),
	).One(ctx, s.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("chain %d not found", chainID)
		}
		return fmt.Errorf("failed to fetch chain: %w", err)
	}

	// Update enabled status
	chain.IsEnabled = null.BoolFrom(enabled)

	// Save to database
	if _, err := chain.Update(ctx, s.db, boil.Infer()); err != nil {
		return fmt.Errorf("failed to update chain status: %w", err)
	}

	// Invalidate cache
	s.invalidateCache(chainID)

	status := "disabled"
	if enabled {
		status = "enabled"
	}

	log.Info().
		Int64("chain_id", chainID).
		Str("status", status).
		Msg("Chain status updated successfully")

	return nil
}

// validateChainCanBeDeleted checks if a chain can safely be deleted
func (s *service) validateChainCanBeDeleted(ctx context.Context, chainID int64) error {
	// Check if there are any supported tokens on this chain
	tokenCount, err := models.SupportedTokens(
		models.SupportedTokenWhere.ChainID.EQ(chainID),
	).Count(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to check tokens on chain: %w", err)
	}
	if tokenCount > 0 {
		return fmt.Errorf("cannot delete chain %d: %d tokens are configured on this chain", chainID, tokenCount)
	}

	// Check if there are any user accounts on this chain
	accountCount, err := models.UserAccounts(
		models.UserAccountWhere.ChainID.EQ(chainID),
	).Count(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to check user accounts on chain: %w", err)
	}
	if accountCount > 0 {
		return fmt.Errorf("cannot delete chain %d: %d user accounts exist on this chain", chainID, accountCount)
	}

	// Check if there are any transactions on this chain
	transactionCount, err := models.Transactions(
		models.TransactionWhere.ChainID.EQ(chainID),
	).Count(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to check transactions on chain: %w", err)
	}
	if transactionCount > 0 {
		return fmt.Errorf("cannot delete chain %d: %d transactions exist on this chain", chainID, transactionCount)
	}

	// Check if there are any user balances on this chain
	balanceCount, err := models.UserBalances(
		models.UserBalanceWhere.ChainID.EQ(chainID),
	).Count(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to check user balances on chain: %w", err)
	}
	if balanceCount > 0 {
		return fmt.Errorf("cannot delete chain %d: %d user balances exist on this chain", chainID, balanceCount)
	}

	log.Info().
		Int64("chain_id", chainID).
		Msg("Chain validation passed - safe to delete")

	return nil
}
