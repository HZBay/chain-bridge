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

	// Parse chain ID from path
	chainIDStr := c.Param("chain_id")
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
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

	// Use proper response type
	response := &types.ChainResponse{
		ChainID:                 chainConfig.ChainID,
		Name:                    chainConfig.Name,
		ShortName:               chainConfig.ShortName,
		RPCURL:                  chainConfig.RPCURL,
		ExplorerURL:             chainConfig.ExplorerURL,
		EntryPointAddress:       chainConfig.EntryPointAddress,
		CpopTokenAddress:        chainConfig.CpopTokenAddress,
		MasterAggregatorAddress: chainConfig.MasterAggregatorAddress,
		AccountManagerAddress:   chainConfig.AccountManagerAddress,
		OptimalBatchSize:        int64(chainConfig.BatchConfig.OptimalBatchSize),
		MaxBatchSize:            int64(chainConfig.BatchConfig.MaxBatchSize),
		MinBatchSize:            int64(chainConfig.BatchConfig.MinBatchSize),
		IsEnabled:               chainConfig.IsEnabled,
		CreatedAt:               strfmt.DateTime(chainConfig.CreatedAt),
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
