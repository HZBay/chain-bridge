-- +migrate Up
-- Add confirmation tracking fields to batches table
ALTER TABLE batches
    ADD COLUMN confirmations int;

ALTER TABLE batches
    ADD COLUMN confirmed_block bigint;

ALTER TABLE batches
    ADD COLUMN retry_count int DEFAULT 0;

ALTER TABLE batches
    ADD COLUMN failure_reason varchar(255);

ALTER TABLE batches
    ADD COLUMN submitted_at timestamptz;

ALTER TABLE batches
    ADD COLUMN updated_at timestamptz DEFAULT NOW();

-- Add indexes
CREATE INDEX idx_batches_pending ON batches (status, submitted_at)
WHERE
    status = 'submitted';

CREATE INDEX idx_batches_retry ON batches (retry_count)
WHERE
    status = 'submitted';

-- +migrate Down
-- Remove indexes
DROP INDEX IF EXISTS idx_batches_pending;

DROP INDEX IF EXISTS idx_batches_retry;

-- Remove columns
ALTER TABLE batches
    DROP COLUMN IF EXISTS confirmations;

ALTER TABLE batches
    DROP COLUMN IF EXISTS confirmed_block;

ALTER TABLE batches
    DROP COLUMN IF EXISTS retry_count;

ALTER TABLE batches
    DROP COLUMN IF EXISTS failure_reason;

ALTER TABLE batches
    DROP COLUMN IF EXISTS submitted_at;

ALTER TABLE batches
    DROP COLUMN IF EXISTS updated_at;

