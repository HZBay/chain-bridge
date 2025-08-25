package chains

import (
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// GetChainsRoute creates the route for getting all chains
func GetChainsRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.ChainsService)
	return s.Router.Management.GET("/chains", handler.GetChains)
}

// GetChains handles GET /chains requests
func (h *Handler) GetChains(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Get all enabled chains
	chainConfigs, err := h.chainsService.GetAllEnabledChains(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get chains")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve chains")
	}

	log.Info().
		Int("chains_count", len(chainConfigs)).
		Msg("Retrieved chains configuration")

	// Convert to generated types
	chains := make([]*types.Chain, len(chainConfigs))
	for i, config := range chainConfigs {
		minBatchSize := int64(config.BatchConfig.MinBatchSize)
		maxBatchSize := int64(config.BatchConfig.MaxBatchSize)
		optimalBatchSize := int64(config.BatchConfig.OptimalBatchSize)

		rpcURL := strfmt.URI(config.RPCURL)
		chain := &types.Chain{
			ChainID:     &config.ChainID,
			Name:        &config.Name,
			Symbol:      config.ShortName,
			RPCURL:      &rpcURL,
			ExplorerURL: strfmt.URI(config.ExplorerURL),
			IsEnabled:   &config.IsEnabled,
			BatchConfig: &types.BatchConfig{
				MinBatchSize:     &minBatchSize,
				MaxBatchSize:     &maxBatchSize,
				OptimalBatchSize: &optimalBatchSize,
			},
			CreatedAt: strfmt.DateTime(config.CreatedAt),
		}
		chains[i] = chain
	}

	// Use proper response wrapper if available, or simple JSON return
	return c.JSON(http.StatusOK, map[string]interface{}{
		"data": chains,
	})
}
