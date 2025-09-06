package nft

import (
	"net/http"
	"strconv"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/api/httperrors"
	"github.com/hzbay/chain-bridge/internal/services/nft"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// PostBatchBurnNFTsRoute creates the route for NFT batch burning
func PostBatchBurnNFTsRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.NFTService)
	return s.Router.APIV1Assets.POST("/nft/burn", handler.BatchBurnNFTs)
}

// BatchBurnNFTs handles POST /api/v1/assets/nft/burn requests
func (h *Handler) BatchBurnNFTs(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate request body
	var request types.BatchBurnNFTsBody
	if err := util.BindAndValidateBody(c, &request); err != nil {
		log.Debug().Err(err).Msg("Failed to bind and validate batch burn NFTs request body")
		return err
	}

	log.Info().
		Str("operation_id", *request.OperationID).
		Str("collection_id", *request.CollectionID).
		Int64("chain_id", *request.ChainID).
		Int("operations_count", len(request.BurnOperations)).
		Msg("Processing NFT batch burn request")

	// Validate request limits
	if len(request.BurnOperations) == 0 {
		return httperrors.NewHTTPErrorWithDetail(
			http.StatusBadRequest,
			"validation_error",
			"Invalid Request Parameters",
			"At least one burn operation is required",
		)
	}

	if len(request.BurnOperations) > MaxNFTOperationsPerRequest {
		return httperrors.NewHTTPErrorWithDetail(
			http.StatusBadRequest,
			"validation_error",
			"Invalid Request Parameters",
			"Too many operations. Maximum "+strconv.Itoa(MaxNFTOperationsPerRequest)+" operations allowed per request",
		)
	}

	// Convert request to service format
	serviceRequest := &nft.BatchBurnRequest{
		OperationID:      *request.OperationID,
		CollectionID:     *request.CollectionID,
		ChainID:          *request.ChainID,
		BurnOperations:   make([]nft.BurnOperation, 0, len(request.BurnOperations)),
		BatchPreferences: convertBatchPreferences(request.BatchPreferences),
	}

	// Convert burn operations
	for _, op := range request.BurnOperations {
		burnOp := nft.BurnOperation{
			OwnerUserID:  *op.OwnerUserID,
			TokenID:      *op.TokenID,
			BusinessType: *op.BusinessType,
			ReasonType:   *op.ReasonType,
			ReasonDetail: op.ReasonDetail,
		}

		serviceRequest.BurnOperations = append(serviceRequest.BurnOperations, burnOp)
	}

	// Process the batch burn request
	response, batchInfo, err := h.nftService.BatchBurnNFTs(ctx, serviceRequest)
	if err != nil {
		log.Error().Err(err).
			Str("operation_id", *request.OperationID).
			Msg("Failed to process NFT batch burn")

		// Handle specific business errors
		if nft.IsCollectionNotFoundError(err) {
			return httperrors.NewHTTPErrorWithDetail(
				http.StatusNotFound,
				"collection_not_found",
				"Collection Not Found",
				err.Error(),
			)
		}
		if nft.IsChainNotSupportedError(err) {
			return httperrors.NewHTTPErrorWithDetail(
				http.StatusUnprocessableEntity,
				"chain_not_supported",
				"Chain Not Supported",
				err.Error(),
			)
		}
		if nft.IsNFTNotFoundError(err) {
			return httperrors.NewHTTPErrorWithDetail(
				http.StatusNotFound,
				"nft_not_found",
				"NFT Not Found",
				err.Error(),
			)
		}
		if nft.IsOwnershipError(err) {
			return httperrors.NewHTTPErrorWithDetail(
				http.StatusForbidden,
				"ownership_error",
				"Ownership Error",
				err.Error(),
			)
		}
		if nft.IsValidationError(err) {
			return httperrors.NewHTTPErrorWithDetail(
				http.StatusBadRequest,
				"validation_error",
				"Validation Error",
				err.Error(),
			)
		}

		return httperrors.NewHTTPErrorWithDetail(
			http.StatusInternalServerError,
			"internal_error",
			"Internal Server Error",
			"Failed to process NFT batch burn request",
		)
	}

	log.Info().
		Str("operation_id", response.OperationID).
		Int("processed_count", response.ProcessedCount).
		Str("status", response.Status).
		Msg("NFT batch burn processed successfully")

	// Convert response
	apiResponse := &types.NFTBatchBurnResponse{
		OperationID:    &response.OperationID,
		ProcessedCount: func() *int64 { v := int64(response.ProcessedCount); return &v }(),
		Status:         &response.Status,
	}

	// Build composite response with Validate method
	compositeResponse := &struct {
		Data      *types.NFTBatchBurnResponse `json:"data"`
		BatchInfo *types.BatchInfo            `json:"batch_info"`
	}{
		Data:      apiResponse,
		BatchInfo: convertBatchInfo(batchInfo),
	}

	return c.JSON(http.StatusOK, compositeResponse)
}
