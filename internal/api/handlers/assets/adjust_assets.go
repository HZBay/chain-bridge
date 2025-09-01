package assets

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/api/httperrors"
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
		// Check if it's a validation error for operation_id
		if strings.HasPrefix(err.Error(), "validation_error:operation_id:") {
			message := strings.TrimPrefix(err.Error(), "validation_error:operation_id:")
			log.Debug().Err(err).
				Str("operation_id", *request.OperationID).
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
							In:    swag.String("body.adjustments[].token_symbol"),
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
							In:    swag.String("body.adjustments[].chain_id"),
							Error: swag.String(fmt.Sprintf("Chain ID %s is not supported", chainID)),
						},
					},
				)
			}
		}

		// Check for user account not found error
		if strings.Contains(err.Error(), "validation_error:user_account_not_found:") {
			parts := strings.Split(err.Error(), ":")
			if len(parts) >= 4 {
				userID := parts[2]
				chainID := parts[3]
				log.Debug().Err(err).
					Str("user_id", userID).
					Str("chain_id", chainID).
					Msg("User account not found on specified chain")

				return httperrors.NewHTTPValidationError(
					http.StatusBadRequest,
					httperrors.HTTPErrorTypeGeneric,
					"Validation Error",
					[]*types.HTTPValidationErrorDetail{
						{
							Key:   swag.String("user_id"),
							In:    swag.String("body.adjustments[].user_id"),
							Error: swag.String(fmt.Sprintf("User '%s' does not have an account on chain %s. Please create an account first.", userID, chainID)),
						},
					},
				)
			}
		}

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
