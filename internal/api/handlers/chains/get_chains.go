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

	// Convert to generated types - ChainsResponse is now an array type
	chains := make([]*types.ChainResponse, len(chainConfigs))
	for i, config := range chainConfigs {
		chainData := &types.ChainResponse{
			ChainID:                 config.ChainID,
			Name:                    config.Name,
			ShortName:               config.ShortName,
			RPCURL:                  config.RPCURL,
			ExplorerURL:             config.ExplorerURL,
			EntryPointAddress:       config.EntryPointAddress,
			CpopTokenAddress:        config.CpopTokenAddress,
			MasterAggregatorAddress: config.MasterAggregatorAddress,
			AccountManagerAddress:   config.AccountManagerAddress,
			OptimalBatchSize:        int64(config.BatchConfig.OptimalBatchSize),
			MaxBatchSize:            int64(config.BatchConfig.MaxBatchSize),
			MinBatchSize:            int64(config.BatchConfig.MinBatchSize),
			IsEnabled:               config.IsEnabled,
			CreatedAt:               strfmt.DateTime(config.CreatedAt),
		}
		chains[i] = chainData
	}

	// ChainsResponse is now just an array type
	response := types.ChainsResponse(chains)

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
