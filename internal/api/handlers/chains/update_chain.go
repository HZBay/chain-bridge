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

// PutUpdateChainRoute creates the route for updating a chain
func PutUpdateChainRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.ChainsService)
	return s.Router.Management.PUT("/chains/:chain_id", handler.UpdateChain)
}

// UpdateChain handles PUT /chains/{chain_id} requests
func (h *Handler) UpdateChain(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse chain ID from path
	chainIDStr := c.Param("chain_id")
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid chain_id parameter")
	}

	// Bind and validate request body
	var request types.UpdateChainRequest
	if err := util.BindAndValidateBody(c, &request); err != nil {
		return err
	}

	log.Info().
		Int64("chain_id", chainID).
		Msg("Updating chain configuration")

	// Convert to service request
	serviceRequest := &chains.UpdateChainRequest{}

	// Set fields if provided
	if request.Name != "" {
		serviceRequest.Name = &request.Name
	}
	if request.ShortName != "" {
		serviceRequest.ShortName = &request.ShortName
	}
	if request.RPCURL != "" {
		serviceRequest.RPCURL = &request.RPCURL
	}
	if request.ExplorerURL != "" {
		serviceRequest.ExplorerURL = &request.ExplorerURL
	}
	if request.EntryPointAddress != "" {
		serviceRequest.EntryPointAddress = &request.EntryPointAddress
	}
	if request.CpopTokenAddress != "" {
		serviceRequest.CpopTokenAddress = &request.CpopTokenAddress
	}
	if request.MasterAggregatorAddress != "" {
		serviceRequest.MasterAggregatorAddress = &request.MasterAggregatorAddress
	}
	if request.AccountManagerAddress != "" {
		serviceRequest.AccountManagerAddress = &request.AccountManagerAddress
	}
	if request.OptimalBatchSize != 0 {
		optimalSize := int(request.OptimalBatchSize)
		serviceRequest.OptimalBatchSize = &optimalSize
	}
	if request.MaxBatchSize != 0 {
		maxSize := int(request.MaxBatchSize)
		serviceRequest.MaxBatchSize = &maxSize
	}
	if request.MinBatchSize != 0 {
		minSize := int(request.MinBatchSize)
		serviceRequest.MinBatchSize = &minSize
	}
	serviceRequest.IsEnabled = &request.IsEnabled

	// Update chain
	err = h.chainsService.UpdateChain(ctx, chainID, serviceRequest)
	if err != nil {
		if containsSubstring(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Chain not found")
		}
		log.Error().Err(err).Int64("chain_id", chainID).Msg("Failed to update chain")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update chain")
	}

	log.Info().
		Int64("chain_id", chainID).
		Msg("Chain updated successfully")

	message := "Chain configuration updated successfully"
	response := &types.ChainUpdateResponse{
		Message: &message,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
