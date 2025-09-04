package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"

	"github.com/hzbay/chain-bridge/internal/blockchain"
)

// TxConfirmationWatcher monitors confirmed batches for NFT special handling
type TxConfirmationWatcher struct {
	db                    *sql.DB
	callers               map[int64]*blockchain.BatchCaller // Changed from cpopCallers
	notificationProcessor NotificationProcessor             // Added for sending success notifications

	// Configuration
	confirmationBlocks int
	pollInterval       time.Duration
	maxRetries         int

	// Control channels
	stopChan chan struct{}
	workerWg sync.WaitGroup

	// Metrics
	processedCount int64
	failedCount    int64
	startedAt      time.Time
}

// NotificationProcessor defines the interface for sending notifications
type NotificationProcessor interface {
	PublishNotification(ctx context.Context, notification NotificationJob) error
}

// PendingBatch represents a batch waiting for confirmation
type PendingBatch struct {
	BatchID     string    `db:"batch_id"`
	ChainID     int64     `db:"chain_id"`
	TokenID     int       `db:"token_id"`
	TxHash      string    `db:"tx_hash"`
	Status      string    `db:"status"`
	SubmittedAt time.Time `db:"submitted_at"`
	RetryCount  int       `db:"retry_count"`
	BatchType   string    `db:"batch_type"`
	Operations  []BatchOperation
}

// BatchOperation represents an operation within a batch
type BatchOperation struct {
	TxID          string          `db:"tx_id"`
	OperationID   sql.NullString  `db:"operation_id"`
	UserID        string          `db:"user_id"`
	RelatedUserID sql.NullString  `db:"related_user_id"`
	TxType        string          `db:"tx_type"`
	Amount        decimal.Decimal `db:"amount"`
	Direction     sql.NullString  `db:"transfer_direction"`
	// NFT-specific fields
	CollectionID sql.NullString `db:"collection_id"`
	NFTTokenID   sql.NullString `db:"nft_token_id"`
}

// NewTxConfirmationWatcher creates a new NFT special handling watcher
func NewTxConfirmationWatcher(
	db *sql.DB,
	callers map[int64]*blockchain.BatchCaller,
	notificationProcessor NotificationProcessor,
) *TxConfirmationWatcher {
	return &TxConfirmationWatcher{
		db:                    db,
		callers:               callers,
		notificationProcessor: notificationProcessor,

		// Default configuration
		confirmationBlocks: 6,                // 6 blocks for confirmation
		pollInterval:       30 * time.Second, // Check every 30 seconds
		maxRetries:         10,               // Maximum retry attempts

		stopChan: make(chan struct{}),
	}
}

// Start starts the confirmation watcher
func (w *TxConfirmationWatcher) Start(ctx context.Context) error {
	log.Info().
		Int("confirmation_blocks", w.confirmationBlocks).
		Dur("poll_interval", w.pollInterval).
		Int("max_retries", w.maxRetries).
		Msg("NFT confirmation watcher initialized (synchronous mode)")

	w.startedAt = time.Now()

	// Note: No background goroutines started in synchronous mode
	// This watcher now only provides ProcessSingleNFTBatch method
	// for synchronous NFT batch processing called by RabbitMQBatchConsumer

	log.Info().Msg("NFT confirmation watcher initialized successfully (synchronous mode)")
	return nil
}

// Stop stops the confirmation watcher
func (w *TxConfirmationWatcher) Stop(_ context.Context) error {
	log.Info().Msg("Stopping transaction confirmation watcher")

	// Signal stop
	close(w.stopChan)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		w.workerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info().
			Int64("processed", w.processedCount).
			Int64("failed", w.failedCount).
			Dur("uptime", time.Since(w.startedAt)).
			Msg("Transaction confirmation watcher stopped successfully")
	case <-time.After(30 * time.Second):
		log.Warn().Msg("Timeout waiting for confirmation watcher to stop")
	}

	return nil
}

// getBatchOperations retrieves all operations for a specific batch
func (w *TxConfirmationWatcher) getBatchOperations(ctx context.Context, batchID string) ([]BatchOperation, error) {
	query := `
		SELECT 
			tx_id, operation_id, user_id, related_user_id, tx_type, 
			amount, transfer_direction, collection_id, nft_token_id
		FROM transactions 
		WHERE batch_id = $1 
		ORDER BY created_at ASC`

	rows, err := w.db.QueryContext(ctx, query, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to query batch operations: %w", err)
	}
	defer rows.Close()

	var operations []BatchOperation
	for rows.Next() {
		var op BatchOperation
		err := rows.Scan(
			&op.TxID,
			&op.OperationID,
			&op.UserID,
			&op.RelatedUserID,
			&op.TxType,
			&op.Amount,
			&op.Direction,
			&op.CollectionID,
			&op.NFTTokenID,
		)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan operation row")
			continue
		}
		operations = append(operations, op)
	}

	return operations, rows.Err()
}

// isNFTBatch checks if the batch type is an NFT operation
func (w *TxConfirmationWatcher) isNFTBatch(batchType string) bool {
	return batchType == "nft_mint" || batchType == "nft_burn" || batchType == "nft_transfer"
}

// finalizeNFTBatch performs NFT-specific finalization for confirmed batches
func (w *TxConfirmationWatcher) finalizeNFTBatch(ctx context.Context, batch PendingBatch) error {
	// Start database transaction
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get batch operations for NFT finalization
	operations, err := w.getBatchOperations(ctx, batch.BatchID)
	if err != nil {
		return fmt.Errorf("failed to get batch operations: %w", err)
	}

	// Create PendingBatch with operations for finalization
	pendingBatch := PendingBatch{
		BatchID:     batch.BatchID,
		ChainID:     batch.ChainID,
		TokenID:     batch.TokenID,
		TxHash:      batch.TxHash,
		Status:      batch.Status,
		SubmittedAt: batch.SubmittedAt,
		RetryCount:  batch.RetryCount,
		BatchType:   batch.BatchType,
		Operations:  operations,
	}

	// Finalize NFT assets
	err = w.finalizeNFTAssetsForBatch(tx, pendingBatch)
	if err != nil {
		return fmt.Errorf("failed to finalize NFT assets: %w", err)
	}

	// Extract and update NFT token IDs for mint operations
	// This is needed because NFT mint operations start with placeholder token_id "-1"
	// and need to be updated with actual token IDs from blockchain result
	if batch.BatchType == "nft_mint" {
		// Get NFT batch result from blockchain
		nftResult, err := w.getNFTBatchResult(ctx, batch.TxHash)
		if err != nil {
			log.Warn().Err(err).
				Str("batch_id", batch.BatchID).
				Str("tx_hash", batch.TxHash).
				Msg("Failed to get NFT batch result, token IDs will not be updated")
		} else if nftResult != nil {
			// Extract and update token IDs
			err = w.extractAndUpdateNFTTokenIDs(ctx, batch.BatchID, nftResult)
			if err != nil {
				log.Warn().Err(err).
					Str("batch_id", batch.BatchID).
					Str("tx_hash", batch.TxHash).
					Msg("Failed to extract and update NFT token IDs")
			}
		}
	}

	// Send NFT success notifications
	w.sendSuccessNotifications(ctx, pendingBatch)

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Info().
		Str("batch_id", batch.BatchID).
		Str("batch_type", batch.BatchType).
		Msg("NFT batch finalized successfully")

	return nil
}

// GetMetrics returns monitoring metrics for the confirmation watcher
func (w *TxConfirmationWatcher) GetMetrics() map[string]interface{} {
	uptime := time.Since(w.startedAt)

	return map[string]interface{}{
		"processed_count":     w.processedCount,
		"failed_count":        w.failedCount,
		"uptime_seconds":      uptime.Seconds(),
		"poll_interval":       w.pollInterval.String(),
		"confirmation_blocks": w.confirmationBlocks,
		"max_retries":         w.maxRetries,
		"started_at":          w.startedAt,
	}
}

// checkTransactionConfirmation checks if a transaction has enough confirmations
func (w *TxConfirmationWatcher) checkTransactionConfirmation(
	ctx context.Context,
	caller *blockchain.BatchCaller,
	txHash string,
	requiredBlocks int,
) (bool, int, error) {
	// Get the ethereum client from the CPOP caller
	client := caller.GetEthClient()
	if client == nil {
		return false, 0, fmt.Errorf("ethereum client not available")
	}

	// Convert string hash to common.Hash type
	hash := common.HexToHash(txHash)

	// 1. Get transaction receipt
	receipt, err := client.TransactionReceipt(ctx, hash)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			// Transaction not yet mined, return unconfirmed status
			log.Debug().
				Str("tx_hash", txHash).
				Msg("Transaction not yet mined")
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	// 2. Check transaction status (success or failure)
	if receipt.Status != types.ReceiptStatusSuccessful {
		log.Error().
			Str("tx_hash", txHash).
			Uint64("status", receipt.Status).
			Msg("Transaction failed or was reverted")
		return false, 0, fmt.Errorf("transaction failed or was reverted")
	}

	// 3. Get current latest block number
	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return false, 0, fmt.Errorf("failed to get latest block header: %w", err)
	}

	currentBlock := header.Number
	txBlock := receipt.BlockNumber

	// 4. Calculate confirmations
	confirmations := new(big.Int).Sub(currentBlock, txBlock).Int64()
	if confirmations < 0 {
		// Should not happen theoretically, but treat as 0 confirmations if it does
		confirmations = 0
	}

	// 5. Check if required confirmations are met
	confirmed := int(confirmations) >= requiredBlocks

	// 6. Confirmation status is updated by RabbitMQBatchConsumer

	log.Debug().
		Str("tx_hash", txHash).
		Int("confirmations", int(confirmations)).
		Int("required", requiredBlocks).
		Bool("confirmed", confirmed).
		Msg("Transaction confirmation check result")

	return confirmed, int(confirmations), nil
}

// ProcessSingleNFTBatch processes a single NFT batch confirmation and special handling
func (w *TxConfirmationWatcher) ProcessSingleNFTBatch(
	ctx context.Context,
	batchID string,
	txHash string,
	chainID int64,
	batchType string,
) error {
	log.Info().
		Str("batch_id", batchID).
		Str("tx_hash", txHash).
		Str("batch_type", batchType).
		Msg("Processing single NFT batch confirmation")

	// Get the appropriate caller for this chain
	caller, exists := w.callers[chainID]
	if !exists {
		return fmt.Errorf("no caller found for chain %d", chainID)
	}

	// Wait for confirmation
	confirmed, confirmations, err := w.WaitForConfirmation(ctx, caller, txHash, w.confirmationBlocks, 10*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to wait for confirmation: %w", err)
	}

	if !confirmed {
		return fmt.Errorf("transaction not confirmed within timeout")
	}

	// Create batch structure for NFT processing
	batch := PendingBatch{
		BatchID:   batchID,
		ChainID:   chainID,
		TxHash:    txHash,
		BatchType: batchType,
	}

	// Process NFT-specific finalization
	err = w.finalizeNFTBatch(ctx, batch)
	if err != nil {
		return fmt.Errorf("failed to finalize NFT batch: %w", err)
	}

	log.Info().
		Str("batch_id", batchID).
		Str("tx_hash", txHash).
		Int("confirmations", confirmations).
		Msg("NFT batch processed successfully")

	return nil
}

// WaitForConfirmation waits for a transaction to reach the specified number of confirmations
func (w *TxConfirmationWatcher) WaitForConfirmation(
	ctx context.Context,
	caller *blockchain.BatchCaller,
	txHash string,
	requiredBlocks int,
	timeout time.Duration,
) (bool, int, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(15 * time.Second) // Check every 15 seconds
	defer ticker.Stop()

	log.Info().
		Str("tx_hash", txHash).
		Int("required_blocks", requiredBlocks).
		Dur("timeout", timeout).
		Msg("Waiting for transaction confirmation")

	for {
		select {
		case <-timeoutCtx.Done():
			log.Warn().
				Str("tx_hash", txHash).
				Dur("timeout", timeout).
				Msg("Timeout waiting for transaction confirmation")
			return false, 0, fmt.Errorf("timeout waiting for transaction confirmation")

		case <-ticker.C:
			confirmed, confirmations, err := w.checkTransactionConfirmation(timeoutCtx, caller, txHash, requiredBlocks)
			if err != nil {
				log.Error().
					Str("tx_hash", txHash).
					Err(err).
					Msg("Error checking transaction confirmation")
				return false, 0, err
			}

			if confirmed {
				log.Info().
					Str("tx_hash", txHash).
					Int("confirmations", confirmations).
					Int("required", requiredBlocks).
					Msg("Transaction confirmed")
				return true, confirmations, nil
			}

			log.Debug().
				Str("tx_hash", txHash).
				Int("confirmations", confirmations).
				Int("required", requiredBlocks).
				Msg("Transaction confirmation in progress")
		}
	}
}

// IsHealthy checks if the confirmation watcher is healthy
func (w *TxConfirmationWatcher) IsHealthy() bool {
	// Check if we can connect to database
	if w.db == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := w.db.PingContext(ctx)
	return err == nil
}

// finalizeNFTAssetsForBatch finalizes NFT assets for all NFT operations in the batch
func (w *TxConfirmationWatcher) finalizeNFTAssetsForBatch(tx *sql.Tx, batch PendingBatch) error {
	for _, op := range batch.Operations {
		// Only process NFT operations
		switch op.TxType {
		case "nft_mint":
			err := w.finalizeNFTForMint(tx, op, batch.ChainID)
			if err != nil {
				return fmt.Errorf("failed to finalize NFT mint: %w", err)
			}
		case "nft_burn":
			err := w.finalizeNFTForBurn(tx, op, batch.ChainID)
			if err != nil {
				return fmt.Errorf("failed to finalize NFT burn: %w", err)
			}
		case "nft_transfer":
			err := w.finalizeNFTForTransfer(tx, op, batch.ChainID)
			if err != nil {
				return fmt.Errorf("failed to finalize NFT transfer: %w", err)
			}
		}
	}
	return nil
}

// finalizeNFTForMint finalizes NFT asset after successful mint
func (w *TxConfirmationWatcher) finalizeNFTForMint(tx *sql.Tx, op BatchOperation, _ int64) error {
	if !op.CollectionID.Valid {
		return fmt.Errorf("NFT mint operation missing collection_id")
	}

	// For NFT mint operations, if token_id is still "-1", we need to get the actual token_id from blockchain result
	// This will be handled by a separate process that extracts token IDs from the blockchain transaction receipt

	// Update NFT asset to minted status
	// Note: token_id might still be "-1" at this point, will be updated by UpdateTokenIDAfterMinting
	query := `
		UPDATE nft_assets
		SET 
			is_minted = true,
			is_locked = false,
			updated_at = $3
		WHERE collection_id = $1 AND owner_user_id = $2 AND operation_id = $4 AND token_id = '-1'`

	_, err := tx.Exec(query, op.CollectionID.String, op.UserID, time.Now(), op.OperationID.String)
	if err != nil {
		return fmt.Errorf("failed to finalize NFT mint: %w", err)
	}

	return nil
}

// finalizeNFTForBurn finalizes NFT asset after successful burn
func (w *TxConfirmationWatcher) finalizeNFTForBurn(tx *sql.Tx, op BatchOperation, _ int64) error {
	if !op.CollectionID.Valid || !op.NFTTokenID.Valid {
		return fmt.Errorf("NFT burn operation missing collection_id or nft_token_id")
	}

	// Update NFT asset to burned status
	query := `
		UPDATE nft_assets
		SET 
			is_burned = true,
			is_locked = false,
			updated_at = $4
		WHERE collection_id = $1 AND token_id = $2 AND owner_user_id = $3`

	_, err := tx.Exec(query, op.CollectionID.String, op.NFTTokenID.String, op.UserID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize NFT burn: %w", err)
	}

	return nil
}

// finalizeNFTForTransfer finalizes NFT asset after successful transfer
func (w *TxConfirmationWatcher) finalizeNFTForTransfer(tx *sql.Tx, op BatchOperation, _ int64) error {
	if !op.CollectionID.Valid || !op.NFTTokenID.Valid || !op.RelatedUserID.Valid {
		return fmt.Errorf("NFT transfer operation missing collection_id, nft_token_id, or related_user_id")
	}

	// Update NFT asset ownership and unlock
	query := `
		UPDATE nft_assets
		SET 
			owner_user_id = $4,
			is_locked = false,
			updated_at = $5
		WHERE collection_id = $1 AND token_id = $2 AND owner_user_id = $3`

	_, err := tx.Exec(query, op.CollectionID.String, op.NFTTokenID.String, op.UserID, op.RelatedUserID.String, time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize NFT transfer: %w", err)
	}

	return nil
}

// sendSuccessNotifications sends success notifications for all operations in the batch
func (w *TxConfirmationWatcher) sendSuccessNotifications(ctx context.Context, batch PendingBatch) {
	if w.notificationProcessor == nil {
		return // No notification processor available, skip silently
	}

	for _, op := range batch.Operations {
		// Only send success notifications for NFT operations
		switch op.TxType {
		case "nft_mint":
			w.sendNFTSuccessNotification(ctx, op, "nft_mint_success", batch.ChainID, batch.TxHash)
		case "nft_burn":
			w.sendNFTSuccessNotification(ctx, op, "nft_burn_success", batch.ChainID, batch.TxHash)
		case "nft_transfer":
			w.sendNFTSuccessNotification(ctx, op, "nft_transfer_success", batch.ChainID, batch.TxHash)
			// Also send notification to recipient for transfers
			if op.RelatedUserID.Valid {
				w.sendNFTTransferReceivedNotification(ctx, op, batch.ChainID, batch.TxHash)
			}
		}
	}
}

// sendNFTSuccessNotification sends a success notification for NFT operations
func (w *TxConfirmationWatcher) sendNFTSuccessNotification(ctx context.Context, op BatchOperation, notificationType string, chainID int64, txHash string) {
	// Build notification data
	notificationData := map[string]interface{}{
		"type":      notificationType,
		"user_id":   op.UserID,
		"chain_id":  chainID,
		"tx_hash":   txHash,
		"timestamp": time.Now().Unix(),
		"status":    "confirmed",
	}

	// Add operation_id if available
	if op.OperationID.Valid {
		notificationData["operation_id"] = op.OperationID.String
	}

	// Add NFT-specific data if available
	if op.CollectionID.Valid {
		notificationData["collection_id"] = op.CollectionID.String
	}
	if op.NFTTokenID.Valid {
		notificationData["nft_token_id"] = op.NFTTokenID.String
	}
	if op.RelatedUserID.Valid && notificationType == "nft_transfer_success" {
		notificationData["to_user_id"] = op.RelatedUserID.String
	}

	notification := NotificationJob{
		ID:        uuid.New().String(),
		JobType:   JobTypeNotification,
		UserID:    op.UserID,
		EventType: notificationType,
		Data:      notificationData,
		Priority:  PriorityNormal, // Normal priority for success notifications
		CreatedAt: time.Now(),
	}

	if err := w.notificationProcessor.PublishNotification(ctx, notification); err != nil {
		log.Warn().Err(err).
			Str("tx_id", op.TxID).
			Str("user_id", op.UserID).
			Str("notification_type", notificationType).
			Msg("Failed to send NFT success notification")
	}
}

// sendNFTTransferReceivedNotification sends a notification to the recipient of an NFT transfer
func (w *TxConfirmationWatcher) sendNFTTransferReceivedNotification(ctx context.Context, op BatchOperation, chainID int64, txHash string) {
	// Build notification data for recipient
	notificationData := map[string]interface{}{
		"type":         "nft_transfer_received",
		"user_id":      op.RelatedUserID.String, // Recipient
		"chain_id":     chainID,
		"tx_hash":      txHash,
		"timestamp":    time.Now().Unix(),
		"status":       "confirmed",
		"from_user_id": op.UserID, // Sender
	}

	// Add operation_id if available
	if op.OperationID.Valid {
		notificationData["operation_id"] = op.OperationID.String
	}

	// Add NFT-specific data if available
	if op.CollectionID.Valid {
		notificationData["collection_id"] = op.CollectionID.String
	}
	if op.NFTTokenID.Valid {
		notificationData["nft_token_id"] = op.NFTTokenID.String
	}

	notification := NotificationJob{
		ID:        uuid.New().String(),
		JobType:   JobTypeNotification,
		UserID:    op.RelatedUserID.String, // Send to recipient
		EventType: "nft_transfer_received",
		Data:      notificationData,
		Priority:  PriorityNormal, // Normal priority for success notifications
		CreatedAt: time.Now(),
	}

	if err := w.notificationProcessor.PublishNotification(ctx, notification); err != nil {
		log.Warn().Err(err).
			Str("tx_id", op.TxID).
			Str("recipient_user_id", op.RelatedUserID.String).
			Str("from_user_id", op.UserID).
			Msg("Failed to send NFT transfer received notification")
	}
}

// extractAndUpdateNFTTokenIDs extracts token IDs from NFT batch result and updates database
func (w *TxConfirmationWatcher) extractAndUpdateNFTTokenIDs(ctx context.Context, batchID string, nftResult *blockchain.NFTBatchResult) error {
	if nftResult == nil || len(nftResult.TokenIDs) == 0 {
		return nil // No token IDs to process
	}

	log.Info().
		Str("batch_id", batchID).
		Int("token_count", len(nftResult.TokenIDs)).
		Msg("Extracting and updating NFT token IDs from blockchain result")

	// Get mint operations from this batch that have placeholder token IDs
	// Order by created_at to maintain the same order as blockchain minting
	query := `
		SELECT operation_id, user_id, collection_id 
		FROM transactions 
		WHERE batch_id = $1 AND tx_type = 'nft_mint' AND nft_token_id = '-1' AND operation_id IS NOT NULL
		ORDER BY created_at ASC`

	rows, err := w.db.QueryContext(ctx, query, batchID)
	if err != nil {
		return fmt.Errorf("failed to query mint operations: %w", err)
	}
	defer rows.Close()

	var operations []struct {
		OperationID  string
		UserID       string
		CollectionID string
	}

	for rows.Next() {
		var op struct {
			OperationID  string
			UserID       string
			CollectionID string
		}
		err := rows.Scan(&op.OperationID, &op.UserID, &op.CollectionID)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan mint operation")
			continue
		}
		operations = append(operations, op)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate mint operations: %w", err)
	}

	// Map token IDs to operations based on creation order
	// Since we process mint operations in order (ORDER BY created_at ASC) and
	// blockchain returns token_ids in the same minting order,
	// we can map by index position: operations[i] -> tokenIDs[i]
	if len(operations) != len(nftResult.TokenIDs) {
		log.Warn().
			Int("operations_count", len(operations)).
			Int("token_ids_count", len(nftResult.TokenIDs)).
			Str("batch_id", batchID).
			Msg("Mismatch between operations and token IDs count - this should not happen")
		// Proceed with minimum count to avoid index errors
	}

	minCount := len(operations)
	if len(nftResult.TokenIDs) < minCount {
		minCount = len(nftResult.TokenIDs)
	}

	// Update each operation with its corresponding token ID
	for i := 0; i < minCount; i++ {
		op := operations[i]
		actualTokenID := nftResult.TokenIDs[i]

		log.Debug().
			Str("operation_id", op.OperationID).
			Str("user_id", op.UserID).
			Str("actual_token_id", actualTokenID).
			Msg("Updating NFT with actual token ID")

		// Update token ID directly
		err = w.updateNFTTokenIDDirect(ctx, op.OperationID, actualTokenID, op.CollectionID)
		if err != nil {
			log.Error().Err(err).
				Str("operation_id", op.OperationID).
				Str("token_id", actualTokenID).
				Msg("Failed to update NFT token ID")
			continue
		}
	}

	return nil
}

// updateNFTTokenIDDirect updates NFT token ID directly in the database
func (w *TxConfirmationWatcher) updateNFTTokenIDDirect(ctx context.Context, operationID, actualTokenID, collectionID string) error {
	// Begin database transaction
	tx, err := w.db.BeginTx(ctx, nil)
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
	var baseURI string
	err = tx.QueryRowContext(ctx, `
		SELECT base_uri FROM nft_collections WHERE collection_id = $1
	`, collectionID).Scan(&baseURI)
	if err != nil {
		return fmt.Errorf("failed to get collection base_uri: %w", err)
	}

	// Build metadata URI with actual token ID
	metadataURI := buildMetadataURI(baseURI, actualTokenID)

	// Update nft_assets table
	_, err = tx.ExecContext(ctx, `
		UPDATE nft_assets 
		SET token_id = $1, metadata_uri = $2, updated_at = NOW() 
		WHERE operation_id = $3 AND token_id = '-1'
	`, actualTokenID, metadataURI, operationID)
	if err != nil {
		return fmt.Errorf("failed to update nft_assets token_id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// buildMetadataURI builds the metadata URI for an NFT
func buildMetadataURI(baseURI, tokenID string) string {
	if baseURI == "" {
		return ""
	}
	// Ensure baseURI ends with /
	if baseURI[len(baseURI)-1] != '/' {
		baseURI += "/"
	}
	return baseURI + tokenID
}

// getNFTBatchResult retrieves NFT batch result from blockchain
func (w *TxConfirmationWatcher) getNFTBatchResult(ctx context.Context, txHash string) (*blockchain.NFTBatchResult, error) {
	// Get the caller for the chain
	caller, err := w.getCallerForTxHash(ctx, txHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get caller for tx hash: %w", err)
	}

	// Get transaction receipt to extract token IDs
	receipt, err := caller.GetTransactionReceipt(ctx, common.HexToHash(txHash))
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	// Extract token IDs from receipt logs
	tokenIDs, err := w.extractTokenIDsFromReceipt(receipt)
	if err != nil {
		return nil, fmt.Errorf("failed to extract token IDs from receipt: %w", err)
	}

	return &blockchain.NFTBatchResult{
		TxHash:      txHash,
		TokenIDs:    tokenIDs,
		BlockNumber: receipt.BlockNumber,
		Status:      "success",
	}, nil
}

// getCallerForTxHash gets the appropriate caller for a transaction hash
func (w *TxConfirmationWatcher) getCallerForTxHash(ctx context.Context, txHash string) (*blockchain.BatchCaller, error) {
	// Get chain ID from batch record
	var chainID int64
	err := w.db.QueryRowContext(ctx, `
		SELECT chain_id FROM batches WHERE tx_hash = $1
	`, txHash).Scan(&chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID for tx hash: %w", err)
	}

	// Get caller from the callers map
	caller, exists := w.callers[chainID]
	if !exists {
		return nil, fmt.Errorf("no caller found for chain %d", chainID)
	}

	return caller, nil
}

// extractTokenIDsFromReceipt extracts token IDs from transaction receipt logs
func (w *TxConfirmationWatcher) extractTokenIDsFromReceipt(receipt *types.Receipt) ([]string, error) {
	var tokenIDs []string

	log.Debug().
		Str("tx_hash", receipt.TxHash.Hex()).
		Int("logs_count", len(receipt.Logs)).
		Msg("Extracting token IDs from transaction receipt")

	// Parse Transfer events to extract token IDs
	// Transfer event signature: Transfer(address indexed from, address indexed to, uint256 indexed tokenId)
	transferEventSignature := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	for _, logEntry := range receipt.Logs {
		// Check if this is a Transfer event
		if len(logEntry.Topics) >= 4 && logEntry.Topics[0] == transferEventSignature {
			// Extract token ID from the third indexed parameter
			tokenID := new(big.Int).SetBytes(logEntry.Topics[3].Bytes())
			tokenIDs = append(tokenIDs, tokenID.String())

			log.Debug().
				Str("token_id", tokenID.String()).
				Str("from", common.BytesToAddress(logEntry.Topics[1].Bytes()).Hex()).
				Str("to", common.BytesToAddress(logEntry.Topics[2].Bytes()).Hex()).
				Msg("Found NFT Transfer event")
		}
	}

	// Sort token IDs to ensure consistent order
	sort.Strings(tokenIDs)

	log.Info().
		Str("tx_hash", receipt.TxHash.Hex()).
		Int("token_count", len(tokenIDs)).
		Strs("token_ids", tokenIDs).
		Msg("Extracted token IDs from transaction receipt")

	return tokenIDs, nil
}
