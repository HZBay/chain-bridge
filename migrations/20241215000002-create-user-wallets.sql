-- +migrate Up
-- ChainBridge: Create user wallets table
-- Tracks AA wallet deployments for users across different chains
-- Create enum for wallet status
CREATE TYPE wallet_status AS ENUM (
    'pending',
    'deploying',
    'deployed',
    'failed'
);

-- Create user_wallets table
CREATE TABLE user_wallets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid (),
    user_id uuid NOT NULL, -- User identifier (external)
    chain_id bigint NOT NULL REFERENCES chains (id),
    -- Wallet information
    address char(42) UNIQUE, -- AA wallet address (NULL if not deployed)
    factory_address char(42), -- Factory contract used for deployment
    salt bytea, -- Salt used for CREATE2 deployment
    init_code_hash char(66), -- Hash of initialization code
    -- Deployment status
    status wallet_status NOT NULL DEFAULT 'pending',
    deployment_tx_hash char(66), -- Transaction hash of deployment
    deployment_block_number bigint, -- Block number of deployment
    deployment_gas_used integer, -- Gas used for deployment
    -- Master signer configuration
    master_signer char(42), -- Master signer address (optional)
    owner_address char(42) NOT NULL, -- Wallet owner address
    -- Deposit management (for gas payments)
    deposit_balance DECIMAL(78, 18) DEFAULT 0, -- ETH deposit balance
    daily_gas_limit DECIMAL(78, 18) DEFAULT 0.1, -- Daily gas spending limit
    daily_gas_used DECIMAL(78, 18) DEFAULT 0, -- Gas used today
    gas_reset_date date DEFAULT CURRENT_DATE, -- Date for daily gas reset
    -- Session keys (JSON array of active session keys)
    session_keys jsonb DEFAULT '[]' ::jsonb,
    -- Metadata
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deployed_at timestamptz,
    -- Constraints
    CONSTRAINT unique_user_chain UNIQUE (user_id, chain_id),
    CONSTRAINT valid_address CHECK (address IS NULL OR address ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT valid_master_signer CHECK (master_signer IS NULL OR master_signer ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT valid_owner CHECK (owner_address ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT valid_tx_hash CHECK (deployment_tx_hash IS NULL OR deployment_tx_hash ~ '^0x[a-fA-F0-9]{64}$')
);

-- Create indexes
CREATE INDEX idx_user_wallets_user_id ON user_wallets (user_id);

CREATE INDEX idx_user_wallets_chain_id ON user_wallets (chain_id);

CREATE INDEX idx_user_wallets_address ON user_wallets (address);

CREATE INDEX idx_user_wallets_status ON user_wallets (status);

CREATE INDEX idx_user_wallets_user_chain ON user_wallets (user_id, chain_id);

CREATE INDEX idx_user_wallets_deployment_tx ON user_wallets (deployment_tx_hash);

CREATE INDEX idx_user_wallets_master_signer ON user_wallets (master_signer);

-- GIN index for session_keys JSONB
CREATE INDEX idx_user_wallets_session_keys ON user_wallets USING GIN (session_keys);

-- Create trigger for updated_at
CREATE TRIGGER update_user_wallets_updated_at
    BEFORE UPDATE ON user_wallets
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column ();

-- Create function to reset daily gas usage
CREATE OR REPLACE FUNCTION reset_daily_gas_usage ()
    RETURNS void
    AS $$
BEGIN
    UPDATE
        user_wallets
    SET
        daily_gas_used = 0,
        gas_reset_date = CURRENT_DATE
    WHERE
        gas_reset_date < CURRENT_DATE;
END;
$$
LANGUAGE plpgsql;

-- +migrate Down
DROP TABLE IF EXISTS user_wallets;

DROP TYPE IF EXISTS wallet_status;

DROP FUNCTION IF EXISTS reset_daily_gas_usage ();

