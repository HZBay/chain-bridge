package auth

import (
	"time"

	"github.com/hzbay/chain-bridge/internal/data/dto"
)

type Result struct {
	Token      string
	User       *dto.User
	ValidUntil time.Time
	Scopes     []string
}
