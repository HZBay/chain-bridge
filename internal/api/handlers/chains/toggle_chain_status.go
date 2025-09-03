package chains

import (
	"net/http"
	"strconv"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// PutToggleChainStatusRoute creates the route for toggling chain status
func PutToggleChainStatusRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.ChainsService)
	return s.Router.Management.PUT("/chains/:chain_id/toggle-status", handler.ToggleChainStatus)
}

// ToggleChainStatus handles PUT /chains/{chain_id}/toggle-status requests
func (h *Handler) ToggleChainStatus(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse chain ID from path
	chainIDStr := c.Param("chain_id")
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid chain_id parameter")
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
		Int64("chain_id", chainID).
		Bool("enabled", enabled).
		Msg("Toggling chain status")

	// Toggle chain status
	err = h.chainsService.ToggleChainStatus(ctx, chainID, enabled)
	if err != nil {
		if containsSubstring(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Chain not found")
		}
		log.Error().Err(err).Int64("chain_id", chainID).Msg("Failed to toggle chain status")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update chain status")
	}

	statusMsg := "disabled"
	if enabled {
		statusMsg = "enabled"
	}

	log.Info().
		Int64("chain_id", chainID).
		Bool("enabled", enabled).
		Msg("Chain status updated successfully")

	message := "Chain " + statusMsg + " successfully"
	response := &types.ChainToggleStatusResponse{
		Message: &message,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
