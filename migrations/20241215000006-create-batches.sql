-- +migrate Up
-- ChainBridge: Create batches table
-- Tracks batch operations for gas optimization
-- Create enum for batch status
CREATE TYPE batch_status AS ENUM (
    'collecting', -- Collecting transactions for batch
    'ready', -- Ready for execution
    'executing', -- Currently executing
    'completed', -- Successfully completed
    'failed', -- Failed execution
    'cancelled' -- Cancelled before execution
);

-- Create enum for batch type
CREATE TYPE batch_type AS ENUM (
    'transfer', -- Regular transfer batch
    'reward', -- Reward distribution batch
    'deployment', -- Wallet deployment batch
    'mixed' -- Mixed operations
);

-- Create batches table
CREATE TABLE batches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid (),
    -- Batch identification
    chain_id bigint NOT NULL REFERENCES chains (id),
    batch_type batch_type NOT NULL,
    status batch_status NOT NULL DEFAULT 'collecting',
    -- Batch configuration
    max_operations integer DEFAULT 50, -- Maximum operations in batch
    current_operations integer DEFAULT 0, -- Current number of operations
    max_wait_time interval DEFAULT '15 seconds', -- Maximum wait time before execution
    priority_threshold integer DEFAULT 10, -- High priority operation count for immediate execution
    -- Execution information
    executor_address char(42), -- Address executing the batch
    tx_hash char(66), -- Batch transaction hash
    block_number bigint, -- Block number of execution
    -- Gas information
    estimated_gas bigint, -- Estimated gas for batch
    gas_limit bigint, -- Gas limit set
    gas_used bigint, -- Actual gas used
    gas_price DECIMAL(20, 9), -- Gas price in gwei
    gas_fee DECIMAL(78, 18), -- Total gas fee
    gas_savings_percent DECIMAL(5, 2), -- Gas savings percentage vs individual txs
    -- Cost analysis
    individual_cost DECIMAL(78, 18), -- Cost if executed individually
    batch_cost DECIMAL(78, 18), -- Actual batch cost
    savings_amount DECIMAL(78, 18), -- Amount saved
    -- Timing information
    collection_started_at timestamptz DEFAULT NOW(),
    collection_ended_at timestamptz,
    execution_started_at timestamptz,
    execution_completed_at timestamptz,
    -- Error handling
    error_message text, -- Error description if failed
    error_code varchar(50), -- Error code
    retry_count integer DEFAULT 0, -- Retry attempts
    max_retries integer DEFAULT 2, -- Maximum retries
    -- Activity tracking (for application layer)
    activity_id varchar(100), -- Associated activity ID
    activity_type varchar(50), -- Activity type (weekly_reward, etc.)
    -- Metadata
    metadata jsonb DEFAULT '{}' ::jsonb, -- Additional metadata
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    -- Constraints
    CONSTRAINT valid_executor_address CHECK (executor_address IS NULL OR executor_address ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT valid_tx_hash CHECK (tx_hash IS NULL OR tx_hash ~ '^0x[a-fA-F0-9]{64}$'),
    CONSTRAINT valid_operation_counts CHECK (current_operations >= 0 AND current_operations <= max_operations),
    CONSTRAINT valid_gas_savings CHECK (gas_savings_percent IS NULL OR (gas_savings_percent >= 0 AND gas_savings_percent <= 100))
);

-- Create indexes
CREATE INDEX idx_batches_chain_id ON batches (chain_id);

CREATE INDEX idx_batches_status ON batches (status);

CREATE INDEX idx_batches_type ON batches (batch_type);

CREATE INDEX idx_batches_tx_hash ON batches (tx_hash);

CREATE INDEX idx_batches_activity ON batches (activity_id);

CREATE INDEX idx_batches_created_at ON batches (created_at);

CREATE INDEX idx_batches_collection_started ON batches (collection_started_at);

-- Compound indexes for batch processing queries
CREATE INDEX idx_batches_ready_execution ON batches (chain_id, status, collection_started_at)
WHERE
    status IN ('ready', 'collecting');

CREATE INDEX idx_batches_executing ON batches (status, execution_started_at)
WHERE
    status = 'executing';

-- GIN index for metadata JSONB
CREATE INDEX idx_batches_metadata ON batches USING GIN (metadata);

-- Create trigger for updated_at
CREATE TRIGGER update_batches_updated_at
    BEFORE UPDATE ON batches
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column ();

-- Create function to check if batch should be executed
CREATE OR REPLACE FUNCTION should_execute_batch (p_batch_id uuid)
    RETURNS boolean
    AS $$
DECLARE
    batch_record RECORD;
    high_priority_count integer;
    time_elapsed interval;
BEGIN
    -- Get batch information
    SELECT
        * INTO batch_record
    FROM
        batches
    WHERE
        id = p_batch_id
        AND status = 'collecting';
    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;
    -- Check if batch is full
    IF batch_record.current_operations >= batch_record.max_operations THEN
        RETURN TRUE;
    END IF;
    -- Check time limit
    time_elapsed := NOW() - batch_record.collection_started_at;
    IF time_elapsed >= batch_record.max_wait_time THEN
        RETURN TRUE;
    END IF;
    -- Check high priority operations
    SELECT
        COUNT(*) INTO high_priority_count
    FROM
        transactions
    WHERE
        batch_id = p_batch_id
        AND priority = 'high'
        AND status = 'pending';
    IF high_priority_count >= batch_record.priority_threshold THEN
        RETURN TRUE;
    END IF;
    RETURN FALSE;
END;
$$
LANGUAGE plpgsql;

-- Create function to add transaction to batch
CREATE OR REPLACE FUNCTION add_to_batch (p_transaction_id uuid, p_chain_id bigint, p_batch_type batch_type DEFAULT 'transfer')
    RETURNS uuid
    AS $$
DECLARE
    target_batch_id uuid;
    batch_full boolean := FALSE;
BEGIN
    -- Find an available batch or create new one
    SELECT
        id INTO target_batch_id
    FROM
        batches
    WHERE
        chain_id = p_chain_id
        AND batch_type = p_batch_type
        AND status = 'collecting'
        AND current_operations < max_operations
    ORDER BY
        collection_started_at ASC
    LIMIT 1;
    -- Create new batch if none available
    IF target_batch_id IS NULL THEN
        INSERT INTO batches (chain_id, batch_type, status)
            VALUES (p_chain_id, p_batch_type, 'collecting')
        RETURNING
            id INTO target_batch_id;
    END IF;
    -- Add transaction to batch
    UPDATE
        transactions
    SET
        batch_id = target_batch_id,
        batch_index = (
            SELECT
                COALESCE(MAX(batch_index), -1) + 1
            FROM
                transactions
            WHERE
                batch_id = target_batch_id)
    WHERE
        id = p_transaction_id;
    -- Update batch operation count
    UPDATE
        batches
    SET
        current_operations = current_operations + 1,
        updated_at = NOW()
    WHERE
        id = target_batch_id;
    -- Check if batch should be marked as ready
    IF should_execute_batch (target_batch_id) THEN
        UPDATE
            batches
        SET
            status = 'ready',
            collection_ended_at = NOW(),
            updated_at = NOW()
        WHERE
            id = target_batch_id;
    END IF;
    RETURN target_batch_id;
END;
$$
LANGUAGE plpgsql;

-- Create function to calculate gas savings
CREATE OR REPLACE FUNCTION calculate_batch_savings (p_batch_id uuid)
    RETURNS VOID
    AS $$
DECLARE
    batch_record RECORD;
    operation_count integer;
    individual_gas bigint;
    batch_gas bigint;
    savings_percent DECIMAL(5, 2);
BEGIN
    SELECT
        * INTO batch_record
    FROM
        batches
    WHERE
        id = p_batch_id;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    -- Count operations in batch
    SELECT
        COUNT(*) INTO operation_count
    FROM
        transactions
    WHERE
        batch_id = p_batch_id;
    -- Calculate individual gas cost (estimate)
    individual_gas := operation_count * 56000;
    -- Average gas per transfer
    -- Use actual gas used if available, otherwise estimate
    batch_gas := COALESCE(batch_record.gas_used, operation_count * 25000 + 50000);
    -- Calculate savings percentage
    IF individual_gas > 0 THEN
        savings_percent := ((individual_gas - batch_gas)::DECIMAL / individual_gas) * 100;
    ELSE
        savings_percent := 0;
    END IF;
    -- Update batch with savings information
    UPDATE
        batches
    SET
        gas_savings_percent = savings_percent,
        individual_cost = individual_gas,
        batch_cost = batch_gas,
        savings_amount = individual_gas - batch_gas,
        updated_at = NOW()
    WHERE
        id = p_batch_id;
END;
$$
LANGUAGE plpgsql;

-- Create function to get batch statistics
CREATE OR REPLACE FUNCTION get_batch_stats (p_chain_id bigint DEFAULT NULL, p_days integer DEFAULT 7)
    RETURNS TABLE (
        total_batches bigint,
        total_operations bigint,
        avg_operations_per_batch DECIMAL(10, 2),
        avg_gas_savings_percent DECIMAL(5, 2),
        total_gas_saved bigint
    )
    AS $$
BEGIN
    RETURN QUERY
    SELECT
        COUNT(*)::bigint,
        SUM(current_operations)::bigint,
        AVG(current_operations)::DECIMAL(10, 2),
        AVG(gas_savings_percent)::DECIMAL(5, 2),
        SUM(savings_amount)::bigint
    FROM
        batches
    WHERE
        status = 'completed'
        AND created_at >= NOW() - (p_days || ' days')::interval
        AND (p_chain_id IS NULL
            OR chain_id = p_chain_id);
END;
$$
LANGUAGE plpgsql;

-- +migrate Down
DROP TABLE IF EXISTS batches;

DROP TYPE IF EXISTS batch_status;

DROP TYPE IF EXISTS batch_type;

DROP FUNCTION IF EXISTS should_execute_batch (uuid);

DROP FUNCTION IF EXISTS add_to_batch (uuid, bigint, batch_type);

DROP FUNCTION IF EXISTS calculate_batch_savings (uuid);

DROP FUNCTION IF EXISTS get_batch_stats (bigint, integer);

