-- +migrate Up
-- Add token address to chains table
ALTER TABLE chains
    ADD COLUMN toekn_address char(42);

-- Add comment for the new field
COMMENT ON COLUMN chains.toekn_address IS 'Token address for chains';

-- +migrate Down
-- Remove token address column
ALTER TABLE chains
    DROP COLUMN IF EXISTS toekn_address;

