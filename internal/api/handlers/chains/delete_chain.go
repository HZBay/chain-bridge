package chains

import (
	"net/http"
	"strconv"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// DeleteChainRoute creates the route for deleting a chain
func DeleteChainRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.ChainsService)
	return s.Router.Management.DELETE("/chains/:chain_id", handler.DeleteChain)
}

// DeleteChain handles DELETE /chains/{chain_id} requests
func (h *Handler) DeleteChain(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse chain ID from path
	chainIDStr := c.Param("chain_id")
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid chain_id parameter")
	}

	log.Info().
		Int64("chain_id", chainID).
		Msg("Deleting chain")

	// Delete chain
	err = h.chainsService.DeleteChain(ctx, chainID)
	if err != nil {
		if containsSubstring(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Chain not found")
		}
		if containsSubstring(err.Error(), "is in use and cannot be deleted") {
			return echo.NewHTTPError(http.StatusConflict, "Chain is in use and cannot be deleted")
		}
		log.Error().Err(err).Int64("chain_id", chainID).Msg("Failed to delete chain")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete chain")
	}

	log.Info().
		Int64("chain_id", chainID).
		Msg("Chain deleted successfully")

	message := "Chain deleted successfully"
	response := &types.ChainDeleteResponse{
		Message: &message,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
