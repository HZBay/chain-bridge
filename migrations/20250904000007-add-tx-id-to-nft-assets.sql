-- +migrate Up
-- Add tx_id column to nft_assets table for linking with transactions
ALTER TABLE nft_assets
    ADD COLUMN tx_id UUID;

-- Create index on tx_id for better query performance
CREATE INDEX idx_nft_assets_tx_id ON nft_assets (tx_id);

-- +migrate Down
-- Remove tx_id column from nft_assets table
DROP INDEX IF EXISTS idx_nft_assets_tx_id;

ALTER TABLE nft_assets
    DROP COLUMN tx_id;

