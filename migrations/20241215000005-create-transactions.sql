-- +migrate Up
-- ChainBridge: Create transactions table
-- Tracks all transfer operations and their status
-- Create enum for transaction status
CREATE TYPE transaction_status AS ENUM (
    'pending', -- Transaction created, not yet submitted
    'submitted', -- Submitted to blockchain
    'confirmed', -- Confirmed on blockchain
    'failed', -- Failed execution
    'cancelled' -- Cancelled before submission
);

-- Create enum for transaction type
CREATE TYPE transaction_type AS ENUM (
    'transfer', -- Regular transfer
    'p2p', -- Peer-to-peer transfer
    'batch', -- Batch operation
    'deployment', -- Wallet deployment
    'reward' -- Reward distribution
);

-- Create enum for gas mode
CREATE TYPE gas_mode AS ENUM (
    'sponsored',
    'self'
);

-- Create enum for priority level
CREATE TYPE priority_level AS ENUM (
    'low',
    'normal',
    'high'
);

-- Create transactions table
CREATE TABLE transactions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid (),
    -- Basic transaction info
    user_id uuid NOT NULL, -- Initiating user
    chain_id bigint NOT NULL REFERENCES chains (id),
    transaction_type transaction_type NOT NULL,
    status transaction_status NOT NULL DEFAULT 'pending',
    -- Source and destination
    from_address char(42) NOT NULL, -- Source wallet address
    to_address char(42) NOT NULL, -- Destination address
    to_user_id uuid, -- Destination user (for P2P)
    -- Asset information
    token_id uuid REFERENCES supported_tokens (id),
    contract_address char(42), -- Token contract (NULL for native)
    amount DECIMAL(78, 18) NOT NULL, -- Transfer amount
    amount_usd DECIMAL(20, 8), -- USD value at time of transaction
    -- Transaction details
    memo text, -- Optional memo
    gas_mode gas_mode DEFAULT 'sponsored',
    priority priority_level DEFAULT 'normal',
    -- Blockchain information
    tx_hash char(66), -- Blockchain transaction hash
    block_number bigint, -- Block number
    block_timestamp timestamptz, -- Block timestamp
    gas_price DECIMAL(20, 9), -- Gas price in gwei
    gas_limit integer, -- Gas limit
    gas_used integer, -- Actual gas used
    gas_fee DECIMAL(78, 18), -- Total gas fee in native token
    gas_fee_usd DECIMAL(20, 8), -- Gas fee in USD
    -- Nonce management
    nonce bigint, -- Transaction nonce
    -- Batch information
    batch_id uuid, -- Batch ID if part of batch
    batch_index integer, -- Position in batch
    -- Error handling
    error_message text, -- Error description if failed
    error_code varchar(50), -- Error code for categorization
    retry_count integer DEFAULT 0, -- Number of retry attempts
    max_retries integer DEFAULT 3, -- Maximum retry attempts
    -- Timing information
    submitted_at timestamptz, -- When submitted to blockchain
    confirmed_at timestamptz, -- When confirmed
    estimated_confirmation timestamptz, -- Estimated confirmation time
    -- External references
    external_id varchar(100), -- External system reference
    user_operation_hash char(66), -- EIP-4337 UserOperation hash
    -- Metadata
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    -- Constraints
    CONSTRAINT valid_from_address CHECK (from_address ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT valid_to_address CHECK (to_address ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT valid_contract_address CHECK (contract_address IS NULL OR contract_address ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT valid_tx_hash CHECK (tx_hash IS NULL OR tx_hash ~ '^0x[a-fA-F0-9]{64}$'),
    CONSTRAINT valid_user_op_hash CHECK (user_operation_hash IS NULL OR user_operation_hash ~ '^0x[a-fA-F0-9]{64}$'),
    CONSTRAINT positive_amount CHECK (amount > 0),
    CONSTRAINT valid_retry_count CHECK (retry_count >= 0 AND retry_count <= max_retries)
);

-- Create indexes
CREATE INDEX idx_transactions_user_id ON transactions (user_id);

CREATE INDEX idx_transactions_chain_id ON transactions (chain_id);

CREATE INDEX idx_transactions_status ON transactions (status);

CREATE INDEX idx_transactions_type ON transactions (transaction_type);

CREATE INDEX idx_transactions_from_address ON transactions (from_address);

CREATE INDEX idx_transactions_to_address ON transactions (to_address);

CREATE INDEX idx_transactions_to_user_id ON transactions (to_user_id);

CREATE INDEX idx_transactions_tx_hash ON transactions (tx_hash);

CREATE INDEX idx_transactions_batch_id ON transactions (batch_id);

CREATE INDEX idx_transactions_block_number ON transactions (block_number);

CREATE INDEX idx_transactions_created_at ON transactions (created_at);

CREATE INDEX idx_transactions_submitted_at ON transactions (submitted_at);

CREATE INDEX idx_transactions_confirmed_at ON transactions (confirmed_at);

-- Compound indexes for common queries
CREATE INDEX idx_transactions_user_chain_status ON transactions (user_id, chain_id, status);

CREATE INDEX idx_transactions_user_created ON transactions (user_id, created_at DESC);

CREATE INDEX idx_transactions_chain_block ON transactions (chain_id, block_number);

CREATE INDEX idx_transactions_pending_retry ON transactions (status, retry_count)
WHERE
    status IN ('pending', 'failed') AND retry_count < max_retries;

-- Create trigger for updated_at
CREATE TRIGGER update_transactions_updated_at
    BEFORE UPDATE ON transactions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column ();

-- Create function to update transaction status
CREATE OR REPLACE FUNCTION update_transaction_status (p_transaction_id uuid, p_new_status transaction_status, p_tx_hash char(66) DEFAULT NULL, p_block_number bigint DEFAULT NULL, p_gas_used integer DEFAULT NULL, p_error_message text DEFAULT NULL)
    RETURNS boolean
    AS $$
DECLARE
    current_status transaction_status;
    status_changed boolean := FALSE;
BEGIN
    -- Get current status
    SELECT
        status INTO current_status
    FROM
        transactions
    WHERE
        id = p_transaction_id;
    -- Only update if status actually changes
    IF current_status != p_new_status THEN
        UPDATE
            transactions
        SET
            status = p_new_status,
            tx_hash = COALESCE(p_tx_hash, tx_hash),
            block_number = COALESCE(p_block_number, block_number),
            gas_used = COALESCE(p_gas_used, gas_used),
            error_message = COALESCE(p_error_message, error_message),
            submitted_at = CASE WHEN p_new_status = 'submitted'
                AND submitted_at IS NULL THEN
                NOW()
            ELSE
                submitted_at
            END,
            confirmed_at = CASE WHEN p_new_status = 'confirmed'
                AND confirmed_at IS NULL THEN
                NOW()
            ELSE
                confirmed_at
            END,
            updated_at = NOW()
        WHERE
            id = p_transaction_id;
        status_changed := TRUE;
    END IF;
    RETURN status_changed;
END;
$$
LANGUAGE plpgsql;

-- Create function to get user transaction history
CREATE OR REPLACE FUNCTION get_user_transactions (p_user_id uuid, p_chain_id bigint DEFAULT NULL, p_limit integer DEFAULT 20, p_offset integer DEFAULT 0)
    RETURNS TABLE (
        transaction_id uuid,
        chain_id bigint,
        transaction_type transaction_type,
        status transaction_status,
        from_address char(42),
        to_address char(42),
        amount DECIMAL(78, 18),
        amount_usd DECIMAL(20, 8),
        token_symbol varchar(20),
        tx_hash char(66),
        created_at timestamptz,
        confirmed_at timestamptz
    )
    AS $$
BEGIN
    RETURN QUERY
    SELECT
        t.id,
        t.chain_id,
        t.transaction_type,
        t.status,
        t.from_address,
        t.to_address,
        t.amount,
        t.amount_usd,
        st.symbol,
        t.tx_hash,
        t.created_at,
        t.confirmed_at
    FROM
        transactions t
    LEFT JOIN supported_tokens st ON t.token_id = st.id
WHERE
    t.user_id = p_user_id
        AND (p_chain_id IS NULL
            OR t.chain_id = p_chain_id)
    ORDER BY
        t.created_at DESC
    LIMIT p_limit OFFSET p_offset;
END;
$$
LANGUAGE plpgsql;

-- +migrate Down
DROP TABLE IF EXISTS transactions;

DROP TYPE IF EXISTS transaction_status;

DROP TYPE IF EXISTS transaction_type;

DROP TYPE IF EXISTS gas_mode;

DROP TYPE IF EXISTS priority_level;

DROP FUNCTION IF EXISTS update_transaction_status (uuid, transaction_status, char(66), bigint, integer, text);

DROP FUNCTION IF EXISTS get_user_transactions (uuid, bigint, integer, integer);

