-- +migrate Up
-- Set default value for token_id to 0 in transactions table
ALTER TABLE transactions
    ALTER COLUMN token_id SET DEFAULT 0;

-- +migrate Down
-- Remove default value for token_id
ALTER TABLE transactions
    ALTER COLUMN token_id DROP DEFAULT;

