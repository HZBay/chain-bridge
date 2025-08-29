package queue

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"

	"github.com/hzbay/chain-bridge/internal/blockchain"
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

	// Step 1: Insert/Update transactions to 'batching' status
	batchID := uuid.New()
	err := c.updateTransactionsToBatching(ctx, messages, batchID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update transactions to batching status")
		c.nackAllMessages(messages)
		return
	}

	// Step 2: Execute blockchain batch operation
	result, err := c.executeBlockchainBatch(ctx, messages, group)
	if err != nil {
		log.Error().Err(err).Msg("Blockchain batch operation failed")
		c.handleBatchFailure(ctx, messages, batchID, err)
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

	// Step 3: For now, we complete the batch immediately
	// In a production system, you would have a separate confirmation service
	err = c.completeSuccessfulBatch(ctx, messages, group, batchID, result, processingTime)
	if err != nil {
		log.Error().Err(err).
			Str("tx_hash", result.TxHash).
			Msg("Failed to complete successful batch")
		// NOTE: Blockchain operation succeeded but DB completion failed
		// The confirmation monitor should handle this case
		c.nackAllMessages(messages)
		return
	}

	// Step 4: ACK all messages after successful processing
	c.ackAllMessages(messages)

	// Step 5: Record performance metrics
	if c.batchOptimizer != nil {
		performance := BatchPerformance{
			BatchSize:        len(messages),
			ProcessingTime:   processingTime,
			GasSaved:         float64(result.GasSaved),
			EfficiencyRating: result.Efficiency,
			Timestamp:        time.Now(),
			ChainID:          group.ChainID,
			TokenID:          group.TokenID,
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

// updateTransactionsToBatching updates transaction records to 'batching' status and freezes balances
func (c *RabbitMQBatchConsumer) updateTransactionsToBatching(ctx context.Context, messages []*MessageWrapper, batchID uuid.UUID) error {
	if c.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

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
		userUUID, err := uuid.Parse(j.UserID)
		if err != nil {
			return fmt.Errorf("invalid user ID: %s", j.UserID)
		}

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
			j.TransactionID, uuid.New(), userUUID, j.ChainID, j.AdjustmentType, j.BusinessType,
			j.TokenID, amount, "batching", batchID, true,
			j.ReasonType, j.ReasonDetail, j.CreatedAt,
		}

	case TransferJob:
		fromUserUUID, err := uuid.Parse(j.FromUserID)
		if err != nil {
			return fmt.Errorf("invalid from user ID: %s", j.FromUserID)
		}
		toUserUUID, err := uuid.Parse(j.ToUserID)
		if err != nil {
			return fmt.Errorf("invalid to user ID: %s", j.ToUserID)
		}

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
			j.TransactionID, uuid.New(), fromUserUUID, j.ChainID, "transfer", j.BusinessType,
			toUserUUID, "outgoing", j.TokenID, amount, "batching",
			batchID, true, j.ReasonType, j.ReasonDetail, j.CreatedAt,
		}

	default:
		return fmt.Errorf("unsupported job type: %T", job)
	}

	_, err := tx.Exec(query, args...)
	return err
}

// executeBlockchainBatch executes the blockchain batch operation
func (c *RabbitMQBatchConsumer) executeBlockchainBatch(ctx context.Context, messages []*MessageWrapper, group BatchGroup) (*blockchain.BatchResult, error) {
	caller := c.cpopCallers[group.ChainID]
	if caller == nil {
		return nil, fmt.Errorf("no CPOP caller found for chain %d", group.ChainID)
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
	default:
		return nil, fmt.Errorf("unsupported job type: %s", group.JobType)
	}
}

// processAssetAdjustBatch processes mint/burn batch
func (c *RabbitMQBatchConsumer) processAssetAdjustBatch(ctx context.Context, caller *blockchain.CPOPBatchCaller, jobs []BatchJob) (*blockchain.BatchResult, error) {
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

// processTransferBatch processes transfer batch
func (c *RabbitMQBatchConsumer) processTransferBatch(ctx context.Context, caller *blockchain.CPOPBatchCaller, jobs []BatchJob) (*blockchain.BatchResult, error) {
	var transferJobs []TransferJob
	for _, job := range jobs {
		if transferJob, ok := job.(TransferJob); ok {
			transferJobs = append(transferJobs, transferJob)
		}
	}

	if len(transferJobs) == 0 {
		return nil, fmt.Errorf("no valid transfer jobs found")
	}

	recipients, amounts, err := c.prepareTransferParams(ctx, transferJobs)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare transfer parameters: %w", err)
	}
	return caller.BatchTransfer(ctx, recipients, amounts)
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

		amount, ok := new(big.Int).SetString(job.Amount, 10)
		if !ok {
			return nil, nil, fmt.Errorf("invalid amount format: %s", job.Amount)
		}
		amounts[i] = amount
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

		// 处理负数金额（burn时amount可能是负数）
		amountStr := job.Amount
		if len(amountStr) > 0 && amountStr[0] == '-' {
			amountStr = amountStr[1:] // 移除负号
		}
		amount, ok := new(big.Int).SetString(amountStr, 10)
		if !ok {
			return nil, nil, fmt.Errorf("invalid amount format: %s", job.Amount)
		}
		amounts[i] = amount
	}

	return accounts, amounts, nil
}

// prepareTransferParams prepares parameters for batch transfer
func (c *RabbitMQBatchConsumer) prepareTransferParams(ctx context.Context, jobs []TransferJob) ([]common.Address, []*big.Int, error) {
	recipients := make([]common.Address, len(jobs))
	amounts := make([]*big.Int, len(jobs))

	for i, job := range jobs {
		// 根据用户ID和链ID查询接收者的AA钱包地址
		aaAddress, err := c.getUserAAAddress(ctx, job.ToUserID, job.ChainID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get AA address for recipient %s on chain %d: %w", job.ToUserID, job.ChainID, err)
		}
		recipients[i] = aaAddress

		amount, ok := new(big.Int).SetString(job.Amount, 10)
		if !ok {
			return nil, nil, fmt.Errorf("invalid amount format: %s", job.Amount)
		}
		amounts[i] = amount
	}

	return recipients, amounts, nil
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
	defer tx.Rollback()

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
		"batch_operation", // Default operation type
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
	group BatchGroup,
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
	defer tx.Rollback()

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

	log.Info().
		Str("batch_id", batchID.String()).
		Str("tx_hash", result.TxHash).
		Msg("Batch completed successfully with all status updates")

	return nil
}

// updateBatchToConfirmed updates batch status to confirmed
func (c *RabbitMQBatchConsumer) updateBatchToConfirmed(tx *sql.Tx, batchID uuid.UUID, result *blockchain.BatchResult, processingTime time.Duration) error {
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
func (c *RabbitMQBatchConsumer) updateTransactionsToConfirmed(tx *sql.Tx, batchID uuid.UUID, result *blockchain.BatchResult) error {
	query := `
		UPDATE transactions 
		SET 
			status = 'confirmed',
			confirmed_at = $2
		WHERE batch_id = $1`

	_, err := tx.Exec(query, batchID.String(), time.Now())
	if err != nil {
		return fmt.Errorf("failed to update transactions to confirmed: %w", err)
	}

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
	userUUID, err := uuid.Parse(job.UserID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %s", job.UserID)
	}

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

	_, err = tx.Exec(query, userUUID, job.ChainID, job.TokenID, amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize balance for mint: %w", err)
	}

	return nil
}

// finalizeBalanceForBurn finalizes balance after successful burn
func (c *RabbitMQBatchConsumer) finalizeBalanceForBurn(tx *sql.Tx, job AssetAdjustJob) error {
	userUUID, err := uuid.Parse(job.UserID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %s", job.UserID)
	}

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

	_, err = tx.Exec(query, userUUID, job.ChainID, job.TokenID, amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize balance for burn: %w", err)
	}

	return nil
}

// finalizeBalanceForTransfer finalizes balance after successful transfer
func (c *RabbitMQBatchConsumer) finalizeBalanceForTransfer(tx *sql.Tx, job TransferJob) error {
	fromUserUUID, err := uuid.Parse(job.FromUserID)
	if err != nil {
		return fmt.Errorf("invalid from user ID: %s", job.FromUserID)
	}

	toUserUUID, err := uuid.Parse(job.ToUserID)
	if err != nil {
		return fmt.Errorf("invalid to user ID: %s", job.ToUserID)
	}

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

	_, err = tx.Exec(senderQuery, fromUserUUID, job.ChainID, job.TokenID, amount, time.Now())
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

	_, err = tx.Exec(receiverQuery, toUserUUID, job.ChainID, job.TokenID, amount, time.Now())
	if err != nil {
		return fmt.Errorf("failed to finalize receiver balance for transfer: %w", err)
	}

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
		defer tx.Rollback()

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
	
	query := `
		INSERT INTO batches (
			batch_id, chain_id, token_id, batch_type, operation_count, 
			optimal_batch_size, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := tx.Exec(query, 
		batchID.String(), 
		firstJob.GetChainID(),
		firstJob.GetTokenID(),
		string(firstJob.GetJobType()),
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
	userUUID, err := uuid.Parse(job.UserID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %s", job.UserID)
	}

	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	// Burn amount might be negative, take absolute value
	if amount.IsNegative() {
		amount = amount.Abs()
	}

	// Update user_balances: move from confirmed_balance to pending_balance
	query := `
		INSERT INTO user_balances (user_id, chain_id, token_id, confirmed_balance, pending_balance, created_at, updated_at)
		VALUES ($1, $2, $3, -$4, $4, $5, $5)
		ON CONFLICT (user_id, chain_id, token_id) 
		DO UPDATE SET 
			confirmed_balance = user_balances.confirmed_balance - $4,
			pending_balance = user_balances.pending_balance + $4,
			updated_at = $5,
			last_change_time = $5
		WHERE user_balances.confirmed_balance >= $4`

	result, err := tx.Exec(query, userUUID, job.ChainID, job.TokenID, amount, time.Now())
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
	fromUserUUID, err := uuid.Parse(job.FromUserID)
	if err != nil {
		return fmt.Errorf("invalid from user ID: %s", job.FromUserID)
	}

	amount, err := decimal.NewFromString(job.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount format: %s", job.Amount)
	}

	// Transfer amount should be positive
	if amount.IsNegative() {
		return fmt.Errorf("transfer amount cannot be negative: %s", job.Amount)
	}

	// Update user_balances: move from confirmed_balance to pending_balance for sender
	query := `
		INSERT INTO user_balances (user_id, chain_id, token_id, confirmed_balance, pending_balance, created_at, updated_at)
		VALUES ($1, $2, $3, -$4, $4, $5, $5)
		ON CONFLICT (user_id, chain_id, token_id) 
		DO UPDATE SET 
			confirmed_balance = user_balances.confirmed_balance - $4,
			pending_balance = user_balances.pending_balance + $4,
			updated_at = $5,
			last_change_time = $5
		WHERE user_balances.confirmed_balance >= $4`

	result, err := tx.Exec(query, fromUserUUID, job.ChainID, job.TokenID, amount, time.Now())
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
	userUUID, err := uuid.Parse(job.UserID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %s", job.UserID)
	}

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

	result, err := tx.Exec(query, userUUID, job.ChainID, job.TokenID, amount, time.Now())
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
	fromUserUUID, err := uuid.Parse(job.FromUserID)
	if err != nil {
		return fmt.Errorf("invalid from user ID: %s", job.FromUserID)
	}

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

	result, err := tx.Exec(query, fromUserUUID, job.ChainID, job.TokenID, amount, time.Now())
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

	// Parse userID to UUID
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return common.Address{}, fmt.Errorf("invalid user ID format: %s", userID)
	}

	query := `
		SELECT aa_address 
		FROM user_accounts 
		WHERE user_id = $1 AND chain_id = $2 AND is_deployed = true
		LIMIT 1`

	var aaAddress string
	err = c.db.QueryRowContext(ctx, query, userUUID, chainID).Scan(&aaAddress)
	if err != nil {
		if err == sql.ErrNoRows {
			return common.Address{}, fmt.Errorf("no deployed AA wallet found for user %s on chain %d", userID, chainID)
		}
		return common.Address{}, fmt.Errorf("failed to query AA address: %w", err)
	}

	// Validate address format
	if !common.IsHexAddress(aaAddress) {
		return common.Address{}, fmt.Errorf("invalid AA address format: %s", aaAddress)
	}

	return common.HexToAddress(aaAddress), nil
}
