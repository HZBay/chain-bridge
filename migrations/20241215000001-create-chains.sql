-- +migrate Up
-- ChainBridge: Create chains table
-- Tracks supported blockchain networks
-- Create enum for chain status
CREATE TYPE chain_status AS ENUM (
    'active',
    'maintenance',
    'deprecated'
);

-- Create chains table
CREATE TABLE chains (
    id bigint PRIMARY KEY, -- Chain ID (1=Ethereum, 56=BSC, etc.)
    name varchar(100) NOT NULL, -- Chain name (e.g., "Ethereum", "BSC")
    symbol varchar(10) NOT NULL, -- Native token symbol (e.g., "ETH", "BNB")
    rpc_url varchar(500) NOT NULL, -- Primary RPC endpoint
    explorer_url varchar(500) NOT NULL, -- Block explorer base URL
    alchemy_api_key varchar(100), -- Alchemy API key for this chain
    alchemy_app_id varchar(100), -- Alchemy application ID
    status chain_status NOT NULL DEFAULT 'active',
    -- Gas configuration
    gas_price_gwei DECIMAL(20, 9) DEFAULT 20, -- Default gas price in gwei
    gas_limit_default integer DEFAULT 21000, -- Default gas limit
    gas_limit_erc20 integer DEFAULT 65000, -- Gas limit for ERC20 transfers
    gas_limit_batch integer DEFAULT 500000, -- Gas limit for batch operations
    -- Block configuration
    block_time_seconds integer DEFAULT 12, -- Average block time
    confirmations_required integer DEFAULT 12, -- Required confirmations
    -- Contract addresses
    entry_point_address char(42), -- EIP-4337 EntryPoint contract
    paymaster_address char(42), -- Gas Paymaster contract
    cpop_token_address char(42), -- CPOP token contract
    wallet_factory_address char(42), -- AA Wallet factory contract
    -- Metadata
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);

-- Create indexes
CREATE INDEX idx_chains_status ON chains (status);

CREATE INDEX idx_chains_name ON chains (name);

-- Create trigger for updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column ()
    RETURNS TRIGGER
    AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$
LANGUAGE 'plpgsql';

CREATE TRIGGER update_chains_updated_at
    BEFORE UPDATE ON chains
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column ();

-- Insert supported chains
INSERT INTO chains (id, name, symbol, rpc_url, explorer_url, gas_price_gwei, block_time_seconds, confirmations_required)
    VALUES (1, 'Ethereum', 'ETH', 'https://eth-mainnet.g.alchemy.com/v2/', 'https://etherscan.io', 20, 12, 12),
    (56, 'BNB Smart Chain', 'BNB', 'https://bnb-mainnet.g.alchemy.com/v2/', 'https://bscscan.com', 5, 3, 15),
    (137, 'Polygon', 'MATIC', 'https://polygon-mainnet.g.alchemy.com/v2/', 'https://polygonscan.com', 30, 2, 20),
    (42161, 'Arbitrum', 'ETH', 'https://arb-mainnet.g.alchemy.com/v2/', 'https://arbiscan.io', 0.1, 1, 10);

-- +migrate Down
DROP TABLE IF EXISTS chains;

DROP TYPE IF EXISTS chain_status;

DROP FUNCTION IF EXISTS update_updated_at_column ();

