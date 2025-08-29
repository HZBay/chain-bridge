package monitoring

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// QueueMetricsRoute creates the route for getting queue metrics
func GetQueueMetricsRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.QueueMonitor, s.BatchOptimizer)
	return s.Router.Management.GET("/monitoring/queue/metrics", handler.GetQueueMetrics)
}

// GetQueueMetrics handles GET /monitoring/queue/metrics requests
func (h *Handler) GetQueueMetrics(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	rawMetrics := h.monitor.GetMetrics()

	// Convert to generated type
	connectionStatus := rawMetrics.ConnectionStatus
	processorType := rawMetrics.ProcessorType
	queueDepth := rawMetrics.QueueDepth
	totalProcessed := rawMetrics.TotalJobsProcessed

	log.Debug().
		Int64("queue_depth", queueDepth).
		Int64("total_processed", totalProcessed).
		Str("connection_status", connectionStatus).
		Msg("Retrieved queue metrics")

	avgProcessingTime := int64(rawMetrics.AverageProcessingTime)

	metrics := &types.QueueMetrics{
		QueueDepth:              &queueDepth,
		TotalJobsProcessed:      &totalProcessed,
		ConnectionStatus:        &connectionStatus,
		ProcessorType:           &processorType,
		AverageProcessingTimeMs: avgProcessingTime,
	}

	// QueueMetricsResponse is now just an embedded QueueMetrics
	response := &types.QueueMetricsResponse{
		QueueMetrics: *metrics,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
