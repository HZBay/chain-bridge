package tokens

import (
	"net/http"
	"strconv"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// DeleteTokenRoute creates the route for deleting a token
func DeleteTokenRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.TokensService)
	return s.Router.Management.DELETE("/tokens/:token_id", handler.DeleteToken)
}

// DeleteToken handles DELETE /tokens/{token_id} requests
func (h *Handler) DeleteToken(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse token ID from path
	tokenIDStr := c.Param("token_id")
	tokenID, err := strconv.Atoi(tokenIDStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid token_id parameter")
	}

	log.Info().
		Int("token_id", tokenID).
		Msg("Deleting token")

	// Delete token
	err = h.tokensService.DeleteToken(ctx, tokenID)
	if err != nil {
		if err.Error() == "token with ID "+tokenIDStr+" not found" {
			return echo.NewHTTPError(http.StatusNotFound, "Token not found")
		}
		if containsSubstring(err.Error(), "is in use and cannot be deleted") {
			return echo.NewHTTPError(http.StatusConflict, "Token is in use and cannot be deleted")
		}
		log.Error().Err(err).Int("token_id", tokenID).Msg("Failed to delete token")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete token")
	}

	log.Info().
		Int("token_id", tokenID).
		Msg("Token deleted successfully")

	message := "Token deleted successfully"
	response := &types.TokenDeleteResponse{
		Message: &message,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
