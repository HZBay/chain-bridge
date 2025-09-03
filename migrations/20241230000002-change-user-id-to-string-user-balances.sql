-- +migrate Up
-- Change user_id from UUID to TEXT in user_balances table
ALTER TABLE user_balances
    ALTER COLUMN user_id TYPE TEXT;

-- No need to recreate indexes as there's no specific index on user_id alone in user_balances
-- +migrate Down
-- Revert user_id from TEXT to UUID in user_balances table
ALTER TABLE user_balances
    ALTER COLUMN user_id TYPE uuid
    USING user_id::uuid;

