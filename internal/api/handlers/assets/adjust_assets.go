package assets

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/assets"
	"github.com/hzbay/chain-bridge/internal/types/cpop"
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
	return s.Router.Management.POST("/assets/adjust", handler.Handle)
}

// Handle processes asset adjustment requests
func (h *AdjustAssetsHandler) Handle(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate parameters
	params := cpop.NewAdjustAssetsParams()
	if err := params.BindRequest(c.Request(), nil); err != nil {
		log.Debug().Err(err).Msg("Failed to bind request parameters")
		return err
	}

	// Validate request body
	if params.Request == nil {
		log.Debug().Msg("Missing request body")
		return echo.NewHTTPError(http.StatusBadRequest, "Request body is required")
	}

	// Validate adjustments
	if params.Request.Adjustments == nil || len(params.Request.Adjustments) == 0 {
		log.Debug().Msg("No adjustments provided")
		return echo.NewHTTPError(http.StatusBadRequest, "At least one adjustment is required")
	}

	// Log asset adjustment request for audit
	log.Info().
		Str("operation_id", *params.Request.OperationID).
		Int("adjustment_count", len(params.Request.Adjustments)).
		Msg("Processing asset adjustment request")

	// Call assets service
	adjustResponse, batchInfo, err := h.assetsService.AdjustAssets(ctx, params.Request)
	if err != nil {
		log.Error().Err(err).
			Str("operation_id", *params.Request.OperationID).
			Interface("adjustments", params.Request.Adjustments).
			Msg("Failed to process asset adjustments")
		return err
	}

	// Build response matching API specification
	response := map[string]interface{}{
		"data":       adjustResponse,
		"batch_info": batchInfo,
	}

	log.Info().
		Str("operation_id", *adjustResponse.OperationID).
		Str("status", *adjustResponse.Status).
		Int64("processed_count", *adjustResponse.ProcessedCount).
		Bool("will_be_batched", batchInfo.WillBeBatched).
		Msg("Asset adjustment request completed successfully")

	return c.JSON(http.StatusOK, response)
}
