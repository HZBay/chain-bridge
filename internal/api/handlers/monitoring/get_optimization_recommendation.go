package monitoring

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/types/monitoring"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// BatchOptimizationRoute creates the route for getting batch optimization recommendations
func GetBatchOptimizationRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.QueueMonitor, s.BatchOptimizer, s.PaymentEventService)
	return s.Router.Management.GET("/monitoring/optimization/:chain_id/:token_id", handler.GetOptimizationRecommendation)
}

// GetOptimizationRecommendation handles GET /monitoring/optimization/{chain_id}/{token_id} requests
func (h *Handler) GetOptimizationRecommendation(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate parameters using swagger-generated method
	params := monitoring.NewGetOptimizationRecommendationParams()
	if err := params.BindRequest(c.Request(), nil); err != nil {
		log.Debug().Err(err).Msg("Failed to bind request parameters")
		return err
	}

	chainID := params.ChainID
	tokenID := int(params.TokenID)

	rawRecommendation := h.optimizer.GetOptimizationRecommendation(chainID, tokenID)

	log.Debug().
		Int64("chain_id", chainID).
		Int("token_id", tokenID).
		Int("current_size", rawRecommendation.CurrentBatchSize).
		Int("recommended_size", rawRecommendation.RecommendedSize).
		Float64("improvement", rawRecommendation.ExpectedImprovement).
		Msg("Generated optimization recommendation")

	// TODO: 放到service中处理好一些
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
