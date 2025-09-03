package chains

import (
	"net/http"
	"strconv"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/chains"
	"github.com/hzbay/chain-bridge/internal/types"
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

	// Parse chain ID from path
	chainIDStr := c.Param("chain_id")
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid chain_id parameter")
	}

	// Parse parameters from query
	batchSizeStr := c.QueryParam("batch_size")
	batchTimeoutStr := c.QueryParam("batch_timeout")

	if batchSizeStr == "" || batchTimeoutStr == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Missing required parameters: batch_size and batch_timeout")
	}

	batchSize, err := strconv.Atoi(batchSizeStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid batch_size parameter")
	}

	batchTimeout, err := strconv.Atoi(batchTimeoutStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid batch_timeout parameter")
	}

	// Convert to service type
	serviceBatchConfig := &chains.BatchConfig{
		OptimalBatchSize: batchSize,
		MaxBatchSize:     batchSize + 15, // Set max to be larger than optimal
		MinBatchSize:     batchSize - 15, // Set min to be smaller than optimal
	}

	// Update batch configuration
	if err := h.chainsService.UpdateBatchConfig(ctx, chainID, serviceBatchConfig); err != nil {
		log.Error().Err(err).Int64("chain_id", chainID).Msg("Failed to update batch config")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update batch configuration")
	}

	log.Info().
		Int64("chain_id", chainID).
		Int("optimal_batch_size", batchSize).
		Int("batch_timeout", batchTimeout).
		Msg("Updated batch configuration")

	// Use proper response type
	message := "Batch configuration updated successfully"
	response := &types.BatchConfigUpdateResponse{
		Message: &message,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
