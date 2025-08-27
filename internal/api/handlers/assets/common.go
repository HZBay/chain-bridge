package assets

import (
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/labstack/echo/v4"
)

// RegisterRoutes registers all assets-related routes
func RegisterRoutes(s *api.Server) []*echo.Route {
	var routes []*echo.Route

	// Register adjust assets route
	routes = append(routes, PostAdjustAssetsRoute(s))

	return routes
}

// Constants for asset operations
const (
	// Asset adjustment types
	AdjustmentTypeMint = "mint"
	AdjustmentTypeBurn = "burn"

	// Business types
	BusinessTypeReward      = "reward"
	BusinessTypeGasFee      = "gas_fee"
	BusinessTypeConsumption = "consumption"
	BusinessTypeRefund      = "refund"

	// Asset adjustment statuses
	StatusRecorded  = "recorded"
	StatusBatching  = "batching"
	StatusSubmitted = "submitted"
	StatusConfirmed = "confirmed"
	StatusFailed    = "failed"

	// Batch operation types
	BatchTypeAdjustAssets = "batchAdjustAssets"

	// Limits
	MaxAdjustmentsPerRequest = 100
)

// AssetValidationError represents validation errors for asset operations
type AssetValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// Error implements the error interface
func (e *AssetValidationError) Error() string {
	return e.Message
}
