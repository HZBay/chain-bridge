package chains

import (
	"net/http"
	"strconv"

	"github.com/go-openapi/strfmt"
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// GetChainConfigRoute creates the route for getting chain configuration
func GetChainConfigRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.ChainsService)
	return s.Router.Management.GET("/chains/:chain_id", handler.GetChainConfig)
}

// GetChainConfig handles GET /chains/{chain_id} requests
func (h *Handler) GetChainConfig(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse chain ID parameter
	chainIDStr := c.Param("chain_id")
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
		log.Debug().Str("chain_id", chainIDStr).Msg("Invalid chain_id parameter")
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid chain_id parameter")
	}

	// Get chain configuration
	chainConfig, err := h.chainsService.GetChainConfig(ctx, chainID)
	if err != nil {
		log.Error().Err(err).Int64("chain_id", chainID).Msg("Failed to get chain config")
		return echo.NewHTTPError(http.StatusNotFound, "Chain not found")
	}

	log.Info().
		Int64("chain_id", chainID).
		Str("chain_name", chainConfig.Name).
		Bool("is_enabled", chainConfig.IsEnabled).
		Msg("Retrieved chain configuration")

	// Convert to generated type
	minBatchSize := int64(chainConfig.BatchConfig.MinBatchSize)
	maxBatchSize := int64(chainConfig.BatchConfig.MaxBatchSize)
	optimalBatchSize := int64(chainConfig.BatchConfig.OptimalBatchSize)

	rpcURL := strfmt.URI(chainConfig.RPCURL)
	chain := &types.Chain{
		ChainID:     &chainConfig.ChainID,
		Name:        &chainConfig.Name,
		Symbol:      chainConfig.ShortName,
		RPCURL:      &rpcURL,
		ExplorerURL: strfmt.URI(chainConfig.ExplorerURL),
		IsEnabled:   &chainConfig.IsEnabled,
		BatchConfig: &types.BatchConfig{
			MinBatchSize:     &minBatchSize,
			MaxBatchSize:     &maxBatchSize,
			OptimalBatchSize: &optimalBatchSize,
		},
		CreatedAt: strfmt.DateTime(chainConfig.CreatedAt),
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"data": chain,
	})
}
