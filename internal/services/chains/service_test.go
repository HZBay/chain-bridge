package chains

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockChainsService for testing
type MockChainsService struct {
	mock.Mock
}

func (m *MockChainsService) GetChainConfig(ctx context.Context, chainID int64) (*ChainConfig, error) {
	args := m.Called(ctx, chainID)
	return args.Get(0).(*ChainConfig), args.Error(1)
}

func (m *MockChainsService) GetAllEnabledChains(ctx context.Context) ([]*ChainConfig, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*ChainConfig), args.Error(1)
}

func (m *MockChainsService) GetBatchConfig(ctx context.Context, chainID int64) (*BatchConfig, error) {
	args := m.Called(ctx, chainID)
	return args.Get(0).(*BatchConfig), args.Error(1)
}

func (m *MockChainsService) UpdateBatchConfig(ctx context.Context, chainID int64, config *BatchConfig) error {
	args := m.Called(ctx, chainID, config)
	return args.Error(0)
}

func (m *MockChainsService) IsChainEnabled(ctx context.Context, chainID int64) (bool, error) {
	args := m.Called(ctx, chainID)
	return args.Bool(0), args.Error(1)
}

func (m *MockChainsService) RefreshCache(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestDefaultBatchConfig(t *testing.T) {
	assert.Equal(t, 25, DefaultBatchConfig.OptimalBatchSize)
	assert.Equal(t, 40, DefaultBatchConfig.MaxBatchSize)
	assert.Equal(t, 10, DefaultBatchConfig.MinBatchSize)
}

func TestChainConfig_Structure(t *testing.T) {
	// Test that ChainConfig has all expected fields
	config := &ChainConfig{
		ChainID:   56,
		Name:      "BSC",
		ShortName: "bsc",
		RPCURL:    "https://bsc-dataseed1.binance.org/",
		BatchConfig: BatchConfig{
			OptimalBatchSize: 25,
			MaxBatchSize:     40,
			MinBatchSize:     10,
		},
		IsEnabled: true,
		CreatedAt: time.Now(),
	}

	assert.Equal(t, int64(56), config.ChainID)
	assert.Equal(t, "BSC", config.Name)
	assert.Equal(t, "bsc", config.ShortName)
	assert.True(t, config.IsEnabled)
	assert.Equal(t, 25, config.BatchConfig.OptimalBatchSize)
	assert.Equal(t, 40, config.BatchConfig.MaxBatchSize)
	assert.Equal(t, 10, config.BatchConfig.MinBatchSize)
}

func TestBatchConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  BatchConfig
		isValid bool
	}{
		{
			name: "Valid configuration",
			config: BatchConfig{
				OptimalBatchSize: 25,
				MaxBatchSize:     40,
				MinBatchSize:     10,
			},
			isValid: true,
		},
		{
			name: "Invalid min size (zero)",
			config: BatchConfig{
				OptimalBatchSize: 25,
				MaxBatchSize:     40,
				MinBatchSize:     0,
			},
			isValid: false,
		},
		{
			name: "Invalid max smaller than min",
			config: BatchConfig{
				OptimalBatchSize: 25,
				MaxBatchSize:     5,
				MinBatchSize:     10,
			},
			isValid: false,
		},
		{
			name: "Invalid optimal too small",
			config: BatchConfig{
				OptimalBatchSize: 5,
				MaxBatchSize:     40,
				MinBatchSize:     10,
			},
			isValid: false,
		},
		{
			name: "Invalid optimal too large",
			config: BatchConfig{
				OptimalBatchSize: 50,
				MaxBatchSize:     40,
				MinBatchSize:     10,
			},
			isValid: false,
		},
		{
			name: "Invalid max too large",
			config: BatchConfig{
				OptimalBatchSize: 25,
				MaxBatchSize:     150,
				MinBatchSize:     10,
			},
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a service to test validation
			service := &service{}
			err := service.validateBatchConfig(&tt.config)

			if tt.isValid {
				assert.NoError(t, err, "Expected config to be valid")
			} else {
				assert.Error(t, err, "Expected config to be invalid")
			}
		})
	}
}

// Integration tests would go here when running with a real database
// func TestService_Integration(t *testing.T) {
//     if testing.Short() {
//         t.Skip("Skipping integration test in short mode")
//     }
//     // Test with real database connection
// }
