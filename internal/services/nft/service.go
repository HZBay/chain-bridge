package nft

import (
	"context"
	"database/sql"
	"encoding/json"
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

		// Create NFT mint job for batch processing
		mintJob := queue.NFTMintJob{
			ID:            uuid.New().String(),
			JobType:       queue.JobTypeNFTMint,
			TransactionID: transactionID,
			ChainID:       request.ChainID,
			CollectionID:  request.CollectionID,
			ToUserID:      mintOp.ToUserID,
			TokenID:       tokenID,
			MetadataURI:   buildMetadataURI(collection.BaseURI, tokenID),
			BusinessType:  mintOp.BusinessType,
			ReasonType:    mintOp.ReasonType,
			Priority:      queue.PriorityNormal,
			CreatedAt:     time.Now(),
		}

		if mintOp.ReasonDetail != nil {
			mintJob.ReasonDetail = *mintOp.ReasonDetail
		}

		// Queue the job for batch processing
		if err := s.batchProcessor.PublishNFTMint(ctx, mintJob); err != nil {
			return nil, nil, fmt.Errorf("failed to queue NFT mint job: %w", err)
		}

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

// BatchBurnNFTs processes a batch burn request
func (s *service) BatchBurnNFTs(ctx context.Context, request *BatchBurnRequest) (*BatchBurnResponse, *BatchInfo, error) {
	log.Info().
		Str("operation_id", request.OperationID).
		Str("collection_id", request.CollectionID).
		Int64("chain_id", request.ChainID).
		Int("burn_count", len(request.BurnOperations)).
		Msg("Processing NFT batch burn request")

	// Validate collection exists and is enabled
	collection, err := s.GetCollection(ctx, request.CollectionID)
	if err != nil {
		return nil, nil, err
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

	// Begin database transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	processedCount := 0
	batchID := generateBatchID()

	for _, burnOp := range request.BurnOperations {
		// Validate NFT ownership
		var currentOwner string
		var isBurned, isMinted bool
		err := tx.QueryRowContext(ctx, `
			SELECT owner_user_id, is_burned, is_minted
			FROM nft_assets
			WHERE collection_id = $1 AND token_id = $2
		`, request.CollectionID, burnOp.TokenID).Scan(&currentOwner, &isBurned, &isMinted)

		if err != nil {
			if err == sql.ErrNoRows {
				return nil, nil, NFTError{
					Type:    ErrorTypeNFTNotFound,
					Message: fmt.Sprintf("NFT with token ID %s not found in collection %s", burnOp.TokenID, request.CollectionID),
				}
			}
			return nil, nil, fmt.Errorf("failed to query NFT asset: %w", err)
		}

		if !isMinted {
			return nil, nil, NFTError{
				Type:    ErrorTypeValidation,
				Message: fmt.Sprintf("NFT with token ID %s is not yet minted", burnOp.TokenID),
			}
		}

		if isBurned {
			return nil, nil, NFTError{
				Type:    ErrorTypeValidation,
				Message: fmt.Sprintf("NFT with token ID %s is already burned", burnOp.TokenID),
			}
		}

		if currentOwner != burnOp.OwnerUserID {
			return nil, nil, NFTError{
				Type:    ErrorTypeOwnership,
				Message: fmt.Sprintf("User %s does not own NFT with token ID %s", burnOp.OwnerUserID, burnOp.TokenID),
			}
		}

		// Create transaction record
		transactionID := uuid.New()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO transactions (
				transaction_id, user_id, chain_id, tx_type, status, amount,
				collection_id, nft_token_id, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		`, transactionID, burnOp.OwnerUserID, request.ChainID, "nft_burn", "recorded",
			"1", request.CollectionID, burnOp.TokenID)

		if err != nil {
			return nil, nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		// Create NFT burn job for batch processing
		burnJob := queue.NFTBurnJob{
			ID:            uuid.New().String(),
			JobType:       queue.JobTypeNFTBurn,
			TransactionID: transactionID,
			ChainID:       request.ChainID,
			CollectionID:  request.CollectionID,
			OwnerUserID:   burnOp.OwnerUserID,
			TokenID:       burnOp.TokenID,
			BusinessType:  "burn",
			ReasonType:    "user_burn",
			Priority:      queue.PriorityNormal,
			CreatedAt:     time.Now(),
		}

		// Queue the job for batch processing
		if err := s.batchProcessor.PublishNFTBurn(ctx, burnJob); err != nil {
			return nil, nil, fmt.Errorf("failed to queue NFT burn job: %w", err)
		}

		processedCount++
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	batchInfo := &BatchInfo{
		BatchID:              batchID,
		QueuedTransactions:   processedCount,
		EstimatedProcessTime: "5-10 minutes",
		Priority:             getPriority(request.BatchPreferences),
	}

	response := &BatchBurnResponse{
		OperationID:    request.OperationID,
		ProcessedCount: processedCount,
		Status:         "recorded",
	}

	return response, batchInfo, nil
}

// BatchTransferNFTs processes a batch transfer request
func (s *service) BatchTransferNFTs(ctx context.Context, request *BatchTransferRequest) (*BatchTransferResponse, *BatchInfo, error) {
	log.Info().
		Str("operation_id", request.OperationID).
		Str("collection_id", request.CollectionID).
		Int64("chain_id", request.ChainID).
		Int("transfer_count", len(request.TransferOperations)).
		Msg("Processing NFT batch transfer request")

	// Validate collection exists and is enabled
	collection, err := s.GetCollection(ctx, request.CollectionID)
	if err != nil {
		return nil, nil, err
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

	// Begin database transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	processedCount := 0
	batchID := generateBatchID()

	for _, transferOp := range request.TransferOperations {
		// Validate NFT ownership and status
		var currentOwner string
		var isBurned, isMinted, isLocked bool
		err := tx.QueryRowContext(ctx, `
			SELECT owner_user_id, is_burned, is_minted, is_locked
			FROM nft_assets
			WHERE collection_id = $1 AND token_id = $2 FOR UPDATE
		`, request.CollectionID, transferOp.TokenID).Scan(&currentOwner, &isBurned, &isMinted, &isLocked)

		if err != nil {
			if err == sql.ErrNoRows {
				return nil, nil, NFTError{
					Type:    ErrorTypeNFTNotFound,
					Message: fmt.Sprintf("NFT with token ID %s not found in collection %s", transferOp.TokenID, request.CollectionID),
				}
			}
			return nil, nil, fmt.Errorf("failed to query NFT asset: %w", err)
		}

		if !isMinted {
			return nil, nil, NFTError{
				Type:    ErrorTypeValidation,
				Message: fmt.Sprintf("NFT with token ID %s is not yet minted", transferOp.TokenID),
			}
		}

		if isBurned {
			return nil, nil, NFTError{
				Type:    ErrorTypeValidation,
				Message: fmt.Sprintf("NFT with token ID %s is burned", transferOp.TokenID),
			}
		}

		if isLocked {
			return nil, nil, NFTError{
				Type:    ErrorTypeValidation,
				Message: fmt.Sprintf("NFT with token ID %s is locked", transferOp.TokenID),
			}
		}

		if currentOwner != transferOp.FromUserID {
			return nil, nil, NFTError{
				Type:    ErrorTypeOwnership,
				Message: fmt.Sprintf("User %s does not own NFT with token ID %s", transferOp.FromUserID, transferOp.TokenID),
			}
		}

		// Create transaction record
		transactionID := uuid.New()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO transactions (
				transaction_id, user_id, chain_id, tx_type, status, amount,
				collection_id, nft_token_id, related_user_id, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		`, transactionID, transferOp.FromUserID, request.ChainID, "nft_transfer", "recorded",
			"1", request.CollectionID, transferOp.TokenID, transferOp.ToUserID)

		if err != nil {
			return nil, nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		// Update NFT asset ownership temporarily (will be finalized on blockchain confirmation)
		_, err = tx.ExecContext(ctx, `
			UPDATE nft_assets
			SET is_locked = true
			WHERE collection_id = $1 AND token_id = $2
		`, request.CollectionID, transferOp.TokenID)

		if err != nil {
			return nil, nil, fmt.Errorf("failed to lock NFT asset: %w", err)
		}

		// Create NFT transfer job for batch processing
		transferJob := queue.NFTTransferJob{
			ID:            uuid.New().String(),
			JobType:       queue.JobTypeNFTTransfer,
			TransactionID: transactionID,
			ChainID:       request.ChainID,
			CollectionID:  request.CollectionID,
			FromUserID:    transferOp.FromUserID,
			ToUserID:      transferOp.ToUserID,
			TokenID:       transferOp.TokenID,
			BusinessType:  "transfer",
			ReasonType:    "user_transfer",
			Priority:      queue.PriorityNormal,
			CreatedAt:     time.Now(),
		}

		// Queue the job for batch processing
		if err := s.batchProcessor.PublishNFTTransfer(ctx, transferJob); err != nil {
			return nil, nil, fmt.Errorf("failed to queue NFT transfer job: %w", err)
		}

		processedCount++
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	batchInfo := &BatchInfo{
		BatchID:              batchID,
		QueuedTransactions:   processedCount,
		EstimatedProcessTime: "5-10 minutes",
		Priority:             getPriority(request.BatchPreferences),
	}

	response := &BatchTransferResponse{
		OperationID:    request.OperationID,
		ProcessedCount: processedCount,
		Status:         "recorded",
	}

	return response, batchInfo, nil
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
	
	// Create a map for JSON marshaling
	metaMap := map[string]interface{}{
		"name":         meta.Name,
		"description":  meta.Description,
		"image":        meta.Image,
		"external_url": meta.ExternalURL,
	}
	
	if len(meta.Attributes) > 0 {
		metaMap["attributes"] = meta.Attributes
	}
	
	jsonData, err := json.Marshal(metaMap)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal NFT metadata")
		return "{}"
	}
	
	return string(jsonData)
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
