-- +migrate Up
-- Insert Sepolia testnet chain with official NFT contract address
INSERT INTO chains (chain_id, name, short_name, rpc_url, explorer_url, entry_point_address, cpop_token_address, master_aggregator_address, account_manager_address, official_nft_contract_address, is_enabled)
    VALUES (11155111, -- Sepolia testnet chain ID
        'Sepolia Testnet', 'Sepolia', 'https://rpc.sepolia.org', 'https://sepolia.etherscan.io', '', -- entry_point_address
        '', -- cpop_token_address
        '', -- master_aggregator_address
        '', -- account_manager_address
        '0xEa81A317a4Bc82084359028A207e282F8F503d16', -- official_nft_contract_address
        TRUE)
ON CONFLICT (chain_id)
    DO UPDATE SET
        name = EXCLUDED.name, short_name = EXCLUDED.short_name, rpc_url = EXCLUDED.rpc_url, explorer_url = EXCLUDED.explorer_url, official_nft_contract_address = EXCLUDED.official_nft_contract_address, is_enabled = EXCLUDED.is_enabled;

-- +migrate Down
-- Reset official NFT contract address for Sepolia testnet
UPDATE
    chains
SET
    official_nft_contract_address = ''
WHERE
    chain_id = 11155111;

