package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

	// TODO: Implement actual database queries
	// This would typically:
	// 1. Query user's accounts from the database
	// 2. Query balances for each chain from blockchain nodes
	// 3. Calculate USD values using price feeds
	// 4. Get synchronization status for each asset
	// TODO: 要集成AlchemyAPI
	// Mock response for now - this should be replaced with real implementation
	totalValueUsd := 1250.50
	assets := []types.AssetInfo{
		{
			ChainID:          int64Ptr(56),
			ChainName:        "BSC",
			Symbol:           strPtr("CPOP"),
			Name:             "ChainBridge PoP Token",
			ContractAddress:  "0x742d35Cc6634C0532925a3b8D238b45D2F78d8F3",
			Decimals:         18,
			ConfirmedBalance: strPtr("5000.0"),
			PendingBalance:   strPtr("5050.0"),
			LockedBalance:    strPtr("0.0"),
			BalanceUsd:       250.0,
			SyncStatus:       strPtr(types.AssetInfoSyncStatusSynced),
		},
		{
			ChainID:          int64Ptr(1),
			ChainName:        "Ethereum",
			Symbol:           strPtr("CPOP"),
			Name:             "ChainBridge PoP Token",
			ContractAddress:  "0x832d35Cc6634C0532925a3b8D238b45D2F78e9A1",
			Decimals:         18,
			ConfirmedBalance: strPtr("3000.0"),
			PendingBalance:   strPtr("3000.0"),
			LockedBalance:    strPtr("100.0"),
			BalanceUsd:       1000.5,
			SyncStatus:       strPtr(types.AssetInfoSyncStatusSynced),
		},
	}

	// Convert assets slice to pointer slice
	assetPointers := make([]*types.AssetInfo, len(assets))
	for i := range assets {
		assetPointers[i] = &assets[i]
	}

	// Convert totalValueUsd to float32 pointer
	totalValueUsdFloat32 := float32(totalValueUsd)

	response := &types.AssetsResponse{
		UserID:        strPtr(userID),
		TotalValueUsd: &totalValueUsdFloat32,
		Assets:        assetPointers,
	}

	// Mock batch info - get pending operations info for this user
	batchInfo := &types.BatchInfo{
		PendingOperations:   3,
		NextBatchEstimate:   "5-10 minutes",
		WillBeBatched:       true,
		BatchID:             "batch_daily_rewards_20241215",
		CurrentBatchSize:    24,
		OptimalBatchSize:    25,
		ExpectedEfficiency:  "75-77%",
		EstimatedGasSavings: "156.80 USD",
		BatchType:           "batchTransferFrom",
	}

	log.Info().
		Str("user_id", userID).
		Float64("total_value_usd", totalValueUsd).
		Int("asset_count", len(assets)).
		Msg("User assets retrieved successfully")

	return response, batchInfo, nil
}

// Helper functions for creating pointers
func strPtr(s string) *string {
	return &s
}

func int64Ptr(i int64) *int64 {
	return &i
}
