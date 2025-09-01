package transfer

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/transfer"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// AssetsHandler handles POST /transfer requests
type AssetsHandler struct {
	transferService transfer.Service
}

// NewAssetsHandler creates a new transfer assets handler
func NewAssetsHandler(transferService transfer.Service) *AssetsHandler {
	return &AssetsHandler{
		transferService: transferService,
	}
}

// PostTransferAssetsRoute creates the route for asset transfers
func PostTransferAssetsRoute(s *api.Server) *echo.Route {
	handler := NewAssetsHandler(
		transfer.NewService(s.DB, s.BatchProcessor, s.BatchOptimizer),
	)
	return s.Router.APIV1Assets.POST("/transfer", handler.Handle)
}

// Handle processes asset transfer requests
func (h *AssetsHandler) Handle(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate request body
	var request types.TransferRequest
	if err := util.BindAndValidateBody(c, &request); err != nil {
		log.Debug().Err(err).Msg("Failed to bind and validate request body")
		return err
	}

	// Log transfer request for audit
	log.Info().
		Interface("request", &request).
		Msg("Processing transfer assets request")

	// Call transfer service
	transferResponse, batchInfo, err := h.transferService.TransferAssets(ctx, &request)
	if err != nil {
		log.Error().Err(err).
			Interface("request", &request).
			Msg("Failed to process transfer")
		return err
	}

	log.Info().
		Str("operation_id", *transferResponse.OperationID).
		Str("status", *transferResponse.Status).
		Bool("will_be_batched", batchInfo.WillBeBatched).
		Msg("Transfer assets request completed successfully")

	// Build response matching API specification
	response := &types.TransferCompleteResponse{
		Data:      transferResponse,
		BatchInfo: batchInfo,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}
