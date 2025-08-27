package monitoring

import (
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// QueueStatsRoute creates the route for getting detailed queue statistics
func GetQueueStatsRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.QueueMonitor, s.BatchOptimizer)
	return s.Router.Management.GET("/monitoring/queue/stats", handler.GetQueueStats)
}

// GetQueueStats handles GET /monitoring/queue/stats requests
func (h *Handler) GetQueueStats(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	rawStats := h.monitor.GetDetailedStats()

	log.Debug().
		Int("queue_count", len(rawStats)).
		Msg("Retrieved detailed queue statistics")

	// Convert to generated types
	stats := make([]*types.QueueStats, 0, len(rawStats))
	for queueName, rawStat := range rawStats {
		pendingJobs := int64(rawStat.PendingCount)
		processedJobs := rawStat.CompletedCount
		failedJobs := rawStat.FailedCount

		queueStat := &types.QueueStats{
			QueueName:     &queueName,
			PendingJobs:   &pendingJobs,
			ProcessedJobs: &processedJobs,
			FailedJobs:    &failedJobs,
		}

		if !rawStat.LastProcessedAt.IsZero() {
			lastProcessing := strfmt.DateTime(rawStat.LastProcessedAt)
			queueStat.LastProcessingTime = lastProcessing
		}

		stats = append(stats, queueStat)
	}

	response := &types.QueueStatsResponse{
		Data: stats,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
