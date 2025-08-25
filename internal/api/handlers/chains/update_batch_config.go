package chains

import (
	"fmt"
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

	// Parse chain ID parameter
	chainIDStr := c.Param("chain_id")
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
		log.Debug().Str("chain_id", chainIDStr).Msg("Invalid chain_id parameter")
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid chain_id parameter")
	}

	// Parse request body
	var batchConfig chains.BatchConfig
	if err := c.Bind(&batchConfig); err != nil {
		log.Debug().Err(err).Msg("Failed to bind batch config request")
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate batch configuration
	if err := validateBatchConfig(&batchConfig); err != nil {
		log.Debug().Err(err).Msg("Invalid batch configuration")
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Update batch configuration
	if err := h.chainsService.UpdateBatchConfig(ctx, chainID, &batchConfig); err != nil {
		log.Error().Err(err).Int64("chain_id", chainID).Msg("Failed to update batch config")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update batch configuration")
	}

	log.Info().
		Int64("chain_id", chainID).
		Int("optimal_batch_size", batchConfig.OptimalBatchSize).
		Int("max_batch_size", batchConfig.MaxBatchSize).
		Int("min_batch_size", batchConfig.MinBatchSize).
		Msg("Updated batch configuration")

	// Convert to generated type
	minBatchSize := int64(batchConfig.MinBatchSize)
	maxBatchSize := int64(batchConfig.MaxBatchSize)
	optimalBatchSize := int64(batchConfig.OptimalBatchSize)

	responseBatchConfig := &types.BatchConfig{
		MinBatchSize:     &minBatchSize,
		MaxBatchSize:     &maxBatchSize,
		OptimalBatchSize: &optimalBatchSize,
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Batch configuration updated successfully",
		"data":    responseBatchConfig,
	})
}

// validateBatchConfig validates batch configuration parameters
func validateBatchConfig(config *chains.BatchConfig) error {
	if config.MinBatchSize <= 0 || config.MinBatchSize > 100 {
		return fmt.Errorf("min_batch_size must be between 1 and 100")
	}
	if config.MaxBatchSize <= 0 || config.MaxBatchSize > 100 {
		return fmt.Errorf("max_batch_size must be between 1 and 100")
	}
	if config.OptimalBatchSize <= 0 || config.OptimalBatchSize > 100 {
		return fmt.Errorf("optimal_batch_size must be between 1 and 100")
	}
	if config.MinBatchSize > config.MaxBatchSize {
		return fmt.Errorf("min_batch_size cannot be greater than max_batch_size")
	}
	if config.OptimalBatchSize < config.MinBatchSize || config.OptimalBatchSize > config.MaxBatchSize {
		return fmt.Errorf("optimal_batch_size must be between min_batch_size and max_batch_size")
	}
	return nil
}
