package tokens

import (
	"net/http"
	"strconv"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// PutTokenStatusRoute creates the route for toggling token status
func PutTokenStatusRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.TokensService)
	return s.Router.Management.PUT("/tokens/:token_id/toggle-status", handler.ToggleTokenStatus)
}

// ToggleTokenStatus handles PUT /tokens/{token_id}/toggle-status requests
func (h *Handler) ToggleTokenStatus(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse token ID from path
	tokenIDStr := c.Param("token_id")
	tokenID, err := strconv.Atoi(tokenIDStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid token_id parameter")
	}

	// Parse enabled parameter from query
	enabledStr := c.QueryParam("enabled")
	if enabledStr == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Missing required 'enabled' parameter")
	}

	enabled, err := strconv.ParseBool(enabledStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid 'enabled' parameter, must be true or false")
	}

	log.Info().
		Int("token_id", tokenID).
		Bool("enabled", enabled).
		Msg("Toggling token status")

	// Toggle token status
	err = h.tokensService.ToggleTokenStatus(ctx, tokenID, enabled)
	if err != nil {
		if err.Error() == "token with ID "+tokenIDStr+" not found" {
			return echo.NewHTTPError(http.StatusNotFound, "Token not found")
		}
		log.Error().Err(err).Int("token_id", tokenID).Msg("Failed to toggle token status")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update token status")
	}

	statusMsg := "disabled"
	if enabled {
		statusMsg = "enabled"
	}

	log.Info().
		Int("token_id", tokenID).
		Bool("enabled", enabled).
		Msg("Token status updated successfully")

	message := "Token " + statusMsg + " successfully"
	response := &types.TokenConfigUpdateResponse{
		Message: &message,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
