package tokens

import (
	"net/http"
	"strconv"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/tokens"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// PutTokenRoute creates the route for updating a token
func PutTokenRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.TokensService)
	return s.Router.Management.PUT("/tokens/:token_id", handler.UpdateToken)
}

// UpdateToken handles PUT /tokens/{token_id} requests
func (h *Handler) UpdateToken(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse token ID from path
	tokenIDStr := c.Param("token_id")
	tokenID, err := strconv.Atoi(tokenIDStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid token_id parameter")
	}

	// Bind and validate request body
	var request types.UpdateTokenRequest
	if err := util.BindAndValidateBody(c, &request); err != nil {
		return err
	}

	log.Info().
		Int("token_id", tokenID).
		Msg("Updating token configuration")

	// Convert to service request
	serviceRequest := &tokens.UpdateTokenRequest{}

	if request.Name != "" {
		serviceRequest.Name = &request.Name
	}

	serviceRequest.SupportsBatchOperations = &request.SupportsBatchOperations

	if request.BatchOperations != nil {
		serviceRequest.BatchOperations = request.BatchOperations.(map[string]interface{})
	}

	serviceRequest.IsEnabled = &request.IsEnabled

	// Update token
	err = h.tokensService.UpdateToken(ctx, tokenID, serviceRequest)
	if err != nil {
		if err.Error() == "token with ID "+tokenIDStr+" not found" {
			return echo.NewHTTPError(http.StatusNotFound, "Token not found")
		}
		log.Error().Err(err).Int("token_id", tokenID).Msg("Failed to update token")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update token")
	}

	log.Info().
		Int("token_id", tokenID).
		Msg("Token updated successfully")

	message := "Token configuration updated successfully"
	response := &types.TokenConfigUpdateResponse{
		Message: &message,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
