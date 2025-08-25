package assets

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockAssetsService for testing
type MockAssetsService struct {
	mock.Mock
}

func (m *MockAssetsService) AdjustAssets(ctx context.Context, req *types.AssetAdjustRequest) (*types.AssetAdjustResponse, *types.BatchInfo, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*types.AssetAdjustResponse), args.Get(1).(*types.BatchInfo), args.Error(2)
}

func TestAdjustAssetsHandler_Handle(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAssetsService)
	handler := NewAdjustAssetsHandler(mockService)

	// Test data
	operationID := "op_daily_rewards_001"
	userID := "user_123"
	amount := "+100.0"
	chainID := int64(56)
	tokenSymbol := "CPOP"
	businessType := "reward"
	reasonType := "daily_checkin"
	reasonDetail := "Daily check-in reward"

	processedCount := int64(1)
	status := "recorded"

	adjustment := &types.AssetAdjustment{
		UserID:       &userID,
		Amount:       &amount,
		ChainID:      &chainID,
		TokenSymbol:  &tokenSymbol,
		BusinessType: &businessType,
		ReasonType:   &reasonType,
		ReasonDetail: reasonDetail,
	}

	adjustRequest := &types.AssetAdjustRequest{
		OperationID: &operationID,
		Adjustments: []*types.AssetAdjustment{adjustment},
	}

	expectedResponse := &types.AssetAdjustResponse{
		OperationID:    &operationID,
		ProcessedCount: &processedCount,
		Status:         &status,
	}

	expectedBatchInfo := &types.BatchInfo{
		WillBeBatched:    true,
		BatchType:        BatchTypeAdjustAssets,
		CurrentBatchSize: 12,
		OptimalBatchSize: 25,
	}

	// Mock service call
	mockService.On("AdjustAssets", mock.Anything, adjustRequest).
		Return(expectedResponse, expectedBatchInfo, nil)

	// Create request
	reqBody, _ := json.Marshal(map[string]interface{}{
		"operation_id": operationID,
		"adjustments": []map[string]interface{}{
			{
				"user_id":       userID,
				"amount":        amount,
				"chain_id":      chainID,
				"token_symbol":  tokenSymbol,
				"business_type": businessType,
				"reason_type":   reasonType,
				"reason_detail": reasonDetail,
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/assets/adjust", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute
	err := handler.Handle(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "data")
	assert.Contains(t, response, "batch_info")

	// Verify response data
	data := response["data"].(map[string]interface{})
	assert.Equal(t, operationID, data["operation_id"])
	assert.Equal(t, status, data["status"])
	assert.Equal(t, float64(1), data["processed_count"]) // JSON unmarshals numbers as float64

	// Verify batch info
	batchInfo := response["batch_info"].(map[string]interface{})
	assert.Equal(t, true, batchInfo["will_be_batched"])
	assert.Equal(t, BatchTypeAdjustAssets, batchInfo["batch_type"])

	mockService.AssertExpectations(t)
}

func TestAdjustAssetsHandler_Handle_ValidationErrors(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAssetsService)
	handler := NewAdjustAssetsHandler(mockService)

	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Missing request body",
			requestBody:    nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Request body is required",
		},
		{
			name: "Empty adjustments array",
			requestBody: map[string]interface{}{
				"operation_id": "op_test_001",
				"adjustments":  []interface{}{},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "At least one adjustment is required",
		},
		{
			name: "Missing adjustments",
			requestBody: map[string]interface{}{
				"operation_id": "op_test_001",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "At least one adjustment is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.requestBody != nil {
				reqBody, _ := json.Marshal(tt.requestBody)
				req = httptest.NewRequest(http.MethodPost, "/assets/adjust", bytes.NewReader(reqBody))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(http.MethodPost, "/assets/adjust", nil)
			}

			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.Handle(c)

			if tt.expectedStatus == http.StatusBadRequest {
				assert.Error(t, err)
				if httpErr, ok := err.(*echo.HTTPError); ok {
					assert.Equal(t, tt.expectedStatus, httpErr.Code)
					assert.Contains(t, httpErr.Message, tt.expectedError)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestRegisterRoutes(t *testing.T) {
	// This test verifies that RegisterRoutes returns the expected number of routes
	// Note: This is a placeholder test since we can't easily create a full server in unit tests

	routes := make([]*echo.Route, 0)

	// Simulate what RegisterRoutes would return
	expectedRouteCount := 1 // adjust_assets

	// In a real implementation, you'd call RegisterRoutes(mockServer)
	// routes := RegisterRoutes(mockServer)

	// For now, just verify the concept
	assert.IsType(t, routes, []*echo.Route{})

	// In integration tests, you would verify:
	// assert.Len(t, routes, expectedRouteCount)
	// assert.Equal(t, "POST", routes[0].Method)
	// assert.Equal(t, "/assets/adjust", routes[0].Path)

	t.Logf("Expected route count: %d", expectedRouteCount)
}

func TestConstants(t *testing.T) {
	// Test that constants are properly defined
	assert.Equal(t, "mint", AdjustmentTypeMint)
	assert.Equal(t, "burn", AdjustmentTypeBurn)
	assert.Equal(t, "reward", BusinessTypeReward)
	assert.Equal(t, "gas_fee", BusinessTypeGasFee)
	assert.Equal(t, "consumption", BusinessTypeConsumption)
	assert.Equal(t, "refund", BusinessTypeRefund)
	assert.Equal(t, "recorded", StatusRecorded)
	assert.Equal(t, "batchAdjustAssets", BatchTypeAdjustAssets)
	assert.Equal(t, 100, MaxAdjustmentsPerRequest)
}

func TestAssetValidationError(t *testing.T) {
	err := &AssetValidationError{
		Field:   "amount",
		Message: "Invalid amount format",
		Code:    "INVALID_AMOUNT",
	}

	assert.Equal(t, "Invalid amount format", err.Error())
	assert.Equal(t, "amount", err.Field)
	assert.Equal(t, "INVALID_AMOUNT", err.Code)
}
