-- +migrate Up
-- Add official NFT contract address to chains table
ALTER TABLE chains
    ADD COLUMN official_nft_contract_address char(42);

-- Add comment for the new field
COMMENT ON COLUMN chains.official_nft_contract_address IS 'Official NFT contract address for the chain';

-- +migrate Down
-- Remove official NFT contract address column
ALTER TABLE chains
    DROP COLUMN IF EXISTS official_nft_contract_address;

