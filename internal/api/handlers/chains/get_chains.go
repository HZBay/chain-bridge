package chains

import (
	"net/http"

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

	// TODO: 把类型的转换放到service中处理好一些
	// Convert to generated types
	chains := make([]*types.ChainsResponseDataItems0, len(chainConfigs))
	for i, config := range chainConfigs {
		chainData := &types.ChainsResponseDataItems0{
			ChainID:      config.ChainID,
			Name:         config.Name,
			RPCURL:       config.RPCURL,
			IsEnabled:    config.IsEnabled,
			BatchSize:    int64(config.BatchConfig.OptimalBatchSize),
			BatchTimeout: 300, // Default 5 minutes
		}
		chains[i] = chainData
	}

	// Use proper response type
	response := &types.ChainsResponse{
		Data: chains,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
