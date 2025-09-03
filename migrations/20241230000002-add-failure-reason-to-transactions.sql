-- +migrate Up
-- Add failure_reason column to transactions table for better error tracking
ALTER TABLE transactions
    ADD COLUMN failure_reason varchar(255);

-- Add index for failed transactions to improve query performance
CREATE INDEX idx_transactions_failed ON transactions (status, failure_reason)
WHERE
    status = 'failed';

-- +migrate Down
-- Remove the index and column
DROP INDEX IF EXISTS idx_transactions_failed;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS failure_reason;

