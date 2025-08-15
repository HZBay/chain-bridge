-- +migrate Up
-- ChainBridge: Create user balances table
-- Tracks cached balances for all supported tokens per user
-- Create user_balances table
CREATE TABLE user_balances (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid (),
    user_id uuid NOT NULL, -- User identifier
    chain_id bigint NOT NULL REFERENCES chains (id),
    token_id uuid NOT NULL REFERENCES supported_tokens (id),
    wallet_address char(42) NOT NULL, -- AA wallet address
    -- Balance information
    balance DECIMAL(78, 18) NOT NULL DEFAULT 0, -- Current balance
    previous_balance DECIMAL(78, 18) DEFAULT 0, -- Previous balance (for change tracking)
    -- Synchronization information
    last_sync_at timestamptz NOT NULL DEFAULT NOW(),
    last_sync_block bigint, -- Last synced block number
    sync_method varchar(20) DEFAULT 'alchemy', -- 'alchemy', 'rpc', 'manual'
    -- Stale data handling
    is_stale boolean DEFAULT FALSE, -- Balance needs refresh
    stale_since timestamptz, -- When balance became stale
    -- Price information (cached from supported_tokens)
    balance_usd DECIMAL(20, 8), -- USD value of balance
    price_usd DECIMAL(20, 8), -- Price per token in USD
    price_updated_at timestamptz, -- When price was last updated
    -- Metadata
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    -- Constraints
    CONSTRAINT unique_user_chain_token UNIQUE (user_id, chain_id, token_id),
    CONSTRAINT valid_wallet_address CHECK (wallet_address ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT non_negative_balance CHECK (balance >= 0),
    CONSTRAINT valid_sync_method CHECK (sync_method IN ('alchemy', 'rpc', 'manual'))
);

-- Create indexes
CREATE INDEX idx_user_balances_user_id ON user_balances (user_id);

CREATE INDEX idx_user_balances_chain_id ON user_balances (chain_id);

CREATE INDEX idx_user_balances_token_id ON user_balances (token_id);

CREATE INDEX idx_user_balances_wallet ON user_balances (wallet_address);

CREATE INDEX idx_user_balances_user_chain ON user_balances (user_id, chain_id);

CREATE INDEX idx_user_balances_sync_at ON user_balances (last_sync_at);

CREATE INDEX idx_user_balances_stale ON user_balances (is_stale)
WHERE
    is_stale = TRUE;

CREATE INDEX idx_user_balances_balance ON user_balances (balance)
WHERE
    balance > 0;

-- Create partial index for non-zero balances
CREATE INDEX idx_user_balances_nonzero ON user_balances (user_id, chain_id)
WHERE
    balance > 0;

-- Create trigger for updated_at
CREATE TRIGGER update_user_balances_updated_at
    BEFORE UPDATE ON user_balances
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column ();

-- Create function to mark balances as stale
CREATE OR REPLACE FUNCTION mark_balance_stale (p_user_id uuid, p_chain_id bigint, p_token_id uuid DEFAULT NULL)
    RETURNS void
    AS $$
BEGIN
    UPDATE
        user_balances
    SET
        is_stale = TRUE,
        stale_since = COALESCE(stale_since, NOW()),
        updated_at = NOW()
    WHERE
        user_id = p_user_id
        AND chain_id = p_chain_id
        AND (p_token_id IS NULL
            OR token_id = p_token_id)
        AND is_stale = FALSE;
END;
$$
LANGUAGE plpgsql;

-- Create function to update balance
CREATE OR REPLACE FUNCTION update_user_balance (p_user_id uuid, p_chain_id bigint, p_token_id uuid, p_wallet_address char(42), p_new_balance DECIMAL(78, 18), p_sync_block bigint DEFAULT NULL, p_sync_method varchar(20) DEFAULT 'alchemy')
    RETURNS void
    AS $$
BEGIN
    INSERT INTO user_balances (user_id, chain_id, token_id, wallet_address, balance, previous_balance, last_sync_block, sync_method, is_stale, stale_since)
        VALUES (p_user_id, p_chain_id, p_token_id, p_wallet_address, p_new_balance, 0, p_sync_block, p_sync_method, FALSE, NULL)
    ON CONFLICT (user_id, chain_id, token_id)
        DO UPDATE SET
            previous_balance = user_balances.balance, balance = p_new_balance, wallet_address = p_wallet_address, last_sync_at = NOW(), last_sync_block = COALESCE(p_sync_block, user_balances.last_sync_block), sync_method = p_sync_method, is_stale = FALSE, stale_since = NULL, updated_at = NOW();
END;
$$
LANGUAGE plpgsql;

-- Create function to calculate total portfolio value
CREATE OR REPLACE FUNCTION get_portfolio_value_usd (p_user_id uuid)
    RETURNS DECIMAL (
        20, 8
)
    AS $$
DECLARE
    total_value DECIMAL(20, 8) := 0;
BEGIN
    SELECT
        COALESCE(SUM(balance_usd), 0) INTO total_value
    FROM
        user_balances ub
        JOIN supported_tokens st ON ub.token_id = st.id
    WHERE
        ub.user_id = p_user_id
        AND ub.balance > 0
        AND st.status = 'active';
    RETURN total_value;
END;
$$
LANGUAGE plpgsql;

-- +migrate Down
DROP TABLE IF EXISTS user_balances;

DROP FUNCTION IF EXISTS mark_balance_stale (uuid, bigint, uuid);

DROP FUNCTION IF EXISTS update_user_balance (uuid, bigint, uuid, char(42), DECIMAL(78, 18), bigint, varchar(20));

DROP FUNCTION IF EXISTS get_portfolio_value_usd (uuid);

