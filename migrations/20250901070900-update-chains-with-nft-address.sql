-- +migrate Up
-- Update existing chains with official NFT contract addresses where applicable
-- Sepolia testnet
UPDATE
    chains
SET
    official_nft_contract_address = '0xEa81A317a4Bc82084359028A207e282F8F503d16'
WHERE
    chain_id = 11155111;

-- You can add more chains here as needed
-- Mainnet
-- UPDATE chains
-- SET official_nft_contract_address = '0x...'
-- WHERE chain_id = 1;
-- BSC
-- UPDATE chains
-- SET official_nft_contract_address = '0x...'
-- WHERE chain_id = 56;
-- +migrate Down
-- Reset official NFT contract addresses
UPDATE
    chains
SET
    official_nft_contract_address = ''
WHERE
    chain_id = 11155111;

