package tokens

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/rs/zerolog/log"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

// Service defines the tokens service interface
type Service interface {
	GetToken(ctx context.Context, tokenID int) (*TokenConfig, error)
	GetAllTokens(ctx context.Context, filters TokenFilters) ([]*TokenConfig, error)
	GetTokensByChain(ctx context.Context, chainID int64, filters TokenFilters) ([]*TokenConfig, error)
	CreateToken(ctx context.Context, request *CreateTokenRequest) (*TokenConfig, error)
	UpdateToken(ctx context.Context, tokenID int, request *UpdateTokenRequest) error
	DeleteToken(ctx context.Context, tokenID int) error
	ToggleTokenStatus(ctx context.Context, tokenID int, enabled bool) error
	RefreshCache(ctx context.Context) error

	// For other services
	GetEnabledTokens(ctx context.Context, chainID int64) ([]*TokenConfig, error)
	IsTokenSupported(ctx context.Context, chainID int64, contractAddress string) (bool, error)
}

// TokenConfig represents token configuration with typed fields
type TokenConfig struct {
	ID                      int                    `json:"id"`
	ChainID                 int64                  `json:"chain_id"`
	ContractAddress         *string                `json:"contract_address,omitempty"`
	Symbol                  string                 `json:"symbol"`
	Name                    string                 `json:"name"`
	Decimals                int                    `json:"decimals"`
	TokenType               string                 `json:"token_type"`
	SupportsBatchOperations bool                   `json:"supports_batch_operations"`
	BatchOperations         map[string]interface{} `json:"batch_operations,omitempty"`
	IsEnabled               bool                   `json:"is_enabled"`
	CreatedAt               time.Time              `json:"created_at"`
}

// TokenFilters represents filtering options for tokens
type TokenFilters struct {
	ChainID          *int64 `json:"chain_id,omitempty"`
	EnabledOnly      bool   `json:"enabled_only"`
	BatchSupportOnly bool   `json:"batch_support_only"`
}

// CreateTokenRequest represents a token creation request
type CreateTokenRequest struct {
	ChainID                 int64                  `json:"chain_id"`
	ContractAddress         *string                `json:"contract_address,omitempty"`
	Symbol                  string                 `json:"symbol"`
	Name                    string                 `json:"name"`
	Decimals                int                    `json:"decimals"`
	TokenType               string                 `json:"token_type"`
	SupportsBatchOperations *bool                  `json:"supports_batch_operations,omitempty"`
	BatchOperations         map[string]interface{} `json:"batch_operations,omitempty"`
}

// UpdateTokenRequest represents a token update request
type UpdateTokenRequest struct {
	Name                    *string                `json:"name,omitempty"`
	SupportsBatchOperations *bool                  `json:"supports_batch_operations,omitempty"`
	BatchOperations         map[string]interface{} `json:"batch_operations,omitempty"`
	IsEnabled               *bool                  `json:"is_enabled,omitempty"`
}

// serviceImpl implements the Service interface
type serviceImpl struct {
	db    *sql.DB
	cache map[int64][]*TokenConfig
	mutex sync.RWMutex
}

// New creates a new tokens service instance
func New(db *sql.DB) Service {
	return &serviceImpl{
		db:    db,
		cache: make(map[int64][]*TokenConfig),
		mutex: sync.RWMutex{},
	}
}

// GetToken retrieves a specific token by ID
func (s *serviceImpl) GetToken(ctx context.Context, tokenID int) (*TokenConfig, error) {
	token, err := models.FindSupportedToken(ctx, s.db, tokenID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("token with ID %d not found", tokenID)
		}
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	return s.convertToTokenConfig(token), nil
}

// GetAllTokens retrieves all tokens with optional filters
func (s *serviceImpl) GetAllTokens(ctx context.Context, filters TokenFilters) ([]*TokenConfig, error) {
	var queryMods []qm.QueryMod

	if filters.ChainID != nil {
		queryMods = append(queryMods, qm.Where("chain_id = ?", *filters.ChainID))
	}

	if filters.EnabledOnly {
		queryMods = append(queryMods, qm.Where("is_enabled = true"))
	}

	if filters.BatchSupportOnly {
		queryMods = append(queryMods, qm.Where("supports_batch_operations = true"))
	}

	queryMods = append(queryMods, qm.OrderBy("chain_id, symbol"))

	tokens, err := models.SupportedTokens(queryMods...).All(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("failed to get tokens: %w", err)
	}

	configs := make([]*TokenConfig, len(tokens))
	for i, token := range tokens {
		configs[i] = s.convertToTokenConfig(token)
	}

	return configs, nil
}

// GetTokensByChain retrieves tokens for a specific chain
func (s *serviceImpl) GetTokensByChain(ctx context.Context, chainID int64, filters TokenFilters) ([]*TokenConfig, error) {
	filters.ChainID = &chainID
	return s.GetAllTokens(ctx, filters)
}

// CreateToken creates a new supported token
func (s *serviceImpl) CreateToken(ctx context.Context, request *CreateTokenRequest) (*TokenConfig, error) {
	// Check if chain exists
	chainExists, err := models.Chains(qm.Where("chain_id = ?", request.ChainID)).Exists(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("failed to check chain existence: %w", err)
	}
	if !chainExists {
		return nil, fmt.Errorf("chain with ID %d does not exist", request.ChainID)
	}

	// Check for existing token (chain_id + contract_address combination)
	var existingQuery []qm.QueryMod
	existingQuery = append(existingQuery, qm.Where("chain_id = ?", request.ChainID))

	if request.ContractAddress != nil {
		existingQuery = append(existingQuery, qm.Where("contract_address = ?", *request.ContractAddress))
	} else {
		existingQuery = append(existingQuery, qm.Where("contract_address IS NULL"))
	}

	exists, err := models.SupportedTokens(existingQuery...).Exists(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("failed to check token existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("token already exists for chain %d with contract address %v", request.ChainID, request.ContractAddress)
	}

	// Create new token
	token := &models.SupportedToken{
		ChainID:   request.ChainID,
		Symbol:    request.Symbol,
		Name:      request.Name,
		Decimals:  request.Decimals,
		TokenType: request.TokenType,
		IsEnabled: null.BoolFrom(true),
		CreatedAt: null.TimeFrom(time.Now()),
	}

	if request.ContractAddress != nil {
		token.ContractAddress = null.StringFrom(*request.ContractAddress)
	}

	if request.SupportsBatchOperations != nil {
		token.SupportsBatchOperations = null.BoolFrom(*request.SupportsBatchOperations)
	}

	if request.BatchOperations != nil {
		batchOpsBytes, err := json.Marshal(request.BatchOperations)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal batch operations: %w", err)
		}
		token.BatchOperations = null.JSONFrom(batchOpsBytes)
	}

	err = token.Insert(ctx, s.db, boil.Infer())
	if err != nil {
		return nil, fmt.Errorf("failed to create token: %w", err)
	}

	s.invalidateCache()

	return s.convertToTokenConfig(token), nil
}

// UpdateToken updates an existing token
func (s *serviceImpl) UpdateToken(ctx context.Context, tokenID int, request *UpdateTokenRequest) error {
	token, err := models.FindSupportedToken(ctx, s.db, tokenID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("token with ID %d not found", tokenID)
		}
		return fmt.Errorf("failed to find token: %w", err)
	}

	// Update fields if provided
	if request.Name != nil {
		token.Name = *request.Name
	}

	if request.SupportsBatchOperations != nil {
		token.SupportsBatchOperations = null.BoolFrom(*request.SupportsBatchOperations)
	}

	if request.BatchOperations != nil {
		batchOpsBytes, err := json.Marshal(request.BatchOperations)
		if err != nil {
			return fmt.Errorf("failed to marshal batch operations: %w", err)
		}
		token.BatchOperations = null.JSONFrom(batchOpsBytes)
	}

	if request.IsEnabled != nil {
		token.IsEnabled = null.BoolFrom(*request.IsEnabled)
	}

	_, err = token.Update(ctx, s.db, boil.Infer())
	if err != nil {
		return fmt.Errorf("failed to update token: %w", err)
	}

	s.invalidateCache()

	return nil
}

// DeleteToken removes a token
func (s *serviceImpl) DeleteToken(ctx context.Context, tokenID int) error {
	// Check if token is in use (has transactions or balances)
	hasTransactions, err := models.Transactions(qm.Where("token_id = ?", tokenID)).Exists(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to check token usage in transactions: %w", err)
	}
	if hasTransactions {
		return fmt.Errorf("token is in use and cannot be deleted")
	}

	hasBalances, err := models.UserBalances(qm.Where("token_id = ?", tokenID)).Exists(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to check token usage in balances: %w", err)
	}
	if hasBalances {
		return fmt.Errorf("token is in use and cannot be deleted")
	}

	token, err := models.FindSupportedToken(ctx, s.db, tokenID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("token with ID %d not found", tokenID)
		}
		return fmt.Errorf("failed to find token: %w", err)
	}

	_, err = token.Delete(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}

	s.invalidateCache()

	return nil
}

// ToggleTokenStatus enables or disables a token
func (s *serviceImpl) ToggleTokenStatus(ctx context.Context, tokenID int, enabled bool) error {
	token, err := models.FindSupportedToken(ctx, s.db, tokenID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("token with ID %d not found", tokenID)
		}
		return fmt.Errorf("failed to find token: %w", err)
	}

	token.IsEnabled = null.BoolFrom(enabled)

	_, err = token.Update(ctx, s.db, boil.Whitelist(models.SupportedTokenColumns.IsEnabled))
	if err != nil {
		return fmt.Errorf("failed to update token status: %w", err)
	}

	s.invalidateCache()

	return nil
}

// RefreshCache clears the token cache
func (s *serviceImpl) RefreshCache(ctx context.Context) error {
	s.invalidateCache()
	log.Info().Msg("Token cache refreshed")
	return nil
}

// GetEnabledTokens retrieves enabled tokens for a chain
func (s *serviceImpl) GetEnabledTokens(ctx context.Context, chainID int64) ([]*TokenConfig, error) {
	s.mutex.RLock()
	cached, exists := s.cache[chainID]
	s.mutex.RUnlock()

	if exists {
		return cached, nil
	}

	filters := TokenFilters{
		ChainID:     &chainID,
		EnabledOnly: true,
	}
	tokens, err := s.GetAllTokens(ctx, filters)
	if err != nil {
		return nil, err
	}

	s.mutex.Lock()
	s.cache[chainID] = tokens
	s.mutex.Unlock()

	return tokens, nil
}

// IsTokenSupported checks if a token is supported on a chain
func (s *serviceImpl) IsTokenSupported(ctx context.Context, chainID int64, contractAddress string) (bool, error) {
	var queryMods []qm.QueryMod
	queryMods = append(queryMods, qm.Where("chain_id = ?", chainID))
	queryMods = append(queryMods, qm.Where("is_enabled = true"))

	if contractAddress != "" {
		queryMods = append(queryMods, qm.Where("contract_address = ?", contractAddress))
	} else {
		queryMods = append(queryMods, qm.Where("contract_address IS NULL"))
	}

	return models.SupportedTokens(queryMods...).Exists(ctx, s.db)
}

// convertToTokenConfig converts a database model to service config
func (s *serviceImpl) convertToTokenConfig(token *models.SupportedToken) *TokenConfig {
	config := &TokenConfig{
		ID:                      token.ID,
		ChainID:                 token.ChainID,
		Symbol:                  token.Symbol,
		Name:                    token.Name,
		Decimals:                token.Decimals,
		TokenType:               token.TokenType,
		SupportsBatchOperations: token.SupportsBatchOperations.Bool,
		IsEnabled:               token.IsEnabled.Bool,
		CreatedAt:               token.CreatedAt.Time,
	}

	if token.ContractAddress.Valid {
		config.ContractAddress = &token.ContractAddress.String
	}

	if token.BatchOperations.Valid {
		var batchOps map[string]interface{}
		if err := token.BatchOperations.Unmarshal(&batchOps); err == nil {
			config.BatchOperations = batchOps
		}
	}

	return config
}

// invalidateCache clears the cache
func (s *serviceImpl) invalidateCache() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cache = make(map[int64][]*TokenConfig)
}
