package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/hzbay/chain-bridge/internal/queue"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/rs/zerolog/log"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	sqltypes "github.com/volatiletech/sqlboiler/v4/types"
)

// Service defines the assets service interface
type Service interface {
	AdjustAssets(ctx context.Context, req *types.AssetAdjustRequest) (*types.AssetAdjustResponse, *types.BatchInfo, error)
	GetUserAssets(ctx context.Context, userID string) (*types.AssetsResponse, *types.BatchInfo, error)
}

// service implements the assets service
type service struct {
	db             *sql.DB
	batchProcessor queue.BatchProcessor
	batchOptimizer *queue.BatchOptimizer

	// Token caching
	tokenCache map[string]int // "chainID:symbol" -> tokenID
	cacheMutex sync.RWMutex
}

// NewService creates a new assets service
func NewService(db *sql.DB, batchProcessor queue.BatchProcessor, batchOptimizer *queue.BatchOptimizer) Service {
	return &service{
		db:             db,
		batchProcessor: batchProcessor,
		batchOptimizer: batchOptimizer,
		tokenCache:     make(map[string]int),
	}
}

// AdjustAssets handles batch asset adjustments (mint/burn operations)
func (s *service) AdjustAssets(ctx context.Context, req *types.AssetAdjustRequest) (*types.AssetAdjustResponse, *types.BatchInfo, error) {
	log.Info().
		Str("operation_id", *req.OperationID).
		Int("adjustment_count", len(req.Adjustments)).
		Msg("Processing asset adjustment request")

	// Request validation is handled at handler layer

	// Validate amount format for all adjustments before processing
	for i, adjustment := range req.Adjustments {
		if _, err := strconv.ParseFloat(*adjustment.Amount, 64); err != nil {
			return nil, nil, fmt.Errorf("invalid amount format in adjustment %d: %s", i+1, *adjustment.Amount)
		}
	}

	// 1. 幂等性检查 - 检查 OperationID 是否已经存在
	// Validate operation_id format - it should be a valid UUID
	mainOperationID, err := uuid.Parse(*req.OperationID)
	if err != nil {
		// Return a validation error for invalid UUID format
		return nil, nil, fmt.Errorf("validation_error:operation_id:Must be a valid UUID format (e.g., 550e8400-e29b-41d4-a716-446655440000)")
	}

	existingTx, err := models.Transactions(
		models.TransactionWhere.OperationID.EQ(null.StringFrom(mainOperationID.String())),
	).One(ctx, s.db)

	if err == nil && existingTx != nil {
		// OperationID 已存在，返回已有结果
		log.Info().
			Str("operation_id", *req.OperationID).
			Str("existing_tx_id", existingTx.TXID).
			Msg("Operation already processed, returning existing result")

		// 统计已处理的记录数
		processedCount, err := models.Transactions(
			models.TransactionWhere.OperationID.EQ(null.StringFrom(mainOperationID.String())),
		).Count(ctx, s.db)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to count existing transactions")
			processedCount = int64(len(req.Adjustments)) // fallback
		}

		// 返回已有结果
		status := existingTx.Status.String
		if status == "" {
			status = "recorded"
		}

		response := &types.AssetAdjustResponse{
			OperationID:    req.OperationID,
			ProcessedCount: &processedCount,
			Status:         &status,
		}

		// Build batch info for idempotent response
		var chainID int64
		var tokenID int
		if len(req.Adjustments) > 0 {
			chainID = *req.Adjustments[0].ChainID
			tokenID = s.getTokenIDBySymbol(*req.Adjustments[0].ChainID, *req.Adjustments[0].TokenSymbol)
		}

		batchInfo := &types.BatchInfo{
			WillBeBatched:      true,
			BatchType:          "batchAdjustAssets",
			CurrentBatchSize:   int64(s.getCurrentBatchSize(chainID, tokenID)),
			OptimalBatchSize:   int64(s.getOptimalBatchSize(chainID, tokenID)),
			ExpectedEfficiency: "74-76%",
		}

		return response, batchInfo, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		// 数据库查询错误（非记录不存在）
		return nil, nil, fmt.Errorf("failed to check operation idempotency: %w", err)
	}

	// OperationID 不存在，继续正常处理
	log.Debug().Str("operation_id", *req.OperationID).Msg("New operation, proceeding with processing")

	// 2. Process each adjustment
	var adjustJobs []queue.AssetAdjustJob

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Warn().Err(err).Msg("Failed to rollback transaction")
		}
	}()

	for _, adjustment := range req.Adjustments {
		// Validate chain exists first
		if err := s.validateChainExists(*adjustment.ChainID); err != nil {
			return nil, nil, err
		}

		// Validate token exists and get ID
		tokenID, err := s.getTokenIDBySymbolWithValidation(*adjustment.ChainID, *adjustment.TokenSymbol)
		if err != nil {
			return nil, nil, err
		}

		// Validate user account exists on the specified chain
		if err := s.validateUserAccountExists(ctx, *adjustment.UserID, *adjustment.ChainID); err != nil {
			return nil, nil, err
		}

		// Generate unique transaction ID for this adjustment
		txID := uuid.New()

		// Determine adjustment type based on amount sign
		adjustmentType := "mint"
		amount := *adjustment.Amount
		if len(amount) > 0 && amount[0] == '-' {
			adjustmentType = "burn"
		}

		// Create transaction record
		transaction := &models.Transaction{
			TXID:         txID.String(),
			OperationID:  null.StringFrom(mainOperationID.String()),
			UserID:       *adjustment.UserID,
			ChainID:      *adjustment.ChainID,
			TXType:       adjustmentType, // Use "mint" or "burn" based on amount sign
			BusinessType: *adjustment.BusinessType,
			TokenID:      tokenID, // Use validated token ID
			// Amount field removed - using raw SQL insert to bypass decimal encoding issue
			Status:           null.StringFrom("pending"),
			IsBatchOperation: null.BoolFrom(true),
			ReasonType:       *adjustment.ReasonType,
			ReasonDetail:     null.StringFromPtr(&adjustment.ReasonDetail),
		}

		// Insert transaction record using raw SQL to bypass decimal encoding issue
		insertQuery := `
			INSERT INTO transactions (
				tx_id, operation_id, user_id, chain_id, tx_type, business_type, 
				token_id, amount, status, is_batch_operation, reason_type, reason_detail, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8::numeric, $9, $10, $11, $12, NOW()
			)`
		_, err = tx.ExecContext(ctx, insertQuery,
			transaction.TXID,
			transaction.OperationID,
			transaction.UserID,
			transaction.ChainID,
			transaction.TXType,
			transaction.BusinessType,
			transaction.TokenID,
			amount, // Use string amount directly
			transaction.Status,
			transaction.IsBatchOperation,
			transaction.ReasonType,
			transaction.ReasonDetail,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to insert transaction for user %s: %w", *adjustment.UserID, err)
		}

		// Create asset adjust job
		adjustJob := queue.AssetAdjustJob{
			ID:             txID.String(),
			JobType:        queue.JobTypeAssetAdjust,
			TransactionID:  txID,
			ChainID:        *adjustment.ChainID,
			TokenID:        tokenID, // Use validated token ID
			UserID:         *adjustment.UserID,
			Amount:         amount,
			AdjustmentType: adjustmentType,
			BusinessType:   *adjustment.BusinessType,
			ReasonType:     *adjustment.ReasonType,
			ReasonDetail:   adjustment.ReasonDetail,
			Priority:       s.determinePriority(*adjustment.BusinessType),
			CreatedAt:      time.Now(),
		}
		adjustJobs = append(adjustJobs, adjustJob)
	}

	// Commit transaction records
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit transactions: %w", err)
	}

	// 3. Publish all jobs to batch processor
	for _, job := range adjustJobs {
		if err := s.batchProcessor.PublishAssetAdjust(ctx, job); err != nil {
			log.Error().Err(err).
				Str("job_id", job.ID).
				Str("user_id", job.UserID).
				Msg("Failed to publish asset adjust job, but transaction is recorded")

			// Note: We don't return error here because transactions are already recorded
			// The batch processor failure doesn't mean the adjustment failed

			// Mark corresponding transaction as failed since it cannot be processed
			if updateErr := s.markTransactionAsFailed(context.Background(), job.ID, "queue_publish_failed"); updateErr != nil {
				log.Error().Err(updateErr).
					Str("job_id", job.ID).
					Msg("Failed to mark transaction as failed")
			}
		}
	}

	// 4. Build response
	processedCount := int64(len(req.Adjustments))
	status := "recorded"

	response := &types.AssetAdjustResponse{
		OperationID:    req.OperationID,
		ProcessedCount: &processedCount,
		Status:         &status,
	}

	// 5. Build batch info
	var chainID int64
	var tokenID int
	if len(req.Adjustments) > 0 {
		chainID = *req.Adjustments[0].ChainID
		// Since we've already validated the token, we can safely use the cached value
		if validatedTokenID, err := s.getTokenIDBySymbolWithValidation(*req.Adjustments[0].ChainID, *req.Adjustments[0].TokenSymbol); err == nil {
			tokenID = validatedTokenID
		} else {
			// This should not happen since we already validated, but provide fallback
			tokenID = 1
		}
	}

	batchInfo := &types.BatchInfo{
		WillBeBatched:      true,
		BatchType:          "batchAdjustAssets",
		CurrentBatchSize:   int64(s.getCurrentBatchSize(chainID, tokenID)),
		OptimalBatchSize:   int64(s.getOptimalBatchSize(chainID, tokenID)),
		ExpectedEfficiency: "74-76%",
	}

	log.Info().
		Str("operation_id", *req.OperationID).
		Int64("processed_count", processedCount).
		Str("status", status).
		Msg("Asset adjustment request processed successfully")

	return response, batchInfo, nil
}

// Helper methods

func (s *service) getTokenIDBySymbol(chainID int64, symbol string) int {
	cacheKey := fmt.Sprintf("%d:%s", chainID, symbol)

	// Check cache first
	s.cacheMutex.RLock()
	if tokenID, exists := s.tokenCache[cacheKey]; exists {
		s.cacheMutex.RUnlock()
		return tokenID
	}
	s.cacheMutex.RUnlock()

	// Query database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := models.SupportedTokens(
		models.SupportedTokenWhere.ChainID.EQ(chainID),
		models.SupportedTokenWhere.Symbol.EQ(symbol),
		models.SupportedTokenWhere.IsEnabled.EQ(null.BoolFrom(true)),
	).One(ctx, s.db)

	if err != nil {
		log.Warn().
			Int64("chain_id", chainID).
			Str("symbol", symbol).
			Err(err).
			Msg("Failed to find token, using default")
		return 1 // Fallback to default
	}

	tokenID := token.ID

	// Update cache
	s.cacheMutex.Lock()
	s.tokenCache[cacheKey] = tokenID
	s.cacheMutex.Unlock()

	log.Debug().
		Int64("chain_id", chainID).
		Str("symbol", symbol).
		Int("token_id", tokenID).
		Msg("Token found and cached")

	return tokenID
}

// getTokenIDBySymbolWithValidation validates token exists and returns ID or validation error
func (s *service) getTokenIDBySymbolWithValidation(chainID int64, symbol string) (int, error) {
	cacheKey := fmt.Sprintf("%d:%s", chainID, symbol)

	// Check cache first
	s.cacheMutex.RLock()
	if tokenID, exists := s.tokenCache[cacheKey]; exists {
		s.cacheMutex.RUnlock()
		return tokenID, nil
	}
	s.cacheMutex.RUnlock()

	// Query database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := models.SupportedTokens(
		models.SupportedTokenWhere.ChainID.EQ(chainID),
		models.SupportedTokenWhere.Symbol.EQ(symbol),
		models.SupportedTokenWhere.IsEnabled.EQ(null.BoolFrom(true)),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Return structured validation error
			return 0, fmt.Errorf("validation_error:token_not_found:%d:%s", chainID, symbol)
		}
		return 0, fmt.Errorf("database error while looking up token: %w", err)
	}

	tokenID := token.ID

	// Update cache
	s.cacheMutex.Lock()
	s.tokenCache[cacheKey] = tokenID
	s.cacheMutex.Unlock()

	log.Debug().
		Int64("chain_id", chainID).
		Str("symbol", symbol).
		Int("token_id", tokenID).
		Msg("Token found and cached")

	return tokenID, nil
}

// validateChainExists checks if a chain exists and is enabled
func (s *service) validateChainExists(chainID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := models.Chains(
		models.ChainWhere.ChainID.EQ(chainID),
		models.ChainWhere.IsEnabled.EQ(null.BoolFrom(true)),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("validation_error:chain_not_found:%d", chainID)
		}
		return fmt.Errorf("database error while validating chain: %w", err)
	}

	return nil
}

// validateUserAccountExists checks if a user has an account on the specified chain
func (s *service) validateUserAccountExists(ctx context.Context, userID string, chainID int64) error {
	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := models.UserAccounts(
		models.UserAccountWhere.UserID.EQ(userID),
		models.UserAccountWhere.ChainID.EQ(chainID),
	).One(ctxTimeout, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("validation_error:user_account_not_found:%s:%d", userID, chainID)
		}
		return fmt.Errorf("database error while validating user account: %w", err)
	}

	return nil
}

func (s *service) getCurrentBatchSize(chainID int64, tokenID int) int32 {
	if s.batchOptimizer != nil {
		optimalSize := s.batchOptimizer.GetOptimalBatchSize(chainID, tokenID)
		if optimalSize <= 0 {
			return 25 // Default fallback
		}
		if optimalSize <= math.MaxInt32 {
			return int32(optimalSize)
		}
		return math.MaxInt32
	}
	// Fallback to optimal batch size from chain config
	optimalSize := s.getOptimalBatchSize(chainID, tokenID)
	if optimalSize <= 0 {
		return 25 // Default fallback
	}
	if optimalSize <= math.MaxInt32 {
		return int32(optimalSize)
	}
	return math.MaxInt32
}

func (s *service) getOptimalBatchSize(chainID int64, tokenID int) int {
	// Try to get from batch optimizer first
	if s.batchOptimizer != nil {
		optimized := s.batchOptimizer.GetOptimalBatchSize(chainID, tokenID)
		if optimized > 0 {
			return optimized
		}
	}

	// Fallback to chain configuration
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	chain, err := models.Chains(
		models.ChainWhere.ChainID.EQ(chainID),
		models.ChainWhere.IsEnabled.EQ(null.BoolFrom(true)),
	).One(ctx, s.db)

	if err != nil {
		log.Warn().
			Int64("chain_id", chainID).
			Err(err).
			Msg("Failed to get chain config, using default batch size")
		return 25 // Default fallback
	}

	// Return optimal batch size from chain config, or default if not set
	if chain.OptimalBatchSize.Valid && chain.OptimalBatchSize.Int > 0 {
		return chain.OptimalBatchSize.Int
	}

	return 25 // Default fallback
}

func (s *service) determinePriority(businessType string) queue.Priority {
	switch businessType {
	case "gas_fee":
		return queue.PriorityHigh // Gas fees need quick processing
	case "reward":
		return queue.PriorityNormal // Rewards are standard priority
	case "consumption":
		return queue.PriorityNormal // Consumption is standard priority
	case "refund":
		return queue.PriorityHigh // Refunds need quick processing
	default:
		return queue.PriorityNormal
	}
}

// GetUserAssets retrieves user assets across all supported chains
func (s *service) GetUserAssets(ctx context.Context, userID string) (*types.AssetsResponse, *types.BatchInfo, error) {
	log.Info().Str("user_id", userID).Msg("Getting user assets")

	// Query user balances with chain and token information
	// Note: Removed ConfirmedBalance.GT filter to avoid types.NullDecimal encoding issues
	userBalances, err := models.UserBalances(
		models.UserBalanceWhere.UserID.EQ(userID),
		qm.Load(models.UserBalanceRels.Chain),
		qm.Load(models.UserBalanceRels.Token),
		qm.OrderBy(models.UserBalanceColumns.ChainID+" ASC, "+models.UserBalanceColumns.TokenID+" ASC"),
	).All(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// User has no assets, return empty response
			log.Debug().Str("user_id", userID).Msg("No assets found for user")

			response := &types.AssetsResponse{
				UserID:        strPtr(userID),
				TotalValueUsd: float32Ptr(0.0),
				Assets:        []*types.AssetInfo{},
			}

			batchInfo := s.getBatchInfoForUser(ctx, userID)
			return response, batchInfo, nil
		}
		return nil, nil, fmt.Errorf("failed to query user balances: %w", err)
	}

	// Convert database records to API response format
	var assets []*types.AssetInfo
	var totalValueUsd float64

	for _, balance := range userBalances {
		// Ensure relationships are loaded
		if balance.R == nil || balance.R.Chain == nil || balance.R.Token == nil {
			log.Warn().
				Str("user_id", userID).
				Int64("chain_id", balance.ChainID).
				Int("token_id", balance.TokenID).
				Msg("Missing relationship data for user balance, skipping")
			continue
		}

		chain := balance.R.Chain
		token := balance.R.Token

		// Skip assets with zero confirmed balance to match previous behavior
		if balance.ConfirmedBalance.Big == nil || balance.ConfirmedBalance.Big.Sign() <= 0 {
			log.Debug().
				Str("user_id", userID).
				Int64("chain_id", balance.ChainID).
				Int("token_id", balance.TokenID).
				Msg("Skipping asset with zero confirmed balance")
			continue
		}

		// Convert balance values
		confirmedBalance := s.formatDecimal(balance.ConfirmedBalance)
		pendingBalance := s.formatDecimal(balance.PendingBalance)
		lockedBalance := s.formatDecimal(balance.LockedBalance)

		// Calculate USD value (placeholder - in production would use price feeds)
		balanceUsd := s.calculateBalanceUSD(balance.ConfirmedBalance, token.Symbol)
		totalValueUsd += balanceUsd

		// Determine sync status based on last sync time
		syncStatus := s.determineSyncStatus(balance.LastSyncTime)

		// Build asset info
		assetInfo := &types.AssetInfo{
			ChainID:          &balance.ChainID,
			ChainName:        chain.ShortName,
			Symbol:           &token.Symbol,
			Name:             token.Name,
			ContractAddress:  token.ContractAddress.String,
			Decimals:         int64(token.Decimals),
			ConfirmedBalance: &confirmedBalance,
			PendingBalance:   &pendingBalance,
			LockedBalance:    &lockedBalance,
			BalanceUsd:       float32(balanceUsd),
			SyncStatus:       &syncStatus,
		}

		assets = append(assets, assetInfo)
	}

	// Build response
	totalValueUsdFloat32 := float32(totalValueUsd)
	response := &types.AssetsResponse{
		UserID:        strPtr(userID),
		TotalValueUsd: &totalValueUsdFloat32,
		Assets:        assets,
	}

	// Get batch info for this user
	batchInfo := s.getBatchInfoForUser(ctx, userID)

	log.Info().
		Str("user_id", userID).
		Float64("total_value_usd", totalValueUsd).
		Int("asset_count", len(assets)).
		Msg("User assets retrieved successfully")

	return response, batchInfo, nil
}

// Helper methods for GetUserAssets

// formatDecimal converts a NullDecimal to a formatted string
func (s *service) formatDecimal(d sqltypes.NullDecimal) string {
	if d.Big == nil {
		return "0.0"
	}
	return d.Big.String()
}

// calculateBalanceUSD calculates USD value for a balance (placeholder implementation)
func (s *service) calculateBalanceUSD(balance sqltypes.NullDecimal, tokenSymbol string) float64 {
	if balance.Big == nil {
		return 0.0
	}

	// Placeholder price calculation - in production, this would:
	// 1. Query price feeds (CoinGecko, CoinMarketCap, etc.)
	// 2. Use cached prices with fallback to external APIs
	// 3. Handle different tokens with different prices

	// Mock prices for common tokens
	var priceUSD float64
	switch tokenSymbol {
	case "CPOP":
		priceUSD = 0.05 // $0.05 per CPOP
	case "ETH":
		priceUSD = 2300.0 // $2300 per ETH
	case "BNB":
		priceUSD = 550.0 // $550 per BNB
	case "USDT", "USDC":
		priceUSD = 1.0 // $1 for stablecoins
	default:
		priceUSD = 0.01 // Default price for unknown tokens
	}

	balanceFloat, _ := strconv.ParseFloat(balance.Big.String(), 64)
	return balanceFloat * priceUSD
}

// determineSyncStatus determines sync status based on last sync time
func (s *service) determineSyncStatus(lastSync null.Time) string {
	if !lastSync.Valid {
		return "pending" // Never synced
	}

	timeSinceSync := time.Since(lastSync.Time)

	// Consider assets synced if last sync was within 10 minutes
	if timeSinceSync <= 10*time.Minute {
		return "synced"
	}

	// Consider assets syncing if last sync was within 1 hour
	if timeSinceSync <= 1*time.Hour {
		return "syncing"
	}

	// Otherwise, consider pending
	return "pending"
}

// getBatchInfoForUser gets batch information for a specific user
func (s *service) getBatchInfoForUser(ctx context.Context, userID string) *types.BatchInfo {
	// Query pending transactions for this user
	pendingCount, err := models.Transactions(
		models.TransactionWhere.UserID.EQ(userID),
		models.TransactionWhere.Status.EQ(null.StringFrom("pending")),
	).Count(ctx, s.db)

	if err != nil {
		log.Warn().Err(err).Str("user_id", userID).Msg("Failed to count pending transactions")
		pendingCount = 0
	}

	// Get optimal batch size from the first enabled chain (fallback)
	optimalSize := int64(25)
	currentSize := int64(0)

	if s.batchOptimizer != nil {
		// Try to get batch info from the first available chain
		chain, err := models.Chains(
			models.ChainWhere.IsEnabled.EQ(null.BoolFrom(true)),
			qm.Limit(1),
		).One(ctx, s.db)

		if err == nil {
			optimalSize = int64(s.batchOptimizer.GetOptimalBatchSize(chain.ChainID, 1)) // Use token ID 1 as fallback
			currentSize = int64(s.getCurrentBatchSize(chain.ChainID, 1))
		}
	}

	return &types.BatchInfo{
		PendingOperations:   pendingCount,
		NextBatchEstimate:   s.estimateNextBatch(pendingCount),
		WillBeBatched:       pendingCount > 0,
		BatchID:             fmt.Sprintf("batch_user_%s_%d", userID, time.Now().Unix()),
		CurrentBatchSize:    currentSize,
		OptimalBatchSize:    optimalSize,
		ExpectedEfficiency:  "74-78%",
		EstimatedGasSavings: fmt.Sprintf("%.2f USD", float64(pendingCount)*0.15),
		BatchType:           "mixed",
	}
}

// estimateNextBatch estimates when the next batch will be processed
func (s *service) estimateNextBatch(pendingCount int64) string {
	if pendingCount == 0 {
		return "no pending operations"
	}

	if pendingCount < 5 {
		return "15-30 minutes"
	}
	if pendingCount < 15 {
		return "5-15 minutes"
	}
	return "2-5 minutes"
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
			failure_reason = $2
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
		Msg("Transaction marked as failed")

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
		"token_id":       tx.TokenID,
		"business_type":  tx.BusinessType,
		"tx_type":        tx.TXType,
		"timestamp":      time.Now().Unix(), // Use Unix timestamp for consistency
	}

	// Add amount if available
	if tx.Amount.Big != nil {
		notificationData["amount"] = tx.Amount.Big.String()
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
			Msg("Failed to send transaction failed notification")
	}
}

// Helper functions for creating pointers
func strPtr(s string) *string {
	return &s
}

func float32Ptr(f float32) *float32 {
	return &f
}
