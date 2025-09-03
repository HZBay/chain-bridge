package assets

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/assets"
	"github.com/hzbay/chain-bridge/internal/types/cpop"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// GetUserAssetsHandler handles GET /users/{user_id}/assets requests
type GetUserAssetsHandler struct {
	assetsService assets.Service
}

// NewGetUserAssetsHandler creates a new get user assets handler
func NewGetUserAssetsHandler(assetsService assets.Service) *GetUserAssetsHandler {
	return &GetUserAssetsHandler{
		assetsService: assetsService,
	}
}

// GetUserAssetsRoute creates the route for getting user assets
func GetUserAssetsRoute(s *api.Server) *echo.Route {
	handler := NewGetUserAssetsHandler(
		assets.NewService(s.DB, s.BatchProcessor, s.BatchOptimizer),
	)
	return s.Router.APIV1Assets.GET("/:user_id", handler.Handle)
}

// Handle processes get user assets requests
func (h *GetUserAssetsHandler) Handle(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate parameters using swagger-generated method
	params := cpop.NewGetUserAssetsParams()
	if err := util.BindAndValidatePathParams(c, &params); err != nil {
		log.Debug().Err(err).Msg("Failed to bind request parameters")
		return err
	}

	log.Info().Str("user_id", params.UserID).Msg("Getting user assets")

	// Call assets service to get user assets
	assetsData, _, err := h.assetsService.GetUserAssets(ctx, params.UserID)
	if err != nil {
		log.Error().Err(err).Str("user_id", params.UserID).Msg("Failed to get user assets")
		return err
	}

	log.Info().
		Str("user_id", params.UserID).
		Float32("total_value_usd", *assetsData.TotalValueUsd).
		Int("asset_count", len(assetsData.Assets)).
		Msg("User assets retrieved successfully")

	// Return assets data directly without batch info wrapper
	return util.ValidateAndReturn(c, http.StatusOK, assetsData)
}
