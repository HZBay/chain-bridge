package chains

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// PostChainsRefreshCacheRoute creates the route for refreshing chains cache
func PostChainsRefreshCacheRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.ChainsService)
	return s.Router.Management.POST("/chains/refresh-cache", handler.RefreshCache)
}

// RefreshCache handles POST /chains/refresh-cache requests
func (h *Handler) RefreshCache(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Refresh chains cache
	if err := h.chainsService.RefreshCache(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to refresh chains cache")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to refresh cache")
	}

	log.Info().Msg("Chains cache refreshed successfully")

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Cache refreshed successfully",
	})
}
