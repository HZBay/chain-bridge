package queue

import (
	"context"
	"math"
	"time"

	"github.com/hzbay/chain-bridge/internal/services/chains"
	"github.com/rs/zerolog/log"
)

// BatchOptimizer optimizes batch processing parameters for maximum efficiency
type BatchOptimizer struct {
	monitor           *QueueMonitor
	chainsService     chains.Service
	currentBatchSize  int
	optimalBatchSize  int
	performanceWindow []BatchPerformance
	maxWindowSize     int
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
}

// NewBatchOptimizer creates a new batch optimizer
func NewBatchOptimizer(monitor *QueueMonitor, chainsService chains.Service) *BatchOptimizer {
	return &BatchOptimizer{
		monitor:          monitor,
		chainsService:    chainsService,
		currentBatchSize: 25, // Default starting size
		optimalBatchSize: 25,
		maxWindowSize:    100, // Keep last 100 batch performances
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
		Msg("Recorded batch performance")

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

	// Filter performance data for this specific chain/token combination
	var relevantPerformances []BatchPerformance
	for _, perf := range o.performanceWindow {
		if perf.ChainID == chainID && perf.TokenID == tokenID {
			relevantPerformances = append(relevantPerformances, perf)
		}
	}

	if len(relevantPerformances) < 5 {
		// Not enough data for this chain/token, return database configured optimal
		log.Debug().
			Int64("chain_id", chainID).
			Int("token_id", tokenID).
			Int("optimal_size", batchConfig.OptimalBatchSize).
			Msg("Using database configured optimal batch size")
		return batchConfig.OptimalBatchSize
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

// GetOptimizationRecommendation provides detailed optimization recommendations
func (o *BatchOptimizer) GetOptimizationRecommendation(chainID int64, tokenID int) *OptimizationRecommendation {
	currentSize := o.currentBatchSize
	optimalSize := o.GetOptimalBatchSize(chainID, tokenID)

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

	reason := o.generateRecommendationReason(currentSize, optimalSize, improvement)

	return &OptimizationRecommendation{
		CurrentBatchSize:    currentSize,
		RecommendedSize:     optimalSize,
		ExpectedImprovement: improvement,
		Confidence:          confidence,
		Reason:              reason,
		ChainID:             chainID,
		TokenID:             tokenID,
	}
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
		return "Current batch size is already optimal based on recent performance data"
	}

	if improvement > 10 {
		return "Significant efficiency improvement possible with recommended batch size"
	} else if improvement > 5 {
		return "Moderate efficiency improvement possible with recommended batch size"
	} else {
		return "Minor efficiency improvement possible with recommended batch size"
	}
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
