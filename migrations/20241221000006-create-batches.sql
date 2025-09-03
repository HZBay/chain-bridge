-- +migrate Up
CREATE TYPE batch_type AS ENUM (
    'mint',
    'burn',
    'transfer'
);

CREATE TYPE network_condition AS ENUM (
    'low',
    'medium',
    'high'
);

CREATE TYPE cpop_operation_type AS ENUM (
    'batch_mint',
    'batch_burn',
    'batch_transfer'
);

CREATE TYPE batch_status AS ENUM (
    'preparing',
    'submitted',
    'confirmed',
    'failed'
);

CREATE TABLE batches (
    id serial PRIMARY KEY,
    batch_id uuid UNIQUE NOT NULL,
    -- 基本信息
    chain_id bigint NOT NULL,
    token_id int NOT NULL,
    batch_type batch_type NOT NULL,
    -- 批量优化信息
    operation_count int NOT NULL,
    optimal_batch_size int NOT NULL,
    actual_efficiency numeric(5, 2),
    batch_strategy varchar(50),
    network_condition network_condition,
    -- Gas分析
    actual_gas_used bigint,
    gas_saved bigint,
    gas_saved_percentage numeric(5, 2),
    gas_saved_usd numeric(10, 2),
    -- CPOP特定
    cpop_operation_type cpop_operation_type,
    master_aggregator_used boolean DEFAULT FALSE,
    -- 状态
    status batch_status DEFAULT 'preparing',
    tx_hash char(66),
    created_at timestamptz DEFAULT NOW(),
    confirmed_at timestamptz,
    FOREIGN KEY (chain_id) REFERENCES chains (chain_id),
    FOREIGN KEY (token_id) REFERENCES supported_tokens (id)
);

CREATE INDEX idx_batch_status ON batches USING btree (status, created_at DESC);

CREATE INDEX idx_efficiency ON batches USING btree (actual_efficiency DESC);

CREATE INDEX idx_batches_fk_chain_id ON batches USING btree (chain_id);

CREATE INDEX idx_batches_fk_token_id ON batches USING btree (token_id);

-- +migrate Down
DROP TABLE IF EXISTS batches;

DROP TYPE IF EXISTS batch_type;

DROP TYPE IF EXISTS network_condition;

DROP TYPE IF EXISTS cpop_operation_type;

DROP TYPE IF EXISTS batch_status;

