package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog/log"

	"github.com/hzbay/chain-bridge/internal/blockchain"
)

// TxConfirmationWatcher provides transaction confirmation utilities
type TxConfirmationWatcher struct {
	// Configuration
	confirmationBlocks int
}

// NewTxConfirmationWatcher creates a new transaction confirmation watcher tool
func NewTxConfirmationWatcher() *TxConfirmationWatcher {
	return &TxConfirmationWatcher{
		// Default configuration
		confirmationBlocks: 6, // 6 blocks for confirmation
	}
}

// WaitForConfirmation waits for a transaction to be confirmed with the required number of blocks
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

// checkTransactionConfirmation checks if a transaction has enough confirmations
func (w *TxConfirmationWatcher) checkTransactionConfirmation(
	ctx context.Context,
	caller *blockchain.BatchCaller,
	txHash string,
	requiredBlocks int,
) (bool, int, error) {
	// Get transaction receipt
	receipt, err := caller.GetTransactionReceipt(ctx, common.HexToHash(txHash))
	if err != nil {
		return false, 0, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	if receipt == nil {
		log.Debug().
			Str("tx_hash", txHash).
			Msg("Transaction receipt not available yet")
		return false, 0, nil
	}

	// Check if transaction was successful
	if receipt.Status != 1 {
		return false, 0, fmt.Errorf("transaction failed with status %d", receipt.Status)
	}

	// Get current block number
	currentBlockNumber, err := caller.GetEthClient().BlockNumber(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("failed to get current block number: %w", err)
	}

	// Calculate confirmations
	//nolint:gosec
	confirmations := int(currentBlockNumber - receipt.BlockNumber.Uint64())

	log.Debug().
		Str("tx_hash", txHash).
		Uint64("tx_block", receipt.BlockNumber.Uint64()).
		Uint64("current_block", currentBlockNumber).
		Int("confirmations", confirmations).
		Int("required", requiredBlocks).
		Msg("Transaction confirmation status")

	return confirmations >= requiredBlocks, confirmations, nil
}

// SetConfirmationBlocks sets the default number of confirmation blocks
func (w *TxConfirmationWatcher) SetConfirmationBlocks(blocks int) {
	w.confirmationBlocks = blocks
}

// GetConfirmationBlocks returns the default number of confirmation blocks
func (w *TxConfirmationWatcher) GetConfirmationBlocks() int {
	return w.confirmationBlocks
}
