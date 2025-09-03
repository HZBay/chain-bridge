package monitoring

import (
	"github.com/hzbay/chain-bridge/internal/events"
	"github.com/hzbay/chain-bridge/internal/queue"
)

// Handler handles monitoring and optimization requests
type Handler struct {
	monitor             *queue.Monitor
	optimizer           *queue.BatchOptimizer
	paymentEventService *events.PaymentEventService
}

// NewHandler creates a new monitoring handler
func NewHandler(monitor *queue.Monitor, optimizer *queue.BatchOptimizer, paymentEventService *events.PaymentEventService) *Handler {
	return &Handler{
		monitor:             monitor,
		optimizer:           optimizer,
		paymentEventService: paymentEventService,
	}
}
