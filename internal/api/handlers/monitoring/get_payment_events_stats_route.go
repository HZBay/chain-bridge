package monitoring

import (
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/labstack/echo/v4"
)

// GetPaymentEventsStatsRoute creates the route for getting payment events statistics
func GetPaymentEventsStatsRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.QueueMonitor, s.BatchOptimizer, s.PaymentEventService)
	return s.Router.Management.GET("/monitoring/payment-events/stats", handler.HandleGetPaymentEventsStats)
}
