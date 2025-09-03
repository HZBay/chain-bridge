-- +migrate Up
-- Insert official NFT collection only if the chain exists
INSERT INTO nft_collections (collection_id, chain_id, contract_address, name, symbol, contract_type, base_uri, is_enabled)
SELECT
    'cpop_official_nft_collection',
    11155111, -- Sepolia testnet chain ID
    '0xEa81A317a4Bc82084359028A207e282F8F503d16',
    'CPOP NFT Collection',
    'CPNFT',
    'ERC721',
    'https://api.cpop.io/nft/',
    TRUE
WHERE
    EXISTS (
        SELECT
            1
        FROM
            chains
        WHERE
            chain_id = 11155111);

-- Also insert initial collection stats only if the collection was inserted
INSERT INTO nft_collection_stats (collection_id, total_supply, total_owners, floor_price_usd, market_cap_usd)
SELECT
    'cpop_official_nft_collection',
    0,
    0,
    0.0,
    0.0
WHERE
    EXISTS (
        SELECT
            1
        FROM
            nft_collections
        WHERE
            collection_id = 'cpop_official_nft_collection');

-- +migrate Down
-- Remove official NFT collection
DELETE FROM nft_collections
WHERE collection_id = 'cpop_official_nft_collection'
    AND contract_address = '0xEa81A317a4Bc82084359028A207e282F8F503d16';

-- Remove collection stats
DELETE FROM nft_collection_stats
WHERE collection_id = 'cpop_official_nft_collection';

