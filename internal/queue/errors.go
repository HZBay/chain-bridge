package queue

import "errors"

// Common queue errors
var (
	ErrQueueNotFound                 = errors.New("queue not found")
	ErrInvalidMessage                = errors.New("invalid message format")
	ErrProcessingFailed              = errors.New("message processing failed")
	ErrBatchSizeExceeded             = errors.New("batch size exceeded maximum limit")
	ErrConsumerManagerNotInitialized = errors.New("consumer manager not initialized")
	ErrChainNotEnabled               = errors.New("chain not enabled")
	ErrConsumerNotFound              = errors.New("consumer not found for chain")
)
