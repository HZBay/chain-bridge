package chains

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/types/cpop"
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

	// Parse and validate path parameters using swagger-generated method
	params := cpop.NewGetChainConfigParams()
	if err := params.BindRequest(c.Request(), nil); err != nil {
		log.Debug().Err(err).Msg("Invalid path parameters")
		return err
	}

	chainID := params.ChainID

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

	// TODO: 把类型的转换放到service中处理好一些
	// Convert to generated type
	chainData := &types.ChainResponseData{
		ChainID:      chainConfig.ChainID,
		Name:         chainConfig.Name,
		RPCURL:       chainConfig.RPCURL,
		IsEnabled:    chainConfig.IsEnabled,
		BatchSize:    int64(chainConfig.BatchConfig.OptimalBatchSize),
		BatchTimeout: 300, // Default 5 minutes
	}

	// Use proper response type
	response := &types.ChainResponse{
		Data: chainData,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
