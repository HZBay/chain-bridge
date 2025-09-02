package queue

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/hzbay/chain-bridge/internal/services/chains"
	"github.com/rs/zerolog/log"
)

// BatchOptimizer optimizes batch processing parameters for maximum efficiency
type BatchOptimizer struct {
	monitor           *Monitor
	chainsService     chains.Service
	currentBatchSize  int
	optimalBatchSize  int
	performanceWindow []BatchPerformance
	maxWindowSize     int
	// Chain-specific cache for optimization
	chainOptimizationCache map[int64]*ChainOptimization
}

// ChainOptimization holds chain-specific optimization data
type ChainOptimization struct {
	ChainID              int64     `json:"chain_id"`
	OptimalBatchSize     int       `json:"optimal_batch_size"`
	OptimalWaitTime      int       `json:"optimal_wait_time_ms"`
	OptimalConsumerCount int       `json:"optimal_consumer_count"`
	LastUpdated          time.Time `json:"last_updated"`
	Confidence           float64   `json:"confidence"`
	// Performance characteristics
	AvgGasCost    float64 `json:"avg_gas_cost"`
	AvgBlockTime  float64 `json:"avg_block_time_seconds"`
	AvgThroughput float64 `json:"avg_throughput_tps"`
}

// BatchPerformance tracks performance metrics for a batch
type BatchPerformance struct {
	BatchSize        int           `json:"batch_size"`
	ProcessingTime   time.Duration `json:"processing_time"`
	GasSaved         float64       `json:"gas_saved"`
	EfficiencyRating float64       `json:"efficiency_rating"`
	Timestamp        time.Time     `json:"timestamp"`
	ChainID          int64         `json:"chain_id"`
	TokenID          int           `json:"token_id"`
	// Extended metrics for chain optimization
	GasPrice      float64 `json:"gas_price"`
	BlockNumber   int64   `json:"block_number"`
	WaitTime      int     `json:"wait_time_ms"`
	ConsumerCount int     `json:"consumer_count"`
}

// OptimizationRecommendation provides recommendations for batch optimization
type OptimizationRecommendation struct {
	CurrentBatchSize    int     `json:"current_batch_size"`
	RecommendedSize     int     `json:"recommended_batch_size"`
	ExpectedImprovement float64 `json:"expected_improvement_percent"`
	Confidence          float64 `json:"confidence_percent"`
	Reason              string  `json:"reason"`
	ChainID             int64   `json:"chain_id"`
	TokenID             int     `json:"token_id"`
	// Chain-specific recommendations
	RecommendedWaitTime      int    `json:"recommended_wait_time_ms"`
	RecommendedConsumerCount int    `json:"recommended_consumer_count"`
	ChainCharacteristics     string `json:"chain_characteristics"`
}

// NewBatchOptimizer creates a new batch optimizer
func NewBatchOptimizer(monitor *Monitor, chainsService chains.Service) *BatchOptimizer {
	return &BatchOptimizer{
		monitor:                monitor,
		chainsService:          chainsService,
		currentBatchSize:       25, // Default starting size
		optimalBatchSize:       25,
		maxWindowSize:          100, // Keep last 100 batch performances
		chainOptimizationCache: make(map[int64]*ChainOptimization),
	}
}

// RecordBatchPerformance records performance data for a completed batch
func (o *BatchOptimizer) RecordBatchPerformance(performance BatchPerformance) {
	o.performanceWindow = append(o.performanceWindow, performance)

	// Keep only the most recent performances
	if len(o.performanceWindow) > o.maxWindowSize {
		o.performanceWindow = o.performanceWindow[1:]
	}

	log.Debug().
		Int("batch_size", performance.BatchSize).
		Dur("processing_time", performance.ProcessingTime).
		Float64("gas_saved", performance.GasSaved).
		Float64("efficiency", performance.EfficiencyRating).
		Int64("chain_id", performance.ChainID).
		Float64("gas_price", performance.GasPrice).
		Int("wait_time_ms", performance.WaitTime).
		Int("consumer_count", performance.ConsumerCount).
		Msg("Recorded batch performance")

	// Update chain-specific optimization data
	o.updateChainOptimization(performance)

	// Trigger optimization analysis
	o.analyzeAndOptimize()
}

// analyzeAndOptimize analyzes recent performance and adjusts optimal batch size
func (o *BatchOptimizer) analyzeAndOptimize() {
	if len(o.performanceWindow) < 10 {
		return // Need at least 10 data points for meaningful analysis
	}

	// Group by batch size and calculate average efficiency
	sizeEfficiency := make(map[int][]float64)
	for _, perf := range o.performanceWindow {
		sizeEfficiency[perf.BatchSize] = append(sizeEfficiency[perf.BatchSize], perf.EfficiencyRating)
	}

	// Find the batch size with highest average efficiency
	bestSize := o.currentBatchSize
	bestEfficiency := 0.0

	for size, efficiencies := range sizeEfficiency {
		if len(efficiencies) < 3 {
			continue // Need at least 3 samples
		}

		avgEfficiency := average(efficiencies)
		if avgEfficiency > bestEfficiency {
			bestEfficiency = avgEfficiency
			bestSize = size
		}
	}

	// Update optimal batch size if we found a better one
	if bestSize != o.optimalBatchSize {
		log.Info().
			Int("old_optimal", o.optimalBatchSize).
			Int("new_optimal", bestSize).
			Float64("efficiency_improvement", bestEfficiency).
			Msg("Updating optimal batch size")

		o.optimalBatchSize = bestSize
	}
}

// updateChainOptimization updates chain-specific optimization data
func (o *BatchOptimizer) updateChainOptimization(performance BatchPerformance) {
	chainOpt, exists := o.chainOptimizationCache[performance.ChainID]
	if !exists {
		chainOpt = &ChainOptimization{
			ChainID:     performance.ChainID,
			LastUpdated: time.Now(),
			Confidence:  0.0,
		}
		o.chainOptimizationCache[performance.ChainID] = chainOpt
	}

	// Update chain characteristics based on performance data
	if performance.GasPrice > 0 {
		// Update average gas cost using exponential moving average
		if chainOpt.AvgGasCost == 0 {
			chainOpt.AvgGasCost = performance.GasPrice
		} else {
			chainOpt.AvgGasCost = 0.9*chainOpt.AvgGasCost + 0.1*performance.GasPrice
		}
	}

	// Calculate throughput from processing time and batch size
	if performance.ProcessingTime > 0 {
		throughput := float64(performance.BatchSize) / performance.ProcessingTime.Seconds()
		if chainOpt.AvgThroughput == 0 {
			chainOpt.AvgThroughput = throughput
		} else {
			chainOpt.AvgThroughput = 0.9*chainOpt.AvgThroughput + 0.1*throughput
		}
	}

	// Update optimal parameters based on efficiency
	if performance.EfficiencyRating > 80.0 { // High efficiency threshold
		chainOpt.OptimalBatchSize = performance.BatchSize
		chainOpt.OptimalWaitTime = performance.WaitTime
		chainOpt.OptimalConsumerCount = performance.ConsumerCount
	}

	chainOpt.LastUpdated = time.Now()
	chainOpt.Confidence = math.Min(chainOpt.Confidence+1.0, 100.0)

	log.Debug().
		Int64("chain_id", performance.ChainID).
		Float64("avg_gas_cost", chainOpt.AvgGasCost).
		Float64("avg_throughput", chainOpt.AvgThroughput).
		Int("optimal_batch_size", chainOpt.OptimalBatchSize).
		Int("optimal_wait_time", chainOpt.OptimalWaitTime).
		Float64("confidence", chainOpt.Confidence).
		Msg("Updated chain optimization data")
}

// GetOptimalBatchSize returns the current optimal batch size for given chain and token
func (o *BatchOptimizer) GetOptimalBatchSize(chainID int64, tokenID int) int {
	ctx := context.Background()

	// Get chain configuration from database
	batchConfig, err := o.chainsService.GetBatchConfig(ctx, chainID)
	if err != nil {
		log.Warn().Err(err).
			Int64("chain_id", chainID).
			Msg("Failed to get batch config from database, using default")
		return o.optimalBatchSize
	}

	// Check if we have chain-specific optimization data
	chainOpt, hasChainData := o.chainOptimizationCache[chainID]
	if hasChainData && chainOpt.Confidence > 50.0 && time.Since(chainOpt.LastUpdated) < 6*time.Hour {
		// Use machine learning optimized values if confidence is high and data is recent
		optimalSize := chainOpt.OptimalBatchSize

		// Ensure it's within database constraints
		if optimalSize < batchConfig.MinBatchSize {
			optimalSize = batchConfig.MinBatchSize
		} else if optimalSize > batchConfig.MaxBatchSize {
			optimalSize = batchConfig.MaxBatchSize
		}

		log.Debug().
			Int64("chain_id", chainID).
			Int("token_id", tokenID).
			Int("optimal_size", optimalSize).
			Float64("confidence", chainOpt.Confidence).
			Float64("avg_gas_cost", chainOpt.AvgGasCost).
			Float64("avg_throughput", chainOpt.AvgThroughput).
			Msg("Using ML-optimized batch size for chain")

		return optimalSize
	}

	// Filter performance data for this specific chain/token combination
	var relevantPerformances []BatchPerformance
	for _, perf := range o.performanceWindow {
		if perf.ChainID == chainID && perf.TokenID == tokenID {
			relevantPerformances = append(relevantPerformances, perf)
		}
	}

	if len(relevantPerformances) < 5 {
		// Not enough data for this chain/token, apply chain characteristics
		optimalSize := o.getChainAwareOptimalSize(chainID, batchConfig)

		log.Debug().
			Int64("chain_id", chainID).
			Int("token_id", tokenID).
			Int("optimal_size", optimalSize).
			Msg("Using chain-aware optimal batch size")
		return optimalSize
	}

	// Find optimal size for this specific chain/token within database constraints
	sizeEfficiency := make(map[int][]float64)
	for _, perf := range relevantPerformances {
		// Only consider sizes within the chain's configured limits
		if perf.BatchSize >= batchConfig.MinBatchSize && perf.BatchSize <= batchConfig.MaxBatchSize {
			sizeEfficiency[perf.BatchSize] = append(sizeEfficiency[perf.BatchSize], perf.EfficiencyRating)
		}
	}

	bestSize := batchConfig.OptimalBatchSize
	bestEfficiency := 0.0

	for size, efficiencies := range sizeEfficiency {
		if len(efficiencies) < 2 {
			continue
		}

		avgEfficiency := average(efficiencies)
		if avgEfficiency > bestEfficiency {
			bestEfficiency = avgEfficiency
			bestSize = size
		}
	}

	// Ensure the size is within configured limits
	if bestSize < batchConfig.MinBatchSize {
		bestSize = batchConfig.MinBatchSize
	} else if bestSize > batchConfig.MaxBatchSize {
		bestSize = batchConfig.MaxBatchSize
	}

	log.Debug().
		Int64("chain_id", chainID).
		Int("token_id", tokenID).
		Int("optimal_size", bestSize).
		Int("min_size", batchConfig.MinBatchSize).
		Int("max_size", batchConfig.MaxBatchSize).
		Float64("efficiency", bestEfficiency).
		Msg("Calculated optimal batch size with database constraints")

	return bestSize
}

// getChainAwareOptimalSize determines optimal batch size based on chain characteristics
func (o *BatchOptimizer) getChainAwareOptimalSize(chainID int64, batchConfig *chains.BatchConfig) int {
	chainOpt, hasChainData := o.chainOptimizationCache[chainID]

	if !hasChainData {
		// No chain data available, use database default
		return batchConfig.OptimalBatchSize
	}

	// Adjust batch size based on chain characteristics
	optimalSize := batchConfig.OptimalBatchSize

	// Higher gas cost chains should use larger batches to amortize costs
	if chainOpt.AvgGasCost > 50.0 { // High gas cost threshold (adjust based on actual values)
		optimalSize = int(float64(optimalSize) * 1.2) // Increase by 20%
	} else if chainOpt.AvgGasCost > 0 && chainOpt.AvgGasCost < 10.0 { // Low gas cost
		optimalSize = int(float64(optimalSize) * 0.8) // Decrease by 20%
	}

	// Lower throughput chains should use smaller batches to reduce wait time
	if chainOpt.AvgThroughput > 0 && chainOpt.AvgThroughput < 5.0 { // Low throughput threshold
		optimalSize = int(float64(optimalSize) * 0.7) // Decrease by 30%
	} else if chainOpt.AvgThroughput > 20.0 { // High throughput
		optimalSize = int(float64(optimalSize) * 1.3) // Increase by 30%
	}

	// Ensure within bounds
	if optimalSize < batchConfig.MinBatchSize {
		optimalSize = batchConfig.MinBatchSize
	} else if optimalSize > batchConfig.MaxBatchSize {
		optimalSize = batchConfig.MaxBatchSize
	}

	log.Debug().
		Int64("chain_id", chainID).
		Float64("avg_gas_cost", chainOpt.AvgGasCost).
		Float64("avg_throughput", chainOpt.AvgThroughput).
		Int("original_optimal", batchConfig.OptimalBatchSize).
		Int("adjusted_optimal", optimalSize).
		Msg("Applied chain-aware batch size optimization")

	return optimalSize
}

// GetOptimizationRecommendation provides detailed optimization recommendations
func (o *BatchOptimizer) GetOptimizationRecommendation(chainID int64, tokenID int) *OptimizationRecommendation {
	ctx := context.Background()
	currentSize := o.currentBatchSize
	optimalSize := o.GetOptimalBatchSize(chainID, tokenID)

	// Get chain configuration for additional recommendations
	batchConfig, err := o.chainsService.GetBatchConfig(ctx, chainID)
	if err != nil {
		log.Warn().Err(err).Int64("chain_id", chainID).Msg("Failed to get batch config for recommendations")
	}

	// Calculate expected improvement
	currentEfficiency := o.getAverageEfficiencyForSize(currentSize, chainID, tokenID)
	optimalEfficiency := o.getAverageEfficiencyForSize(optimalSize, chainID, tokenID)

	improvement := ((optimalEfficiency - currentEfficiency) / currentEfficiency) * 100
	if improvement < 0 {
		improvement = 0
	}

	// Calculate confidence based on data availability
	dataPoints := o.getDataPointsForChainToken(chainID, tokenID)
	confidence := math.Min(float64(dataPoints)/20.0*100, 100) // 100% confidence with 20+ data points

	// Get chain-specific recommendations
	recommendedWaitTime, recommendedConsumerCount, chainCharacteristics := o.getChainSpecificRecommendations(chainID, batchConfig)

	reason := o.generateRecommendationReason(currentSize, optimalSize, improvement)

	return &OptimizationRecommendation{
		CurrentBatchSize:         currentSize,
		RecommendedSize:          optimalSize,
		ExpectedImprovement:      improvement,
		Confidence:               confidence,
		Reason:                   reason,
		ChainID:                  chainID,
		TokenID:                  tokenID,
		RecommendedWaitTime:      recommendedWaitTime,
		RecommendedConsumerCount: recommendedConsumerCount,
		ChainCharacteristics:     chainCharacteristics,
	}
}

// getChainSpecificRecommendations provides chain-specific optimization recommendations
func (o *BatchOptimizer) getChainSpecificRecommendations(chainID int64, batchConfig *chains.BatchConfig) (int, int, string) {
	chainOpt, hasChainData := o.chainOptimizationCache[chainID]

	// Default recommendations from database
	recommendedWaitTime := 15000 // Default 15 seconds
	recommendedConsumerCount := 1
	chainCharacteristics := "Standard chain characteristics"

	// Use database configuration as baseline if available
	if batchConfig != nil {
		// Use database optimal batch size as a reference for wait time calculation
		if batchConfig.OptimalBatchSize > 30 {
			recommendedWaitTime = 20000 // Larger batches can wait longer
		} else if batchConfig.OptimalBatchSize < 15 {
			recommendedWaitTime = 10000 // Smaller batches should be faster
		}
	}

	if !hasChainData {
		return recommendedWaitTime, recommendedConsumerCount, chainCharacteristics
	}

	// Use optimized values if available and confident
	if chainOpt.Confidence > 70.0 {
		if chainOpt.OptimalWaitTime > 0 {
			recommendedWaitTime = chainOpt.OptimalWaitTime
		}
		if chainOpt.OptimalConsumerCount > 0 {
			recommendedConsumerCount = chainOpt.OptimalConsumerCount
		}
	}

	// Generate chain characteristics description
	if chainOpt.AvgGasCost > 50.0 {
		chainCharacteristics = "High gas cost chain - benefits from larger batches to amortize transaction costs"
		recommendedWaitTime = int(float64(recommendedWaitTime) * 1.5) // Wait longer for larger batches
	} else if chainOpt.AvgGasCost > 0 && chainOpt.AvgGasCost < 10.0 {
		chainCharacteristics = "Low gas cost chain - can use smaller batches for faster processing"
		recommendedWaitTime = int(float64(recommendedWaitTime) * 0.7) // Shorter wait time
	}

	if chainOpt.AvgThroughput > 0 {
		if chainOpt.AvgThroughput < 5.0 {
			chainCharacteristics += ". Low throughput detected - recommend smaller batches and multiple consumers"
			recommendedConsumerCount = 2
		} else if chainOpt.AvgThroughput > 20.0 {
			chainCharacteristics += ". High throughput detected - can handle larger batches efficiently"
		}
	}

	// Ensure wait time is reasonable (5-60 seconds)
	if recommendedWaitTime < 5000 {
		recommendedWaitTime = 5000
	} else if recommendedWaitTime > 60000 {
		recommendedWaitTime = 60000
	}

	// Ensure consumer count is reasonable (1-4)
	if recommendedConsumerCount < 1 {
		recommendedConsumerCount = 1
	} else if recommendedConsumerCount > 4 {
		recommendedConsumerCount = 4
	}

	return recommendedWaitTime, recommendedConsumerCount, chainCharacteristics
}

// getAverageEfficiencyForSize calculates average efficiency for a specific batch size
func (o *BatchOptimizer) getAverageEfficiencyForSize(size int, chainID int64, tokenID int) float64 {
	var efficiencies []float64

	for _, perf := range o.performanceWindow {
		if perf.BatchSize == size && perf.ChainID == chainID && perf.TokenID == tokenID {
			efficiencies = append(efficiencies, perf.EfficiencyRating)
		}
	}

	if len(efficiencies) == 0 {
		return 75.0 // Default efficiency assumption
	}

	return average(efficiencies)
}

// getDataPointsForChainToken counts available data points for a chain/token combination
func (o *BatchOptimizer) getDataPointsForChainToken(chainID int64, tokenID int) int {
	count := 0
	for _, perf := range o.performanceWindow {
		if perf.ChainID == chainID && perf.TokenID == tokenID {
			count++
		}
	}
	return count
}

// generateRecommendationReason generates human-readable reason for the recommendation
func (o *BatchOptimizer) generateRecommendationReason(currentSize, optimalSize int, improvement float64) string {
	if optimalSize == currentSize {
		return "Current batch size is already optimal based on recent performance data and chain characteristics"
	}

	sizeChange := optimalSize - currentSize
	direction := "increase"
	if sizeChange < 0 {
		direction = "decrease"
		sizeChange = -sizeChange
	}

	if improvement > 15 {
		return fmt.Sprintf("Significant efficiency improvement (+%.1f%%) possible by %sing batch size by %d operations", improvement, direction, sizeChange)
	}
	if improvement > 8 {
		return fmt.Sprintf("Moderate efficiency improvement (+%.1f%%) possible by %sing batch size by %d operations", improvement, direction, sizeChange)
	}
	if improvement > 3 {
		return fmt.Sprintf("Minor efficiency improvement (+%.1f%%) possible by %sing batch size by %d operations", improvement, direction, sizeChange)
	}
	return fmt.Sprintf("Marginal efficiency improvement (+%.1f%%) possible with recommended batch size", improvement)
}

// StartAdaptiveOptimization begins continuous optimization based on real-time performance
func (o *BatchOptimizer) StartAdaptiveOptimization(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Info().
		Dur("interval", interval).
		Msg("Starting adaptive batch size optimization")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping adaptive optimization")
			return
		case <-ticker.C:
			o.performAdaptiveOptimization()
		}
	}
}

// performAdaptiveOptimization performs periodic optimization analysis
func (o *BatchOptimizer) performAdaptiveOptimization() {
	if len(o.performanceWindow) < 10 {
		return
	}

	// Check if we should experiment with different batch sizes
	recentPerformances := o.getRecentPerformances(time.Hour) // Last hour
	if len(recentPerformances) < 5 {
		return
	}

	avgEfficiency := average(extractEfficiencies(recentPerformances))

	// If efficiency is below 70%, suggest experimenting with different sizes
	if avgEfficiency < 70.0 {
		log.Info().
			Float64("current_efficiency", avgEfficiency).
			Msg("Low efficiency detected, recommending batch size experimentation")

		// TODO: Trigger batch size experimentation
	}
}

// getRecentPerformances returns performances within the specified duration
func (o *BatchOptimizer) getRecentPerformances(duration time.Duration) []BatchPerformance {
	cutoff := time.Now().Add(-duration)
	var recent []BatchPerformance

	for _, perf := range o.performanceWindow {
		if perf.Timestamp.After(cutoff) {
			recent = append(recent, perf)
		}
	}

	return recent
}

// GetChainOptimizationInsights returns detailed optimization insights for a specific chain
func (o *BatchOptimizer) GetChainOptimizationInsights(chainID int64) *ChainOptimization {
	chainOpt, exists := o.chainOptimizationCache[chainID]
	if !exists {
		return &ChainOptimization{
			ChainID:    chainID,
			Confidence: 0.0,
		}
	}

	// Return a copy to prevent external modification
	return &ChainOptimization{
		ChainID:              chainOpt.ChainID,
		OptimalBatchSize:     chainOpt.OptimalBatchSize,
		OptimalWaitTime:      chainOpt.OptimalWaitTime,
		OptimalConsumerCount: chainOpt.OptimalConsumerCount,
		LastUpdated:          chainOpt.LastUpdated,
		Confidence:           chainOpt.Confidence,
		AvgGasCost:           chainOpt.AvgGasCost,
		AvgBlockTime:         chainOpt.AvgBlockTime,
		AvgThroughput:        chainOpt.AvgThroughput,
	}
}

// ClearChainOptimizationCache clears the optimization cache for a specific chain or all chains
func (o *BatchOptimizer) ClearChainOptimizationCache(chainID int64) {
	if chainID == 0 {
		// Clear all chains
		o.chainOptimizationCache = make(map[int64]*ChainOptimization)
		log.Info().Msg("Cleared all chain optimization cache")
	} else {
		// Clear specific chain
		delete(o.chainOptimizationCache, chainID)
		log.Info().Int64("chain_id", chainID).Msg("Cleared chain optimization cache")
	}
}

// Helper functions
func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func extractEfficiencies(performances []BatchPerformance) []float64 {
	efficiencies := make([]float64, len(performances))
	for i, perf := range performances {
		efficiencies[i] = perf.EfficiencyRating
	}
	return efficiencies
}
