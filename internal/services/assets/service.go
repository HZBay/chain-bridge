package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/ericlagergren/decimal"
	"github.com/google/uuid"
	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/hzbay/chain-bridge/internal/queue"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/rs/zerolog/log"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
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

	// 1. 幂等性检查 - 检查 OperationID 是否已经存在
	mainOperationID := uuid.MustParse(*req.OperationID)
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
	// TODO: 是否要更新user_balances表

	// 2. Process each adjustment
	var adjustJobs []queue.AssetAdjustJob
	var transactionRecords []*models.Transaction

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
			TXType:       "asset_adjust",
			BusinessType: *adjustment.BusinessType,
			TokenID:      s.getTokenIDBySymbol(*adjustment.ChainID, *adjustment.TokenSymbol),
			Amount: sqltypes.NewDecimal(func() *decimal.Big {
				d, _ := decimal.New(0, 0).SetString(amount)
				return d
			}()),
			Status:           null.StringFrom("pending"),
			IsBatchOperation: null.BoolFrom(true),
			ReasonType:       *adjustment.ReasonType,
			ReasonDetail:     null.StringFromPtr(&adjustment.ReasonDetail),
		}

		// Insert transaction record
		if err := transaction.Insert(ctx, tx, boil.Infer()); err != nil {
			return nil, nil, fmt.Errorf("failed to insert transaction for user %s: %w", *adjustment.UserID, err)
		}

		transactionRecords = append(transactionRecords, transaction)

		// Create asset adjust job
		adjustJob := queue.AssetAdjustJob{
			ID:             txID.String(),
			TransactionID:  txID,
			ChainID:        *adjustment.ChainID,
			TokenID:        s.getTokenIDBySymbol(*adjustment.ChainID, *adjustment.TokenSymbol),
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

			// TODO: 是否要将对应的transaction设置为failed
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
		tokenID = s.getTokenIDBySymbol(*req.Adjustments[0].ChainID, *req.Adjustments[0].TokenSymbol)
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

func (s *service) getCurrentBatchSize(chainID int64, tokenID int) int32 {
	if s.batchOptimizer != nil {
		return int32(s.batchOptimizer.GetOptimalBatchSize(chainID, tokenID))
	}
	// Fallback to optimal batch size from chain config
	return int32(s.getOptimalBatchSize(chainID, tokenID))
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
	userBalances, err := models.UserBalances(
		models.UserBalanceWhere.UserID.EQ(userID),
		models.UserBalanceWhere.ConfirmedBalance.GT(sqltypes.NewNullDecimal(decimal.New(0, 0))),
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
	} else if pendingCount < 15 {
		return "5-15 minutes"
	} else {
		return "2-5 minutes"
	}
}

// Helper functions for creating pointers
func strPtr(s string) *string {
	return &s
}

func int64Ptr(i int64) *int64 {
	return &i
}

func float32Ptr(f float32) *float32 {
	return &f
}
