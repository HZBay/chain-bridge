package nft

import (
	"net/http"
	"strings"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/api/httperrors"
	"github.com/hzbay/chain-bridge/internal/services/nft"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// GetNFTMetadataRoute creates the route for NFT metadata retrieval (public, no auth)
func GetNFTMetadataRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.NFTService)
	return s.Router.Root.GET("/api/v1/meta/:token_id", handler.GetNFTMetadata)
}

// GetNFTMetadata handles GET /api/v1/meta/{token_id} requests
func (h *Handler) GetNFTMetadata(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Get token ID from path parameter
	tokenID := c.Param("token_id")
	if tokenID == "" {
		return httperrors.NewHTTPErrorWithDetail(
			http.StatusBadRequest,
			"validation_error",
			"Invalid Request Parameters",
			"token_id is required",
		)
	}

	// Validate token ID format (basic validation)
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return httperrors.NewHTTPErrorWithDetail(
			http.StatusBadRequest,
			"validation_error",
			"Invalid Request Parameters",
			"token_id cannot be empty",
		)
	}

	log.Debug().
		Str("token_id", tokenID).
		Msg("Retrieving NFT metadata")

	// Get NFT metadata from service
	metadata, err := h.nftService.GetNFTMetadataByTokenID(ctx, tokenID)
	if err != nil {
		log.Debug().Err(err).
			Str("token_id", tokenID).
			Msg("Failed to retrieve NFT metadata")

		if nft.IsNotFoundError(err) {
			return httperrors.NewHTTPErrorWithDetail(
				http.StatusNotFound,
				"nft_not_found",
				"NFT Not Found",
				"NFT with the specified token ID was not found",
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
			"Failed to retrieve NFT metadata",
		)
	}

	log.Debug().
		Str("token_id", tokenID).
		Str("name", metadata.Name).
		Msg("NFT metadata retrieved successfully")

	// Return metadata in OpenSea standard format
	return c.JSON(http.StatusOK, metadata)
}
