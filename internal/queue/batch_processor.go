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

	processingTime := time.Since(startTime)

	// Step 3: Atomic update of all three tables
	err = c.updateThreeTablesAfterSuccess(ctx, messages, group, batchID, result, processingTime)
	if err != nil {
		log.Error().Err(err).
			Str("tx_hash", result.TxHash).
			Msg("Failed to update database after successful blockchain operation")
		// NOTE: Blockchain operation succeeded but DB sync failed - need compensation logic
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

// updateTransactionsToBatching updates transaction records to 'batching' status
func (c *RabbitMQBatchConsumer) updateTransactionsToBatching(ctx context.Context, messages []*MessageWrapper, batchID uuid.UUID) error {
	if c.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, msgWrapper := range messages {
		job := msgWrapper.Job

		// Insert or update transaction record
		err = c.upsertTransactionRecord(tx, job, batchID)
		if err != nil {
			return fmt.Errorf("failed to upsert transaction record: %w", err)
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
		recipients, amounts := c.prepareMintParams(mintJobs)
		return caller.BatchMint(ctx, recipients, amounts)
	}

	// Process burn jobs
	if len(burnJobs) > 0 {
		accounts, amounts := c.prepareBurnParams(burnJobs)
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

	recipients, amounts := c.prepareTransferParams(transferJobs)
	return caller.BatchTransfer(ctx, recipients, amounts)
}

// prepareMintParams prepares parameters for batch mint
func (c *RabbitMQBatchConsumer) prepareMintParams(jobs []AssetAdjustJob) ([]common.Address, []*big.Int) {
	recipients := make([]common.Address, len(jobs))
	amounts := make([]*big.Int, len(jobs))

	for i, job := range jobs {
		recipients[i] = common.HexToAddress(job.UserID)
		amount, _ := new(big.Int).SetString(job.Amount, 10)
		amounts[i] = amount
	}

	return recipients, amounts
}

// prepareBurnParams prepares parameters for batch burn
func (c *RabbitMQBatchConsumer) prepareBurnParams(jobs []AssetAdjustJob) ([]common.Address, []*big.Int) {
	accounts := make([]common.Address, len(jobs))
	amounts := make([]*big.Int, len(jobs))

	for i, job := range jobs {
		accounts[i] = common.HexToAddress(job.UserID)
		amount, _ := new(big.Int).SetString(job.Amount, 10)
		amounts[i] = amount
	}

	return accounts, amounts
}

// prepareTransferParams prepares parameters for batch transfer
func (c *RabbitMQBatchConsumer) prepareTransferParams(jobs []TransferJob) ([]common.Address, []*big.Int) {
	recipients := make([]common.Address, len(jobs))
	amounts := make([]*big.Int, len(jobs))

	for i, job := range jobs {
		recipients[i] = common.HexToAddress(job.ToUserID)
		amount, _ := new(big.Int).SetString(job.Amount, 10)
		amounts[i] = amount
	}

	return recipients, amounts
}

// updateThreeTablesAfterSuccess performs atomic update of all three tables after successful blockchain operation
func (c *RabbitMQBatchConsumer) updateThreeTablesAfterSuccess(
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

	// Begin transaction for atomic updates of all three tables
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	jobs := make([]BatchJob, len(messages))
	for i, msg := range messages {
		jobs[i] = msg.Job
	}

	// 1. Create batch record
	err = c.createBatchRecord(tx, batchID, group, jobs, result, processingTime)
	if err != nil {
		return fmt.Errorf("failed to create batch record: %w", err)
	}

	// 2. Update transaction statuses
	err = c.updateTransactionStatuses(tx, jobs, batchID, result)
	if err != nil {
		return fmt.Errorf("failed to update transaction statuses: %w", err)
	}

	// 3. Update user balances
	err = c.updateUserBalances(tx, jobs, group)
	if err != nil {
		return fmt.Errorf("failed to update user balances: %w", err)
	}

	// Commit all changes atomically
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit three-table update: %w", err)
	}

	log.Debug().
		Str("batch_id", batchID.String()).
		Int("jobs_count", len(jobs)).
		Msg("Successfully updated all three tables atomically")

	return nil
}

// handleBatchFailure handles batch processing failures
func (c *RabbitMQBatchConsumer) handleBatchFailure(ctx context.Context, messages []*MessageWrapper, batchID uuid.UUID, failureErr error) {
	log.Error().Err(failureErr).Str("batch_id", batchID.String()).Msg("Handling batch failure")

	if c.db != nil {
		// Update transactions to failed status
		jobIDs := make([]uuid.UUID, len(messages))
		for i, msg := range messages {
			if txID, err := uuid.Parse(msg.Job.GetID()); err == nil {
				jobIDs[i] = txID
			}
		}

		query := `UPDATE transactions SET status = 'failed' WHERE tx_id = ANY($1)`
		_, err := c.db.Exec(query, pq.Array(jobIDs))
		if err != nil {
			log.Error().Err(err).Msg("Failed to update failed transaction statuses")
		}
	}

	// NACK all messages for retry or dead letter queue
	c.nackAllMessages(messages)
}
