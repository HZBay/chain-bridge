package monitoring

import (
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/labstack/echo/v4"
)

// PostReloadPaymentEventsConfigRoute creates the route for reloading payment events configuration
func PostReloadPaymentEventsConfigRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.QueueMonitor, s.BatchOptimizer, s.PaymentEventService)
	return s.Router.Management.POST("/monitoring/payment-events/reload", handler.HandleReloadPaymentEventsConfig)
}
