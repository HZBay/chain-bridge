-- +migrate Up
-- ChainBridge: Create supported tokens table
-- Tracks ERC20 tokens and other assets supported on each chain
-- Create enum for token types
CREATE TYPE token_type AS ENUM (
    'NATIVE',
    'ERC20',
    'ERC721',
    'ERC1155',
    'CPOP'
);

-- Create enum for token status
CREATE TYPE token_status AS ENUM (
    'active',
    'inactive',
    'deprecated'
);

-- Create supported_tokens table
CREATE TABLE supported_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid (),
    chain_id bigint NOT NULL REFERENCES chains (id),
    -- Token identification
    contract_address char(42), -- Contract address (NULL for native tokens)
    token_type token_type NOT NULL,
    symbol varchar(20) NOT NULL, -- Token symbol (e.g., "USDT", "ETH")
    name varchar(100) NOT NULL, -- Token name (e.g., "Tether USD")
    decimals integer NOT NULL DEFAULT 18, -- Token decimals
    -- Token metadata
    logo_url varchar(500), -- Token logo URL
    description text, -- Token description
    website_url varchar(500), -- Official website
    status token_status NOT NULL DEFAULT 'active',
    -- Trading information
    coingecko_id varchar(100), -- CoinGecko API ID for price data
    price_usd DECIMAL(20, 8), -- Current USD price
    price_updated_at timestamptz, -- Last price update
    -- Configuration
    min_transfer_amount DECIMAL(78, 18) DEFAULT 0, -- Minimum transfer amount
    max_transfer_amount DECIMAL(78, 18), -- Maximum transfer amount (NULL = no limit)
    transfer_fee_percent DECIMAL(5, 4) DEFAULT 0, -- Transfer fee percentage
    is_stable_coin boolean DEFAULT FALSE, -- Is this a stable coin?
    -- Alchemy API configuration
    alchemy_supported boolean DEFAULT TRUE, -- Supported by Alchemy API
    custom_indexing boolean DEFAULT FALSE, -- Requires custom indexing
    -- Metadata
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    -- Constraints
    CONSTRAINT unique_token_chain UNIQUE (chain_id, contract_address),
    CONSTRAINT valid_contract_address CHECK (contract_address IS NULL OR contract_address ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT valid_decimals CHECK (decimals >= 0 AND decimals <= 30),
    CONSTRAINT native_token_no_address CHECK ((token_type = 'NATIVE' AND contract_address IS NULL) OR (token_type != 'NATIVE' AND contract_address IS NOT NULL))
);

-- Create indexes
CREATE INDEX idx_supported_tokens_chain_id ON supported_tokens (chain_id);

CREATE INDEX idx_supported_tokens_contract ON supported_tokens (contract_address);

CREATE INDEX idx_supported_tokens_symbol ON supported_tokens (symbol);

CREATE INDEX idx_supported_tokens_type ON supported_tokens (token_type);

CREATE INDEX idx_supported_tokens_status ON supported_tokens (status);

CREATE INDEX idx_supported_tokens_chain_symbol ON supported_tokens (chain_id, symbol);

CREATE INDEX idx_supported_tokens_coingecko ON supported_tokens (coingecko_id);

-- Create trigger for updated_at
CREATE TRIGGER update_supported_tokens_updated_at
    BEFORE UPDATE ON supported_tokens
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column ();

-- Insert native tokens for each chain
INSERT INTO supported_tokens (chain_id, token_type, symbol, name, decimals, coingecko_id, is_stable_coin)
    VALUES (1, 'NATIVE', 'ETH', 'Ethereum', 18, 'ethereum', FALSE),
    (56, 'NATIVE', 'BNB', 'BNB', 18, 'binancecoin', FALSE),
    (137, 'NATIVE', 'MATIC', 'Polygon', 18, 'matic-network', FALSE),
    (42161, 'NATIVE', 'ETH', 'Ethereum', 18, 'ethereum', FALSE);

-- Insert popular ERC20 tokens on Ethereum
INSERT INTO supported_tokens (chain_id, contract_address, token_type, symbol, name, decimals, coingecko_id, is_stable_coin)
    VALUES (1, '0xdAC17F958D2ee523a2206206994597C13D831ec7', 'ERC20', 'USDT', 'Tether USD', 6, 'tether', TRUE),
    (1, '0xA0b86a33E6Eb06925a0f7b5dd4f0C8Cf6F2F4e1E', 'ERC20', 'USDC', 'USD Coin', 6, 'usd-coin', TRUE),
    (1, '0x6B175474E89094C44Da98b954EedeAC495271d0F', 'ERC20', 'DAI', 'Dai Stablecoin', 18, 'dai', TRUE);

-- Insert popular tokens on BSC
INSERT INTO supported_tokens (chain_id, contract_address, token_type, symbol, name, decimals, coingecko_id, is_stable_coin)
    VALUES (56, '0x55d398326f99059fF775485246999027B3197955', 'ERC20', 'USDT', 'Tether USD', 18, 'tether', TRUE),
    (56, '0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d', 'ERC20', 'USDC', 'USD Coin', 18, 'usd-coin', TRUE);

-- Insert popular tokens on Polygon
INSERT INTO supported_tokens (chain_id, contract_address, token_type, symbol, name, decimals, coingecko_id, is_stable_coin)
    VALUES (137, '0xc2132D05D31c914a87C6611C10748AEb04B58e8F', 'ERC20', 'USDT', 'Tether USD', 6, 'tether', TRUE),
    (137, '0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174', 'ERC20', 'USDC', 'USD Coin', 6, 'usd-coin', TRUE);

-- Insert popular tokens on Arbitrum
INSERT INTO supported_tokens (chain_id, contract_address, token_type, symbol, name, decimals, coingecko_id, is_stable_coin)
    VALUES (42161, '0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9', 'ERC20', 'USDT', 'Tether USD', 6, 'tether', TRUE),
    (42161, '0xA0b86a33E6Eb06925a0f7b5dd4f0C8Cf6F2F4e1E', 'ERC20', 'USDC', 'USD Coin', 6, 'usd-coin', TRUE);

-- +migrate Down
DROP TABLE IF EXISTS supported_tokens;

DROP TYPE IF EXISTS token_type;

DROP TYPE IF EXISTS token_status;

