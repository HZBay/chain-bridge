package tokens

import (
	"net/http"
	"strconv"

	"github.com/go-openapi/strfmt"
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/tokens"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// GetTokensRoute creates the route for getting all tokens
func GetTokensRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.TokensService)
	return s.Router.Management.GET("/tokens", handler.GetTokens)
}

// GetTokens handles GET /tokens requests
func (h *Handler) GetTokens(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse query parameters
	filters := tokens.TokenFilters{}

	if chainIDStr := c.QueryParam("chain_id"); chainIDStr != "" {
		chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid chain_id parameter")
		}
		filters.ChainID = &chainID
	}

	if enabledOnly := c.QueryParam("enabled_only"); enabledOnly == "true" {
		filters.EnabledOnly = true
	}

	if batchSupportOnly := c.QueryParam("batch_support_only"); batchSupportOnly == "true" {
		filters.BatchSupportOnly = true
	}

	// Get tokens with filters
	tokenConfigs, err := h.tokensService.GetAllTokens(ctx, filters)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get tokens")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve tokens")
	}

	log.Info().
		Int("tokens_count", len(tokenConfigs)).
		Interface("filters", filters).
		Msg("Retrieved tokens configuration")

	// Convert to generated types
	tokens := make([]*types.TokenResponse, len(tokenConfigs))
	for i, config := range tokenConfigs {
		tokenData := &types.TokenResponse{
			ID:                      int64(config.ID),
			ChainID:                 config.ChainID,
			Symbol:                  config.Symbol,
			Name:                    config.Name,
			Decimals:                int64(config.Decimals),
			TokenType:               config.TokenType,
			SupportsBatchOperations: config.SupportsBatchOperations,
			IsEnabled:               config.IsEnabled,
			CreatedAt:               strfmt.DateTime(config.CreatedAt),
		}

		if config.ContractAddress != nil {
			tokenData.ContractAddress = *config.ContractAddress
		}

		if config.BatchOperations != nil {
			tokenData.BatchOperations = config.BatchOperations
		}

		tokens[i] = tokenData
	}

	// TokensResponse is an array type
	response := types.TokensResponse(tokens)

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
