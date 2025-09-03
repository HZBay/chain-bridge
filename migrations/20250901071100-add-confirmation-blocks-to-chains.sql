-- +migrate Up
-- Add confirmation_blocks column to chains table
ALTER TABLE chains
    ADD COLUMN confirmation_blocks INTEGER DEFAULT 12;

-- Add comment for the new field
COMMENT ON COLUMN chains.confirmation_blocks IS 'Number of confirmation blocks required for payment events';

-- +migrate Down
-- Remove confirmation_blocks column
ALTER TABLE chains
    DROP COLUMN IF EXISTS confirmation_blocks;

