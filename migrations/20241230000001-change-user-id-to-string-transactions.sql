-- +migrate Up
-- Change user_id and related_user_id from UUID to TEXT in transactions table
ALTER TABLE transactions
    ALTER COLUMN user_id TYPE TEXT,
    ALTER COLUMN related_user_id TYPE TEXT;

-- Update indexes to handle TEXT type
DROP INDEX IF EXISTS idx_user_txs;

CREATE INDEX idx_user_txs ON transactions USING btree (user_id, chain_id, created_at DESC);

-- +migrate Down
-- Revert user_id and related_user_id from TEXT to UUID in transactions table
ALTER TABLE transactions
    ALTER COLUMN user_id TYPE uuid
    USING user_id::uuid,
    ALTER COLUMN related_user_id TYPE uuid
    USING related_user_id::uuid;

-- Revert indexes
DROP INDEX IF EXISTS idx_user_txs;

CREATE INDEX idx_user_txs ON transactions USING btree (user_id, chain_id, created_at DESC);

