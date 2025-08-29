package tokens

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// PostTokensRefreshCacheRoute creates the route for refreshing tokens cache
func PostTokensRefreshCacheRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.TokensService)
	return s.Router.Management.POST("/tokens/refresh-cache", handler.RefreshCache)
}

// RefreshCache handles POST /tokens/refresh-cache requests
func (h *Handler) RefreshCache(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	log.Info().Msg("Refreshing tokens cache")

	// Refresh cache
	err := h.tokensService.RefreshCache(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to refresh tokens cache")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to refresh cache")
	}

	log.Info().Msg("Tokens cache refreshed successfully")

	// Create properly typed response
	type CacheRefreshResponse struct {
		Message string `json:"message"`
	}

	response := &CacheRefreshResponse{
		Message: "Tokens cache refreshed successfully",
	}

	return c.JSON(http.StatusOK, response)
}
