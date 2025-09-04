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

// TxConfirmationWatcher monitors blockchain transactions for confirmations
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

// NewTxConfirmationWatcher creates a new transaction confirmation watcher
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
		Msg("Starting transaction confirmation watcher")

	w.startedAt = time.Now()

	// Start main monitoring loop
	w.workerWg.Add(1)
	go w.runConfirmationLoop(ctx)

	// Start cleanup job for old failed batches
	w.workerWg.Add(1)
	go w.runCleanupLoop(ctx)

	log.Info().Msg("Transaction confirmation watcher started successfully")
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

// runConfirmationLoop runs the main confirmation monitoring loop
func (w *TxConfirmationWatcher) runConfirmationLoop(ctx context.Context) {
	defer w.workerWg.Done()

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	log.Info().Msg("Starting confirmation monitoring loop")

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.processPendingBatches(ctx)
		}
	}
}

// runCleanupLoop runs the cleanup loop for old failed batches
func (w *TxConfirmationWatcher) runCleanupLoop(ctx context.Context) {
	defer w.workerWg.Done()

	// Clean up every hour
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	log.Info().Msg("Starting confirmation watcher cleanup loop")

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.cleanupOldFailedBatches(ctx)
		}
	}
}

// processPendingBatches processes all pending batches waiting for confirmation
func (w *TxConfirmationWatcher) processPendingBatches(ctx context.Context) {
	pendingBatches, err := w.getPendingBatches(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get pending batches")
		return
	}

	if len(pendingBatches) == 0 {
		log.Debug().Msg("No pending batches to process")
		return
	}

	log.Info().
		Int("pending_count", len(pendingBatches)).
		Msg("Processing pending batches for confirmation")

	for _, batch := range pendingBatches {
		select {
		case <-ctx.Done():
			return
		case <-w.stopChan:
			return
		default:
			w.processBatchConfirmation(ctx, batch)
		}
	}
}

// getPendingBatches retrieves all batches in submitted status
func (w *TxConfirmationWatcher) getPendingBatches(ctx context.Context) ([]PendingBatch, error) {
	query := `
		SELECT 
			batch_id, chain_id, token_id, tx_hash, status, 
			COALESCE(submitted_at, created_at) as submitted_at,
			COALESCE(retry_count, 0) as retry_count,
			batch_type
		FROM batches 
		WHERE status = 'submitted' 
		AND tx_hash IS NOT NULL 
		AND tx_hash != ''
		AND COALESCE(retry_count, 0) < $1
		ORDER BY submitted_at ASC
		LIMIT 50`

	rows, err := w.db.QueryContext(ctx, query, w.maxRetries)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending batches: %w", err)
	}
	defer rows.Close()

	var batches []PendingBatch
	for rows.Next() {
		var batch PendingBatch
		err := rows.Scan(
			&batch.BatchID,
			&batch.ChainID,
			&batch.TokenID,
			&batch.TxHash,
			&batch.Status,
			&batch.SubmittedAt,
			&batch.RetryCount,
			&batch.BatchType,
		)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan batch row")
			continue
		}

		// Get operations for this batch
		operations, err := w.getBatchOperations(ctx, batch.BatchID)
		if err != nil {
			log.Error().
				Str("batch_id", batch.BatchID).
				Err(err).
				Msg("Failed to get batch operations")
			continue
		}
		batch.Operations = operations

		batches = append(batches, batch)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate batch rows: %w", err)
	}

	return batches, nil
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

// processBatchConfirmation processes confirmation for a single batch
func (w *TxConfirmationWatcher) processBatchConfirmation(ctx context.Context, batch PendingBatch) {
	log.Debug().
		Str("batch_id", batch.BatchID).
		Int64("chain_id", batch.ChainID).
		Str("tx_hash", batch.TxHash).
		Msg("Processing batch confirmation")

	caller := w.callers[batch.ChainID]
	if caller == nil {
		log.Error().
			Int64("chain_id", batch.ChainID).
			Str("batch_id", batch.BatchID).
			Msg("No unified caller found for chain")
		w.markBatchAsFailed(ctx, batch, "no_caller_found")
		return
	}

	// Check transaction confirmation status (simplified implementation)
	confirmed, confirmationCount, err := w.checkTransactionConfirmation(ctx, caller, batch.TxHash, w.confirmationBlocks)
	if err != nil {
		log.Error().
			Str("batch_id", batch.BatchID).
			Str("tx_hash", batch.TxHash).
			Err(err).
			Msg("Failed to check transaction confirmation")

		// Increment retry count
		w.incrementBatchRetryCount(ctx, batch)
		return
	}

	if confirmed {
		// Transaction is confirmed
		log.Info().
			Str("batch_id", batch.BatchID).
			Str("tx_hash", batch.TxHash).
			Int("confirmations", confirmationCount).
			Msg("Batch transaction confirmed")

		err = w.confirmBatch(ctx, batch)
		if err != nil {
			log.Error().
				Str("batch_id", batch.BatchID).
				Err(err).
				Msg("Failed to confirm batch")
			w.failedCount++
		} else {
			w.processedCount++
		}
	} else {
		// Check if transaction has been pending too long
		if time.Since(batch.SubmittedAt) > 10*time.Minute {
			log.Warn().
				Str("batch_id", batch.BatchID).
				Str("tx_hash", batch.TxHash).
				Dur("pending_time", time.Since(batch.SubmittedAt)).
				Int("confirmations", confirmationCount).
				Msg("Batch transaction pending for too long")

			// For now, just increment retry count
			// In production, you might want more sophisticated logic
			w.incrementBatchRetryCount(ctx, batch)
		} else {
			log.Debug().
				Str("batch_id", batch.BatchID).
				Str("tx_hash", batch.TxHash).
				Int("confirmations", confirmationCount).
				Int("required", w.confirmationBlocks).
				Msg("Batch transaction not yet confirmed")
		}
	}
}

// confirmBatch confirms a successful batch transaction
func (w *TxConfirmationWatcher) confirmBatch(ctx context.Context, batch PendingBatch) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Debug().Err(err).Msg("Transaction rollback error (expected if committed)")
		}
	}()

	// Update batch status to confirmed
	err = w.updateBatchToConfirmed(tx, batch.BatchID)
	if err != nil {
		return fmt.Errorf("failed to update batch to confirmed: %w", err)
	}

	// Update all associated transactions to confirmed
	err = w.updateTransactionsToConfirmed(tx, batch.BatchID)
	if err != nil {
		return fmt.Errorf("failed to update transactions to confirmed: %w", err)
	}

	// Finalize user balances based on operations
	err = w.finalizeUserBalancesForBatch(tx, batch)
	if err != nil {
		return fmt.Errorf("failed to finalize user balances: %w", err)
	}

	// Finalize NFT assets for NFT operations
	err = w.finalizeNFTAssetsForBatch(tx, batch)
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

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch confirmation: %w", err)
	}

	log.Info().
		Str("batch_id", batch.BatchID).
		Str("tx_hash", batch.TxHash).
		Int("operations", len(batch.Operations)).
		Msg("Batch successfully confirmed")

	// Send success notifications after successful confirmation
	w.sendSuccessNotifications(ctx, batch)

	return nil
}

// updateBatchToConfirmed updates batch status to confirmed
func (w *TxConfirmationWatcher) updateBatchToConfirmed(tx *sql.Tx, batchID string) error {
	query := `
		UPDATE batches 
		SET 
			status = 'confirmed',
			confirmed_at = $2
		WHERE batch_id = $1`

	_, err := tx.Exec(query, batchID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update batch to confirmed: %w", err)
	}

	return nil
}

// updateTransactionsToConfirmed updates transactions status to confirmed
func (w *TxConfirmationWatcher) updateTransactionsToConfirmed(tx *sql.Tx, batchID string) error {
	query := `
		UPDATE transactions 
		SET 
			status = 'confirmed',
			confirmed_at = $2
		WHERE batch_id = $1`

	_, err := tx.Exec(query, batchID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update transactions to confirmed: %w", err)
	}

	return nil
}

// finalizeUserBalancesForBatch finalizes user balances for all operations in the batch
func (w *TxConfirmationWatcher) finalizeUserBalancesForBatch(tx *sql.Tx, batch PendingBatch) error {
	for _, op := range batch.Operations {
		switch op.TxType {
		case "mint":
			err := w.finalizeBalanceForMint(tx, op, batch.ChainID, batch.TokenID)
			if err != nil {
				return fmt.Errorf("failed to finalize mint balance: %w", err)
			}
		case "burn":
			err := w.finalizeBalanceForBurn(tx, op, batch.ChainID, batch.TokenID)
			if err != nil {
				return fmt.Errorf("failed to finalize burn balance: %w", err)
			}
		case "transfer":
			err := w.finalizeBalanceForTransfer(tx, op, batch.ChainID, batch.TokenID)
			if err != nil {
				return fmt.Errorf("failed to finalize transfer balance: %w", err)
			}
		default:
			log.Warn().
				Str("tx_type", op.TxType).
				Str("tx_id", op.TxID).
				Msg("Unknown transaction type in batch operations")
		}
	}
	return nil
}

// finalizeBalanceForMint finalizes balance after successful mint
func (w *TxConfirmationWatcher) finalizeBalanceForMint(tx *sql.Tx, op BatchOperation, chainID int64, tokenID int) error {
	// Add minted amount directly to confirmed_balance
	query := `
		INSERT INTO user_balances (user_id, chain_id, token_id, confirmed_balance, pending_balance, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 0, $5, $5)
		ON CONFLICT (user_id, chain_id, token_id) 
		DO UPDATE SET 
			confirmed_balance = user_balances.confirmed_balance + $4,
			updated_at = $5,
			last_change_time = $5`

	_, err := tx.Exec(query, op.UserID, chainID, tokenID, op.Amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize mint balance: %w", err)
	}

	return nil
}

// finalizeBalanceForBurn finalizes balance after successful burn
func (w *TxConfirmationWatcher) finalizeBalanceForBurn(tx *sql.Tx, op BatchOperation, chainID int64, tokenID int) error {
	// Clear the pending_balance (amount was frozen during batching)
	query := `
		UPDATE user_balances 
		SET 
			pending_balance = pending_balance - $4,
			updated_at = $5,
			last_change_time = $5
		WHERE user_id = $1 AND chain_id = $2 AND token_id = $3`

	_, err := tx.Exec(query, op.UserID, chainID, tokenID, op.Amount.Abs(), time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize burn balance: %w", err)
	}

	return nil
}

// finalizeBalanceForTransfer finalizes balance after successful transfer
func (w *TxConfirmationWatcher) finalizeBalanceForTransfer(tx *sql.Tx, op BatchOperation, chainID int64, tokenID int) error {
	if !op.RelatedUserID.Valid {
		return fmt.Errorf("transfer operation missing related user ID")
	}

	// Check transfer direction and process accordingly
	if op.Direction.Valid && op.Direction.String == "outgoing" {
		// Clear sender's pending_balance (was frozen)
		senderQuery := `
			UPDATE user_balances 
			SET 
				pending_balance = pending_balance - $4,
				updated_at = $5,
				last_change_time = $5
			WHERE user_id = $1 AND chain_id = $2 AND token_id = $3`

		_, err := tx.Exec(senderQuery, op.UserID, chainID, tokenID, op.Amount, time.Now())
		if err != nil {
			return fmt.Errorf("failed to finalize sender balance: %w", err)
		}

		// Add amount to receiver's confirmed_balance
		receiverQuery := `
			INSERT INTO user_balances (user_id, chain_id, token_id, confirmed_balance, pending_balance, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 0, $5, $5)
			ON CONFLICT (user_id, chain_id, token_id) 
			DO UPDATE SET 
				confirmed_balance = user_balances.confirmed_balance + $4,
				updated_at = $5,
				last_change_time = $5`

		_, err = tx.Exec(receiverQuery, op.RelatedUserID.String, chainID, tokenID, op.Amount, time.Now())
		if err != nil {
			return fmt.Errorf("failed to finalize receiver balance: %w", err)
		}
	}

	return nil
}

// markBatchAsFailed marks a batch as failed
func (w *TxConfirmationWatcher) markBatchAsFailed(ctx context.Context, batch PendingBatch, reason string) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to begin transaction for batch failure")
		return
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Debug().Err(err).Msg("Transaction rollback error (expected if committed)")
		}
	}()

	// Update batch status to failed
	batchQuery := `
		UPDATE batches 
		SET 
			status = 'failed',
			failure_reason = $2
		WHERE batch_id = $1`

	_, err = tx.Exec(batchQuery, batch.BatchID, reason)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update batch to failed")
		return
	}

	// Update transactions to failed
	txQuery := `UPDATE transactions SET status = 'failed' WHERE batch_id = $1`
	_, err = tx.Exec(txQuery, batch.BatchID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update transactions to failed")
		return
	}

	// Unfreeze balances for failed operations
	for _, op := range batch.Operations {
		err = w.unfreezeBalanceForOperation(tx, op, batch.ChainID, batch.TokenID)
		if err != nil {
			log.Error().
				Err(err).
				Str("tx_id", op.TxID).
				Msg("Failed to unfreeze balance for failed operation")
		}
	}

	if err = tx.Commit(); err != nil {
		log.Error().Err(err).Msg("Failed to commit batch failure")
	} else {
		log.Info().
			Str("batch_id", batch.BatchID).
			Str("reason", reason).
			Msg("Batch marked as failed")
		w.failedCount++
	}
}

// unfreezeBalanceForOperation unfreezes balance for a failed operation
func (w *TxConfirmationWatcher) unfreezeBalanceForOperation(tx *sql.Tx, op BatchOperation, chainID int64, tokenID int) error {
	switch op.TxType {
	case "burn":
		// Unfreeze: move from pending_balance back to confirmed_balance
		query := `
			UPDATE user_balances 
			SET 
				confirmed_balance = confirmed_balance + $4,
				pending_balance = pending_balance - $4,
				updated_at = $5,
				last_change_time = $5
			WHERE user_id = $1 AND chain_id = $2 AND token_id = $3 
			AND pending_balance >= $4`

		_, err := tx.Exec(query, op.UserID, chainID, tokenID, op.Amount.Abs(), time.Now())
		return err

	case "transfer":
		if op.Direction.Valid && op.Direction.String == "outgoing" {
			// Unfreeze sender's balance
			query := `
				UPDATE user_balances 
				SET 
					confirmed_balance = confirmed_balance + $4,
					pending_balance = pending_balance - $4,
					updated_at = $5,
					last_change_time = $5
				WHERE user_id = $1 AND chain_id = $2 AND token_id = $3 
				AND pending_balance >= $4`

			_, err := tx.Exec(query, op.UserID, chainID, tokenID, op.Amount, time.Now())
			return err
		}
	}

	// Mint operations don't need unfreezing
	return nil
}

// incrementBatchRetryCount increments the retry count for a batch
func (w *TxConfirmationWatcher) incrementBatchRetryCount(ctx context.Context, batch PendingBatch) {
	query := `
		UPDATE batches 
		SET retry_count = COALESCE(retry_count, 0) + 1 
		WHERE batch_id = $1`

	_, err := w.db.ExecContext(ctx, query, batch.BatchID)
	if err != nil {
		log.Error().
			Str("batch_id", batch.BatchID).
			Err(err).
			Msg("Failed to increment batch retry count")
	} else {
		log.Debug().
			Str("batch_id", batch.BatchID).
			Int("retry_count", batch.RetryCount+1).
			Msg("Incremented batch retry count")
	}
}

// cleanupOldFailedBatches cleans up old failed batches
func (w *TxConfirmationWatcher) cleanupOldFailedBatches(ctx context.Context) {
	// Mark batches that have exceeded max retries as failed
	failedQuery := `
		UPDATE batches 
		SET 
			status = 'failed',
			failure_reason = 'max_retries_exceeded'
		WHERE status = 'submitted' 
		AND COALESCE(retry_count, 0) >= $1
		AND submitted_at < $2`

	cutoffTime := time.Now().Add(-24 * time.Hour) // 24 hours old
	result, err := w.db.ExecContext(ctx, failedQuery, w.maxRetries, cutoffTime)
	if err != nil {
		log.Error().Err(err).Msg("Failed to mark old batches as failed")
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected > 0 {
		log.Info().
			Int64("batches_failed", rowsAffected).
			Msg("Marked old batches as failed due to max retries")
	}
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

	// 6. Update database confirmation status if confirmed
	if confirmed {
		err = w.updateConfirmationStatus(ctx, txHash, int(confirmations), receipt.BlockNumber.Uint64())
		if err != nil {
			log.Warn().
				Err(err).
				Str("tx_hash", txHash).
				Msg("Failed to update confirmation status in database")
		}
	}

	log.Debug().
		Str("tx_hash", txHash).
		Int("confirmations", int(confirmations)).
		Int("required", requiredBlocks).
		Bool("confirmed", confirmed).
		Msg("Transaction confirmation check result")

	return confirmed, int(confirmations), nil
}

// updateConfirmationStatus updates database with transaction confirmation status
func (w *TxConfirmationWatcher) updateConfirmationStatus(
	ctx context.Context,
	txHash string,
	confirmations int,
	blockNumber uint64,
) error {
	query := `
		UPDATE batches 
		SET 
			confirmations = $2, 
			confirmed_block = $3,
			updated_at = $4
		WHERE tx_hash = $1`

	_, err := w.db.ExecContext(ctx, query, txHash, confirmations, blockNumber, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update confirmation status: %w", err)
	}

	log.Debug().
		Str("tx_hash", txHash).
		Int("confirmations", confirmations).
		Uint64("block_number", blockNumber).
		Msg("Updated batch confirmation status in database")

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
