-- +migrate Up
-- Change user_id from UUID to TEXT in user_accounts table
ALTER TABLE user_accounts
    ALTER COLUMN user_id TYPE TEXT;

-- Update indexes to handle TEXT type
DROP INDEX IF EXISTS idx_user_chain;

CREATE INDEX idx_user_chain ON user_accounts USING btree (user_id, chain_id);

-- +migrate Down
-- Revert user_id from TEXT to UUID in user_accounts table
ALTER TABLE user_accounts
    ALTER COLUMN user_id TYPE uuid
    USING user_id::uuid;

-- Revert indexes
DROP INDEX IF EXISTS idx_user_chain;

CREATE INDEX idx_user_chain ON user_accounts USING btree (user_id, chain_id);

