package queue

// This file contains legacy message acknowledgment code that has been replaced
// by the new atomic batch processing state management system.
//
// All functionality has been moved to batch_processor.go with improved:
// - Three-phase state transitions (preparing -> submitted -> confirmed/failed)
// - Balance freezing/unfreezing mechanisms
// - Atomic database transactions for data consistency
// - Comprehensive error handling and rollback
//
// This file is kept for reference and will be removed in a future cleanup.
