package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"

	"github.com/hzbay/chain-bridge/internal/blockchain"
)

// TxConfirmationWatcher monitors blockchain transactions for confirmations
type TxConfirmationWatcher struct {
	db          *sql.DB
	cpopCallers map[int64]*blockchain.CPOPBatchCaller

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
	UserID        string          `db:"user_id"`
	RelatedUserID sql.NullString  `db:"related_user_id"`
	TxType        string          `db:"tx_type"`
	Amount        decimal.Decimal `db:"amount"`
	Direction     sql.NullString  `db:"transfer_direction"`
}

// NewTxConfirmationWatcher creates a new transaction confirmation watcher
func NewTxConfirmationWatcher(
	db *sql.DB,
	cpopCallers map[int64]*blockchain.CPOPBatchCaller,
) *TxConfirmationWatcher {
	return &TxConfirmationWatcher{
		db:          db,
		cpopCallers: cpopCallers,

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
			tx_id, user_id, related_user_id, tx_type, 
			amount, transfer_direction
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
			&op.UserID,
			&op.RelatedUserID,
			&op.TxType,
			&op.Amount,
			&op.Direction,
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

	caller := w.cpopCallers[batch.ChainID]
	if caller == nil {
		log.Error().
			Int64("chain_id", batch.ChainID).
			Str("batch_id", batch.BatchID).
			Msg("No CPOP caller found for chain")
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

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch confirmation: %w", err)
	}

	log.Info().
		Str("batch_id", batch.BatchID).
		Str("tx_hash", batch.TxHash).
		Int("operations", len(batch.Operations)).
		Msg("Batch successfully confirmed")

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
	caller *blockchain.CPOPBatchCaller,
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
	caller *blockchain.CPOPBatchCaller,
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
