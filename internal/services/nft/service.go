package nft

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/hzbay/chain-bridge/internal/queue"
	"github.com/rs/zerolog/log"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
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
	GetNFTMetadataByTokenID(ctx context.Context, tokenID string) (*NFTMetadata, error)

	// Token ID management after blockchain operations
	UpdateTokenIDAfterMinting(ctx context.Context, operationID, actualTokenID string) error
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
//
//nolint:revive // NFTMetadata name is intentional to avoid ambiguity
type NFTMetadata struct {
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Image       string         `json:"image,omitempty"`
	ExternalURL string         `json:"external_url,omitempty"`
	Attributes  []NFTAttribute `json:"attributes,omitempty"`
}

// NFTAttribute represents an NFT attribute
//
//nolint:revive // NFTAttribute name is intentional to avoid ambiguity
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
//
//nolint:revive // NFTAsset name is intentional to avoid ambiguity
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
//
//nolint:revive // NFTError name is intentional to avoid ambiguity
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
	var nftErr NFTError
	return errors.As(err, &nftErr) && nftErr.Type == ErrorTypeValidation
}

func IsCollectionNotFoundError(err error) bool {
	var nftErr NFTError
	return errors.As(err, &nftErr) && nftErr.Type == ErrorTypeCollectionNotFound
}

func IsChainNotSupportedError(err error) bool {
	var nftErr NFTError
	return errors.As(err, &nftErr) && nftErr.Type == ErrorTypeChainNotSupported
}

func IsNotFoundError(err error) bool {
	var nftErr NFTError
	return errors.As(err, &nftErr) && nftErr.Type == ErrorTypeNFTNotFound
}

func IsNFTNotFoundError(err error) bool {
	var nftErr NFTError
	return errors.As(err, &nftErr) && nftErr.Type == ErrorTypeNFTNotFound
}

func IsOwnershipError(err error) bool {
	var nftErr NFTError
	return errors.As(err, &nftErr) && nftErr.Type == ErrorTypeOwnership
}

// BatchMintNFTs processes a batch mint request
func (s *service) BatchMintNFTs(ctx context.Context, request *BatchMintRequest) (*BatchMintResponse, *BatchInfo, error) {
	log.Info().
		Str("operation_id", request.OperationID).
		Str("collection_id", request.CollectionID).
		Int64("chain_id", request.ChainID).
		Int("mint_count", len(request.MintOperations)).
		Msg("Processing NFT batch mint request")

	// 1. 幂等性检查 - 检查 OperationID 是否已经存在
	// Validate operation_id format - it should be a valid UUID
	mainOperationID, err := uuid.Parse(request.OperationID)
	if err != nil {
		// Return a validation error for invalid UUID format
		return nil, nil, NFTError{
			Type:    ErrorTypeValidation,
			Message: fmt.Sprintf("operation_id must be a valid UUID format (e.g., 550e8400-e29b-41d4-a716-446655440000): %s", request.OperationID),
		}
	}

	existingTx, err := models.Transactions(
		models.TransactionWhere.OperationID.EQ(null.StringFrom(mainOperationID.String())),
	).One(ctx, s.db)

	if err == nil && existingTx != nil {
		// OperationID 已存在，返回已有结果
		log.Info().
			Str("operation_id", request.OperationID).
			Str("existing_tx_id", existingTx.TXID).
			Msg("Operation already processed, returning existing result")

		// 统计已处理的记录数
		processedCount, err := models.Transactions(
			models.TransactionWhere.OperationID.EQ(null.StringFrom(mainOperationID.String())),
		).Count(ctx, s.db)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to count existing transactions")
			processedCount = int64(len(request.MintOperations)) // fallback
		}

		// 返回已有结果
		status := existingTx.Status.String
		if status == "" {
			status = "pending"
		}

		response := &BatchMintResponse{
			OperationID:    request.OperationID,
			ProcessedCount: int(processedCount),
			Status:         status,
		}

		// Build batch info for idempotent response
		batchInfo := &BatchInfo{
			BatchID:              generateBatchID(),
			QueuedTransactions:   int(processedCount),
			EstimatedProcessTime: "5-10 minutes",
			Priority:             getPriority(request.BatchPreferences),
		}

		return response, batchInfo, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		// 数据库查询错误（非记录不存在）
		return nil, nil, fmt.Errorf("failed to check operation idempotency: %w", err)
	}

	// OperationID 不存在，继续正常处理
	log.Debug().Str("operation_id", request.OperationID).Msg("New operation, proceeding with processing")

	// 2. Validate collection exists and is enabled
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
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			log.Warn().Err(rollbackErr).Msg("Failed to rollback transaction")
		}
	}()

	for _, mintOp := range request.MintOperations {
		// Create transaction record - token_id will be set to -1 until minting is complete
		transactionID := uuid.New()
		// Each mint operation gets its own unique operation_id for tracking
		individualOperationID := uuid.New()

		// Insert transaction record with individual operation_id for proper tracking
		_, err := tx.ExecContext(ctx, `
			INSERT INTO transactions (
				transaction_id, operation_id, user_id, chain_id, tx_type, business_type, status, amount, 
				collection_id, nft_token_id, nft_metadata, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		`, transactionID, individualOperationID.String(), mintOp.ToUserID, request.ChainID, "nft_mint", mintOp.BusinessType, "pending",
			"1", request.CollectionID, "-1", convertMetadataToJSON(mintOp.Meta))

		if err != nil {
			return nil, nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		// Create NFT asset record (not yet minted) with individual operation_id for tracking
		_, err = tx.ExecContext(ctx, `
			INSERT INTO nft_assets (
				collection_id, token_id, owner_user_id, chain_id, operation_id,
				metadata_uri, name, description, image_url, attributes,
				is_burned, is_minted, is_locked, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW())
		`, request.CollectionID, "-1", mintOp.ToUserID, request.ChainID, individualOperationID.String(),
			"", // metadata_uri will be set after token_id is known
			mintOp.Meta.Name, mintOp.Meta.Description, mintOp.Meta.Image,
			convertMetadataToJSON(mintOp.Meta), false, false, false)

		if err != nil {
			return nil, nil, fmt.Errorf("failed to create NFT asset record: %w", err)
		}

		// Create NFT mint job for batch processing with individual operation_id
		mintJob := queue.NFTMintJob{
			ID:            uuid.New().String(),
			JobType:       queue.JobTypeNFTMint,
			TransactionID: transactionID,
			ChainID:       request.ChainID,
			CollectionID:  request.CollectionID,
			ToUserID:      mintOp.ToUserID,
			TokenID:       "-1", // Will be updated when blockchain minting completes
			MetadataURI:   "", // Will be built with actual token ID after minting
			BusinessType:  mintOp.BusinessType,
			ReasonType:    mintOp.ReasonType,
			Priority:      queue.PriorityNormal,
			CreatedAt:     time.Now(),
			// Store both batch operation_id and individual operation_id for mapping
			BatchOperationID:      mainOperationID.String(), // Batch-level tracking
			IndividualOperationID: individualOperationID.String(), // Individual operation tracking
		}

		if mintOp.ReasonDetail != nil {
			mintJob.ReasonDetail = *mintOp.ReasonDetail
		}

		// Queue the job for batch processing
		if err := s.batchProcessor.PublishNFTMint(ctx, mintJob); err != nil {
			log.Error().Err(err).
				Str("job_id", mintJob.ID).
				Str("user_id", mintJob.ToUserID).
				Msg("Failed to publish NFT mint job, but transaction is recorded")

			// Note: We don't return error here because transactions are already recorded
			// The batch processor failure doesn't mean the mint failed

			// Mark corresponding transaction as failed since it cannot be processed
			if updateErr := s.markTransactionAsFailed(context.Background(), transactionID.String(), "queue_publish_failed"); updateErr != nil {
				log.Error().Err(updateErr).
					Str("job_id", mintJob.ID).
					Msg("Failed to mark transaction as failed")
			}
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
		Status:         "pending",
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

	// 1. 幂等性检查 - 检查 OperationID 是否已经存在
	// Validate operation_id format - it should be a valid UUID
	mainOperationID, err := uuid.Parse(request.OperationID)
	if err != nil {
		// Return a validation error for invalid UUID format
		return nil, nil, NFTError{
			Type:    ErrorTypeValidation,
			Message: fmt.Sprintf("operation_id must be a valid UUID format (e.g., 550e8400-e29b-41d4-a716-446655440000): %s", request.OperationID),
		}
	}

	existingTx, err := models.Transactions(
		models.TransactionWhere.OperationID.EQ(null.StringFrom(mainOperationID.String())),
	).One(ctx, s.db)

	if err == nil && existingTx != nil {
		// OperationID 已存在，返回已有结果
		log.Info().
			Str("operation_id", request.OperationID).
			Str("existing_tx_id", existingTx.TXID).
			Msg("Operation already processed, returning existing result")

		// 统计已处理的记录数
		processedCount, err := models.Transactions(
			models.TransactionWhere.OperationID.EQ(null.StringFrom(mainOperationID.String())),
		).Count(ctx, s.db)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to count existing transactions")
			processedCount = int64(len(request.BurnOperations)) // fallback
		}

		// 返回已有结果
		status := existingTx.Status.String
		if status == "" {
			status = "pending"
		}

		response := &BatchBurnResponse{
			OperationID:    request.OperationID,
			ProcessedCount: int(processedCount),
			Status:         status,
		}

		// Build batch info for idempotent response
		batchInfo := &BatchInfo{
			BatchID:              generateBatchID(),
			QueuedTransactions:   int(processedCount),
			EstimatedProcessTime: "5-10 minutes",
			Priority:             getPriority(request.BatchPreferences),
		}

		return response, batchInfo, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		// 数据库查询错误（非记录不存在）
		return nil, nil, fmt.Errorf("failed to check operation idempotency: %w", err)
	}

	// OperationID 不存在，继续正常处理
	log.Debug().Str("operation_id", request.OperationID).Msg("New operation, proceeding with processing")

	// 2. Validate collection exists and is enabled
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
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			log.Warn().Err(rollbackErr).Msg("Failed to rollback transaction")
		}
	}()

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
			if errors.Is(err, sql.ErrNoRows) {
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

		// Create transaction record with individual operation_id for proper tracking
		transactionID := uuid.New()
		individualOperationID := uuid.New()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO transactions (
				transaction_id, operation_id, user_id, chain_id, tx_type, business_type, status, amount,
				collection_id, nft_token_id, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		`, transactionID, individualOperationID.String(), burnOp.OwnerUserID, request.ChainID, "nft_burn", "consumption", "pending",
			"1", request.CollectionID, burnOp.TokenID)

		if err != nil {
			return nil, nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		// Lock NFT asset before burn processing
		_, err = tx.ExecContext(ctx, `
			UPDATE nft_assets
			SET is_locked = true
			WHERE collection_id = $1 AND token_id = $2
		`, request.CollectionID, burnOp.TokenID)

		if err != nil {
			return nil, nil, fmt.Errorf("failed to lock NFT asset for burn: %w", err)
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
			// Store both batch operation_id and individual operation_id for mapping
			BatchOperationID:      mainOperationID.String(), // Batch-level tracking
			IndividualOperationID: individualOperationID.String(), // Individual operation tracking
		}

		// Queue the job for batch processing
		if err := s.batchProcessor.PublishNFTBurn(ctx, burnJob); err != nil {
			log.Error().Err(err).
				Str("job_id", burnJob.ID).
				Str("user_id", burnJob.OwnerUserID).
				Msg("Failed to publish NFT burn job, but transaction is recorded")

			// Note: We don't return error here because transactions are already recorded
			// The batch processor failure doesn't mean the burn failed

			// Mark corresponding transaction as failed since it cannot be processed
			if updateErr := s.markTransactionAsFailed(context.Background(), transactionID.String(), "queue_publish_failed"); updateErr != nil {
				log.Error().Err(updateErr).
					Str("job_id", burnJob.ID).
					Msg("Failed to mark transaction as failed")
			}
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
		Status:         "pending",
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

	// 1. 幂等性检查 - 检查 OperationID 是否已经存在
	// Validate operation_id format - it should be a valid UUID
	mainOperationID, err := uuid.Parse(request.OperationID)
	if err != nil {
		// Return a validation error for invalid UUID format
		return nil, nil, NFTError{
			Type:    ErrorTypeValidation,
			Message: fmt.Sprintf("operation_id must be a valid UUID format (e.g., 550e8400-e29b-41d4-a716-446655440000): %s", request.OperationID),
		}
	}

	existingTx, err := models.Transactions(
		models.TransactionWhere.OperationID.EQ(null.StringFrom(mainOperationID.String())),
	).One(ctx, s.db)

	if err == nil && existingTx != nil {
		// OperationID 已存在，返回已有结果
		log.Info().
			Str("operation_id", request.OperationID).
			Str("existing_tx_id", existingTx.TXID).
			Msg("Operation already processed, returning existing result")

		// 统计已处理的记录数
		processedCount, err := models.Transactions(
			models.TransactionWhere.OperationID.EQ(null.StringFrom(mainOperationID.String())),
		).Count(ctx, s.db)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to count existing transactions")
			processedCount = int64(len(request.TransferOperations)) // fallback
		}

		// 返回已有结果
		status := existingTx.Status.String
		if status == "" {
			status = "pending"
		}

		response := &BatchTransferResponse{
			OperationID:    request.OperationID,
			ProcessedCount: int(processedCount),
			Status:         status,
		}

		// Build batch info for idempotent response
		batchInfo := &BatchInfo{
			BatchID:              generateBatchID(),
			QueuedTransactions:   int(processedCount),
			EstimatedProcessTime: "5-10 minutes",
			Priority:             getPriority(request.BatchPreferences),
		}

		return response, batchInfo, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		// 数据库查询错误（非记录不存在）
		return nil, nil, fmt.Errorf("failed to check operation idempotency: %w", err)
	}

	// OperationID 不存在，继续正常处理
	log.Debug().Str("operation_id", request.OperationID).Msg("New operation, proceeding with processing")

	// 2. Validate collection exists and is enabled
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
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			log.Warn().Err(rollbackErr).Msg("Failed to rollback transaction")
		}
	}()

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
			if errors.Is(err, sql.ErrNoRows) {
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

		// Create transaction record with individual operation_id for proper tracking
		transactionID := uuid.New()
		individualOperationID := uuid.New()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO transactions (
				transaction_id, operation_id, user_id, chain_id, tx_type, business_type, status, amount,
				collection_id, nft_token_id, related_user_id, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		`, transactionID, individualOperationID.String(), transferOp.FromUserID, request.ChainID, "nft_transfer", "transfer", "pending",
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
			// Store both batch operation_id and individual operation_id for mapping
			BatchOperationID:      mainOperationID.String(), // Batch-level tracking
			IndividualOperationID: individualOperationID.String(), // Individual operation tracking
		}

		// Queue the job for batch processing
		if err := s.batchProcessor.PublishNFTTransfer(ctx, transferJob); err != nil {
			log.Error().Err(err).
				Str("job_id", transferJob.ID).
				Str("from_user_id", transferJob.FromUserID).
				Str("to_user_id", transferJob.ToUserID).
				Msg("Failed to publish NFT transfer job, but transaction is recorded")

			// Note: We don't return error here because transactions are already recorded
			// The batch processor failure doesn't mean the transfer failed

			// Mark corresponding transaction as failed since it cannot be processed
			if updateErr := s.markTransactionAsFailed(context.Background(), transactionID.String(), "queue_publish_failed"); updateErr != nil {
				log.Error().Err(updateErr).
					Str("job_id", transferJob.ID).
					Msg("Failed to mark transaction as failed")
			}

			// Also unlock the NFT since the transfer failed to queue
			_, unlockErr := tx.ExecContext(ctx, `
				UPDATE nft_assets
				SET is_locked = false
				WHERE collection_id = $1 AND token_id = $2
			`, request.CollectionID, transferOp.TokenID)
			if unlockErr != nil {
				log.Error().Err(unlockErr).
					Str("collection_id", request.CollectionID).
					Str("token_id", transferOp.TokenID).
					Msg("Failed to unlock NFT after transfer queue failure")
			}
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
		Status:         "pending",
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
		if errors.Is(err, sql.ErrNoRows) {
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
	log.Debug().Int64("chain_id", chainID).Msg("Getting collections by chain")

	// Query collections for the specified chain
	collections, err := models.NFTCollections(
		models.NFTCollectionWhere.ChainID.EQ(chainID),
		models.NFTCollectionWhere.IsEnabled.EQ(null.BoolFrom(true)),
		qm.OrderBy(models.NFTCollectionColumns.Name+" ASC"),
	).All(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []*Collection{}, nil // Return empty slice if no collections found
		}
		return nil, fmt.Errorf("failed to query collections for chain %d: %w", chainID, err)
	}

	// Convert database models to service models
	var result []*Collection
	for _, dbCollection := range collections {
		collection := &Collection{
			ID:              dbCollection.ID,
			CollectionID:    dbCollection.CollectionID,
			ChainID:         dbCollection.ChainID,
			ContractAddress: dbCollection.ContractAddress,
			Name:            dbCollection.Name,
			Symbol:          dbCollection.Symbol,
			ContractType:    dbCollection.ContractType,
			BaseURI:         dbCollection.BaseURI.String,
			IsEnabled:       dbCollection.IsEnabled.Bool,
			CreatedAt:       dbCollection.CreatedAt.Time,
		}
		result = append(result, collection)
	}

	log.Debug().
		Int64("chain_id", chainID).
		Int("collection_count", len(result)).
		Msg("Collections retrieved successfully")

	return result, nil
}

func (s *service) GetNFTAsset(ctx context.Context, collectionID, tokenID string) (*NFTAsset, error) {
	log.Debug().
		Str("collection_id", collectionID).
		Str("token_id", tokenID).
		Msg("Getting NFT asset")

	// Query NFT asset with collection relationship
	nftAsset, err := models.NFTAssets(
		models.NFTAssetWhere.CollectionID.EQ(collectionID),
		models.NFTAssetWhere.TokenID.EQ(tokenID),
		qm.Load(models.NFTAssetRels.Collection),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, NFTError{
				Type:    ErrorTypeNFTNotFound,
				Message: fmt.Sprintf("NFT asset with collection_id %s and token_id %s not found", collectionID, tokenID),
			}
		}
		return nil, fmt.Errorf("failed to query NFT asset: %w", err)
	}

	// Convert database model to service model
	result := &NFTAsset{
		ID:           nftAsset.ID,
		CollectionID: nftAsset.CollectionID,
		TokenID:      nftAsset.TokenID,
		OwnerUserID:  nftAsset.OwnerUserID,
		ChainID:      nftAsset.ChainID,
		MetadataURI:  nftAsset.MetadataURI.String,
		Name:         nftAsset.Name.String,
		Description:  nftAsset.Description.String,
		ImageURL:     nftAsset.ImageURL.String,
		IsBurned:     nftAsset.IsBurned.Bool,
		IsMinted:     nftAsset.IsMinted.Bool,
		IsLocked:     nftAsset.IsLocked.Bool,
		CreatedAt:    nftAsset.CreatedAt.Time,
	}

	// Parse attributes JSON if available with comprehensive handling
	if nftAsset.Attributes.Valid && len(nftAsset.Attributes.JSON) > 0 {
		attributes := s.parseNFTAttributes(tokenID, nftAsset.Attributes.JSON)
		if len(attributes) > 0 {
			result.Attributes = &NFTMetadata{
				Attributes: attributes,
			}
		}
	}

	log.Debug().
		Str("collection_id", collectionID).
		Str("token_id", tokenID).
		Str("owner_user_id", result.OwnerUserID).
		Bool("is_minted", result.IsMinted).
		Bool("is_burned", result.IsBurned).
		Msg("NFT asset retrieved successfully")

	return result, nil
}

func (s *service) GetUserNFTAssets(ctx context.Context, userID string, chainID *int64) ([]*NFTAsset, error) {
	log.Debug().
		Str("user_id", userID).
		Interface("chain_id", chainID).
		Msg("Getting user NFT assets")

	// Build query conditions
	queryMods := []qm.QueryMod{
		models.NFTAssetWhere.OwnerUserID.EQ(userID),
		models.NFTAssetWhere.IsBurned.EQ(null.BoolFrom(false)), // Only include non-burned NFTs
		qm.Load(models.NFTAssetRels.Collection),
		qm.OrderBy(models.NFTAssetColumns.ChainID + " ASC, " + models.NFTAssetColumns.CollectionID + " ASC, " + models.NFTAssetColumns.TokenID + " ASC"),
	}

	// Add chain filter if specified
	if chainID != nil {
		queryMods = append(queryMods, models.NFTAssetWhere.ChainID.EQ(*chainID))
	}

	// Query user's NFT assets
	nftAssets, err := models.NFTAssets(queryMods...).All(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []*NFTAsset{}, nil // Return empty slice if no NFTs found
		}
		return nil, fmt.Errorf("failed to query user NFT assets: %w", err)
	}

	// Convert database models to service models
	var result []*NFTAsset
	for _, dbAsset := range nftAssets {
		nftAsset := &NFTAsset{
			ID:           dbAsset.ID,
			CollectionID: dbAsset.CollectionID,
			TokenID:      dbAsset.TokenID,
			OwnerUserID:  dbAsset.OwnerUserID,
			ChainID:      dbAsset.ChainID,
			MetadataURI:  dbAsset.MetadataURI.String,
			Name:         dbAsset.Name.String,
			Description:  dbAsset.Description.String,
			ImageURL:     dbAsset.ImageURL.String,
			IsBurned:     dbAsset.IsBurned.Bool,
			IsMinted:     dbAsset.IsMinted.Bool,
			IsLocked:     dbAsset.IsLocked.Bool,
			CreatedAt:    dbAsset.CreatedAt.Time,
		}

		// Parse attributes JSON if available with comprehensive handling
		if dbAsset.Attributes.Valid && len(dbAsset.Attributes.JSON) > 0 {
			attributes := s.parseNFTAttributes(dbAsset.TokenID, dbAsset.Attributes.JSON)
			if len(attributes) > 0 {
				nftAsset.Attributes = &NFTMetadata{
					Attributes: attributes,
				}
			}
		}

		result = append(result, nftAsset)
	}

	log.Debug().
		Str("user_id", userID).
		Interface("chain_id", chainID).
		Int("nft_count", len(result)).
		Msg("User NFT assets retrieved successfully")

	return result, nil
}

// GetNFTMetadataByTokenID retrieves NFT metadata by token ID (public endpoint, no authentication required)
func (s *service) GetNFTMetadataByTokenID(ctx context.Context, tokenID string) (*NFTMetadata, error) {
	log.Debug().
		Str("token_id", tokenID).
		Msg("Getting NFT metadata by token ID")

	// Validate token ID
	if tokenID == "" {
		return nil, NFTError{
			Type:    ErrorTypeValidation,
			Message: "token_id cannot be empty",
		}
	}

	// Query NFT asset by token ID from any collection
	// Since this is a public endpoint, we find the NFT regardless of ownership
	nftAsset, err := models.NFTAssets(
		models.NFTAssetWhere.TokenID.EQ(tokenID),
		models.NFTAssetWhere.IsBurned.EQ(null.BoolFrom(false)), // Exclude burned NFTs
		models.NFTAssetWhere.IsMinted.EQ(null.BoolFrom(true)),  // Only include minted NFTs
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Debug().
				Str("token_id", tokenID).
				Msg("NFT not found or not minted")
			return nil, NFTError{
				Type:    ErrorTypeNFTNotFound,
				Message: fmt.Sprintf("NFT with token ID %s not found or not minted", tokenID),
			}
		}
		return nil, fmt.Errorf("failed to query NFT asset: %w", err)
	}

	// Build metadata response in OpenSea standard format
	metadata := &NFTMetadata{
		Name:        nftAsset.Name.String,
		Description: nftAsset.Description.String,
		Image:       nftAsset.ImageURL.String,
	}

	// Add external URL if metadata URI is available
	if nftAsset.MetadataURI.Valid && nftAsset.MetadataURI.String != "" {
		metadata.ExternalURL = nftAsset.MetadataURI.String
	}

	// Parse and add attributes if available with comprehensive error handling
	if nftAsset.Attributes.Valid && len(nftAsset.Attributes.JSON) > 0 {
		metadata.Attributes = s.parseNFTAttributes(tokenID, nftAsset.Attributes.JSON)
	}

	// Set default values if fields are empty
	if metadata.Name == "" {
		metadata.Name = fmt.Sprintf("NFT #%s", tokenID)
	}
	if metadata.Description == "" {
		metadata.Description = fmt.Sprintf("NFT with token ID %s", tokenID)
	}

	log.Debug().
		Str("token_id", tokenID).
		Str("name", metadata.Name).
		Str("collection_id", nftAsset.CollectionID).
		Bool("has_attributes", len(metadata.Attributes) > 0).
		Msg("NFT metadata retrieved successfully")

	return metadata, nil
}

// parseNFTAttributes provides comprehensive NFT attributes parsing with multiple strategies
func (s *service) parseNFTAttributes(tokenID string, attributesJSON []byte) []NFTAttribute {
	var result []NFTAttribute

	// Strategy 1: Try to parse as direct attributes array
	var directAttributes []NFTAttribute
	if err := json.Unmarshal(attributesJSON, &directAttributes); err == nil {
		// Validate and filter valid attributes
		for _, attr := range directAttributes {
			if s.isValidAttribute(attr) {
				result = append(result, attr)
			}
		}
		if len(result) > 0 {
			log.Debug().
				Str("token_id", tokenID).
				Int("attributes_count", len(result)).
				Msg("Successfully parsed attributes as direct array")
			return result
		}
	}

	// Strategy 2: Try to parse as wrapped object with attributes field
	var wrappedData map[string]interface{}
	if err := json.Unmarshal(attributesJSON, &wrappedData); err != nil {
		log.Warn().Err(err).
			Str("token_id", tokenID).
			Msg("Failed to parse attributes JSON - invalid format")
		return result
	}

	// Try to extract attributes array from wrapped object
	if attrs, ok := wrappedData["attributes"].([]interface{}); ok {
		for _, attr := range attrs {
			if attrMap, ok := attr.(map[string]interface{}); ok {
				nftAttr := s.parseAttributeMap(attrMap)
				if s.isValidAttribute(nftAttr) {
					result = append(result, nftAttr)
				}
			}
		}
		if len(result) > 0 {
			log.Debug().
				Str("token_id", tokenID).
				Int("attributes_count", len(result)).
				Msg("Successfully parsed attributes from wrapped object")
			return result
		}
	}

	// Strategy 3: Try to parse top-level object as attribute mappings
	for key, value := range wrappedData {
		// Skip standard metadata fields
		if key == "name" || key == "description" || key == "image" || key == "external_url" {
			continue
		}

		// Convert other fields to attributes
		nftAttr := NFTAttribute{
			TraitType: key,
			Value:     s.convertValueToString(value),
		}

		if s.isValidAttribute(nftAttr) {
			result = append(result, nftAttr)
		}
	}

	if len(result) > 0 {
		log.Debug().
			Str("token_id", tokenID).
			Int("attributes_count", len(result)).
			Msg("Successfully parsed attributes from top-level mappings")
	} else {
		log.Debug().
			Str("token_id", tokenID).
			Msg("No valid attributes found in any parsing strategy")
	}

	return result
}

// parseAttributeMap safely extracts NFT attribute from a map with type assertions
func (s *service) parseAttributeMap(attrMap map[string]interface{}) NFTAttribute {
	nftAttr := NFTAttribute{}

	// Safe extraction of trait_type
	if traitType, ok := attrMap["trait_type"].(string); ok {
		nftAttr.TraitType = traitType
	} else if displayType, ok := attrMap["display_type"].(string); ok {
		// Support OpenSea display_type as fallback
		nftAttr.TraitType = displayType
	}

	// Safe extraction of value with multiple type support
	if value, ok := attrMap["value"].(string); ok {
		nftAttr.Value = value
	} else {
		// Convert other types to string
		nftAttr.Value = s.convertValueToString(attrMap["value"])
	}

	// Safe extraction of rarity percentage
	if rarity, ok := attrMap["rarity_percentage"].(float64); ok {
		nftAttr.RarityPercentage = float32(rarity)
	} else if rarity, ok := attrMap["rarity"].(float64); ok {
		// Support alternative rarity field name
		nftAttr.RarityPercentage = float32(rarity)
	}

	return nftAttr
}

// convertValueToString safely converts interface{} values to string representation
func (s *service) convertValueToString(value interface{}) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case int, int32, int64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%.2f", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		// For complex types, marshal to JSON
		if jsonBytes, err := json.Marshal(v); err == nil {
			return string(jsonBytes)
		}
		return fmt.Sprintf("%v", v)
	}
}

// isValidAttribute checks if an NFT attribute has meaningful content
func (s *service) isValidAttribute(attr NFTAttribute) bool {
	return attr.TraitType != "" && attr.Value != ""
}

// Helper functions
func generateBatchID() string {
	return "nft_batch_" + uuid.New().String()[:8]
}

func generateTokenID() string {
	return uuid.New().String()
}

// convertMetadataToJSON converts NFT metadata to JSON string with comprehensive error handling
func convertMetadataToJSON(meta *NFTMetadata) string {
	if meta == nil {
		return "{}"
	}

	// Create a map for JSON marshaling - preserve all metadata structure
	metaMap := map[string]interface{}{}
	
	// Add standard fields only if they have values
	if meta.Name != "" {
		metaMap["name"] = meta.Name
	}
	if meta.Description != "" {
		metaMap["description"] = meta.Description
	}
	if meta.Image != "" {
		metaMap["image"] = meta.Image
	}
	if meta.ExternalURL != "" {
		metaMap["external_url"] = meta.ExternalURL
	}

	// Add attributes only if they exist and are valid
	if len(meta.Attributes) > 0 {
		// Convert to interface{} slice for proper JSON marshaling
		attributesList := make([]interface{}, 0, len(meta.Attributes))
		for _, attr := range meta.Attributes {
			attrMap := map[string]interface{}{}
			if attr.TraitType != "" {
				attrMap["trait_type"] = attr.TraitType
			}
			if attr.Value != "" {
				attrMap["value"] = attr.Value
			}
			if attr.RarityPercentage > 0 {
				attrMap["rarity_percentage"] = attr.RarityPercentage
			}
			// Only add attribute if it has meaningful content
			if len(attrMap) > 0 {
				attributesList = append(attributesList, attrMap)
			}
		}
		if len(attributesList) > 0 {
			metaMap["attributes"] = attributesList
		}
	}

	// Fallback to empty JSON object on marshaling error
	jsonData, err := json.Marshal(metaMap)
	if err != nil {
		log.Error().Err(err).
			Interface("metadata", meta).
			Msg("Failed to marshal NFT metadata - using fallback")
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

// markTransactionAsFailed marks a transaction as failed in the database and sends notification
func (s *service) markTransactionAsFailed(ctx context.Context, txID string, failureReason string) error {
	// Get transaction details before updating
	tx, err := models.Transactions(
		models.TransactionWhere.TXID.EQ(txID),
	).One(ctx, s.db)
	if err != nil {
		return fmt.Errorf("failed to fetch transaction for notification: %w", err)
	}

	query := `
		UPDATE transactions 
		SET 
			status = 'failed',
			failure_reason = $2,
			updated_at = NOW()
		WHERE tx_id = $1`

	_, err = s.db.ExecContext(ctx, query, txID, failureReason)
	if err != nil {
		return fmt.Errorf("failed to mark transaction as failed: %w", err)
	}

	// Send transaction status change notification
	s.sendTransactionFailedNotification(ctx, tx, failureReason)

	log.Info().
		Str("tx_id", txID).
		Str("failure_reason", failureReason).
		Msg("NFT transaction marked as failed")

	return nil
}

// sendTransactionFailedNotification sends notification when transaction fails
func (s *service) sendTransactionFailedNotification(ctx context.Context, tx *models.Transaction, failureReason string) {
	if s.batchProcessor == nil {
		return // No notification processor available, skip silently
	}

	// Build notification data following the established patterns from the notification guide
	notificationData := map[string]interface{}{
		"type":           "transaction_status_changed",
		"transaction_id": tx.TXID,
		"user_id":        tx.UserID,
		"old_status":     "pending", // Assuming original status was pending
		"new_status":     "failed",
		"failure_reason": failureReason,
		"chain_id":       tx.ChainID,
		"tx_type":        tx.TXType,
		"timestamp":      time.Now().Unix(), // Use Unix timestamp for consistency
	}

	// Add NFT-specific fields if available
	if tx.CollectionID.Valid {
		notificationData["collection_id"] = tx.CollectionID.String
	}
	if tx.NFTTokenID.Valid {
		notificationData["nft_token_id"] = tx.NFTTokenID.String
	}
	if tx.RelatedUserID.Valid {
		notificationData["related_user_id"] = tx.RelatedUserID.String
	}
	if tx.BusinessType != "" {
		notificationData["business_type"] = tx.BusinessType
	}

	notification := queue.NotificationJob{
		ID:        uuid.New().String(),
		JobType:   queue.JobTypeNotification,
		UserID:    tx.UserID,
		EventType: "transaction_status_changed", // Keep this for routing purposes
		Data:      notificationData,
		Priority:  queue.PriorityHigh, // High priority for failure notifications
		CreatedAt: time.Now(),
	}

	if err := s.batchProcessor.PublishNotification(ctx, notification); err != nil {
		log.Warn().Err(err).
			Str("transaction_id", tx.TXID).
			Str("user_id", tx.UserID).
			Str("failure_reason", failureReason).
			Msg("Failed to send NFT transaction failed notification")
	}

	// For transfers, also send notification to the recipient if different from sender
	if tx.TXType == "nft_transfer" && tx.RelatedUserID.Valid && tx.RelatedUserID.String != tx.UserID {
		recipientNotification := queue.NotificationJob{
			ID:        uuid.New().String(),
			JobType:   queue.JobTypeNotification,
			UserID:    tx.RelatedUserID.String,
			EventType: "transaction_status_changed",
			Data:      notificationData,
			Priority:  queue.PriorityHigh,
			CreatedAt: time.Now(),
		}

		if err := s.batchProcessor.PublishNotification(ctx, recipientNotification); err != nil {
			log.Warn().Err(err).
				Str("transaction_id", tx.TXID).
				Str("recipient_user_id", tx.RelatedUserID.String).
				Str("failure_reason", failureReason).
				Msg("Failed to send NFT transaction failed notification to recipient")
		}
	}
}

// UpdateTokenIDAfterMinting updates both transactions and nft_assets with actual token_id after successful blockchain minting
func (s *service) UpdateTokenIDAfterMinting(ctx context.Context, operationID, actualTokenID string) error {
	log.Info().
		Str("operation_id", operationID).
		Str("actual_token_id", actualTokenID).
		Msg("Updating token_id after successful minting")

	// Begin database transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			log.Warn().Err(rollbackErr).Msg("Failed to rollback transaction")
		}
	}()

	// Update transactions table
	_, err = tx.ExecContext(ctx, `
		UPDATE transactions 
		SET nft_token_id = $1, updated_at = NOW() 
		WHERE operation_id = $2 AND nft_token_id = '-1'
	`, actualTokenID, operationID)
	if err != nil {
		return fmt.Errorf("failed to update transactions token_id: %w", err)
	}

	// Get collection info for building metadata URI
	var collectionID string
	var baseURI string
	err = tx.QueryRowContext(ctx, `
		SELECT na.collection_id, nc.base_uri 
		FROM nft_assets na 
		JOIN nft_collections nc ON na.collection_id = nc.collection_id 
		WHERE na.operation_id = $1 AND na.token_id = '-1' 
		LIMIT 1
	`, operationID).Scan(&collectionID, &baseURI)
	if err != nil {
		return fmt.Errorf("failed to get collection info: %w", err)
	}

	// Build metadata URI with actual token ID
	metadataURI := buildMetadataURI(baseURI, actualTokenID)

	// Update nft_assets table
	_, err = tx.ExecContext(ctx, `
		UPDATE nft_assets 
		SET token_id = $1, metadata_uri = $2, is_minted = true, updated_at = NOW() 
		WHERE operation_id = $3 AND token_id = '-1'
	`, actualTokenID, metadataURI, operationID)
	if err != nil {
		return fmt.Errorf("failed to update nft_assets token_id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Info().
		Str("operation_id", operationID).
		Str("actual_token_id", actualTokenID).
		Str("metadata_uri", metadataURI).
		Msg("Successfully updated token_id after minting")

	return nil
}
