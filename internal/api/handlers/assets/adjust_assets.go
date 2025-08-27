package assets

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/assets"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// AdjustAssetsHandler handles POST /assets/adjust requests
type AdjustAssetsHandler struct {
	assetsService assets.Service
}

// NewAdjustAssetsHandler creates a new adjust assets handler
func NewAdjustAssetsHandler(assetsService assets.Service) *AdjustAssetsHandler {
	return &AdjustAssetsHandler{
		assetsService: assetsService,
	}
}

// PostAdjustAssetsRoute creates the route for asset adjustments
func PostAdjustAssetsRoute(s *api.Server) *echo.Route {
	handler := NewAdjustAssetsHandler(
		assets.NewService(s.DB, s.BatchProcessor, s.BatchOptimizer),
	)
	return s.Router.APIV1Assets.POST("/adjust", handler.Handle)
}

// Handle processes asset adjustment requests
func (h *AdjustAssetsHandler) Handle(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate request body
	var request types.AssetAdjustRequest
	if err := util.BindAndValidateBody(c, &request); err != nil {
		log.Debug().Err(err).Msg("Failed to bind and validate request body")
		return err
	}

	// Log asset adjustment request for audit
	log.Info().
		Str("operation_id", *request.OperationID).
		Int("adjustment_count", len(request.Adjustments)).
		Msg("Processing asset adjustment request")

	// Call assets service
	adjustResponse, batchInfo, err := h.assetsService.AdjustAssets(ctx, &request)
	if err != nil {
		log.Error().Err(err).
			Str("operation_id", *request.OperationID).
			Interface("adjustments", request.Adjustments).
			Msg("Failed to process asset adjustments")
		return err
	}

	log.Info().
		Str("operation_id", *adjustResponse.OperationID).
		Str("status", *adjustResponse.Status).
		Int64("processed_count", *adjustResponse.ProcessedCount).
		Bool("will_be_batched", batchInfo.WillBeBatched).
		Msg("Asset adjustment request completed successfully")

	// Build response matching API specification
	response := &types.AssetAdjustCompleteResponse{
		Data:      adjustResponse,
		BatchInfo: batchInfo,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
