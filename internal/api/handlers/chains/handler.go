package chains

import (
	"github.com/hzbay/chain-bridge/internal/services/chains"
)

// Handler handles chains configuration requests
type Handler struct {
	chainsService chains.Service
}

// NewHandler creates a new chains handler
func NewHandler(chainsService chains.Service) *Handler {
	return &Handler{
		chainsService: chainsService,
	}
}
