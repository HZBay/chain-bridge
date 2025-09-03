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

// PostBatchTransferNFTsRoute creates the route for NFT batch transfer
func PostBatchTransferNFTsRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.NFTService)
	return s.Router.APIV1Assets.POST("/nft/transfer", handler.BatchTransferNFTs)
}

// BatchTransferNFTs handles POST /api/v1/assets/nft/transfer requests
func (h *Handler) BatchTransferNFTs(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate request body
	var request types.BatchTransferNFTsBody
	if err := util.BindAndValidateBody(c, &request); err != nil {
		log.Debug().Err(err).Msg("Failed to bind and validate batch transfer NFTs request body")
		return err
	}

	log.Info().
		Str("operation_id", *request.OperationID).
		Str("collection_id", *request.CollectionID).
		Int64("chain_id", *request.ChainID).
		Int("operations_count", len(request.TransferOperations)).
		Msg("Processing NFT batch transfer request")

	// Validate request limits
	if len(request.TransferOperations) == 0 {
		return httperrors.NewHTTPErrorWithDetail(
			http.StatusBadRequest,
			"validation_error",
			"Invalid Request Parameters",
			"At least one transfer operation is required",
		)
	}

	if len(request.TransferOperations) > MaxNFTOperationsPerRequest {
		return httperrors.NewHTTPErrorWithDetail(
			http.StatusBadRequest,
			"validation_error",
			"Invalid Request Parameters",
			"Too many operations. Maximum "+strconv.Itoa(MaxNFTOperationsPerRequest)+" operations allowed per request",
		)
	}

	// Convert request to service format
	serviceRequest := &nft.BatchTransferRequest{
		OperationID:        *request.OperationID,
		CollectionID:       *request.CollectionID,
		ChainID:            *request.ChainID,
		TransferOperations: make([]nft.TransferOperation, 0, len(request.TransferOperations)),
		BatchPreferences:   convertBatchPreferences(request.BatchPreferences),
	}

	// Convert transfer operations
	for _, op := range request.TransferOperations {
		transferOp := nft.TransferOperation{
			FromUserID: *op.FromUserID,
			ToUserID:   *op.ToUserID,
			TokenID:    *op.TokenID,
		}

		serviceRequest.TransferOperations = append(serviceRequest.TransferOperations, transferOp)
	}

	// Process the batch transfer request
	response, batchInfo, err := h.nftService.BatchTransferNFTs(ctx, serviceRequest)
	if err != nil {
		log.Error().Err(err).
			Str("operation_id", *request.OperationID).
			Msg("Failed to process NFT batch transfer")

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
			"Failed to process NFT batch transfer request",
		)
	}

	log.Info().
		Str("operation_id", response.OperationID).
		Int("processed_count", response.ProcessedCount).
		Str("status", response.Status).
		Msg("NFT batch transfer processed successfully")

	// Convert response
	apiResponse := &types.NFTBatchTransferResponse{
		OperationID:    response.OperationID,
		ProcessedCount: int64(response.ProcessedCount),
		Status:         response.Status,
	}

	// Build composite response
	compositeResponse := struct {
		Data      *types.NFTBatchTransferResponse `json:"data"`
		BatchInfo *types.BatchInfo                `json:"batch_info"`
	}{
		Data:      apiResponse,
		BatchInfo: convertBatchInfo(batchInfo),
	}

	return util.ValidateAndReturn(c, http.StatusOK, compositeResponse)
}
