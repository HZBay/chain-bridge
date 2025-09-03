package tokens

import (
	"github.com/hzbay/chain-bridge/internal/services/tokens"
)

// Handler handles tokens management requests
type Handler struct {
	tokensService tokens.Service
}

// NewHandler creates a new tokens handler
func NewHandler(tokensService tokens.Service) *Handler {
	return &Handler{
		tokensService: tokensService,
	}
}
