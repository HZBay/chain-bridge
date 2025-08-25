package transfer

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/transfer"
	"github.com/hzbay/chain-bridge/internal/types/cpop"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// TransferAssetsHandler handles POST /transfer requests
type TransferAssetsHandler struct {
	transferService transfer.Service
}

// NewTransferAssetsHandler creates a new transfer assets handler
func NewTransferAssetsHandler(transferService transfer.Service) *TransferAssetsHandler {
	return &TransferAssetsHandler{
		transferService: transferService,
	}
}

// PostTransferAssetsRoute creates the route for asset transfers
func PostTransferAssetsRoute(s *api.Server) *echo.Route {
	handler := NewTransferAssetsHandler(
		transfer.NewService(s.DB, s.BatchProcessor, s.BatchOptimizer),
	)
	return s.Router.Management.POST("/transfer", handler.Handle)
}

// Handle processes asset transfer requests
func (h *TransferAssetsHandler) Handle(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate parameters
	params := cpop.NewTransferAssetsParams()
	if err := params.BindRequest(c.Request(), nil); err != nil {
		log.Debug().Err(err).Msg("Failed to bind request parameters")
		return err
	}

	// Validate request body
	if params.Request == nil {
		log.Debug().Msg("Missing request body")
		return echo.NewHTTPError(http.StatusBadRequest, "Request body is required")
	}

	// Log transfer request for audit
	log.Info().
		Interface("request", params.Request).
		Msg("Processing transfer assets request")

	// Call transfer service
	transferResponse, batchInfo, err := h.transferService.TransferAssets(ctx, params.Request)
	if err != nil {
		log.Error().Err(err).
			Interface("request", params.Request).
			Msg("Failed to process transfer")
		return err
	}

	// Build response matching API specification
	response := map[string]interface{}{
		"data":       transferResponse,
		"batch_info": batchInfo,
	}

	log.Info().
		Str("operation_id", *transferResponse.OperationID).
		Str("status", *transferResponse.Status).
		Bool("will_be_batched", batchInfo.WillBeBatched).
		Msg("Transfer assets request completed successfully")

	return c.JSON(http.StatusOK, response)
}
