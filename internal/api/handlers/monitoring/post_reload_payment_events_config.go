package monitoring

import (
	"net/http"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// HandleReloadPaymentEventsConfig reloads payment events configuration
func (h *Handler) HandleReloadPaymentEventsConfig(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	log.Info().Msg("Reloading payment events configuration")

	// Check if payment event service is available
	if h.paymentEventService == nil {
		log.Error().Msg("Payment event service is not available")
		return util.ValidateAndReturn(c, http.StatusInternalServerError, &types.PaymentEventsReloadResponse{
			Success:           boolPtr(false),
			Message:           stringPtr("Payment event service is not available"),
			ReloadedListeners: int64Ptr(0),
			Errors:            []string{"Payment event service is not initialized"},
			Timestamp:         strfmt.DateTime(time.Now()),
		})
	}

	// Get current listener count before reload
	currentStats := h.paymentEventService.GetGlobalStats()
	originalListenerCount := currentStats.TotalListeners

	// Attempt to reload the configuration
	err := h.paymentEventService.ReloadConfig(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to reload payment events configuration")
		return util.ValidateAndReturn(c, http.StatusInternalServerError, &types.PaymentEventsReloadResponse{
			Success:           boolPtr(false),
			Message:           stringPtr("Failed to reload payment events configuration"),
			ReloadedListeners: int64Ptr(0),
			Errors:            []string{err.Error()},
			Timestamp:         strfmt.DateTime(time.Now()),
		})
	}

	// Get new stats after reload
	newStats := h.paymentEventService.GetGlobalStats()
	reloadedCount := newStats.TotalListeners

	log.Info().
		Int64("original_listeners", originalListenerCount).
		Int64("reloaded_listeners", reloadedCount).
		Msg("Payment events configuration reloaded successfully")

	// Build successful response
	response := &types.PaymentEventsReloadResponse{
		Success:           boolPtr(true),
		Message:           stringPtr("Payment events configuration reloaded successfully"),
		ReloadedListeners: int64Ptr(reloadedCount),
		Errors:            []string{}, // Empty array for no errors
		Timestamp:         strfmt.DateTime(time.Now()),
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}

// Helper functions to create pointers
func boolPtr(v bool) *bool {
	return &v
}

func stringPtr(v string) *string {
	return &v
}
