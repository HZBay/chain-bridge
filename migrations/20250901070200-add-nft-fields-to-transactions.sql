-- +migrate Up
-- Add NFT related columns to transactions table
ALTER TABLE transactions
    ADD COLUMN collection_id VARCHAR(100),
    ADD COLUMN nft_token_id VARCHAR(78),
    ADD COLUMN nft_metadata JSONB;

-- Add indexes for the new columns
CREATE INDEX idx_transactions_collection_id ON transactions USING btree (collection_id);

CREATE INDEX idx_transactions_nft_token_id ON transactions USING btree (nft_token_id);

-- +migrate Down
-- Remove the added columns and indexes
DROP INDEX IF EXISTS idx_transactions_nft_token_id;

DROP INDEX IF EXISTS idx_transactions_collection_id;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS collection_id,
    DROP COLUMN IF EXISTS nft_token_id,
    DROP COLUMN IF EXISTS nft_metadata;

