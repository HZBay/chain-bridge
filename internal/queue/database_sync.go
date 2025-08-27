package queue

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"

	"github.com/hzbay/chain-bridge/internal/blockchain"
)

// updateDatabaseAfterBatchSuccess performs atomic update of all three tables after successful batch processing
func (r *RabbitMQProcessor) updateDatabaseAfterBatchSuccess(
	ctx context.Context,
	jobs []BatchJob,
	group BatchGroup,
	result *blockchain.BatchResult,
	processingTime time.Duration,
) error {
	if r.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Begin transaction for atomic updates
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Will be ignored if Commit() succeeds

	// Generate batch ID
	batchID := uuid.New()

	// 1. Create batch record
	err = r.createBatchRecord(tx, batchID, group, jobs, result, processingTime)
	if err != nil {
		return fmt.Errorf("failed to create batch record: %w", err)
	}

	// 2. Update transaction statuses
	err = r.updateTransactionStatuses(tx, jobs, batchID, result)
	if err != nil {
		return fmt.Errorf("failed to update transaction statuses: %w", err)
	}

	// 3. Update user balances
	err = r.updateUserBalances(tx, jobs, group)
	if err != nil {
		return fmt.Errorf("failed to update user balances: %w", err)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Info().
		Str("batch_id", batchID.String()).
		Str("tx_hash", result.TxHash).
		Int("job_count", len(jobs)).
		Msg("Successfully synchronized all database tables")

	return nil
}

// createBatchRecord creates a record in the batches table
func (r *RabbitMQProcessor) createBatchRecord(
	tx *sql.Tx,
	batchID uuid.UUID,
	group BatchGroup,
	jobs []BatchJob,
	result *blockchain.BatchResult,
	processingTime time.Duration,
) error {
	// Determine CPOP operation type
	var cpopOpType string
	switch group.JobType {
	case JobTypeAssetAdjust:
		if r.hasMintJobs(jobs) {
			cpopOpType = "batch_mint"
		} else {
			cpopOpType = "batch_burn"
		}
	case JobTypeTransfer:
		cpopOpType = "batch_transfer"
	default:
		return fmt.Errorf("unsupported job type for CPOP operation: %s", group.JobType)
	}

	// Get optimal batch size from optimizer
	optimalSize := 25 // Default
	if r.batchOptimizer != nil {
		optimalSize = r.batchOptimizer.GetOptimalBatchSize(group.ChainID, group.TokenID)
	}

	// Calculate gas savings percentage
	gasSavedPercentage := result.Efficiency

	query := `
		INSERT INTO batches (
			batch_id, chain_id, token_id, batch_type,
			operation_count, optimal_batch_size, actual_efficiency,
			batch_strategy, network_condition,
			actual_gas_used, gas_saved, gas_saved_percentage,
			cpop_operation_type, status, tx_hash, 
			created_at, confirmed_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW(), NOW()
		)`

	_, err := tx.Exec(query,
		batchID,               // batch_id
		group.ChainID,         // chain_id
		group.TokenID,         // token_id
		string(group.JobType), // batch_type
		len(jobs),             // operation_count
		optimalSize,           // optimal_batch_size
		result.Efficiency,     // actual_efficiency
		"auto_optimized",      // batch_strategy
		"medium",              // network_condition (could be dynamic)
		result.GasUsed,        // actual_gas_used
		result.GasSaved,       // gas_saved
		gasSavedPercentage,    // gas_saved_percentage
		cpopOpType,            // cpop_operation_type
		"confirmed",           // status
		result.TxHash,         // tx_hash
	)

	if err != nil {
		return fmt.Errorf("failed to insert batch record: %w", err)
	}

	log.Debug().
		Str("batch_id", batchID.String()).
		Int64("chain_id", group.ChainID).
		Int("token_id", group.TokenID).
		Str("cpop_operation", cpopOpType).
		Float64("efficiency", result.Efficiency).
		Msg("Created batch record")

	return nil
}

// updateTransactionStatuses updates all transaction records in the batch
func (r *RabbitMQProcessor) updateTransactionStatuses(
	tx *sql.Tx,
	jobs []BatchJob,
	batchID uuid.UUID,
	result *blockchain.BatchResult,
) error {
	// Collect all job IDs (transaction IDs)
	jobIDs := make([]uuid.UUID, len(jobs))
	for i, job := range jobs {
		if txID, err := uuid.Parse(job.GetID()); err == nil {
			jobIDs[i] = txID
		} else {
			return fmt.Errorf("invalid job ID format: %s", job.GetID())
		}
	}

	// Bulk update all transaction records
	query := `
		UPDATE transactions 
		SET 
			status = 'confirmed',
			batch_id = $1,
			is_batch_operation = TRUE,
			tx_hash = $2,
			gas_saved_percentage = $3,
			confirmed_at = NOW()
		WHERE tx_id = ANY($4)`

	result_rows, err := tx.Exec(query, batchID, result.TxHash, result.Efficiency, pq.Array(jobIDs))
	if err != nil {
		return fmt.Errorf("failed to update transaction statuses: %w", err)
	}

	rowsAffected, _ := result_rows.RowsAffected()
	if rowsAffected != int64(len(jobs)) {
		log.Warn().
			Int64("expected", int64(len(jobs))).
			Int64("affected", rowsAffected).
			Msg("Mismatch in updated transaction count")
	}

	log.Debug().
		Str("batch_id", batchID.String()).
		Int("updated_transactions", len(jobIDs)).
		Str("tx_hash", result.TxHash).
		Msg("Updated transaction statuses")

	return nil
}

// updateUserBalances updates user balance records
func (r *RabbitMQProcessor) updateUserBalances(
	tx *sql.Tx,
	jobs []BatchJob,
	group BatchGroup,
) error {
	// Calculate balance changes for each user
	balanceChanges := r.calculateBalanceChanges(jobs, group.JobType)

	if len(balanceChanges) == 0 {
		log.Debug().Msg("No balance changes to apply")
		return nil
	}

	// Apply balance changes for each user
	for userID, change := range balanceChanges {
		err := r.updateSingleUserBalance(tx, userID, group.ChainID, group.TokenID, change)
		if err != nil {
			return fmt.Errorf("failed to update balance for user %s: %w", userID, err)
		}
	}

	log.Debug().
		Int("users_affected", len(balanceChanges)).
		Int64("chain_id", group.ChainID).
		Int("token_id", group.TokenID).
		Msg("Updated user balances")

	return nil
}

// calculateBalanceChanges calculates the net balance change for each user
func (r *RabbitMQProcessor) calculateBalanceChanges(jobs []BatchJob, jobType JobType) map[string]*big.Int {
	changes := make(map[string]*big.Int)

	for _, job := range jobs {
		switch jobType {
		case JobTypeAssetAdjust:
			if adjustJob, ok := job.(AssetAdjustJob); ok {
				amount, ok := new(big.Int).SetString(adjustJob.Amount, 10)
				if !ok {
					log.Error().Str("amount", adjustJob.Amount).Msg("Failed to parse amount")
					continue
				}

				if adjustJob.AdjustmentType == "mint" {
					// Mint increases balance
					r.addToBalance(changes, adjustJob.UserID, amount)
				} else if adjustJob.AdjustmentType == "burn" {
					// Burn decreases balance
					negAmount := new(big.Int).Neg(amount)
					r.addToBalance(changes, adjustJob.UserID, negAmount)
				}
			}

		case JobTypeTransfer:
			if transferJob, ok := job.(TransferJob); ok {
				amount, ok := new(big.Int).SetString(transferJob.Amount, 10)
				if !ok {
					log.Error().Str("amount", transferJob.Amount).Msg("Failed to parse transfer amount")
					continue
				}

				// Sender's balance decreases
				negAmount := new(big.Int).Neg(amount)
				r.addToBalance(changes, transferJob.FromUserID, negAmount)

				// Receiver's balance increases
				r.addToBalance(changes, transferJob.ToUserID, amount)
			}
		}
	}

	return changes
}

// addToBalance adds an amount to a user's balance change
func (r *RabbitMQProcessor) addToBalance(changes map[string]*big.Int, userID string, amount *big.Int) {
	if existing, exists := changes[userID]; exists {
		changes[userID] = new(big.Int).Add(existing, amount)
	} else {
		changes[userID] = new(big.Int).Set(amount)
	}
}

// updateSingleUserBalance updates a single user's balance using upsert
func (r *RabbitMQProcessor) updateSingleUserBalance(
	tx *sql.Tx,
	userID string,
	chainID int64,
	tokenID int,
	change *big.Int,
) error {
	if change.Sign() == 0 {
		// No change needed
		return nil
	}

	// Convert to decimal with 18 decimal places (standard for ERC20)
	changeDecimal := decimal.NewFromBigInt(change, -18)

	// Parse user ID as UUID
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %s", userID)
	}

	// Use PostgreSQL UPSERT (INSERT ... ON CONFLICT DO UPDATE)
	query := `
		INSERT INTO user_balances (user_id, chain_id, token_id, confirmed_balance, last_change_time, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (user_id, chain_id, token_id) 
		DO UPDATE SET 
			confirmed_balance = user_balances.confirmed_balance + EXCLUDED.confirmed_balance,
			last_change_time = NOW(),
			updated_at = NOW()
		WHERE user_balances.confirmed_balance + EXCLUDED.confirmed_balance >= 0`

	result, err := tx.Exec(query, userUUID, chainID, tokenID, changeDecimal)
	if err != nil {
		return fmt.Errorf("failed to update user balance: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("balance update would result in negative balance for user %s", userID)
	}

	log.Debug().
		Str("user_id", userID).
		Int64("chain_id", chainID).
		Int("token_id", tokenID).
		Str("change", changeDecimal.String()).
		Msg("Updated user balance")

	return nil
}

// hasMintJobs checks if there are any mint jobs in the batch
func (r *RabbitMQProcessor) hasMintJobs(jobs []BatchJob) bool {
	for _, job := range jobs {
		if adjustJob, ok := job.(AssetAdjustJob); ok {
			if adjustJob.AdjustmentType == "mint" {
				return true
			}
		}
	}
	return false
}
