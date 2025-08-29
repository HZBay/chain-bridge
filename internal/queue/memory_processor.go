package queue

// This file contains the legacy in-memory processor that has been deprecated
// in favor of the simplified RabbitMQ-only approach.
//
// The MemoryProcessor was originally implemented as a fallback mechanism
// but has been removed to simplify the system architecture and reduce maintenance overhead.
//
// All batch processing now uses RabbitMQ through the HybridBatchProcessor
// which has been simplified to only manage RabbitMQ operations.
//
// This file is kept for reference and will be removed in a future cleanup.