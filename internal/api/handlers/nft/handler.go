package nft

import (
	"github.com/hzbay/chain-bridge/internal/services/nft"
)

// MaxNFTOperationsPerRequest defines the maximum number of operations allowed per request
const MaxNFTOperationsPerRequest = 100

// Handler handles NFT-related HTTP requests
type Handler struct {
	nftService nft.Service
}

// NewHandler creates a new NFT handler
func NewHandler(nftService nft.Service) *Handler {
	return &Handler{
		nftService: nftService,
	}
}