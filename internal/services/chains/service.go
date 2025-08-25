package chains

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/rs/zerolog/log"
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
	WalletManagerAddress    string      `json:"wallet_manager_address,omitempty"`
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

// service implements the chains service
type service struct {
	db              *sql.DB
	cache           map[int64]*ChainConfig
	mutex           sync.RWMutex
	lastCacheUpdate time.Time
	cacheTimeout    time.Duration
}

// NewService creates a new chains service
func NewService(db *sql.DB) Service {
	return &service{
		db:           db,
		cache:        make(map[int64]*ChainConfig),
		cacheTimeout: 5 * time.Minute, // Cache for 5 minutes
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
		if err == sql.ErrNoRows {
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
	// Validate configuration
	if err := s.validateBatchConfig(config); err != nil {
		return fmt.Errorf("invalid batch config: %w", err)
	}

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
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Clear cache
	s.cache = make(map[int64]*ChainConfig)
	s.lastCacheUpdate = time.Time{}

	// Pre-load all enabled chains
	_, err := s.GetAllEnabledChains(ctx)
	if err != nil {
		return fmt.Errorf("failed to refresh chain cache: %w", err)
	}

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
	if chain.WalletManagerAddress.Valid {
		config.WalletManagerAddress = chain.WalletManagerAddress.String
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

func (s *service) validateBatchConfig(config *BatchConfig) error {
	if config.MinBatchSize <= 0 {
		return fmt.Errorf("min_batch_size must be greater than 0")
	}
	if config.MaxBatchSize <= config.MinBatchSize {
		return fmt.Errorf("max_batch_size must be greater than min_batch_size")
	}
	if config.OptimalBatchSize < config.MinBatchSize || config.OptimalBatchSize > config.MaxBatchSize {
		return fmt.Errorf("optimal_batch_size must be between min_batch_size and max_batch_size")
	}
	if config.MaxBatchSize > 100 {
		return fmt.Errorf("max_batch_size cannot exceed 100")
	}
	return nil
}
