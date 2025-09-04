package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"

	"github.com/hzbay/chain-bridge/internal/blockchain"
	"github.com/hzbay/chain-bridge/internal/models"
)

// processBatch processes a batch of messages with blockchain operation and three-table sync
func (c *RabbitMQBatchConsumer) processBatch(ctx context.Context, messages []*MessageWrapper, group BatchGroup) {
	log.Info().
		Int64("chain_id", group.ChainID).
		Int("token_id", group.TokenID).
		Str("job_type", string(group.JobType)).
		Int("batch_size", len(messages)).
		Msg("Processing message batch")

	startTime := time.Now()

	// Step 0: Pre-validate user balances and separate valid/invalid operations
	validMessages, invalidMessages, err := c.validateAndSeparateByBalance(ctx, messages)
	if err != nil {
		log.Error().Err(err).Msg("Failed to validate user balances")
		c.nackAllMessages(messages)
		return
	}

	// Handle invalid messages (insufficient balance)
	if len(invalidMessages) > 0 {
		log.Warn().
			Int("invalid_count", len(invalidMessages)).
			Int("valid_count", len(validMessages)).
			Msg("Some operations have insufficient balance")

		// Process invalid messages separately
		c.handleInsufficientBalanceMessages(ctx, invalidMessages)
	}

	// If no valid messages, exit early
	if len(validMessages) == 0 {
		log.Info().Msg("No valid operations to process after balance validation")
		return
	}

	// Update log to reflect actual processing count
	log.Info().
		Int("original_batch_size", len(messages)).
		Int("valid_operations", len(validMessages)).
		Int("invalid_operations", len(invalidMessages)).
		Msg("Proceeding with valid operations only")

	// Step 1: Insert/Update transactions to 'batching' status (only valid messages)
	batchID := uuid.New()
	err = c.updateTransactionsToBatching(ctx, validMessages, batchID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update transactions to batching status")
		c.nackAllMessages(validMessages)
		return
	}

	// Step 2: Execute blockchain batch operation (only valid messages)
	result, err := c.executeBlockchainBatch(ctx, validMessages, group)
	if err != nil {
		log.Error().Err(err).Msg("Blockchain batch operation failed")
		c.handleBatchFailure(ctx, validMessages, batchID, err)
		return
	}

	// Step 2.5: Update to 'submitted' status after successful blockchain submission
	err = c.updateBatchToSubmitted(ctx, batchID, result)
	if err != nil {
		log.Error().Err(err).
			Str("tx_hash", result.TxHash).
			Msg("Failed to update batch to submitted status")
		// NOTE: Blockchain operation succeeded but status update failed
		// The batch will be picked up by the confirmation monitor
	}

	processingTime := time.Since(startTime)

	// Step 3: Wait for blockchain confirmation using the confirmation watcher
	if c.confirmationWatcher != nil {
		// Get the appropriate caller for this chain
		caller, exists := c.cpopCallers[group.ChainID]
		if !exists {
			log.Error().
				Int64("chain_id", group.ChainID).
				Msg("No unified caller found for chain")
			c.handleBatchFailure(ctx, messages, batchID, fmt.Errorf("no unified caller for chain %d", group.ChainID))
			return
		}

		confirmed, confirmations, err := c.confirmationWatcher.WaitForConfirmation(
			ctx,
			caller,
			result.TxHash,
			6,              // 6 blocks for confirmation
			10*time.Minute, // 10 minute timeout
		)

		if err != nil {
			log.Error().Err(err).
				Str("tx_hash", result.TxHash).
				Msg("Failed to wait for transaction confirmation")
			// Transaction was submitted but confirmation failed
			// Let the background confirmation watcher handle it
		} else if confirmed {
			log.Info().
				Str("tx_hash", result.TxHash).
				Int("confirmations", confirmations).
				Msg("Transaction confirmed, completing batch")

			// Complete the batch with confirmed transaction
			err = c.completeSuccessfulBatch(ctx, messages, group, batchID, result, processingTime)
			if err != nil {
				log.Error().Err(err).
					Str("tx_hash", result.TxHash).
					Msg("Failed to complete confirmed batch")
				c.nackAllMessages(messages)
				return
			}
		} else {
			log.Info().
				Str("tx_hash", result.TxHash).
				Msg("Transaction submitted but not yet confirmed, background watcher will handle completion")
		}
	} else {
		// Fallback: complete immediately if no confirmation watcher available
		log.Warn().Msg("No confirmation watcher available, completing batch immediately")
		err = c.completeSuccessfulBatch(ctx, messages, group, batchID, result, processingTime)
		if err != nil {
			log.Error().Err(err).
				Str("tx_hash", result.TxHash).
				Msg("Failed to complete successful batch")
			c.nackAllMessages(messages)
			return
		}
	}

	// Step 4: ACK all messages after successful processing
	c.ackAllMessages(messages)

	// Step 5: Record performance metrics
	if c.batchOptimizer != nil {
		// Get current gas price from blockchain result or estimate
		gasPrice := 0.0
		if result.GasUsed > 0 {
			// Estimate gas price from total cost / gas used
			gasPrice = float64(result.GasSaved+result.GasUsed) / float64(result.GasUsed)
		}

		performance := BatchPerformance{
			BatchSize:        len(messages),
			ProcessingTime:   processingTime,
			GasSaved:         float64(result.GasSaved),
			EfficiencyRating: result.Efficiency,
			Timestamp:        time.Now(),
			ChainID:          group.ChainID,
			TokenID:          group.TokenID,
			// Enhanced metrics for chain optimization
			GasPrice:      gasPrice,
			BlockNumber:   c.extractBlockNumber(result.BlockNumber),
			WaitTime:      int(c.maxWaitTime.Milliseconds()),
			ConsumerCount: c.consumerCount,
		}
		c.batchOptimizer.RecordBatchPerformance(performance)
	}

	log.Info().
		Str("batch_id", batchID.String()).
		Str("tx_hash", result.TxHash).
		Float64("efficiency", result.Efficiency).
		Dur("processing_time", processingTime).
		Int("messages_processed", len(messages)).
		Msg("Batch processed successfully")
}

// validateAndSeparateByBalance validates user balances and separates valid/invalid operations
func (c *RabbitMQBatchConsumer) validateAndSeparateByBalance(ctx context.Context, messages []*MessageWrapper) ([]*MessageWrapper, []*MessageWrapper, error) {
	var validMessages []*MessageWrapper
	var invalidMessages []*MessageWrapper

	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin read transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Debug().Err(err).Msg("Transaction rollback error (expected if committed)")
		}
	}()

	for _, msg := range messages {
		job := msg.Job
		switch typedJob := job.(type) {
		case AssetAdjustJob:
			if typedJob.AdjustmentType == "burn" {
				valid, err := c.validateBurnBalance(tx, typedJob)
				if err != nil {
					log.Warn().Err(err).Str("job_id", job.GetID()).Msg("Failed to validate burn balance")
					invalidMessages = append(invalidMessages, msg)
				} else if valid {
					validMessages = append(validMessages, msg)
				} else {
					invalidMessages = append(invalidMessages, msg)
				}
			} else {
				// Mint operations don't require balance validation
				validMessages = append(validMessages, msg)
			}
		case TransferJob:
			valid, err := c.validateTransferBalance(tx, typedJob)
			if err != nil {
				log.Warn().Err(err).Str("job_id", job.GetID()).Msg("Failed to validate transfer balance")
				invalidMessages = append(invalidMessages, msg)
			} else if valid {
				validMessages = append(validMessages, msg)
			} else {
				invalidMessages = append(invalidMessages, msg)
			}
		default:
			// Unknown job type, consider valid by default
			validMessages = append(validMessages, msg)
		}
	}

	return validMessages, invalidMessages, nil
}

// validateBurnBalance validates if user has sufficient balance for burn operation
func (c *RabbitMQBatchConsumer) validateBurnBalance(tx *sql.Tx, job AssetAdjustJob) (bool, error) {
	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return false, fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	if amount.IsNegative() {
		amount = amount.Abs()
	}

	query := `
		SELECT COALESCE(confirmed_balance, 0) as balance
		FROM user_balances 
		WHERE user_id = $1 AND chain_id = $2 AND token_id = $3`

	var balanceStr string
	err = tx.QueryRow(query, job.UserID, job.ChainID, job.TokenID).Scan(&balanceStr)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("failed to query user balance: %w", err)
	}

	// If no balance record exists, balance is 0
	var balance decimal.Decimal
	if errors.Is(err, sql.ErrNoRows) {
		balance = decimal.Zero
	} else {
		balance, err = decimal.NewFromString(balanceStr)
		if err != nil {
			return false, fmt.Errorf("failed to parse balance: %w", err)
		}
	}

	return balance.Cmp(amount) >= 0, nil
}

// validateTransferBalance validates if user has sufficient balance for transfer operation
func (c *RabbitMQBatchConsumer) validateTransferBalance(tx *sql.Tx, job TransferJob) (bool, error) {
	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return false, fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	if amount.IsNegative() {
		return false, fmt.Errorf("transfer amount cannot be negative: %s", job.Amount)
	}

	query := `
		SELECT COALESCE(confirmed_balance, 0) as balance
		FROM user_balances 
		WHERE user_id = $1 AND chain_id = $2 AND token_id = $3`

	var balanceStr string
	err = tx.QueryRow(query, job.FromUserID, job.ChainID, job.TokenID).Scan(&balanceStr)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("failed to query user balance: %w", err)
	}

	// If no balance record exists, balance is 0
	var balance decimal.Decimal
	if errors.Is(err, sql.ErrNoRows) {
		balance = decimal.Zero
	} else {
		balance, err = decimal.NewFromString(balanceStr)
		if err != nil {
			return false, fmt.Errorf("failed to parse balance: %w", err)
		}
	}

	return balance.Cmp(amount) >= 0, nil
}

// handleInsufficientBalanceMessages handles messages with insufficient balance
func (c *RabbitMQBatchConsumer) handleInsufficientBalanceMessages(ctx context.Context, messages []*MessageWrapper) {
	if len(messages) == 0 {
		return
	}

	log.Warn().Int("count", len(messages)).Msg("Handling insufficient balance operations")

	// Mark transactions as failed due to insufficient balance
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to begin transaction for insufficient balance handling")
		c.nackAllMessages(messages)
		return
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Debug().Err(err).Msg("Transaction rollback error (expected if committed)")
		}
	}()

	for _, msg := range messages {
		job := msg.Job

		// Update transaction status to failed with specific reason
		updateQuery := `
			UPDATE transactions 
			SET 
				status = 'failed',
				failure_reason = 'insufficient_balance',
				updated_at = $2
			WHERE tx_id = $1`

		_, err = tx.Exec(updateQuery, job.GetID(), time.Now())
		if err != nil {
			log.Error().Err(err).
				Str("tx_id", job.GetID()).
				Msg("Failed to update transaction status to failed")
			continue
		}

		// Send notification for insufficient balance
		c.sendInsufficientBalanceNotification(ctx, job)

		// ACK the message since we've processed it (marked as failed)
		if err := msg.Delivery.Ack(false); err != nil {
			// Only log error if client is still healthy
			if c.client != nil && c.client.IsHealthy() {
				log.Error().Err(err).
					Str("job_id", job.GetID()).
					Msg("Failed to ACK insufficient balance message")
			} else {
				log.Debug().Err(err).
					Str("job_id", job.GetID()).
					Msg("Skipping ACK error - RabbitMQ client not healthy")
			}
		}
	}

	if err = tx.Commit(); err != nil {
		log.Error().Err(err).Msg("Failed to commit insufficient balance transaction updates")
		c.nackAllMessages(messages)
		return
	}

	log.Info().
		Int("failed_count", len(messages)).
		Msg("Successfully marked insufficient balance operations as failed")
}

// sendInsufficientBalanceNotification sends notification for insufficient balance
func (c *RabbitMQBatchConsumer) sendInsufficientBalanceNotification(ctx context.Context, job BatchJob) {
	if c.batchProcessor == nil {
		return
	}

	var userID string
	var notificationData map[string]interface{}

	switch typedJob := job.(type) {
	case TransferJob:
		userID = typedJob.FromUserID
		notificationData = map[string]interface{}{
			"type":          "insufficient_balance",
			"operation":     "transfer",
			"amount":        typedJob.Amount,
			"chain_id":      typedJob.ChainID,
			"token_id":      typedJob.TokenID,
			"from_user_id":  typedJob.FromUserID,
			"to_user_id":    typedJob.ToUserID,
			"business_type": typedJob.BusinessType,
			"reason":        "Insufficient balance for transfer operation",
		}
	case AssetAdjustJob:
		userID = typedJob.UserID
		notificationData = map[string]interface{}{
			"type":          "insufficient_balance",
			"operation":     typedJob.AdjustmentType,
			"amount":        typedJob.Amount,
			"chain_id":      typedJob.ChainID,
			"token_id":      typedJob.TokenID,
			"user_id":       typedJob.UserID,
			"business_type": typedJob.BusinessType,
			"reason":        fmt.Sprintf("Insufficient balance for %s operation", typedJob.AdjustmentType),
		}
	default:
		log.Warn().Str("job_type", fmt.Sprintf("%T", job)).Msg("Unknown job type for insufficient balance notification")
		return
	}

	if userID == "" {
		return
	}

	notification := NotificationJob{
		ID:        uuid.New().String(),
		JobType:   JobTypeNotification,
		UserID:    userID,
		EventType: "insufficient_balance",
		Data:      notificationData,
		Priority:  PriorityHigh, // High priority for balance issues
		CreatedAt: time.Now(),
	}

	if err := c.batchProcessor.PublishNotification(ctx, notification); err != nil {
		log.Warn().Err(err).
			Str("user_id", userID).
			Str("job_id", job.GetID()).
			Msg("Failed to send insufficient balance notification")
	}
}

// updateTransactionsToBatching updates transaction records to 'batching' status and freezes balances
func (c *RabbitMQBatchConsumer) updateTransactionsToBatching(ctx context.Context, messages []*MessageWrapper, batchID uuid.UUID) error {
	if c.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Debug().Err(err).Msg("Transaction rollback error (expected if committed)")
		}
	}()

	// 1. Create batch record in 'preparing' status
	err = c.createPreparingBatchRecord(tx, batchID, messages)
	if err != nil {
		return fmt.Errorf("failed to create preparing batch record: %w", err)
	}

	// 2. Update transaction status and freeze balances atomically
	for _, msgWrapper := range messages {
		job := msgWrapper.Job

		// Insert or update transaction record
		err = c.upsertTransactionRecord(tx, job, batchID)
		if err != nil {
			return fmt.Errorf("failed to upsert transaction record: %w", err)
		}

		// Freeze balances for burn/transfer operations
		err = c.freezeUserBalance(tx, job)
		if err != nil {
			return fmt.Errorf("failed to freeze user balance: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batching status update: %w", err)
	}

	return nil
}

// upsertTransactionRecord inserts or updates a transaction record
func (c *RabbitMQBatchConsumer) upsertTransactionRecord(tx *sql.Tx, job BatchJob, batchID uuid.UUID) error {
	var query string
	var args []interface{}

	switch j := job.(type) {
	case AssetAdjustJob:
		query = `
			INSERT INTO transactions (
				tx_id, operation_id, user_id, chain_id, tx_type, business_type,
				token_id, amount, status, batch_id, is_batch_operation,
				reason_type, reason_detail, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (tx_id) DO UPDATE SET
				status = 'batching',
				batch_id = EXCLUDED.batch_id,
				is_batch_operation = TRUE`

		amount, _ := decimal.NewFromString(j.Amount)
		args = []interface{}{
			j.TransactionID, uuid.New(), j.UserID, j.ChainID, j.AdjustmentType, j.BusinessType,
			j.TokenID, amount, "batching", batchID, true,
			j.ReasonType, j.ReasonDetail, j.CreatedAt,
		}

	case TransferJob:
		query = `
			INSERT INTO transactions (
				tx_id, operation_id, user_id, chain_id, tx_type, business_type,
				related_user_id, transfer_direction, token_id, amount, status, 
				batch_id, is_batch_operation, reason_type, reason_detail, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			ON CONFLICT (tx_id) DO UPDATE SET
				status = 'batching',
				batch_id = EXCLUDED.batch_id,
				is_batch_operation = TRUE`

		amount, _ := decimal.NewFromString(j.Amount)
		args = []interface{}{
			j.TransactionID, uuid.New(), j.FromUserID, j.ChainID, "transfer", j.BusinessType,
			j.ToUserID, "outgoing", j.TokenID, amount, "batching",
			batchID, true, j.ReasonType, j.ReasonDetail, j.CreatedAt,
		}

	default:
		return fmt.Errorf("unsupported job type: %T", job)
	}

	// Handle NFT job types
	switch j := job.(type) {
	case NFTMintJob:
		query = `
			INSERT INTO transactions (
				tx_id, operation_id, user_id, chain_id, tx_type, business_type,
				collection_id, nft_token_id, status, batch_id, is_batch_operation,
				reason_type, reason_detail, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (tx_id) DO UPDATE SET
				status = 'batching',
				batch_id = EXCLUDED.batch_id,
				is_batch_operation = TRUE`

		args = []interface{}{
			j.TransactionID, uuid.New(), j.ToUserID, j.ChainID, "nft_mint", j.BusinessType,
			j.CollectionID, j.TokenID, "batching", batchID, true,
			j.ReasonType, j.ReasonDetail, j.CreatedAt,
		}

	case NFTBurnJob:
		query = `
			INSERT INTO transactions (
				tx_id, operation_id, user_id, chain_id, tx_type, business_type,
				collection_id, nft_token_id, status, batch_id, is_batch_operation,
				reason_type, reason_detail, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (tx_id) DO UPDATE SET
				status = 'batching',
				batch_id = EXCLUDED.batch_id,
				is_batch_operation = TRUE`

		args = []interface{}{
			j.TransactionID, uuid.New(), j.OwnerUserID, j.ChainID, "nft_burn", j.BusinessType,
			j.CollectionID, j.TokenID, "batching", batchID, true,
			j.ReasonType, j.ReasonDetail, j.CreatedAt,
		}

	case NFTTransferJob:
		query = `
			INSERT INTO transactions (
				tx_id, operation_id, user_id, chain_id, tx_type, business_type,
				related_user_id, collection_id, nft_token_id, status, batch_id, is_batch_operation,
				reason_type, reason_detail, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (tx_id) DO UPDATE SET
				status = 'batching',
				batch_id = EXCLUDED.batch_id,
				is_batch_operation = TRUE`

		args = []interface{}{
			j.TransactionID, uuid.New(), j.FromUserID, j.ChainID, "nft_transfer", j.BusinessType,
			j.ToUserID, j.CollectionID, j.TokenID, "batching", batchID, true,
			j.ReasonType, j.ReasonDetail, j.CreatedAt,
		}
	}

	_, err := tx.Exec(query, args...)
	return err
}

// executeBlockchainBatch executes the blockchain batch operation
func (c *RabbitMQBatchConsumer) executeBlockchainBatch(ctx context.Context, messages []*MessageWrapper, group BatchGroup) (*blockchain.BatchResult, error) {
	caller := c.cpopCallers[group.ChainID]
	if caller == nil {
		return nil, fmt.Errorf("no unified caller found for chain %d", group.ChainID)
	}

	jobs := make([]BatchJob, len(messages))
	for i, msg := range messages {
		jobs[i] = msg.Job
	}

	switch group.JobType {
	case JobTypeAssetAdjust:
		return c.processAssetAdjustBatch(ctx, caller, jobs)
	case JobTypeTransfer:
		return c.processTransferBatch(ctx, caller, jobs)
	// NFT batch operations
	case JobTypeNFTMint:
		return c.processNFTMintBatch(ctx, caller, jobs)
	case JobTypeNFTBurn:
		return c.processNFTBurnBatch(ctx, caller, jobs)
	case JobTypeNFTTransfer:
		return c.processNFTTransferBatch(ctx, caller, jobs)
	default:
		return nil, fmt.Errorf("unsupported job type: %s", group.JobType)
	}
}

// processAssetAdjustBatch processes mint/burn batch
func (c *RabbitMQBatchConsumer) processAssetAdjustBatch(ctx context.Context, caller *blockchain.BatchCaller, jobs []BatchJob) (*blockchain.BatchResult, error) {
	var mintJobs, burnJobs []AssetAdjustJob

	// Separate mint and burn jobs
	for _, job := range jobs {
		if adjustJob, ok := job.(AssetAdjustJob); ok {
			if adjustJob.AdjustmentType == "mint" {
				mintJobs = append(mintJobs, adjustJob)
			} else if adjustJob.AdjustmentType == "burn" {
				burnJobs = append(burnJobs, adjustJob)
			}
		}
	}

	// Process mint jobs first (if any)
	if len(mintJobs) > 0 {
		recipients, amounts, err := c.prepareMintParams(ctx, mintJobs)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare mint parameters: %w", err)
		}
		return caller.BatchMint(ctx, recipients, amounts)
	}

	// Process burn jobs
	if len(burnJobs) > 0 {
		accounts, amounts, err := c.prepareBurnParams(ctx, burnJobs)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare burn parameters: %w", err)
		}
		return caller.BatchBurn(ctx, accounts, amounts)
	}

	return nil, fmt.Errorf("no valid mint or burn jobs found")
}

// processTransferBatch processes transfer batch using BatchTransferFrom
func (c *RabbitMQBatchConsumer) processTransferBatch(ctx context.Context, caller *blockchain.BatchCaller, jobs []BatchJob) (*blockchain.BatchResult, error) {
	var transferJobs []TransferJob
	for _, job := range jobs {
		if transferJob, ok := job.(TransferJob); ok {
			transferJobs = append(transferJobs, transferJob)
		}
	}

	if len(transferJobs) == 0 {
		return nil, fmt.Errorf("no valid transfer jobs found")
	}

	fromAddresses, toAddresses, amounts, err := c.prepareTransferFromParams(ctx, transferJobs)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare transfer parameters: %w", err)
	}
	return caller.BatchTransferFrom(ctx, fromAddresses, toAddresses, amounts)
}

// processNFTMintBatch processes NFT mint batch using BatchCaller
func (c *RabbitMQBatchConsumer) processNFTMintBatch(ctx context.Context, caller *blockchain.BatchCaller, jobs []BatchJob) (*blockchain.BatchResult, error) {
	// Check if NFT operations are supported on this chain
	if !caller.IsNFTEnabled() {
		return nil, fmt.Errorf("NFT operations not supported on chain %d", caller.GetChainID())
	}

	var mintJobs []NFTMintJob
	for _, job := range jobs {
		if mintJob, ok := job.(NFTMintJob); ok {
			mintJobs = append(mintJobs, mintJob)
		}
	}

	if len(mintJobs) == 0 {
		return nil, fmt.Errorf("no valid NFT mint jobs found")
	}

	recipients, tokenIDs, metadataURIs, err := c.prepareNFTMintParams(ctx, mintJobs)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare NFT mint parameters: %w", err)
	}

	nftResult, err := caller.NFTBatchMint(ctx, recipients, tokenIDs, metadataURIs)
	if err != nil {
		return nil, err
	}

	// Convert NFTBatchResult to blockchain.BatchResult
	return &blockchain.BatchResult{
		TxHash:      nftResult.TxHash,
		BlockNumber: nftResult.BlockNumber,
		GasUsed:     nftResult.GasUsed,
		GasSaved:    nftResult.GasSaved,
		Efficiency:  nftResult.Efficiency,
		Status:      nftResult.Status,
	}, nil
}

// processNFTBurnBatch processes NFT burn batch using BatchCaller
func (c *RabbitMQBatchConsumer) processNFTBurnBatch(ctx context.Context, caller *blockchain.BatchCaller, jobs []BatchJob) (*blockchain.BatchResult, error) {
	// Check if NFT operations are supported on this chain
	if !caller.IsNFTEnabled() {
		return nil, fmt.Errorf("NFT operations not supported on chain %d", caller.GetChainID())
	}

	var burnJobs []NFTBurnJob
	for _, job := range jobs {
		if burnJob, ok := job.(NFTBurnJob); ok {
			burnJobs = append(burnJobs, burnJob)
		}
	}

	if len(burnJobs) == 0 {
		return nil, fmt.Errorf("no valid NFT burn jobs found")
	}

	tokenIDs, err := c.prepareNFTBurnParams(ctx, burnJobs)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare NFT burn parameters: %w", err)
	}

	nftResult, err := caller.NFTBatchBurn(ctx, tokenIDs)
	if err != nil {
		return nil, err
	}

	// Convert NFTBatchResult to blockchain.BatchResult
	return &blockchain.BatchResult{
		TxHash:      nftResult.TxHash,
		BlockNumber: nftResult.BlockNumber,
		GasUsed:     nftResult.GasUsed,
		GasSaved:    nftResult.GasSaved,
		Efficiency:  nftResult.Efficiency,
		Status:      nftResult.Status,
	}, nil
}

// processNFTTransferBatch processes NFT transfer batch using BatchCaller
func (c *RabbitMQBatchConsumer) processNFTTransferBatch(ctx context.Context, caller *blockchain.BatchCaller, jobs []BatchJob) (*blockchain.BatchResult, error) {
	// Check if NFT operations are supported on this chain
	if !caller.IsNFTEnabled() {
		return nil, fmt.Errorf("NFT operations not supported on chain %d", caller.GetChainID())
	}

	var transferJobs []NFTTransferJob
	for _, job := range jobs {
		if transferJob, ok := job.(NFTTransferJob); ok {
			transferJobs = append(transferJobs, transferJob)
		}
	}

	if len(transferJobs) == 0 {
		return nil, fmt.Errorf("no valid NFT transfer jobs found")
	}

	fromAddresses, toAddresses, tokenIDs, err := c.prepareNFTTransferParams(ctx, transferJobs)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare NFT transfer parameters: %w", err)
	}

	nftResult, err := caller.NFTBatchTransferFrom(ctx, fromAddresses, toAddresses, tokenIDs)
	if err != nil {
		return nil, err
	}

	// Convert NFTBatchResult to blockchain.BatchResult
	return &blockchain.BatchResult{
		TxHash:      nftResult.TxHash,
		BlockNumber: nftResult.BlockNumber,
		GasUsed:     nftResult.GasUsed,
		GasSaved:    nftResult.GasSaved,
		Efficiency:  nftResult.Efficiency,
		Status:      nftResult.Status,
	}, nil
}

// prepareMintParams prepares parameters for batch mint
func (c *RabbitMQBatchConsumer) prepareMintParams(ctx context.Context, jobs []AssetAdjustJob) ([]common.Address, []*big.Int, error) {
	recipients := make([]common.Address, len(jobs))
	amounts := make([]*big.Int, len(jobs))

	for i, job := range jobs {
		// 根据用户ID和链ID查询AA钱包地址
		aaAddress, err := c.getUserAAAddress(ctx, job.UserID, job.ChainID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get AA address for user %s on chain %d: %w", job.UserID, job.ChainID, err)
		}
		recipients[i] = aaAddress

		// Process amount: remove +/- prefix and convert to ERC20 18-decimal precision
		amountStr := job.Amount
		if len(amountStr) > 0 && (amountStr[0] == '+' || amountStr[0] == '-') {
			amountStr = amountStr[1:] // Remove prefix
		}

		// Parse as float first to handle decimal values
		amountFloat, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid amount format: %s", job.Amount)
		}

		// Convert to ERC20 18-decimal precision (multiply by 10^18)
		amountInt := new(big.Int)
		// Use big.Float for precision to avoid floating point errors
		amountBigFloat := big.NewFloat(amountFloat)
		// Multiply by 10^18 for ERC20 precision
		multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
		amountBigFloat.Mul(amountBigFloat, multiplier)
		// Convert to big.Int
		amountBigFloat.Int(amountInt)

		amounts[i] = amountInt
	}

	return recipients, amounts, nil
}

// prepareBurnParams prepares parameters for batch burn
func (c *RabbitMQBatchConsumer) prepareBurnParams(ctx context.Context, jobs []AssetAdjustJob) ([]common.Address, []*big.Int, error) {
	accounts := make([]common.Address, len(jobs))
	amounts := make([]*big.Int, len(jobs))

	for i, job := range jobs {
		// 根据用户ID和链ID查询AA钱包地址
		aaAddress, err := c.getUserAAAddress(ctx, job.UserID, job.ChainID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get AA address for user %s on chain %d: %w", job.UserID, job.ChainID, err)
		}
		accounts[i] = aaAddress

		// Process amount: remove +/- prefix and convert to ERC20 18-decimal precision
		amountStr := job.Amount
		if len(amountStr) > 0 && (amountStr[0] == '+' || amountStr[0] == '-') {
			amountStr = amountStr[1:] // Remove prefix
		}

		// Parse as float first to handle decimal values
		amountFloat, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid amount format: %s", job.Amount)
		}

		// Convert to ERC20 18-decimal precision (multiply by 10^18)
		amountInt := new(big.Int)
		// Use big.Float for precision to avoid floating point errors
		amountBigFloat := big.NewFloat(amountFloat)
		// Multiply by 10^18 for ERC20 precision
		multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
		amountBigFloat.Mul(amountBigFloat, multiplier)
		// Convert to big.Int
		amountBigFloat.Int(amountInt)

		amounts[i] = amountInt
	}

	return accounts, amounts, nil
}

// prepareTransferFromParams prepares parameters for batch transfer from
func (c *RabbitMQBatchConsumer) prepareTransferFromParams(ctx context.Context, jobs []TransferJob) ([]common.Address, []common.Address, []*big.Int, error) {
	fromAddresses := make([]common.Address, len(jobs))
	toAddresses := make([]common.Address, len(jobs))
	amounts := make([]*big.Int, len(jobs))

	for i, job := range jobs {
		// 根据用户ID和链ID查询发送者的AA钱包地址
		fromAddress, err := c.getUserAAAddress(ctx, job.FromUserID, job.ChainID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get AA address for sender %s on chain %d: %w", job.FromUserID, job.ChainID, err)
		}
		fromAddresses[i] = fromAddress

		// 根据用户ID和链ID查询接收者的AA钱包地址
		toAddress, err := c.getUserAAAddress(ctx, job.ToUserID, job.ChainID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get AA address for recipient %s on chain %d: %w", job.ToUserID, job.ChainID, err)
		}
		toAddresses[i] = toAddress

		// Process amount: remove +/- prefix and convert to ERC20 18-decimal precision
		amountStr := job.Amount
		if len(amountStr) > 0 && (amountStr[0] == '+' || amountStr[0] == '-') {
			amountStr = amountStr[1:] // Remove prefix
		}

		// Parse as float first to handle decimal values
		amountFloat, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("invalid amount format: %s", job.Amount)
		}

		// Convert to ERC20 18-decimal precision (multiply by 10^18)
		amountInt := new(big.Int)
		// Use big.Float for precision to avoid floating point errors
		amountBigFloat := big.NewFloat(amountFloat)
		// Multiply by 10^18 for ERC20 precision
		multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
		amountBigFloat.Mul(amountBigFloat, multiplier)
		// Convert to big.Int
		amountBigFloat.Int(amountInt)

		amounts[i] = amountInt
	}

	return fromAddresses, toAddresses, amounts, nil
}

// updateBatchToSubmitted updates batch status to 'submitted' after successful blockchain submission
func (c *RabbitMQBatchConsumer) updateBatchToSubmitted(ctx context.Context, batchID uuid.UUID, result *blockchain.BatchResult) error {
	if c.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Debug().Err(err).Msg("Transaction rollback error (expected if committed)")
		}
	}()

	// Determine CPOP operation type based on the batch group
	// We need to get the batch type to determine the correct operation type
	batchTypeQuery := `SELECT batch_type FROM batches WHERE batch_id = $1`
	var batchType string
	err = tx.QueryRow(batchTypeQuery, batchID.String()).Scan(&batchType)
	if err != nil {
		return fmt.Errorf("failed to get batch type: %w", err)
	}

	// Map batch_type to cpop_operation_type
	var cpopOperationType string
	switch batchType {
	case "mint":
		cpopOperationType = models.CpopOperationTypeBatchMint
	case "burn":
		cpopOperationType = models.CpopOperationTypeBatchBurn
	case "transfer":
		cpopOperationType = models.CpopOperationTypeBatchTransfer
	// NFT batch types
	case "nft_mint":
		cpopOperationType = models.CpopOperationTypeBatchNFTMint
	case "nft_burn":
		cpopOperationType = models.CpopOperationTypeBatchNFTBurn
	case "nft_transfer":
		cpopOperationType = models.CpopOperationTypeBatchNFTTransfer
	case "asset_adjust": // Handle legacy data
		// For legacy asset_adjust batches, determine operation type from job data
		// Default to batch_mint for asset_adjust (most common case)
		cpopOperationType = models.CpopOperationTypeBatchMint
	default:
		cpopOperationType = models.CpopOperationTypeBatchMint // Default fallback
	}

	// Update batch status to submitted
	batchQuery := `
		UPDATE batches 
		SET 
			status = 'submitted',
			tx_hash = $2,
			actual_gas_used = $3,
			cpop_operation_type = $4,
			master_aggregator_used = $5
		WHERE batch_id = $1`

	_, err = tx.Exec(batchQuery,
		batchID.String(),
		result.TxHash,
		result.GasUsed,
		cpopOperationType, // Use correct enum value
		true)              // Assume aggregator was used

	if err != nil {
		return fmt.Errorf("failed to update batch to submitted: %w", err)
	}

	// Update all associated transactions to submitted status
	txQuery := `
		UPDATE transactions 
		SET 
			status = 'submitted',
			tx_hash = $2
		WHERE batch_id = $1`

	_, err = tx.Exec(txQuery, batchID.String(), result.TxHash)
	if err != nil {
		return fmt.Errorf("failed to update transactions to submitted: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit submitted status update: %w", err)
	}

	// Send batch status notification after successful commit
	c.sendBatchStatusNotification(ctx, batchID, "submitted", map[string]interface{}{
		"tx_hash":                result.TxHash,
		"gas_used":               result.GasUsed,
		"cpop_operation_type":    cpopOperationType, // Use correct enum value
		"master_aggregator_used": true,
	})

	log.Info().
		Str("batch_id", batchID.String()).
		Str("tx_hash", result.TxHash).
		Msg("Batch and transactions updated to submitted status")

	return nil
}

// completeSuccessfulBatch completes a successful batch with final status updates
func (c *RabbitMQBatchConsumer) completeSuccessfulBatch(
	ctx context.Context,
	messages []*MessageWrapper,
	_ BatchGroup,
	batchID uuid.UUID,
	result *blockchain.BatchResult,
	processingTime time.Duration,
) error {
	if c.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Debug().Err(err).Msg("Transaction rollback error (expected if committed)")
		}
	}()

	// Update batch to confirmed status
	err = c.updateBatchToConfirmed(tx, batchID, result, processingTime)
	if err != nil {
		return fmt.Errorf("failed to update batch to confirmed: %w", err)
	}

	// Update transactions to confirmed status
	err = c.updateTransactionsToConfirmed(tx, batchID, result)
	if err != nil {
		return fmt.Errorf("failed to update transactions to confirmed: %w", err)
	}

	// Update user balances based on final operations
	jobs := make([]BatchJob, len(messages))
	for i, msg := range messages {
		jobs[i] = msg.Job
	}

	err = c.finalizeUserBalances(tx, jobs)
	if err != nil {
		return fmt.Errorf("failed to finalize user balances: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit successful batch completion: %w", err)
	}

	// Send batch status notification after successful commit
	c.sendBatchStatusNotification(ctx, batchID, "confirmed", map[string]interface{}{
		"tx_hash":            result.TxHash,
		"gas_used":           result.GasUsed,
		"gas_saved":          result.GasSaved,
		"efficiency":         result.Efficiency,
		"processing_time_ms": processingTime.Milliseconds(),
		"transaction_count":  len(messages),
	})

	log.Info().
		Str("batch_id", batchID.String()).
		Str("tx_hash", result.TxHash).
		Msg("Batch completed successfully with all status updates")

	return nil
}

// updateBatchToConfirmed updates batch status to confirmed
func (c *RabbitMQBatchConsumer) updateBatchToConfirmed(tx *sql.Tx, batchID uuid.UUID, result *blockchain.BatchResult, _ time.Duration) error {
	query := `
		UPDATE batches 
		SET 
			status = 'confirmed',
			confirmed_at = $2,
			actual_efficiency = $3,
			gas_saved = $4,
			gas_saved_percentage = $5
		WHERE batch_id = $1`

	// Calculate gas saved percentage if GasSaved > 0
	var gasSavedPercentage float64
	if result.GasUsed > 0 && result.GasSaved > 0 {
		gasSavedPercentage = (float64(result.GasSaved) / float64(result.GasUsed+result.GasSaved)) * 100
	}

	_, err := tx.Exec(query,
		batchID.String(),
		time.Now(),
		result.Efficiency,
		result.GasSaved,
		gasSavedPercentage)

	if err != nil {
		return fmt.Errorf("failed to update batch to confirmed: %w", err)
	}

	return nil
}

// updateTransactionsToConfirmed updates transactions status to confirmed
func (c *RabbitMQBatchConsumer) updateTransactionsToConfirmed(tx *sql.Tx, batchID uuid.UUID, _ *blockchain.BatchResult) error {
	// First, get all transactions that will be updated for notifications
	getQuery := `
		SELECT user_id, amount, business_type, reason_type, related_user_id, transfer_direction, token_id, chain_id
		FROM transactions 
		WHERE batch_id = $1`

	rows, err := tx.Query(getQuery, batchID.String())
	if err != nil {
		return fmt.Errorf("failed to get transactions for notifications: %w", err)
	}
	defer rows.Close()

	var transactionNotifications []map[string]interface{}
	for rows.Next() {
		var userID, businessType, reasonType, relatedUserID, transferDirection sql.NullString
		var amount string
		var tokenID int
		var chainID int64

		err := rows.Scan(&userID, &amount, &businessType, &reasonType, &relatedUserID, &transferDirection, &tokenID, &chainID)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan transaction for notification")
			continue
		}

		// Create notification data for each transaction
		notificationData := map[string]interface{}{
			"status":       "confirmed",
			"batch_id":     batchID.String(),
			"amount":       amount,
			"token_id":     tokenID,
			"chain_id":     chainID,
			"confirmed_at": time.Now(),
		}

		if businessType.Valid {
			notificationData["business_type"] = businessType.String
		}
		if reasonType.Valid {
			notificationData["reason_type"] = reasonType.String
		}
		if userID.Valid {
			notificationData["user_id"] = userID.String
		}
		if relatedUserID.Valid {
			notificationData["related_user_id"] = relatedUserID.String

			// For transfers, map related_user_id to from_user_id/to_user_id based on direction
			if transferDirection.Valid {
				notificationData["transfer_direction"] = transferDirection.String
				if transferDirection.String == "outgoing" {
					// User is sender, related_user_id is receiver
					notificationData["from_user_id"] = userID.String
					notificationData["to_user_id"] = relatedUserID.String
				} else if transferDirection.String == "incoming" {
					// User is receiver, related_user_id is sender
					notificationData["from_user_id"] = relatedUserID.String
					notificationData["to_user_id"] = userID.String
				}
			}
		}

		transactionNotifications = append(transactionNotifications, notificationData)
	}

	// Update transactions status
	updateQuery := `
		UPDATE transactions 
		SET 
			status = 'confirmed',
			confirmed_at = $2
		WHERE batch_id = $1`

	_, err = tx.Exec(updateQuery, batchID.String(), time.Now())
	if err != nil {
		return fmt.Errorf("failed to update transactions to confirmed: %w", err)
	}

	// Send transaction status notifications (after successful update)
	c.sendTransactionStatusNotifications(transactionNotifications)

	return nil
}

// finalizeUserBalances finalizes user balances after successful batch completion
func (c *RabbitMQBatchConsumer) finalizeUserBalances(tx *sql.Tx, jobs []BatchJob) error {
	for _, job := range jobs {
		switch j := job.(type) {
		case AssetAdjustJob:
			if j.AdjustmentType == "mint" {
				err := c.finalizeBalanceForMint(tx, j)
				if err != nil {
					return fmt.Errorf("failed to finalize balance for mint: %w", err)
				}
			} else if j.AdjustmentType == "burn" {
				err := c.finalizeBalanceForBurn(tx, j)
				if err != nil {
					return fmt.Errorf("failed to finalize balance for burn: %w", err)
				}
			}
		case TransferJob:
			err := c.finalizeBalanceForTransfer(tx, j)
			if err != nil {
				return fmt.Errorf("failed to finalize balance for transfer: %w", err)
			}
		}
	}
	return nil
}

// finalizeBalanceForMint finalizes balance after successful mint
func (c *RabbitMQBatchConsumer) finalizeBalanceForMint(tx *sql.Tx, job AssetAdjustJob) error {
	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	// Mint should always be positive
	if amount.IsNegative() {
		amount = amount.Abs()
	}

	// Add minted amount directly to confirmed_balance
	query := `
		INSERT INTO user_balances (user_id, chain_id, token_id, confirmed_balance, pending_balance, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 0, $5, $5)
		ON CONFLICT (user_id, chain_id, token_id) 
		DO UPDATE SET 
			confirmed_balance = user_balances.confirmed_balance + $4,
			updated_at = $5,
			last_change_time = $5`

	_, err = tx.Exec(query, job.UserID, job.ChainID, job.TokenID, amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize balance for mint: %w", err)
	}

	// Send balance change notification for mint
	c.sendBalanceChangeNotification(job)

	return nil
}

// finalizeBalanceForBurn finalizes balance after successful burn
func (c *RabbitMQBatchConsumer) finalizeBalanceForBurn(tx *sql.Tx, job AssetAdjustJob) error {
	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	// Burn amount might be negative, take absolute value
	if amount.IsNegative() {
		amount = amount.Abs()
	}

	// Clear the pending_balance (the amount was already frozen)
	query := `
		UPDATE user_balances 
		SET 
			pending_balance = pending_balance - $4,
			updated_at = $5,
			last_change_time = $5
		WHERE user_id = $1 AND chain_id = $2 AND token_id = $3`

	_, err = tx.Exec(query, job.UserID, job.ChainID, job.TokenID, amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize balance for burn: %w", err)
	}

	// Send balance change notification for burn
	c.sendBalanceChangeNotification(job)

	return nil
}

// finalizeBalanceForTransfer finalizes balance after successful transfer
func (c *RabbitMQBatchConsumer) finalizeBalanceForTransfer(tx *sql.Tx, job TransferJob) error {
	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	// Clear sender's pending_balance (was frozen)
	senderQuery := `
		UPDATE user_balances 
		SET 
			pending_balance = pending_balance - $4,
			updated_at = $5,
			last_change_time = $5
		WHERE user_id = $1 AND chain_id = $2 AND token_id = $3`

	_, err = tx.Exec(senderQuery, job.FromUserID, job.ChainID, job.TokenID, amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize sender balance for transfer: %w", err)
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

	_, err = tx.Exec(receiverQuery, job.ToUserID, job.ChainID, job.TokenID, amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize receiver balance for transfer: %w", err)
	}

	// Send balance change notifications for transfer
	c.sendBalanceChangeNotification(job)

	return nil
}

// handleBatchFailure handles batch processing failures
func (c *RabbitMQBatchConsumer) handleBatchFailure(ctx context.Context, messages []*MessageWrapper, batchID uuid.UUID, failureErr error) {
	log.Error().Err(failureErr).Str("batch_id", batchID.String()).Msg("Handling batch failure")

	if c.db != nil {
		tx, err := c.db.BeginTx(ctx, nil)
		if err != nil {
			log.Error().Err(err).Msg("Failed to begin transaction for batch failure handling")
			c.nackAllMessages(messages)
			return
		}
		defer func() {
			if err := tx.Rollback(); err != nil {
				log.Debug().Err(err).Msg("Transaction rollback error (expected if committed)")
			}
		}()

		// Update batch status to failed
		batchQuery := `UPDATE batches SET status = 'failed' WHERE batch_id = $1`
		_, err = tx.Exec(batchQuery, batchID.String())
		if err != nil {
			log.Error().Err(err).Msg("Failed to update batch to failed status")
		}

		// Update transactions to failed status
		jobIDs := make([]string, len(messages))
		for i, msg := range messages {
			jobIDs[i] = msg.Job.GetID()
		}

		txQuery := `UPDATE transactions SET status = 'failed' WHERE tx_id = ANY($1)`
		_, err = tx.Exec(txQuery, pq.Array(jobIDs))
		if err != nil {
			log.Error().Err(err).Msg("Failed to update failed transaction statuses")
		} else {
			// Send transaction status notifications for failed transactions
			for _, msg := range messages {
				var userID string
				switch job := msg.Job.(type) {
				case TransferJob:
					userID = job.FromUserID // Notify sender about failed transaction
				case AssetAdjustJob:
					userID = job.UserID
				case NFTMintJob:
					userID = job.ToUserID
				case NFTBurnJob:
					userID = job.OwnerUserID
				case NFTTransferJob:
					userID = job.FromUserID // Notify sender about failed transaction
				}

				if userID != "" {
					if txID, err := uuid.Parse(msg.Job.GetID()); err == nil {
						extraData := map[string]interface{}{
							"failure_reason": failureErr.Error(),
							"batch_id":       batchID.String(),
							"chain_id":       c.chainID,
						}
						c.sendTransactionStatusNotification(ctx, txID, "failed", userID, extraData)
					}
				}
			}
		}

		// Unfreeze balances for failed operations
		jobs := make([]BatchJob, len(messages))
		for i, msg := range messages {
			jobs[i] = msg.Job
		}

		for _, job := range jobs {
			err = c.unfreezeUserBalance(tx, job)
			if err != nil {
				log.Error().Err(err).Msg("Failed to unfreeze user balance")
			}
		}

		if err = tx.Commit(); err != nil {
			log.Error().Err(err).Msg("Failed to commit batch failure handling")
		} else {
			// Send batch status notification after successful commit
			c.sendBatchStatusNotification(ctx, batchID, "failed", map[string]interface{}{
				"failure_reason":    failureErr.Error(),
				"transaction_count": len(messages),
			})

			log.Info().Str("batch_id", batchID.String()).Msg("Successfully handled batch failure")
		}
	}

	// NACK all messages for retry or dead letter queue
	c.nackAllMessages(messages)
}

// createPreparingBatchRecord creates a batch record in 'preparing' status
func (c *RabbitMQBatchConsumer) createPreparingBatchRecord(tx *sql.Tx, batchID uuid.UUID, messages []*MessageWrapper) error {
	if len(messages) == 0 {
		return fmt.Errorf("no messages provided")
	}

	// Get batch info from first message
	firstJob := messages[0].Job

	// Map JobType to database batch_type enum
	batchType := c.mapJobTypeToBatchType(firstJob)

	query := `
		INSERT INTO batches (
			batch_id, chain_id, token_id, batch_type, operation_count, 
			optimal_batch_size, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := tx.Exec(query,
		batchID.String(),
		firstJob.GetChainID(),
		firstJob.GetTokenID(),
		batchType,
		len(messages),
		len(messages), // For now, use actual batch size as optimal
		"preparing",
		time.Now())

	if err != nil {
		return fmt.Errorf("failed to insert preparing batch record: %w", err)
	}

	return nil
}

// freezeUserBalance freezes user balance for burn/transfer operations
func (c *RabbitMQBatchConsumer) freezeUserBalance(tx *sql.Tx, job BatchJob) error {
	switch j := job.(type) {
	case AssetAdjustJob:
		if j.AdjustmentType == "burn" {
			return c.freezeBalanceForBurn(tx, j)
		}
		// Mint operations don't need to freeze balance
		return nil

	case TransferJob:
		return c.freezeBalanceForTransfer(tx, j)

	default:
		return fmt.Errorf("unsupported job type for balance freezing: %T", job)
	}
}

// freezeBalanceForBurn freezes balance for burn operation
func (c *RabbitMQBatchConsumer) freezeBalanceForBurn(tx *sql.Tx, job AssetAdjustJob) error {
	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	// Burn amount might be negative, take absolute value
	if amount.IsNegative() {
		amount = amount.Abs()
	}

	now := time.Now()

	// Update user_balances: move from confirmed_balance to pending_balance
	// Only update existing records, don't insert new ones with negative confirmed_balance
	query := `
		INSERT INTO user_balances (user_id, chain_id, token_id, confirmed_balance, pending_balance, created_at, updated_at)
		VALUES ($1, $2, $3, 0, 0, $4, $4)
		ON CONFLICT (user_id, chain_id, token_id) 
		DO UPDATE SET 
			confirmed_balance = user_balances.confirmed_balance - $5,
			pending_balance = user_balances.pending_balance + $5,
			updated_at = $4,
			last_change_time = $4
		WHERE user_balances.confirmed_balance >= $5`

	result, err := tx.Exec(query, job.UserID, job.ChainID, job.TokenID, now, amount)
	if err != nil {
		return fmt.Errorf("failed to freeze balance for burn: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("insufficient balance to freeze for burn operation")
	}

	return nil
}

// freezeBalanceForTransfer freezes balance for transfer operation
func (c *RabbitMQBatchConsumer) freezeBalanceForTransfer(tx *sql.Tx, job TransferJob) error {
	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	// Transfer amount should be positive
	if amount.IsNegative() {
		return fmt.Errorf("transfer amount cannot be negative: %s", job.Amount)
	}

	now := time.Now()

	// Update user_balances: move from confirmed_balance to pending_balance for sender
	// Only update existing records, don't insert new ones with negative confirmed_balance
	query := `
		INSERT INTO user_balances (user_id, chain_id, token_id, confirmed_balance, pending_balance, created_at, updated_at)
		VALUES ($1, $2, $3, 0, 0, $4, $4)
		ON CONFLICT (user_id, chain_id, token_id) 
		DO UPDATE SET 
			confirmed_balance = user_balances.confirmed_balance - $5,
			pending_balance = user_balances.pending_balance + $5,
			updated_at = $4,
			last_change_time = $4
		WHERE user_balances.confirmed_balance >= $5`

	result, err := tx.Exec(query, job.FromUserID, job.ChainID, job.TokenID, now, amount)
	if err != nil {
		return fmt.Errorf("failed to freeze balance for transfer: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("insufficient balance to freeze for transfer operation")
	}

	return nil
}

// unfreezeUserBalance unfreezes user balance when batch fails
func (c *RabbitMQBatchConsumer) unfreezeUserBalance(tx *sql.Tx, job BatchJob) error {
	switch j := job.(type) {
	case AssetAdjustJob:
		if j.AdjustmentType == "burn" {
			return c.unfreezeBalanceForBurn(tx, j)
		}
		// Mint operations don't have frozen balance
		return nil

	case TransferJob:
		return c.unfreezeBalanceForTransfer(tx, j)

	default:
		return fmt.Errorf("unsupported job type for balance unfreezing: %T", job)
	}
}

// unfreezeBalanceForBurn unfreezes balance when burn operation fails
func (c *RabbitMQBatchConsumer) unfreezeBalanceForBurn(tx *sql.Tx, job AssetAdjustJob) error {
	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	// Burn amount might be negative, take absolute value
	if amount.IsNegative() {
		amount = amount.Abs()
	}

	// Reverse the freeze: move from pending_balance back to confirmed_balance
	query := `
		UPDATE user_balances 
		SET 
			confirmed_balance = confirmed_balance + $4,
			pending_balance = pending_balance - $4,
			updated_at = $5,
			last_change_time = $5
		WHERE user_id = $1 AND chain_id = $2 AND token_id = $3 
		AND pending_balance >= $4`

	result, err := tx.Exec(query, job.UserID, job.ChainID, job.TokenID, amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to unfreeze balance for burn: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no frozen balance found to unfreeze for burn operation")
	}

	return nil
}

// unfreezeBalanceForTransfer unfreezes balance when transfer operation fails
func (c *RabbitMQBatchConsumer) unfreezeBalanceForTransfer(tx *sql.Tx, job TransferJob) error {
	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	// Reverse the freeze: move from pending_balance back to confirmed_balance
	query := `
		UPDATE user_balances 
		SET 
			confirmed_balance = confirmed_balance + $4,
			pending_balance = pending_balance - $4,
			updated_at = $5,
			last_change_time = $5
		WHERE user_id = $1 AND chain_id = $2 AND token_id = $3 
		AND pending_balance >= $4`

	result, err := tx.Exec(query, job.FromUserID, job.ChainID, job.TokenID, amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to unfreeze balance for transfer: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no frozen balance found to unfreeze for transfer operation")
	}

	return nil
}

// getUserAAAddress retrieves the AA wallet address for a user on a specific chain
func (c *RabbitMQBatchConsumer) getUserAAAddress(ctx context.Context, userID string, chainID int64) (common.Address, error) {
	if c.db == nil {
		return common.Address{}, fmt.Errorf("database connection is nil")
	}

	query := `
		SELECT aa_address 
		FROM user_accounts 
		WHERE user_id = $1 AND chain_id = $2
		LIMIT 1`

	var aaAddress string
	err := c.db.QueryRowContext(ctx, query, userID, chainID).Scan(&aaAddress)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Address{}, fmt.Errorf("no AA wallet found for user %s on chain %d", userID, chainID)
		}
		return common.Address{}, fmt.Errorf("failed to query AA address: %w", err)
	}

	// Validate address format
	if !common.IsHexAddress(aaAddress) {
		return common.Address{}, fmt.Errorf("invalid AA address format: %s", aaAddress)
	}

	return common.HexToAddress(aaAddress), nil
}

// Notification helper methods

// sendBalanceChangeNotification sends notifications for balance changes (transfer, mint, burn)
func (c *RabbitMQBatchConsumer) sendBalanceChangeNotification(job BatchJob) {
	if c.batchProcessor == nil {
		return // No notification processor available, skip silently
	}

	ctx := context.Background()

	switch typedJob := job.(type) {
	case TransferJob:
		c.sendTransferNotifications(ctx, typedJob)
	case AssetAdjustJob:
		c.sendAssetAdjustNotification(ctx, typedJob)
	}
}

// sendTransferNotifications sends balance change notifications for both sender and receiver
func (c *RabbitMQBatchConsumer) sendTransferNotifications(ctx context.Context, job TransferJob) {
	// Sender notification (debit)
	senderNotification := NotificationJob{
		ID:        uuid.New().String(),
		JobType:   JobTypeNotification,
		UserID:    job.FromUserID,
		EventType: "balance_changed",
		Data: map[string]interface{}{
			"type":          "debit",
			"amount":        job.Amount,
			"chain_id":      job.ChainID,
			"token_id":      job.TokenID,
			"business_type": job.BusinessType,
			"reason_type":   job.ReasonType,
			"reason_detail": job.ReasonDetail,
			"to_user_id":    job.ToUserID,
		},
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
	}

	// Receiver notification (credit)
	receiverNotification := NotificationJob{
		ID:        uuid.New().String(),
		JobType:   JobTypeNotification,
		UserID:    job.ToUserID,
		EventType: "balance_changed",
		Data: map[string]interface{}{
			"type":          "credit",
			"amount":        job.Amount,
			"chain_id":      job.ChainID,
			"token_id":      job.TokenID,
			"business_type": job.BusinessType,
			"reason_type":   job.ReasonType,
			"reason_detail": job.ReasonDetail,
			"from_user_id":  job.FromUserID,
		},
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
	}

	// Send both notifications
	if err := c.batchProcessor.PublishNotification(ctx, senderNotification); err != nil {
		log.Warn().Err(err).
			Str("user_id", job.FromUserID).
			Str("job_id", job.ID).
			Msg("Failed to send sender balance change notification")
	}

	if err := c.batchProcessor.PublishNotification(ctx, receiverNotification); err != nil {
		log.Warn().Err(err).
			Str("user_id", job.ToUserID).
			Str("job_id", job.ID).
			Msg("Failed to send receiver balance change notification")
	}
}

// sendAssetAdjustNotification sends notification for asset adjustment (mint/burn)
func (c *RabbitMQBatchConsumer) sendAssetAdjustNotification(ctx context.Context, job AssetAdjustJob) {
	notification := NotificationJob{
		ID:        uuid.New().String(),
		JobType:   JobTypeNotification,
		UserID:    job.UserID,
		EventType: "balance_changed",
		Data: map[string]interface{}{
			"type":            job.AdjustmentType, // "mint" or "burn"
			"amount":          job.Amount,
			"chain_id":        job.ChainID,
			"token_id":        job.TokenID,
			"business_type":   job.BusinessType,
			"reason_type":     job.ReasonType,
			"reason_detail":   job.ReasonDetail,
			"adjustment_type": job.AdjustmentType,
		},
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
	}

	if err := c.batchProcessor.PublishNotification(ctx, notification); err != nil {
		log.Warn().Err(err).
			Str("user_id", job.UserID).
			Str("job_id", job.ID).
			Str("adjustment_type", job.AdjustmentType).
			Msg("Failed to send asset adjustment notification")
	}
}

// sendTransactionStatusNotification sends notification for transaction status changes
func (c *RabbitMQBatchConsumer) sendTransactionStatusNotification(ctx context.Context, txID uuid.UUID, status, userID string, extraData map[string]interface{}) {
	if c.batchProcessor == nil {
		return // No notification processor available, skip silently
	}

	notification := NotificationJob{
		ID:        uuid.New().String(),
		JobType:   JobTypeNotification,
		UserID:    userID,
		EventType: "transaction_status_changed",
		Data: map[string]interface{}{
			"transaction_id": txID.String(),
			"status":         status,
			"timestamp":      time.Now().Format(time.RFC3339),
		},
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
	}

	// Merge additional data if provided
	for k, v := range extraData {
		notification.Data[k] = v
	}

	if err := c.batchProcessor.PublishNotification(ctx, notification); err != nil {
		log.Warn().Err(err).
			Str("transaction_id", txID.String()).
			Str("user_id", userID).
			Str("status", status).
			Msg("Failed to send transaction status notification")
	}
}

// sendTransactionStatusNotifications sends notifications for multiple transaction status changes
func (c *RabbitMQBatchConsumer) sendTransactionStatusNotifications(transactionNotifications []map[string]interface{}) {
	if c.batchProcessor == nil {
		return // No notification processor available, skip silently
	}

	ctx := context.Background()

	for _, txData := range transactionNotifications {
		// Extract user_id - could be in different fields depending on transaction type
		var userID string
		if uid, ok := txData["user_id"].(string); ok && uid != "" {
			userID = uid
		} else if fromUID, ok := txData["from_user_id"].(string); ok && fromUID != "" {
			userID = fromUID
		} else if toUID, ok := txData["to_user_id"].(string); ok && toUID != "" {
			userID = toUID
		}

		// Skip if no user ID available
		if userID == "" {
			continue
		}

		notification := NotificationJob{
			ID:        uuid.New().String(),
			JobType:   JobTypeNotification,
			UserID:    userID,
			EventType: "transaction_status_changed",
			Data:      txData,
			Priority:  PriorityNormal,
			CreatedAt: time.Now(),
		}

		if err := c.batchProcessor.PublishNotification(ctx, notification); err != nil {
			log.Warn().Err(err).
				Str("user_id", userID).
				Interface("tx_data", txData).
				Msg("Failed to send transaction status notification")
		}

		// For transfers, also send notification to the recipient
		if toUID, ok := txData["to_user_id"].(string); ok && toUID != "" && toUID != userID {
			recipientNotification := NotificationJob{
				ID:        uuid.New().String(),
				JobType:   JobTypeNotification,
				UserID:    toUID,
				EventType: "transaction_status_changed",
				Data:      txData,
				Priority:  PriorityNormal,
				CreatedAt: time.Now(),
			}

			if err := c.batchProcessor.PublishNotification(ctx, recipientNotification); err != nil {
				log.Warn().Err(err).
					Str("recipient_user_id", toUID).
					Interface("tx_data", txData).
					Msg("Failed to send recipient transaction status notification")
			}
		}
	}
}

// sendBatchStatusNotification sends notification for batch status changes
func (c *RabbitMQBatchConsumer) sendBatchStatusNotification(ctx context.Context, batchID uuid.UUID, status string, extraData map[string]interface{}) {
	if c.batchProcessor == nil {
		return // No notification processor available, skip silently
	}

	notification := NotificationJob{
		ID:        uuid.New().String(),
		JobType:   JobTypeNotification,
		EventType: "batch_status_changed",
		Data: map[string]interface{}{
			"batch_id":  batchID.String(),
			"status":    status,
			"chain_id":  c.chainID,
			"timestamp": time.Now().Format(time.RFC3339),
		},
		Priority:  PriorityLow, // Batch status changes are lower priority
		CreatedAt: time.Now(),
	}

	// Merge additional data if provided
	for k, v := range extraData {
		notification.Data[k] = v
	}

	if err := c.batchProcessor.PublishNotification(ctx, notification); err != nil {
		log.Warn().Err(err).
			Str("batch_id", batchID.String()).
			Str("status", status).
			Msg("Failed to send batch status notification")
	}
}

// mapJobTypeToBatchType maps JobType to database batch_type enum values
func (c *RabbitMQBatchConsumer) mapJobTypeToBatchType(job BatchJob) string {
	switch job.GetJobType() {
	case JobTypeTransfer:
		return "transfer"
	case JobTypeAssetAdjust:
		// For asset adjust jobs, determine mint/burn based on adjustment type
		if adjustJob, ok := job.(AssetAdjustJob); ok {
			if adjustJob.AdjustmentType == "mint" {
				return "mint"
			} else if adjustJob.AdjustmentType == "burn" {
				return "burn"
			}
		}
		// Fallback to mint if adjustment type is unclear
		return "mint"
	default:
		// For unknown job types (like notifications), default to transfer
		// This shouldn't happen in practice since notifications don't create batches
		return "transfer"
	}
}

// extractBlockNumber safely converts *big.Int block number to int64
// Returns 0 if block number is nil or exceeds int64 range
func (c *RabbitMQBatchConsumer) extractBlockNumber(blockNumber *big.Int) int64 {
	if blockNumber == nil {
		return 0
	}

	// Check if the big.Int value fits in int64
	if !blockNumber.IsInt64() {
		log.Warn().
			Str("block_number", blockNumber.String()).
			Msg("Block number exceeds int64 range, using 0")
		return 0
	}

	return blockNumber.Int64()
}

// Helper methods for NFT parameter preparation

// prepareNFTMintParams prepares parameters for NFT batch mint
func (c *RabbitMQBatchConsumer) prepareNFTMintParams(ctx context.Context, jobs []NFTMintJob) ([]common.Address, []*big.Int, []string, error) {
	recipients := make([]common.Address, len(jobs))
	tokenIDs := make([]*big.Int, len(jobs))
	metadataURIs := make([]string, len(jobs))

	for i, job := range jobs {
		// Convert user ID to address using AA address lookup
		recipientAddr, err := c.getUserAAAddress(ctx, job.ToUserID, job.ChainID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get recipient address for user %s: %w", job.ToUserID, err)
		}
		recipients[i] = recipientAddr

		// Convert token ID string to big.Int
		tokenID, ok := new(big.Int).SetString(job.TokenID, 10)
		if !ok {
			return nil, nil, nil, fmt.Errorf("invalid token ID: %s", job.TokenID)
		}
		tokenIDs[i] = tokenID

		metadataURIs[i] = job.MetadataURI
	}

	return recipients, tokenIDs, metadataURIs, nil
}

// prepareNFTBurnParams prepares parameters for NFT batch burn
func (c *RabbitMQBatchConsumer) prepareNFTBurnParams(_ context.Context, jobs []NFTBurnJob) ([]*big.Int, error) {
	tokenIDs := make([]*big.Int, len(jobs))

	for i, job := range jobs {
		// Convert token ID string to big.Int
		tokenID, ok := new(big.Int).SetString(job.TokenID, 10)
		if !ok {
			return nil, fmt.Errorf("invalid token ID: %s", job.TokenID)
		}
		tokenIDs[i] = tokenID
	}

	return tokenIDs, nil
}

// prepareNFTTransferParams prepares parameters for NFT batch transfer
func (c *RabbitMQBatchConsumer) prepareNFTTransferParams(ctx context.Context, jobs []NFTTransferJob) ([]common.Address, []common.Address, []*big.Int, error) {
	fromAddresses := make([]common.Address, len(jobs))
	toAddresses := make([]common.Address, len(jobs))
	tokenIDs := make([]*big.Int, len(jobs))

	for i, job := range jobs {
		// Convert user IDs to addresses using AA address lookup
		fromAddr, err := c.getUserAAAddress(ctx, job.FromUserID, job.ChainID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get from address for user %s: %w", job.FromUserID, err)
		}
		fromAddresses[i] = fromAddr

		toAddr, err := c.getUserAAAddress(ctx, job.ToUserID, job.ChainID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get to address for user %s: %w", job.ToUserID, err)
		}
		toAddresses[i] = toAddr

		// Convert token ID string to big.Int
		tokenID, ok := new(big.Int).SetString(job.TokenID, 10)
		if !ok {
			return nil, nil, nil, fmt.Errorf("invalid token ID: %s", job.TokenID)
		}
		tokenIDs[i] = tokenID
	}

	return fromAddresses, toAddresses, tokenIDs, nil
}
