-- +migrate Up
-- Add new fields for batch consumer configuration
ALTER TABLE chains
    ADD COLUMN max_wait_time_ms int DEFAULT 15000,
    ADD COLUMN consumer_count int DEFAULT 1;

-- Add comments for the new fields
COMMENT ON COLUMN chains.max_wait_time_ms IS 'Maximum wait time in milliseconds for batch processing';

COMMENT ON COLUMN chains.consumer_count IS 'Number of consumer workers for this chain';

-- Update existing chains with reasonable defaults based on chain characteristics
UPDATE
    chains
SET
    max_wait_time_ms = CASE WHEN optimal_batch_size > 30 THEN
        20000 -- Larger batches can wait longer
    WHEN optimal_batch_size < 15 THEN
        10000 -- Smaller batches should be faster
    ELSE
        15000 -- Default for medium batches
    END,
    consumer_count = CASE WHEN optimal_batch_size > 35 THEN
        1 -- Large batches need fewer consumers
    WHEN optimal_batch_size < 20 THEN
        2 -- Small batches can have more consumers
    ELSE
        1 -- Default for medium batches
    END;

-- +migrate Down
-- Remove the new fields
ALTER TABLE chains
    DROP COLUMN IF EXISTS max_wait_time_ms,
    DROP COLUMN IF EXISTS consumer_count;

