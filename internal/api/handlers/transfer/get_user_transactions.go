package transfer

import (
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/transfer"
	"github.com/hzbay/chain-bridge/internal/types/cpop"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// GetUserTransactionsHandler handles GET /users/{user_id}/transactions requests
type GetUserTransactionsHandler struct {
	transferService transfer.Service
}

// NewGetUserTransactionsHandler creates a new get user transactions handler
func NewGetUserTransactionsHandler(transferService transfer.Service) *GetUserTransactionsHandler {
	return &GetUserTransactionsHandler{
		transferService: transferService,
	}
}

// GetUserTransactionsRoute creates the route for getting user transactions
func GetUserTransactionsRoute(s *api.Server) *echo.Route {
	handler := NewGetUserTransactionsHandler(
		transfer.NewService(s.DB, s.BatchProcessor, s.BatchOptimizer),
	)
	return s.Router.APIV1Assets.GET("/:user_id/transactions", handler.Handle)
}

// Handle retrieves user transaction history with filtering and pagination
func (h *GetUserTransactionsHandler) Handle(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Parse and validate parameters
	params := cpop.NewGetUserTransactionsParams()
	if err := util.BindAndValidatePathAndQueryParams(c, &params); err != nil {
		log.Debug().Err(err).Msg("Failed to bind request parameters")
		return err
	}

	// Validate user ID
	if params.UserID == "" {
		log.Debug().Msg("Missing user_id parameter")
		return echo.NewHTTPError(http.StatusBadRequest, "user_id parameter is required")
	}

	// Build service parameters with defaults
	serviceParams := transfer.GetTransactionsParams{
		ChainID:     params.ChainID,
		TokenSymbol: params.TokenSymbol,
		TxType:      params.TxType,
		Status:      params.Status,
		Page:        1,  // Default page
		Limit:       20, // Default limit
		StartDate:   convertDateToString(params.StartDate),
		EndDate:     convertDateToString(params.EndDate),
	}

	// Override defaults with provided values
	if params.Page != nil {
		serviceParams.Page = int(*params.Page)
	}
	if params.Limit != nil {
		serviceParams.Limit = int(*params.Limit)
	}

	// Validate pagination parameters
	if serviceParams.Page < 1 {
		serviceParams.Page = 1
	}
	if serviceParams.Limit < 1 || serviceParams.Limit > 100 {
		serviceParams.Limit = 20 // Reset to default if invalid
	}

	log.Debug().
		Str("user_id", params.UserID).
		Interface("filters", serviceParams).
		Msg("Retrieving user transactions")

	// Call transfer service
	transactionResponse, err := h.transferService.GetUserTransactions(ctx, params.UserID, serviceParams)
	if err != nil {
		log.Error().Err(err).
			Str("user_id", params.UserID).
			Interface("params", serviceParams).
			Msg("Failed to get user transactions")
		return err
	}

	log.Info().
		Str("user_id", params.UserID).
		Int64("total_count", *transactionResponse.TotalCount).
		Int64("page", *transactionResponse.Page).
		Int64("limit", *transactionResponse.Limit).
		Msg("User transactions retrieved successfully")

	return util.ValidateAndReturn(c, http.StatusOK, transactionResponse)
}

// convertDateToString converts strfmt.Date to string pointer
func convertDateToString(date *strfmt.Date) *string {
	if date == nil {
		return nil
	}
	str := date.String()
	return &str
}
