package transfer

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/api/httperrors"
	"github.com/hzbay/chain-bridge/internal/services/transfer"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
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

// Handle processes asset transfer requests (batch format only)
func (h *AssetsHandler) Handle(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse batch transfer request
	var request types.TransferRequest
	if err := util.BindAndValidateBody(c, &request); err != nil {
		log.Debug().Err(err).Msg("Failed to bind and validate transfer request body")
		return err
	}

	// Log batch transfer request for audit
	log.Info().
		Interface("request", &request).
		Int("transfer_count", len(request.Transfers)).
		Msg("Processing transfer assets request")

	// Call batch transfer service
	transferResponse, batchInfo, err := h.transferService.BatchTransferAssets(ctx, &request)
	if err != nil {
		return h.handleTransferError(err, log, &request)
	}

	log.Info().
		Str("operation_id", *transferResponse.OperationID).
		Int64("transfer_count", *transferResponse.ProcessedCount).
		Bool("will_be_batched", batchInfo.WillBeBatched).
		Msg("Transfer assets request completed successfully")

	// Build response
	response := &types.TransferResponse{
		OperationID:        transferResponse.OperationID,
		ProcessedCount:     transferResponse.ProcessedCount,
		Status:             transferResponse.Status,
		TransferOperations: transferResponse.TransferOperations,
		BatchInfo:          batchInfo,
	}

	return util.ValidateAndReturn(c, http.StatusOK, response)
}

// handleTransferError processes transfer service errors and returns appropriate HTTP responses
func (h *AssetsHandler) handleTransferError(err error, log *zerolog.Logger, request *types.TransferRequest) error {
	// Check if it's a validation error for operation_id
	if strings.HasPrefix(err.Error(), "validation_error:operation_id:") {
		message := strings.TrimPrefix(err.Error(), "validation_error:operation_id:")
		operationID := *request.OperationID
		log.Debug().Err(err).
			Str("operation_id", operationID).
			Msg("Invalid operation_id format provided")

		return httperrors.NewHTTPValidationError(
			http.StatusBadRequest,
			httperrors.HTTPErrorTypeGeneric,
			"Validation Error",
			[]*types.HTTPValidationErrorDetail{
				{
					Key:   swag.String("operation_id"),
					In:    swag.String("body.operation_id"),
					Error: swag.String(message),
				},
			},
		)
	}

	// Check for token not found error
	if strings.Contains(err.Error(), "validation_error:token_not_found:") {
		parts := strings.Split(err.Error(), ":")
		if len(parts) >= 4 {
			chainID := parts[2]
			tokenSymbol := parts[3]
			log.Debug().Err(err).
				Str("chain_id", chainID).
				Str("token_symbol", tokenSymbol).
				Msg("Token not supported on specified chain")

			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				httperrors.HTTPErrorTypeGeneric,
				"Validation Error",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("token_symbol"),
						In:    swag.String("body.token_symbol"),
						Error: swag.String(fmt.Sprintf("Token '%s' is not supported on chain %s", tokenSymbol, chainID)),
					},
				},
			)
		}
	}

	// Check for chain not found error
	if strings.Contains(err.Error(), "validation_error:chain_not_found:") {
		parts := strings.Split(err.Error(), ":")
		if len(parts) >= 3 {
			chainID := parts[2]
			log.Debug().Err(err).
				Str("chain_id", chainID).
				Msg("Chain not supported")

			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				httperrors.HTTPErrorTypeGeneric,
				"Validation Error",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("chain_id"),
						In:    swag.String("body.chain_id"),
						Error: swag.String(fmt.Sprintf("Chain ID %s is not supported", chainID)),
					},
				},
			)
		}
	}

	// Check for from_user account not found error
	if strings.Contains(err.Error(), "validation_error:from_user_account_not_found:") {
		parts := strings.Split(err.Error(), ":")
		if len(parts) >= 4 {
			userID := parts[2]
			chainID := parts[3]
			log.Debug().Err(err).
				Str("from_user_id", userID).
				Str("chain_id", chainID).
				Msg("From user account not found on specified chain")

			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				httperrors.HTTPErrorTypeGeneric,
				"Validation Error",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("from_user_id"),
						In:    swag.String("body.from_user_id"),
						Error: swag.String(fmt.Sprintf("Sender '%s' does not have an account on chain %s. Please create an account first.", userID, chainID)),
					},
				},
			)
		}
	}

	// Check for to_user account not found error
	if strings.Contains(err.Error(), "validation_error:to_user_account_not_found:") {
		parts := strings.Split(err.Error(), ":")
		if len(parts) >= 4 {
			userID := parts[2]
			chainID := parts[3]
			log.Debug().Err(err).
				Str("to_user_id", userID).
				Str("chain_id", chainID).
				Msg("To user account not found on specified chain")

			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				httperrors.HTTPErrorTypeGeneric,
				"Validation Error",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("to_user_id"),
						In:    swag.String("body.to_user_id"),
						Error: swag.String(fmt.Sprintf("Recipient '%s' does not have an account on chain %s. Please create an account first.", userID, chainID)),
					},
				},
			)
		}
	}

	// Log and return generic error
	log.Error().Err(err).
		Interface("request", request).
		Int("transfer_count", len(request.Transfers)).
		Msg("Failed to process batch transfer")
	return err
}
