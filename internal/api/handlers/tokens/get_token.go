package tokens

import (
	"net/http"
	"strconv"

	"github.com/go-openapi/strfmt"
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// GetTokenRoute creates the route for getting a specific token
func GetTokenRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.TokensService)
	return s.Router.Management.GET("/tokens/:token_id", handler.GetToken)
}

// GetToken handles GET /tokens/{token_id} requests
func (h *Handler) GetToken(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse token ID from path
	tokenIDStr := c.Param("token_id")
	tokenID, err := strconv.Atoi(tokenIDStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid token_id parameter")
	}

	// Get token configuration
	tokenConfig, err := h.tokensService.GetToken(ctx, tokenID)
	if err != nil {
		if err.Error() == "token with ID "+tokenIDStr+" not found" {
			return echo.NewHTTPError(http.StatusNotFound, "Token not found")
		}
		log.Error().Err(err).Int("token_id", tokenID).Msg("Failed to get token")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve token")
	}

	log.Info().
		Int("token_id", tokenID).
		Str("symbol", tokenConfig.Symbol).
		Int64("chain_id", tokenConfig.ChainID).
		Msg("Retrieved token configuration")

	// Convert to generated types
	response := &types.TokenResponse{
		ID:                      int64(tokenConfig.ID),
		ChainID:                 tokenConfig.ChainID,
		Symbol:                  tokenConfig.Symbol,
		Name:                    tokenConfig.Name,
		Decimals:                int64(tokenConfig.Decimals),
		TokenType:               tokenConfig.TokenType,
		SupportsBatchOperations: tokenConfig.SupportsBatchOperations,
		IsEnabled:               tokenConfig.IsEnabled,
		CreatedAt:               strfmt.DateTime(tokenConfig.CreatedAt),
	}

	if tokenConfig.ContractAddress != nil {
		response.ContractAddress = *tokenConfig.ContractAddress
	}

	if tokenConfig.BatchOperations != nil {
		response.BatchOperations = tokenConfig.BatchOperations
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
