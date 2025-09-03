package nft

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hzbay/chain-bridge/internal/queue"
	"github.com/rs/zerolog/log"
)

// Service defines the interface for NFT operations
type Service interface {
	// Batch operations
	BatchMintNFTs(ctx context.Context, request *BatchMintRequest) (*BatchMintResponse, *BatchInfo, error)
	BatchBurnNFTs(ctx context.Context, request *BatchBurnRequest) (*BatchBurnResponse, *BatchInfo, error)
	BatchTransferNFTs(ctx context.Context, request *BatchTransferRequest) (*BatchTransferResponse, *BatchInfo, error)

	// Collection management
	GetCollection(ctx context.Context, collectionID string) (*Collection, error)
	GetCollectionsByChain(ctx context.Context, chainID int64) ([]*Collection, error)

	// NFT asset management
	GetNFTAsset(ctx context.Context, collectionID, tokenID string) (*NFTAsset, error)
	GetUserNFTAssets(ctx context.Context, userID string, chainID *int64) ([]*NFTAsset, error)
}

// service implements the NFT Service interface
type service struct {
	db             *sql.DB
	batchProcessor queue.BatchProcessor
}

// NewService creates a new NFT service
func NewService(db *sql.DB, batchProcessor queue.BatchProcessor) Service {
	return &service{
		db:             db,
		batchProcessor: batchProcessor,
	}
}

// BatchMintRequest represents a batch mint request
type BatchMintRequest struct {
	OperationID      string            `json:"operation_id"`
	CollectionID     string            `json:"collection_id"`
	ChainID          int64             `json:"chain_id"`
	MintOperations   []MintOperation   `json:"mint_operations"`
	BatchPreferences *BatchPreferences `json:"batch_preferences,omitempty"`
}

// MintOperation represents a single mint operation
type MintOperation struct {
	ToUserID     string       `json:"to_user_id"`
	BusinessType string       `json:"business_type"`
	ReasonType   string       `json:"reason_type"`
	ReasonDetail *string      `json:"reason_detail,omitempty"`
	Meta         *NFTMetadata `json:"meta,omitempty"`
}

// BatchBurnRequest represents a batch burn request
type BatchBurnRequest struct {
	OperationID      string            `json:"operation_id"`
	CollectionID     string            `json:"collection_id"`
	ChainID          int64             `json:"chain_id"`
	BurnOperations   []BurnOperation   `json:"burn_operations"`
	BatchPreferences *BatchPreferences `json:"batch_preferences,omitempty"`
}

// BurnOperation represents a single burn operation
type BurnOperation struct {
	OwnerUserID string `json:"owner_user_id"`
	TokenID     string `json:"token_id"`
}

// BatchTransferRequest represents a batch transfer request
type BatchTransferRequest struct {
	OperationID        string              `json:"operation_id"`
	CollectionID       string              `json:"collection_id"`
	ChainID            int64               `json:"chain_id"`
	TransferOperations []TransferOperation `json:"transfer_operations"`
	BatchPreferences   *BatchPreferences   `json:"batch_preferences,omitempty"`
}

// TransferOperation represents a single transfer operation
type TransferOperation struct {
	FromUserID string `json:"from_user_id"`
	ToUserID   string `json:"to_user_id"`
	TokenID    string `json:"token_id"`
}

// BatchPreferences represents batch processing preferences
type BatchPreferences struct {
	MaxWaitTime *string `json:"max_wait_time,omitempty"`
	Priority    *string `json:"priority,omitempty"`
}

// NFTMetadata represents NFT metadata
type NFTMetadata struct {
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Image       string         `json:"image,omitempty"`
	ExternalURL string         `json:"external_url,omitempty"`
	Attributes  []NFTAttribute `json:"attributes,omitempty"`
}

// NFTAttribute represents an NFT attribute
type NFTAttribute struct {
	TraitType        string  `json:"trait_type"`
	Value            string  `json:"value"`
	RarityPercentage float32 `json:"rarity_percentage,omitempty"`
}

// Collection represents an NFT collection
type Collection struct {
	ID              int       `json:"id"`
	CollectionID    string    `json:"collection_id"`
	ChainID         int64     `json:"chain_id"`
	ContractAddress string    `json:"contract_address"`
	Name            string    `json:"name"`
	Symbol          string    `json:"symbol"`
	ContractType    string    `json:"contract_type"`
	BaseURI         string    `json:"base_uri,omitempty"`
	IsEnabled       bool      `json:"is_enabled"`
	CreatedAt       time.Time `json:"created_at"`
}

// NFTAsset represents an NFT asset
type NFTAsset struct {
	ID           int          `json:"id"`
	CollectionID string       `json:"collection_id"`
	TokenID      string       `json:"token_id"`
	OwnerUserID  string       `json:"owner_user_id"`
	ChainID      int64        `json:"chain_id"`
	MetadataURI  string       `json:"metadata_uri,omitempty"`
	Name         string       `json:"name,omitempty"`
	Description  string       `json:"description,omitempty"`
	ImageURL     string       `json:"image_url,omitempty"`
	Attributes   *NFTMetadata `json:"attributes,omitempty"`
	IsBurned     bool         `json:"is_burned"`
	IsMinted     bool         `json:"is_minted"`
	IsLocked     bool         `json:"is_locked"`
	CreatedAt    time.Time    `json:"created_at"`
}

// Response types
type BatchMintResponse struct {
	OperationID    string `json:"operation_id"`
	ProcessedCount int    `json:"processed_count"`
	Status         string `json:"status"`
}

type BatchBurnResponse struct {
	OperationID    string `json:"operation_id"`
	ProcessedCount int    `json:"processed_count"`
	Status         string `json:"status"`
}

type BatchTransferResponse struct {
	OperationID    string `json:"operation_id"`
	ProcessedCount int    `json:"processed_count"`
	Status         string `json:"status"`
}

type BatchInfo struct {
	BatchID              string  `json:"batch_id"`
	QueuedTransactions   int     `json:"queued_transactions"`
	EstimatedProcessTime string  `json:"estimated_process_time"`
	EstimatedGasCost     *string `json:"estimated_gas_cost,omitempty"`
	Priority             string  `json:"priority"`
}

// Error types
type NFTError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (e NFTError) Error() string {
	return e.Message
}

// Error type constants
const (
	ErrorTypeValidation         = "validation_error"
	ErrorTypeCollectionNotFound = "collection_not_found"
	ErrorTypeChainNotSupported  = "chain_not_supported"
	ErrorTypeNFTNotFound        = "nft_not_found"
	ErrorTypeOwnership          = "ownership_error"
)

// Error checking functions
func IsValidationError(err error) bool {
	if nftErr, ok := err.(NFTError); ok {
		return nftErr.Type == ErrorTypeValidation
	}
	return false
}

func IsCollectionNotFoundError(err error) bool {
	if nftErr, ok := err.(NFTError); ok {
		return nftErr.Type == ErrorTypeCollectionNotFound
	}
	return false
}

func IsChainNotSupportedError(err error) bool {
	if nftErr, ok := err.(NFTError); ok {
		return nftErr.Type == ErrorTypeChainNotSupported
	}
	return false
}

func IsNFTNotFoundError(err error) bool {
	if nftErr, ok := err.(NFTError); ok {
		return nftErr.Type == ErrorTypeNFTNotFound
	}
	return false
}

func IsOwnershipError(err error) bool {
	if nftErr, ok := err.(NFTError); ok {
		return nftErr.Type == ErrorTypeOwnership
	}
	return false
}

// BatchMintNFTs processes a batch mint request
func (s *service) BatchMintNFTs(ctx context.Context, request *BatchMintRequest) (*BatchMintResponse, *BatchInfo, error) {
	log.Info().
		Str("operation_id", request.OperationID).
		Str("collection_id", request.CollectionID).
		Int64("chain_id", request.ChainID).
		Int("mint_count", len(request.MintOperations)).
		Msg("Processing NFT batch mint request")

	// Validate collection exists and is enabled
	collection, err := s.GetCollection(ctx, request.CollectionID)
	if err != nil {
		return nil, nil, NFTError{
			Type:    ErrorTypeCollectionNotFound,
			Message: fmt.Sprintf("Collection %s not found", request.CollectionID),
		}
	}

	if !collection.IsEnabled {
		return nil, nil, NFTError{
			Type:    ErrorTypeValidation,
			Message: fmt.Sprintf("Collection %s is disabled", request.CollectionID),
		}
	}

	if collection.ChainID != request.ChainID {
		return nil, nil, NFTError{
			Type:    ErrorTypeValidation,
			Message: fmt.Sprintf("Collection %s is not on chain %d", request.CollectionID, request.ChainID),
		}
	}

	// TODO: Add validation for business logic (user limits, rate limiting, etc.)

	// Create transactions in database and queue jobs
	processedCount := 0
	batchID := generateBatchID()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, mintOp := range request.MintOperations {
		// Generate token ID (this could be more sophisticated)
		tokenID := generateTokenID()

		// Create transaction record
		transactionID := uuid.New()

		// Insert transaction record
		_, err := tx.ExecContext(ctx, `
			INSERT INTO transactions (
				transaction_id, user_id, chain_id, tx_type, status, amount, 
				collection_id, nft_token_id, nft_metadata, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		`, transactionID, mintOp.ToUserID, request.ChainID, "nft_mint", "recorded",
			"1", request.CollectionID, tokenID, convertMetadataToJSON(mintOp.Meta))

		if err != nil {
			return nil, nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		// Create NFT asset record (not yet minted)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO nft_assets (
				collection_id, token_id, owner_user_id, chain_id, 
				metadata_uri, name, description, image_url, attributes,
				is_burned, is_minted, is_locked, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
		`, request.CollectionID, tokenID, mintOp.ToUserID, request.ChainID,
			buildMetadataURI(collection.BaseURI, tokenID),
			mintOp.Meta.Name, mintOp.Meta.Description, mintOp.Meta.Image,
			convertMetadataToJSON(mintOp.Meta), false, false, false)

		if err != nil {
			return nil, nil, fmt.Errorf("failed to create NFT asset record: %w", err)
		}

		// TODO: Queue the job for batch processing
		// This would involve creating NFTMintJob and publishing to queue

		processedCount++
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// TODO: Implement actual batch processing logic
	batchInfo := &BatchInfo{
		BatchID:              batchID,
		QueuedTransactions:   processedCount,
		EstimatedProcessTime: "5-10 minutes",
		Priority:             getPriority(request.BatchPreferences),
	}

	response := &BatchMintResponse{
		OperationID:    request.OperationID,
		ProcessedCount: processedCount,
		Status:         "recorded",
	}

	return response, batchInfo, nil
}

// Placeholder implementations for other methods
func (s *service) BatchBurnNFTs(ctx context.Context, request *BatchBurnRequest) (*BatchBurnResponse, *BatchInfo, error) {
	// TODO: Implement batch burn logic
	return &BatchBurnResponse{
			OperationID:    request.OperationID,
			ProcessedCount: len(request.BurnOperations),
			Status:         "recorded",
		}, &BatchInfo{
			BatchID:              generateBatchID(),
			QueuedTransactions:   len(request.BurnOperations),
			EstimatedProcessTime: "5-10 minutes",
			Priority:             getPriority(request.BatchPreferences),
		}, nil
}

func (s *service) BatchTransferNFTs(ctx context.Context, request *BatchTransferRequest) (*BatchTransferResponse, *BatchInfo, error) {
	// TODO: Implement batch transfer logic
	return &BatchTransferResponse{
			OperationID:    request.OperationID,
			ProcessedCount: len(request.TransferOperations),
			Status:         "recorded",
		}, &BatchInfo{
			BatchID:              generateBatchID(),
			QueuedTransactions:   len(request.TransferOperations),
			EstimatedProcessTime: "5-10 minutes",
			Priority:             getPriority(request.BatchPreferences),
		}, nil
}

func (s *service) GetCollection(ctx context.Context, collectionID string) (*Collection, error) {
	collection := &Collection{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, collection_id, chain_id, contract_address, name, symbol, 
			   contract_type, COALESCE(base_uri, ''), is_enabled, created_at
		FROM nft_collections 
		WHERE collection_id = $1
	`, collectionID).Scan(
		&collection.ID, &collection.CollectionID, &collection.ChainID,
		&collection.ContractAddress, &collection.Name, &collection.Symbol,
		&collection.ContractType, &collection.BaseURI, &collection.IsEnabled,
		&collection.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, NFTError{
				Type:    ErrorTypeCollectionNotFound,
				Message: fmt.Sprintf("Collection %s not found", collectionID),
			}
		}
		return nil, fmt.Errorf("failed to get collection: %w", err)
	}

	return collection, nil
}

func (s *service) GetCollectionsByChain(ctx context.Context, chainID int64) ([]*Collection, error) {
	// TODO: Implement
	return nil, fmt.Errorf("not implemented")
}

func (s *service) GetNFTAsset(ctx context.Context, collectionID, tokenID string) (*NFTAsset, error) {
	// TODO: Implement
	return nil, fmt.Errorf("not implemented")
}

func (s *service) GetUserNFTAssets(ctx context.Context, userID string, chainID *int64) ([]*NFTAsset, error) {
	// TODO: Implement
	return nil, fmt.Errorf("not implemented")
}

// Helper functions
func generateBatchID() string {
	return "nft_batch_" + uuid.New().String()[:8]
}

func generateTokenID() string {
	return uuid.New().String()
}

func convertMetadataToJSON(meta *NFTMetadata) string {
	if meta == nil {
		return "{}"
	}
	// TODO: Implement proper JSON marshaling
	return "{}"
}

func buildMetadataURI(baseURI, tokenID string) string {
	if baseURI == "" {
		return ""
	}
	return baseURI + tokenID + ".json"
}

func getPriority(prefs *BatchPreferences) string {
	if prefs != nil && prefs.Priority != nil {
		return *prefs.Priority
	}
	return "normal"
}
