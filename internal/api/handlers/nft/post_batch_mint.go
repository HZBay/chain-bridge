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

// PostBatchMintNFTsRoute creates the route for NFT batch minting
func PostBatchMintNFTsRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.NFTService)
	return s.Router.APIV1Assets.POST("/nft/mint", handler.BatchMintNFTs)
}

// BatchMintNFTs handles POST /api/v1/assets/nft/mint requests
func (h *Handler) BatchMintNFTs(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate request body
	var request types.BatchMintNFTsBody
	if err := util.BindAndValidateBody(c, &request); err != nil {
		log.Debug().Err(err).Msg("Failed to bind and validate batch mint NFTs request body")
		return err
	}

	log.Info().
		Str("operation_id", *request.OperationID).
		Str("collection_id", *request.CollectionID).
		Int64("chain_id", *request.ChainID).
		Int("operations_count", len(request.MintOperations)).
		Msg("Processing NFT batch mint request")

	// Validate request limits
	if len(request.MintOperations) == 0 {
		return httperrors.NewHTTPErrorWithDetail(
			http.StatusBadRequest,
			"validation_error",
			"Invalid Request Parameters",
			"At least one mint operation is required",
		)
	}

	if len(request.MintOperations) > MaxNFTOperationsPerRequest {
		return httperrors.NewHTTPErrorWithDetail(
			http.StatusBadRequest,
			"validation_error",
			"Invalid Request Parameters",
			"Too many operations. Maximum "+strconv.Itoa(MaxNFTOperationsPerRequest)+" operations allowed per request",
		)
	}

	// Convert request to service format
	serviceRequest := &nft.BatchMintRequest{
		OperationID:      *request.OperationID,
		CollectionID:     *request.CollectionID,
		ChainID:          *request.ChainID,
		MintOperations:   make([]nft.MintOperation, 0, len(request.MintOperations)),
		BatchPreferences: convertBatchPreferences(request.BatchPreferences),
	}

	// Convert mint operations
	for _, op := range request.MintOperations {
		mintOp := nft.MintOperation{
			ToUserID:     *op.ToUserID,
			BusinessType: *op.BusinessType,
			ReasonType:   *op.ReasonType,
		}

		if op.ReasonDetail != "" {
			mintOp.ReasonDetail = &op.ReasonDetail
		}

		if op.Meta != nil {
			mintOp.Meta = &nft.NFTMetadata{
				Name:        op.Meta.Name,
				Description: op.Meta.Description,
				Image:       op.Meta.Image,
				ExternalURL: op.Meta.ExternalURL,
			}

			// Convert attributes
			if len(op.Meta.Attributes) > 0 {
				mintOp.Meta.Attributes = make([]nft.NFTAttribute, 0, len(op.Meta.Attributes))
				for _, attr := range op.Meta.Attributes {
					mintOp.Meta.Attributes = append(mintOp.Meta.Attributes, nft.NFTAttribute{
						TraitType:        attr.TraitType,
						Value:            attr.Value,
						RarityPercentage: attr.RarityPercentage,
					})
				}
			}
		}

		serviceRequest.MintOperations = append(serviceRequest.MintOperations, mintOp)
	}

	// Process the batch mint request
	response, batchInfo, err := h.nftService.BatchMintNFTs(ctx, serviceRequest)
	if err != nil {
		log.Error().Err(err).
			Str("operation_id", *request.OperationID).
			Msg("Failed to process NFT batch mint")

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
			"Failed to process NFT batch mint request",
		)
	}

	log.Info().
		Str("operation_id", response.OperationID).
		Int("processed_count", response.ProcessedCount).
		Str("status", response.Status).
		Msg("NFT batch mint processed successfully")

	// Convert response
	apiResponse := &types.NFTBatchMintResponse{
		OperationID:    &response.OperationID,
		ProcessedCount: func() *int64 { v := int64(response.ProcessedCount); return &v }(),
		Status:         &response.Status,
	}

	// Build composite response with Validate method
	compositeResponse := &struct {
		Data      *types.NFTBatchMintResponse `json:"data"`
		BatchInfo *types.BatchInfo            `json:"batch_info"`
	}{
		Data:      apiResponse,
		BatchInfo: convertBatchInfo(batchInfo),
	}

	return c.JSON(http.StatusOK, compositeResponse)
}

// Helper function to convert batch preferences
func convertBatchPreferences(prefs *types.BatchPreference) *nft.BatchPreferences {
	if prefs == nil {
		return nil
	}

	result := &nft.BatchPreferences{}
	if prefs.MaxWaitTime != "" {
		result.MaxWaitTime = &prefs.MaxWaitTime
	}
	if prefs.Priority != "" {
		result.Priority = &prefs.Priority
	}

	return result
}

// Helper function to convert batch info
func convertBatchInfo(batchInfo *nft.BatchInfo) *types.BatchInfo {
	if batchInfo == nil {
		return nil
	}

	result := &types.BatchInfo{
		BatchID:              batchInfo.BatchID,
		QueuedTransactions:   int64(batchInfo.QueuedTransactions),
		EstimatedProcessTime: batchInfo.EstimatedProcessTime,
		Priority:             batchInfo.Priority,
	}

	if batchInfo.EstimatedGasCost != nil {
		result.EstimatedGasCost = *batchInfo.EstimatedGasCost
	}

	return result
}

// Constants for NFT operations
const (
	MaxNFTOperationsPerRequest = 100
)
