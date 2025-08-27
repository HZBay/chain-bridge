package batch

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/batch"
	"github.com/hzbay/chain-bridge/internal/types/cpop"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// GetBatchStatusHandler handles GET /batches/{batch_id} requests
type GetBatchStatusHandler struct {
	batchService batch.Service
}

// NewGetBatchStatusHandler creates a new get batch status handler
func NewGetBatchStatusHandler(batchService batch.Service) *GetBatchStatusHandler {
	return &GetBatchStatusHandler{
		batchService: batchService,
	}
}

// GetBatchStatusRoute creates the route for getting batch status
func GetBatchStatusRoute(s *api.Server) *echo.Route {
	handler := NewGetBatchStatusHandler(
		batch.NewService(s.DB, s.BatchProcessor, s.QueueMonitor),
	)
	return s.Router.Management.GET("/batches/:batch_id", handler.Handle)
}

// Handle processes get batch status requests
func (h *GetBatchStatusHandler) Handle(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate parameters using swagger-generated method
	params := cpop.NewGetBatchStatusParams()
	if err := params.BindRequest(c.Request(), nil); err != nil {
		log.Debug().Err(err).Msg("Failed to bind request parameters")
		return err
	}

	log.Info().Str("batch_id", params.BatchID).Msg("Getting batch status")

	// Call batch service to get batch status
	batchStatus, err := h.batchService.GetBatchStatus(ctx, params.BatchID)
	if err != nil {
		log.Error().Err(err).Str("batch_id", params.BatchID).Msg("Failed to get batch status")
		return err
	}

	log.Info().
		Str("batch_id", params.BatchID).
		Str("status", *batchStatus.Status).
		Msg("Batch status retrieved successfully")

	return util.ValidateAndReturn(c, http.StatusOK, batchStatus)
}
