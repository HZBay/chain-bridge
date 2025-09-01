package monitoring

import (
	"net/http"
	"time"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// QueueHealthRoute creates the route for queue health check
func GetQueueHealthRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.QueueMonitor, s.BatchOptimizer, s.PaymentEventService)
	return s.Router.Management.GET("/monitoring/queue/health", handler.CheckQueueHealth)
}

// CheckQueueHealth handles GET /monitoring/queue/health requests
func (h *Handler) CheckQueueHealth(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	start := time.Now()
	err := h.monitor.HealthCheck(ctx)
	duration := time.Since(start)
	durationMs := duration.Milliseconds()
	timestamp := time.Now().Unix()

	if err != nil {
		log.Error().Err(err).Msg("Queue health check failed")

		healthy := false
		errorResponse := &types.QueueHealthResponse{
			Healthy:    &healthy,
			Error:      err.Error(),
			DurationMs: &durationMs,
			Timestamp:  &timestamp,
		}

		return util.ValidateAndReturn(c, http.StatusServiceUnavailable, errorResponse)
	}

	log.Info().
		Dur("duration", duration).
		Msg("Queue health check passed")

	healthy := true
	response := &types.QueueHealthResponse{
		Healthy:    &healthy,
		DurationMs: &durationMs,
		Timestamp:  &timestamp,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
