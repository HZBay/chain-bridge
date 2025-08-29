package tokens

import (
	"net/http"

	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/services/tokens"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

// PostCreateTokenRoute creates the route for creating a new token
func PostCreateTokenRoute(s *api.Server) *echo.Route {
	handler := NewHandler(s.TokensService)
	return s.Router.Management.POST("/tokens", handler.CreateToken)
}

// CreateToken handles POST /tokens requests
func (h *Handler) CreateToken(c echo.Context) error {
	ctx := c.Request().Context()
	log := util.LogFromContext(ctx)

	// Bind and validate request body
	var request types.CreateTokenRequest
	if err := util.BindAndValidateBody(c, &request); err != nil {
		return err
	}

	log.Info().
		Int64("chain_id", *request.ChainID).
		Str("symbol", *request.Symbol).
		Str("token_type", *request.TokenType).
		Msg("Creating new token")

	// Convert to service request
	serviceRequest := &tokens.CreateTokenRequest{
		ChainID:   *request.ChainID,
		Symbol:    *request.Symbol,
		Name:      *request.Name,
		Decimals:  int(*request.Decimals),
		TokenType: *request.TokenType,
	}

	if request.ContractAddress != "" {
		serviceRequest.ContractAddress = &request.ContractAddress
	}

	serviceRequest.SupportsBatchOperations = &request.SupportsBatchOperations

	if request.BatchOperations != nil {
		serviceRequest.BatchOperations = request.BatchOperations.(map[string]interface{})
	}

	// Create token
	tokenConfig, err := h.tokensService.CreateToken(ctx, serviceRequest)
	if err != nil {
		if err.Error() == "chain with ID "+string(rune(*request.ChainID))+" does not exist" {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "Chain not found")
		}
		if containsError(err.Error(), "already exists") {
			return echo.NewHTTPError(http.StatusConflict, "Token already exists")
		}
		log.Error().Err(err).Msg("Failed to create token")
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create token")
	}

	log.Info().
		Int("token_id", tokenConfig.ID).
		Str("symbol", tokenConfig.Symbol).
		Int64("chain_id", tokenConfig.ChainID).
		Msg("Token created successfully")

	message := "Token created successfully"
	tokenID := int64(tokenConfig.ID)
	response := &types.TokenCreateResponse{
		Message: &message,
		TokenID: &tokenID,
	}

	return util.ValidateAndReturn(c, http.StatusCreated, response)
}

// containsError checks if error message contains specific text
func containsError(err, text string) bool {
	return len(err) >= len(text) && err[:len(text)] == text ||
		len(err) > len(text) && err[len(err)-len(text):] == text ||
		containsSubstring(err, text)
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
