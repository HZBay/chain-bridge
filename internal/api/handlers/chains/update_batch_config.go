package chains

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/chains"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/types/cpop"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// PutChainBatchConfigRoute creates the route for updating batch configuration
func PutChainBatchConfigRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.ChainsService)
	return s.Router.Management.PUT("/chains/:chain_id/batch-config", handler.UpdateBatchConfig)
}

// UpdateBatchConfig handles PUT /chains/{chain_id}/batch-config requests
func (h *Handler) UpdateBatchConfig(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate parameters (path + query) using swagger-generated method
	params := cpop.NewUpdateChainBatchConfigParams()
	if err := params.BindRequest(c.Request(), nil); err != nil {
		log.Debug().Err(err).Msg("Failed to bind request parameters")
		return err
	}

	// Parse and validate request body
	var batchConfig types.BatchConfig
	if err := util.BindAndValidateBody(c, &batchConfig); err != nil {
		log.Debug().Err(err).Msg("Failed to bind and validate batch config request")
		return err
	}

	// Convert to service type
	serviceBatchConfig := &chains.BatchConfig{
		MinBatchSize:     int(*batchConfig.MinBatchSize),
		MaxBatchSize:     int(*batchConfig.MaxBatchSize),
		OptimalBatchSize: int(*batchConfig.OptimalBatchSize),
	}

	// Update batch configuration
	if err := h.chainsService.UpdateBatchConfig(ctx, params.ChainID, serviceBatchConfig); err != nil {
		log.Error().Err(err).Int64("chain_id", params.ChainID).Msg("Failed to update batch config")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update batch configuration")
	}

	log.Info().
		Int64("chain_id", params.ChainID).
		Int("optimal_batch_size", int(*batchConfig.OptimalBatchSize)).
		Int("max_batch_size", int(*batchConfig.MaxBatchSize)).
		Int("min_batch_size", int(*batchConfig.MinBatchSize)).
		Msg("Updated batch configuration")

	// Use proper response type
	message := "Batch configuration updated successfully"
	response := &types.BatchConfigUpdateResponse{
		Message: &message,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
