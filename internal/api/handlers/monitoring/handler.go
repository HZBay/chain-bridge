package monitoring

import (
	"github.com/hzbay/chain-bridge/internal/queue"
)

// Handler handles monitoring and optimization requests
type Handler struct {
	monitor   *queue.QueueMonitor
	optimizer *queue.BatchOptimizer
}

// NewHandler creates a new monitoring handler
func NewHandler(monitor *queue.QueueMonitor, optimizer *queue.BatchOptimizer) *Handler {
	return &Handler{
		monitor:   monitor,
		optimizer: optimizer,
	}
}
