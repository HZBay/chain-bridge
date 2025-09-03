package chains

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/chains"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// PostCreateChainRoute creates the route for creating a new chain
func PostCreateChainRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.ChainsService)
	return s.Router.Management.POST("/chains", handler.CreateChain)
}

// CreateChain handles POST /chains requests
func (h *Handler) CreateChain(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Bind and validate request body
	var request types.CreateChainRequest
	if err := util.BindAndValidateBody(c, &request); err != nil {
		return err
	}

	log.Info().
		Int64("chain_id", *request.ChainID).
		Str("name", *request.Name).
		Str("short_name", *request.ShortName).
		Msg("Creating new chain")

	// Convert to service request
	serviceRequest := &chains.CreateChainRequest{
		ChainID:   *request.ChainID,
		Name:      *request.Name,
		ShortName: *request.ShortName,
		RPCURL:    *request.RPCURL,
	}

	// Set optional fields
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

	// Create chain
	chainConfig, err := h.chainsService.CreateChain(ctx, serviceRequest)
	if err != nil {
		if containsSubstring(err.Error(), "already exists") {
			return echo.NewHTTPError(http.StatusConflict, "Chain already exists")
		}
		log.Error().Err(err).Msg("Failed to create chain")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create chain")
	}

	log.Info().
		Int64("chain_id", chainConfig.ChainID).
		Str("name", chainConfig.Name).
		Msg("Chain created successfully")

	message := "Chain created successfully"
	chainID := chainConfig.ChainID
	response := &types.ChainCreateResponse{
		Message: &message,
		ChainID: &chainID,
	}

	return util.ValidateAndReturn(c, http.StatusCreated, response)
}

// containsSubstring checks if a string contains a substring
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
