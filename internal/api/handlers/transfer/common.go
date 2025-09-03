package transfer

import (
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/transfer"
	"github.com/labstack/echo/v4"
)

// RegisterRoutes registers all transfer-related routes
func RegisterRoutes(s *api.Server) []*echo.Route {
	var routes []*echo.Route

	// Register transfer assets route
	routes = append(routes, PostTransferAssetsRoute(s))

	// Register get user transactions route
	routes = append(routes, GetUserTransactionsRoute(s))

	return routes
}

// createTransferService creates a new transfer service with all dependencies
func createTransferService(s *api.Server) transfer.Service {
	return transfer.NewService(s.DB, s.BatchProcessor, s.BatchOptimizer)
}

// Constants for transfer operations
const (
	// Transfer statuses
	StatusRecorded  = "recorded"
	StatusBatching  = "batching"
	StatusSubmitted = "submitted"
	StatusConfirmed = "confirmed"
	StatusFailed    = "failed"

	// Transfer types
	TypeTransfer = "transfer"

	// Business types
	BusinessTypeTransfer = "transfer"

	// Reason types
	ReasonTypeUserTransfer = "user_transfer"

	// Transfer directions
	DirectionOutgoing = "outgoing"
	DirectionIncoming = "incoming"

	// Default pagination
	DefaultPage  = 1
	DefaultLimit = 20
	MaxLimit     = 100
)

// ValidationError represents validation errors for transfer operations
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// Error implements the error interface
func (e *ValidationError) Error() string {
	return e.Message
}
