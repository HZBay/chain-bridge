package transfer

import (
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/transfer"
	"github.com/labstack/echo/v4"
)

// Handler handles transfer-related HTTP requests
// Deprecated: Use specific handlers in transfer_assets.go and get_user_transactions.go
type Handler struct {
	transferService transfer.Service
}

// NewHandler creates a new transfer handler
// Deprecated: Use NewTransferAssetsHandler or NewGetUserTransactionsHandler
func NewHandler(transferService transfer.Service) *Handler {
	return &Handler{
		transferService: transferService,
	}
}

// TransferAssets handles POST /transfer requests
// Deprecated: Use TransferAssetsHandler.Handle instead
func (h *Handler) TransferAssets(c echo.Context) error {
	// Delegate to the new handler
	handler := NewTransferAssetsHandler(h.transferService)
	return handler.Handle(c)
}

// GetUserTransactions handles GET /users/{user_id}/transactions requests
// Deprecated: Use GetUserTransactionsHandler.Handle instead
func (h *Handler) GetUserTransactions(c echo.Context) error {
	// Delegate to the new handler
	handler := NewGetUserTransactionsHandler(h.transferService)
	return handler.Handle(c)
}

// Legacy route functions for backward compatibility
// These functions are maintained for existing code that depends on them

// LegacyTransferAssetsRoute creates the route for asset transfers
// Deprecated: Use RegisterRoutes() instead
func LegacyTransferAssetsRoute(s *api.Server) *echo.Route {
	handler := NewHandler(createTransferService(s))
	return s.Router.Management.POST("/transfer", handler.TransferAssets)
}

// LegacyGetUserTransactionsRoute creates the route for getting user transactions
// Deprecated: Use RegisterRoutes() instead
func LegacyGetUserTransactionsRoute(s *api.Server) *echo.Route {
	handler := NewHandler(createTransferService(s))
	return s.Router.Management.GET("/users/:user_id/transactions", handler.GetUserTransactions)
}
