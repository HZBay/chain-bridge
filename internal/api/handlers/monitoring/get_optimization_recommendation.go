package monitoring

import (
	"net/http"
	"strconv"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// BatchOptimizationRoute creates the route for getting batch optimization recommendations
func BatchOptimizationRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.QueueMonitor, s.BatchOptimizer)
	return s.Router.Management.GET("/monitoring/optimization/:chain_id/:token_id", handler.GetOptimizationRecommendation)
}

// GetOptimizationRecommendation handles GET /monitoring/optimization/{chain_id}/{token_id} requests
func (h *Handler) GetOptimizationRecommendation(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse path parameters
	chainIDStr := c.Param("chain_id")
	tokenIDStr := c.Param("token_id")

	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
		log.Debug().Str("chain_id", chainIDStr).Msg("Invalid chain_id parameter")
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid chain_id parameter")
	}

	tokenID, err := strconv.Atoi(tokenIDStr)
	if err != nil {
		log.Debug().Str("token_id", tokenIDStr).Msg("Invalid token_id parameter")
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid token_id parameter")
	}

	rawRecommendation := h.optimizer.GetOptimizationRecommendation(chainID, tokenID)

	log.Debug().
		Int64("chain_id", chainID).
		Int("token_id", tokenID).
		Int("current_size", rawRecommendation.CurrentBatchSize).
		Int("recommended_size", rawRecommendation.RecommendedSize).
		Float64("improvement", rawRecommendation.ExpectedImprovement).
		Msg("Generated optimization recommendation")

	// Convert to generated type
	tokenIDInt64 := int64(tokenID)
	currentBatchSize := int64(rawRecommendation.CurrentBatchSize)
	recommendedSize := int64(rawRecommendation.RecommendedSize)
	expectedImprovement := float32(rawRecommendation.ExpectedImprovement)
	confidenceLevel := float32(rawRecommendation.Confidence)

	recommendation := &types.OptimizationRecommendation{
		ChainID:             &chainID,
		TokenID:             &tokenIDInt64,
		CurrentBatchSize:    &currentBatchSize,
		RecommendedSize:     &recommendedSize,
		ExpectedImprovement: &expectedImprovement,
		ConfidenceLevel:     &confidenceLevel,
		Reasoning:           rawRecommendation.Reason,
	}

	response := &types.OptimizationRecommendationResponse{
		Data: recommendation,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
