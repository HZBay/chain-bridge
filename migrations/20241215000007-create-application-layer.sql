-- +migrate Up
-- ChainBridge: Create application layer tables
-- Support for advanced features like activity management, rewards, etc.
-- Create enum for activity status
CREATE TYPE activity_status AS ENUM (
    'active',
    'completed',
    'cancelled',
    'expired'
);

-- Create enum for activity type
CREATE TYPE activity_type AS ENUM (
    'weekly_reward', -- Weekly reward distribution
    'monthly_bonus', -- Monthly bonus
    'task_completion', -- Task completion rewards
    'referral_bonus', -- Referral bonuses
    'staking_reward', -- Staking rewards
    'custom' -- Custom activity
);

-- Create activities table
CREATE TABLE activities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid (),
    -- Activity identification
    external_id varchar(100) UNIQUE, -- External system ID
    name varchar(200) NOT NULL, -- Activity name
    description text, -- Activity description
    activity_type activity_type NOT NULL,
    status activity_status NOT NULL DEFAULT 'active',
    -- Reward configuration
    chain_id bigint NOT NULL REFERENCES chains (id),
    reward_token_id uuid REFERENCES supported_tokens (id), -- Usually CPOP
    total_reward_amount DECIMAL(78, 18), -- Total rewards allocated
    distributed_amount DECIMAL(78, 18) DEFAULT 0, -- Amount already distributed
    -- Activity period
    start_date timestamptz NOT NULL,
    end_date timestamptz,
    distribution_date timestamptz, -- When rewards should be distributed
    -- Participant criteria
    min_participants integer DEFAULT 1, -- Minimum participants required
    max_participants integer, -- Maximum participants allowed
    current_participants integer DEFAULT 0, -- Current participant count
    -- Auto-processing configuration
    auto_distribute boolean DEFAULT FALSE, -- Automatically distribute rewards
    batch_distribute boolean DEFAULT TRUE, -- Use batch distribution for gas savings
    -- Metadata
    metadata jsonb DEFAULT '{}' ::jsonb, -- Additional activity data
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    completed_at timestamptz,
    -- Constraints
    CONSTRAINT valid_reward_amount CHECK (total_reward_amount IS NULL OR total_reward_amount > 0),
    CONSTRAINT valid_distributed_amount CHECK (distributed_amount >= 0),
    CONSTRAINT valid_participant_limits CHECK (max_participants IS NULL OR max_participants >= min_participants),
    CONSTRAINT valid_dates CHECK (end_date IS NULL OR end_date >= start_date)
);

-- Create activity_participants table
CREATE TABLE activity_participants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid (),
    activity_id uuid NOT NULL REFERENCES activities (id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    -- Participation details
    joined_at timestamptz NOT NULL DEFAULT NOW(),
    completion_date timestamptz,
    is_eligible boolean DEFAULT TRUE, -- Eligible for rewards
    -- Reward information
    reward_amount DECIMAL(78, 18), -- Individual reward amount
    is_distributed boolean DEFAULT FALSE, -- Reward distributed flag
    distribution_tx_id uuid REFERENCES transactions (id), -- Transaction that distributed reward
    -- Performance metrics
    score DECIMAL(10, 2), -- Performance score
    rank integer, -- Rank among participants
    -- Metadata
    metadata jsonb DEFAULT '{}' ::jsonb, -- Participant-specific data
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    -- Constraints
    CONSTRAINT unique_user_activity UNIQUE (activity_id, user_id),
    CONSTRAINT valid_reward_amount CHECK (reward_amount IS NULL OR reward_amount >= 0)
);

-- Create u_card_records table (for U-card charging functionality)
CREATE TABLE u_card_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid (),
    -- User and card information
    user_id uuid NOT NULL,
    card_number varchar(50) NOT NULL, -- U-card number
    card_type varchar(20) DEFAULT 'standard', -- Card type
    -- Transaction details
    chain_id bigint NOT NULL REFERENCES chains (id),
    transaction_id uuid REFERENCES transactions (id), -- Associated blockchain transaction
    -- Amounts
    charge_amount DECIMAL(78, 18) NOT NULL, -- Amount charged to card
    token_id uuid REFERENCES supported_tokens (id), -- Token used for charging
    exchange_rate DECIMAL(20, 8), -- Exchange rate at time of charge
    service_fee DECIMAL(78, 18) DEFAULT 0, -- Service fee charged
    -- Status and processing
    status varchar(20) DEFAULT 'pending', -- pending, processing, completed, failed
    processed_at timestamptz,
    -- External references
    external_transaction_id varchar(100), -- External system transaction ID
    payment_processor varchar(50), -- Payment processor used
    -- Metadata
    metadata jsonb DEFAULT '{}' ::jsonb,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    -- Constraints
    CONSTRAINT positive_charge_amount CHECK (charge_amount > 0),
    CONSTRAINT valid_exchange_rate CHECK (exchange_rate IS NULL OR exchange_rate > 0),
    CONSTRAINT non_negative_service_fee CHECK (service_fee >= 0)
);

-- Create cpot_exchanges table (for CPOT token exchange)
CREATE TABLE cpot_exchanges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid (),
    -- User and exchange details
    user_id uuid NOT NULL,
    chain_id bigint NOT NULL REFERENCES chains (id),
    -- Exchange amounts
    cpop_amount DECIMAL(78, 18) NOT NULL, -- CPOP amount to exchange
    cpot_amount DECIMAL(78, 18) NOT NULL, -- CPOT amount to receive
    exchange_rate DECIMAL(20, 8) NOT NULL, -- CPOP to CPOT rate
    -- Transaction references
    burn_tx_id uuid REFERENCES transactions (id), -- CPOP burn transaction
    mint_tx_id uuid REFERENCES transactions (id), -- CPOT mint transaction
    -- Status and processing
    status varchar(20) DEFAULT 'pending', -- pending, processing, completed, failed
    processed_at timestamptz,
    -- Metadata
    metadata jsonb DEFAULT '{}' ::jsonb,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    -- Constraints
    CONSTRAINT positive_cpop_amount CHECK (cpop_amount > 0),
    CONSTRAINT positive_cpot_amount CHECK (cpot_amount > 0),
    CONSTRAINT positive_exchange_rate CHECK (exchange_rate > 0)
);

-- Create indexes
-- Activities indexes
CREATE INDEX idx_activities_status ON activities (status);

CREATE INDEX idx_activities_type ON activities (activity_type);

CREATE INDEX idx_activities_chain_id ON activities (chain_id);

CREATE INDEX idx_activities_external_id ON activities (external_id);

CREATE INDEX idx_activities_distribution_date ON activities (distribution_date);

CREATE INDEX idx_activities_auto_distribute ON activities (auto_distribute, distribution_date)
WHERE
    auto_distribute = TRUE AND status = 'active';

-- Activity participants indexes
CREATE INDEX idx_activity_participants_activity ON activity_participants (activity_id);

CREATE INDEX idx_activity_participants_user ON activity_participants (user_id);

CREATE INDEX idx_activity_participants_eligible ON activity_participants (activity_id, is_eligible);

CREATE INDEX idx_activity_participants_undistributed ON activity_participants (activity_id, is_distributed)
WHERE
    is_distributed = FALSE;

-- U-card records indexes
CREATE INDEX idx_u_card_records_user_id ON u_card_records (user_id);

CREATE INDEX idx_u_card_records_card_number ON u_card_records (card_number);

CREATE INDEX idx_u_card_records_status ON u_card_records (status);

CREATE INDEX idx_u_card_records_created_at ON u_card_records (created_at);

CREATE INDEX idx_u_card_records_external_tx ON u_card_records (external_transaction_id);

-- CPOT exchanges indexes
CREATE INDEX idx_cpot_exchanges_user_id ON cpot_exchanges (user_id);

CREATE INDEX idx_cpot_exchanges_chain_id ON cpot_exchanges (chain_id);

CREATE INDEX idx_cpot_exchanges_status ON cpot_exchanges (status);

CREATE INDEX idx_cpot_exchanges_created_at ON cpot_exchanges (created_at);

-- GIN indexes for JSONB metadata
CREATE INDEX idx_activities_metadata ON activities USING GIN (metadata);

CREATE INDEX idx_activity_participants_metadata ON activity_participants USING GIN (metadata);

CREATE INDEX idx_u_card_records_metadata ON u_card_records USING GIN (metadata);

CREATE INDEX idx_cpot_exchanges_metadata ON cpot_exchanges USING GIN (metadata);

-- Create triggers for updated_at
CREATE TRIGGER update_activities_updated_at
    BEFORE UPDATE ON activities
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column ();

CREATE TRIGGER update_activity_participants_updated_at
    BEFORE UPDATE ON activity_participants
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column ();

CREATE TRIGGER update_u_card_records_updated_at
    BEFORE UPDATE ON u_card_records
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column ();

CREATE TRIGGER update_cpot_exchanges_updated_at
    BEFORE UPDATE ON cpot_exchanges
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column ();

-- Create function to add participant to activity
CREATE OR REPLACE FUNCTION add_activity_participant (p_activity_id uuid, p_user_id uuid, p_reward_amount DECIMAL(78, 18) DEFAULT NULL)
    RETURNS uuid
    AS $$
DECLARE
    participant_id uuid;
    activity_record RECORD;
BEGIN
    -- Get activity information
    SELECT
        * INTO activity_record
    FROM
        activities
    WHERE
        id = p_activity_id;
    IF NOT FOUND OR activity_record.status != 'active' THEN
        RAISE EXCEPTION 'Activity not found or not active';
    END IF;
    -- Check participant limits
    IF activity_record.max_participants IS NOT NULL AND activity_record.current_participants >= activity_record.max_participants THEN
        RAISE EXCEPTION 'Activity has reached maximum participants';
    END IF;
    -- Add participant
    INSERT INTO activity_participants (activity_id, user_id, reward_amount)
        VALUES (p_activity_id, p_user_id, p_reward_amount)
    RETURNING
        id INTO participant_id;
    -- Update participant count
    UPDATE
        activities
    SET
        current_participants = current_participants + 1,
        updated_at = NOW()
    WHERE
        id = p_activity_id;
    RETURN participant_id;
END;
$$
LANGUAGE plpgsql;

-- Create function to distribute activity rewards
CREATE OR REPLACE FUNCTION distribute_activity_rewards (p_activity_id uuid)
    RETURNS uuid
    AS $$
DECLARE
    activity_record RECORD;
    batch_id uuid;
    total_to_distribute DECIMAL(78, 18) := 0;
BEGIN
    -- Get activity information
    SELECT
        * INTO activity_record
    FROM
        activities
    WHERE
        id = p_activity_id;
    IF NOT FOUND OR activity_record.status != 'active' THEN
        RAISE EXCEPTION 'Activity not found or not active';
    END IF;
    -- Calculate total to distribute
    SELECT
        COALESCE(SUM(reward_amount), 0) INTO total_to_distribute
    FROM
        activity_participants
    WHERE
        activity_id = p_activity_id
        AND is_eligible = TRUE
        AND is_distributed = FALSE
        AND reward_amount > 0;
    -- Create batch for reward distribution
    INSERT INTO batches (chain_id, batch_type, activity_id, activity_type)
        VALUES (activity_record.chain_id, 'reward', p_activity_id::text, activity_record.activity_type::text)
    RETURNING
        id INTO batch_id;
    -- Create transactions for each eligible participant
    INSERT INTO transactions (user_id, chain_id, transaction_type, from_address, to_address, token_id, amount, batch_id, status, memo)
    SELECT
        ap.user_id,
        activity_record.chain_id,
        'reward',
        '0x0000000000000000000000000000000000000000', -- System address
        uw.address,
        activity_record.reward_token_id,
        ap.reward_amount,
        batch_id,
        'pending',
        'Activity reward: ' || activity_record.name
    FROM
        activity_participants ap
        JOIN user_wallets uw ON (ap.user_id = uw.user_id
                AND uw.chain_id = activity_record.chain_id)
    WHERE
        ap.activity_id = p_activity_id
        AND ap.is_eligible = TRUE
        AND ap.is_distributed = FALSE
        AND ap.reward_amount > 0
        AND uw.status = 'deployed';
    -- Update batch operation count
    UPDATE
        batches
    SET
        current_operations = (
            SELECT
                COUNT(*)
            FROM
                transactions
            WHERE
                batch_id = batches.id)
    WHERE
        id = batch_id;
    -- Update activity distributed amount
    UPDATE
        activities
    SET
        distributed_amount = distributed_amount + total_to_distribute,
        updated_at = NOW()
    WHERE
        id = p_activity_id;
    RETURN batch_id;
END;
$$
LANGUAGE plpgsql;

-- +migrate Down
DROP TABLE IF EXISTS cpot_exchanges;

DROP TABLE IF EXISTS u_card_records;

DROP TABLE IF EXISTS activity_participants;

DROP TABLE IF EXISTS activities;

DROP TYPE IF EXISTS activity_status;

DROP TYPE IF EXISTS activity_type;

DROP FUNCTION IF EXISTS add_activity_participant (uuid, uuid, DECIMAL(78, 18));

DROP FUNCTION IF EXISTS distribute_activity_rewards (uuid);

