package monitoring

import (
	"net/http"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// HandleGetPaymentEventsStats retrieves payment events statistics
func (h *Handler) HandleGetPaymentEventsStats(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	log.Info().Msg("Getting payment events statistics")

	// Check if payment event service is available
	if h.paymentEventService == nil {
		log.Warn().Msg("Payment event service is not available")
		return util.ValidateAndReturn(c, http.StatusOK, &types.PaymentEventsStatsResponse{
			TotalListeners:       int64Ptr(0),
			HealthyListeners:     int64Ptr(0),
			TotalEventsProcessed: int64Ptr(0),
			TotalErrors:          int64Ptr(0),
			Listeners:            make(map[string]types.PaymentEventListenerStats),
		})
	}

	// Check if service is started
	if !h.paymentEventService.IsStarted() {
		log.Warn().Msg("Payment event service is not started")
		return util.ValidateAndReturn(c, http.StatusOK, &types.PaymentEventsStatsResponse{
			TotalListeners:       int64Ptr(0),
			HealthyListeners:     int64Ptr(0),
			TotalEventsProcessed: int64Ptr(0),
			TotalErrors:          int64Ptr(0),
			Listeners:            make(map[string]types.PaymentEventListenerStats),
		})
	}

	// Get global statistics from the payment event service
	globalStats := h.paymentEventService.GetGlobalStats()

	// Get individual listener statistics
	listenerStats := h.paymentEventService.GetAllListenerStats()

	// Convert listener stats to the typed response format
	listeners := make(map[string]types.PaymentEventListenerStats)
	for chainIDStr, stats := range listenerStats {
		if statsMap, ok := stats.(map[string]interface{}); ok {
			// Convert each listener stat to the proper type
			listenerStat := types.PaymentEventListenerStats{
				ChainID:             getInt64FromMap(statsMap, "chain_id"),
				PaymentAddress:      getStringFromMap(statsMap, "payment_address"),
				LastProcessedBlock:  getInt64FromMap(statsMap, "last_processed_block"),
				TotalEvents:         getInt64FromMap(statsMap, "total_events"),
				TotalErrors:         getInt64FromMap(statsMap, "total_errors"),
				Status:              getStringFromMapOrDefault(statsMap, "status", "unknown"),
				ProcessingLatencyMs: getInt64FromMapOrDefault(statsMap, "processing_latency_ms", 0),
			}

			// Handle optional last_event_time
			if lastEventTimeStr, exists := statsMap["last_event_time"]; exists {
				if timeStr, ok := lastEventTimeStr.(string); ok && timeStr != "" {
					if parsedTime, err := time.Parse(time.RFC3339, timeStr); err == nil {
						listenerStat.LastEventTime = strfmt.DateTime(parsedTime)
					}
				}
			}

			listeners[chainIDStr] = listenerStat
		}
	}

	// Build response
	response := &types.PaymentEventsStatsResponse{
		TotalListeners:       int64Ptr(globalStats.TotalListeners),
		HealthyListeners:     int64Ptr(globalStats.HealthyListeners),
		TotalEventsProcessed: int64Ptr(globalStats.TotalEventsProcessed),
		TotalErrors:          int64Ptr(globalStats.TotalErrors),
		Listeners:            listeners,
	}

	log.Info().
		Int64("total_listeners", globalStats.TotalListeners).
		Int64("healthy_listeners", globalStats.HealthyListeners).
		Int64("total_events", globalStats.TotalEventsProcessed).
		Int64("total_errors", globalStats.TotalErrors).
		Msg("Payment events statistics retrieved successfully")

	return util.ValidateAndReturn(c, http.StatusOK, response)
}

// Helper function to create int64 pointer
func int64Ptr(v int64) *int64 {
	return &v
}

// Helper functions to safely extract values from map
func getInt64FromMap(m map[string]interface{}, key string) *int64 {
	if val, exists := m[key]; exists {
		switch v := val.(type) {
		case int64:
			return &v
		case int:
			val64 := int64(v)
			return &val64
		case float64:
			val64 := int64(v)
			return &val64
		}
	}
	return nil
}

func getStringFromMap(m map[string]interface{}, key string) *string {
	if val, exists := m[key]; exists {
		if str, ok := val.(string); ok {
			return &str
		}
	}
	return nil
}

func getStringFromMapOrDefault(m map[string]interface{}, key string, defaultVal string) string {
	if val, exists := m[key]; exists {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultVal
}

func getInt64FromMapOrDefault(m map[string]interface{}, key string, defaultVal int64) int64 {
	if val, exists := m[key]; exists {
		switch v := val.(type) {
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		}
	}
	return defaultVal
}
