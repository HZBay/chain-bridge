-- +migrate Up
-- Add updated_at column to transactions table
ALTER TABLE transactions
    ADD COLUMN updated_at timestamptz DEFAULT NOW();

-- Create index for updated_at column for performance
CREATE INDEX idx_transactions_updated_at ON transactions USING btree (updated_at);

-- +migrate Down
-- Remove index and column
DROP INDEX IF EXISTS idx_transactions_updated_at;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS updated_at;

