package queue

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hzbay/chain-bridge/internal/config"
)

func TestRabbitMQIntegration(t *testing.T) {
	// Skip this test if RabbitMQ is not available
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create test configuration
	cfg := config.Server{
		RabbitMQ: config.RabbitMQConfig{
			Enabled:  true,
			Host:     "localhost",
			Port:     5672,
			Username: "guest",
			Password: "guest",
			VHost:    "/",
			BatchStrategy: config.BatchProcessingStrategy{
				EnableRabbitMQ:     true,
				RabbitMQPercentage: 100, // Use RabbitMQ for 100% of operations in test
			},
		},
	}

	ctx := context.Background()

	// Create batch processor
	processor, err := NewBatchProcessor(cfg)
	if err != nil {
		t.Fatalf("Failed to create batch processor: %v", err)
	}

	// Create monitor and optimizer
	monitor := NewQueueMonitor(processor)
	// For test purposes, pass nil as chains service
	optimizer := NewBatchOptimizer(monitor, nil)

	// Test publishing a transfer job
	transferJob := TransferJob{
		ID:            "test-transfer-" + uuid.New().String(),
		TransactionID: uuid.New(),
		ChainID:       56, // BSC testnet
		TokenID:       1,
		FromUserID:    "test-user-1",
		ToUserID:      "test-user-2",
		Amount:        "100.0",
		BusinessType:  "transfer",
		ReasonType:    "user_transfer",
		Priority:      PriorityNormal,
		CreatedAt:     time.Now(),
	}

	// Publish transfer job
	err = processor.PublishTransfer(ctx, transferJob)
	if err != nil {
		t.Fatalf("Failed to publish transfer job: %v", err)
	}

	// Test health check
	err = monitor.HealthCheck(ctx)
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	// Test getting metrics
	metrics := monitor.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected metrics, got nil")
	}

	// Test getting optimization recommendation
	recommendation := optimizer.GetOptimizationRecommendation(56, 1)
	if recommendation == nil {
		t.Fatal("Expected optimization recommendation, got nil")
	}

	// Verify recommendation fields
	if recommendation.ChainID != 56 {
		t.Errorf("Expected chain ID 56, got %d", recommendation.ChainID)
	}
	if recommendation.TokenID != 1 {
		t.Errorf("Expected token ID 1, got %d", recommendation.TokenID)
	}

	// Test exporting metrics
	metricsJSON, err := monitor.ExportMetrics()
	if err != nil {
		t.Fatalf("Failed to export metrics: %v", err)
	}
	if len(metricsJSON) == 0 {
		t.Fatal("Expected metrics JSON data, got empty")
	}

	t.Log("RabbitMQ integration test completed successfully")
}

func TestMemoryProcessorFallback(t *testing.T) {
	// Test with RabbitMQ disabled to ensure memory processor fallback works
	cfg := config.Server{
		RabbitMQ: config.RabbitMQConfig{
			Enabled: false,
			BatchStrategy: config.BatchProcessingStrategy{
				EnableRabbitMQ: false,
			},
		},
	}

	ctx := context.Background()

	// Create batch processor (should fall back to memory)
	processor, err := NewBatchProcessor(cfg)
	if err != nil {
		t.Fatalf("Failed to create batch processor: %v", err)
	}

	// Create monitor
	monitor := NewQueueMonitor(processor)

	// Test publishing a transfer job
	transferJob := TransferJob{
		ID:            "test-fallback-" + uuid.New().String(),
		TransactionID: uuid.New(),
		ChainID:       1, // Ethereum
		TokenID:       1,
		FromUserID:    "test-user-1",
		ToUserID:      "test-user-2",
		Amount:        "50.0",
		BusinessType:  "transfer",
		ReasonType:    "user_transfer",
		Priority:      PriorityNormal,
		CreatedAt:     time.Now(),
	}

	// Publish transfer job (should use memory processor)
	err = processor.PublishTransfer(ctx, transferJob)
	if err != nil {
		t.Fatalf("Failed to publish transfer job to memory processor: %v", err)
	}

	// Test health check
	err = monitor.HealthCheck(ctx)
	if err != nil {
		t.Fatalf("Health check failed for memory processor: %v", err)
	}

	// Test getting metrics
	metrics := monitor.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected metrics from memory processor, got nil")
	}

	// Verify processor type is memory
	if metrics.ProcessorType == "rabbitmq" {
		t.Error("Expected memory processor, but got RabbitMQ processor")
	}

	t.Log("Memory processor fallback test completed successfully")
}

func TestBatchOptimization(t *testing.T) {
	// Create a monitor with mock data
	processor := &MemoryProcessor{}
	monitor := NewQueueMonitor(processor)
	// For test purposes, pass nil as chains service
	optimizer := NewBatchOptimizer(monitor, nil)

	// Record some sample performance data
	performances := []BatchPerformance{
		{BatchSize: 20, ProcessingTime: 2000 * time.Millisecond, GasSaved: 120.5, EfficiencyRating: 75.2, ChainID: 56, TokenID: 1, Timestamp: time.Now()},
		{BatchSize: 25, ProcessingTime: 2100 * time.Millisecond, GasSaved: 150.8, EfficiencyRating: 78.1, ChainID: 56, TokenID: 1, Timestamp: time.Now()},
		{BatchSize: 30, ProcessingTime: 2300 * time.Millisecond, GasSaved: 175.2, EfficiencyRating: 76.9, ChainID: 56, TokenID: 1, Timestamp: time.Now()},
		{BatchSize: 25, ProcessingTime: 2050 * time.Millisecond, GasSaved: 148.3, EfficiencyRating: 78.5, ChainID: 56, TokenID: 1, Timestamp: time.Now()},
		{BatchSize: 25, ProcessingTime: 2080 * time.Millisecond, GasSaved: 152.1, EfficiencyRating: 78.3, ChainID: 56, TokenID: 1, Timestamp: time.Now()},
	}

	// Record performance data
	for _, perf := range performances {
		optimizer.RecordBatchPerformance(perf)
	}

	// Get optimization recommendation
	recommendation := optimizer.GetOptimizationRecommendation(56, 1)
	if recommendation == nil {
		t.Fatal("Expected optimization recommendation, got nil")
	}

	// Verify that batch size 25 is recommended (highest average efficiency)
	if recommendation.RecommendedSize != 25 {
		t.Errorf("Expected recommended batch size 25, got %d", recommendation.RecommendedSize)
	}

	// Verify confidence is reasonable
	if recommendation.Confidence < 20 {
		t.Errorf("Expected confidence >= 20, got %f", recommendation.Confidence)
	}

	t.Log("Batch optimization test completed successfully")
	t.Logf("Recommendation: size %d, improvement %.2f%%, confidence %.1f%%",
		recommendation.RecommendedSize, recommendation.ExpectedImprovement, recommendation.Confidence)
}
