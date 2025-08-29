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

// GetTokensByChainRoute creates the route for getting tokens by chain
func GetTokensByChainRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.TokensService)
	return s.Router.Management.GET("/tokens/chain/:chain_id", handler.GetTokensByChain)
}

// GetTokensByChain handles GET /tokens/chain/{chain_id} requests
func (h *Handler) GetTokensByChain(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse chain ID from path
	chainIDStr := c.Param("chain_id")
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid chain_id parameter")
	}

	// Parse query parameters
	filters := tokens.TokenFilters{
		ChainID: &chainID,
	}

	if enabledOnly := c.QueryParam("enabled_only"); enabledOnly == "true" {
		filters.EnabledOnly = true
	}

	if batchSupportOnly := c.QueryParam("batch_support_only"); batchSupportOnly == "true" {
		filters.BatchSupportOnly = true
	}

	// Get tokens for the chain
	tokenConfigs, err := h.tokensService.GetTokensByChain(ctx, chainID, filters)
	if err != nil {
		// Check if chain exists by checking if we get no tokens vs error
		if len(tokenConfigs) == 0 && err == nil {
			// This might be a non-existent chain, but let's return empty list
			response := types.TokensResponse([]*types.TokenResponse{})
			return util.ValidateAndReturn(c, http.StatusOK, response)
		}
		log.Error().Err(err).Int64("chain_id", chainID).Msg("Failed to get chain tokens")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve chain tokens")
	}

	log.Info().
		Int("tokens_count", len(tokenConfigs)).
		Int64("chain_id", chainID).
		Interface("filters", filters).
		Msg("Retrieved chain tokens")

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

	response := types.TokensResponse(tokens)

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
