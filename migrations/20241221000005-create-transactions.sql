-- +migrate Up
CREATE TYPE tx_type AS ENUM (
    'mint',
    'burn',
    'transfer'
);

CREATE TYPE business_type AS ENUM (
    'transfer',
    'reward',
    'gas_fee',
    'consumption',
    'refund'
);

CREATE TYPE transfer_direction AS ENUM (
    'outgoing',
    'incoming'
);

CREATE TYPE transaction_status AS ENUM (
    'pending',
    'batching',
    'submitted',
    'confirmed',
    'failed'
);

CREATE TABLE transactions (
    id serial PRIMARY KEY,
    tx_id uuid UNIQUE NOT NULL,
    operation_id uuid,
    -- 用户和链信息
    user_id uuid NOT NULL,
    chain_id bigint NOT NULL,
    -- 交易类型
    tx_type tx_type NOT NULL,
    business_type business_type NOT NULL,
    -- 转账相关字段
    related_user_id uuid,
    transfer_direction transfer_direction,
    -- 资产信息
    token_id int NOT NULL,
    amount numeric(36, 18) NOT NULL,
    amount_usd numeric(18, 2),
    -- 区块链状态
    status transaction_status DEFAULT 'pending',
    tx_hash char(66),
    block_number bigint,
    -- 批量处理信息
    batch_id uuid,
    is_batch_operation boolean DEFAULT FALSE,
    gas_saved_percentage numeric(5, 2),
    -- 业务信息
    reason_type varchar(50) NOT NULL,
    reason_detail text,
    metadata jsonb,
    created_at timestamptz DEFAULT NOW(),
    confirmed_at timestamptz,
    FOREIGN KEY (chain_id) REFERENCES chains (chain_id),
    FOREIGN KEY (token_id) REFERENCES supported_tokens (id),
    -- transfer操作的约束检查
    CHECK ((tx_type = 'transfer' AND related_user_id IS NOT NULL AND transfer_direction IS NOT NULL) OR (tx_type != 'transfer' AND related_user_id IS NULL AND transfer_direction IS NULL))
);

CREATE INDEX idx_user_txs ON transactions USING btree (user_id, chain_id, created_at DESC);

CREATE INDEX idx_batch ON transactions USING btree (batch_id);

CREATE INDEX idx_pending_batch ON transactions USING btree (status, is_batch_operation, created_at);

CREATE INDEX idx_operation ON transactions USING btree (operation_id);

CREATE INDEX idx_transactions_fk_chain_id ON transactions USING btree (chain_id);

CREATE INDEX idx_transactions_fk_token_id ON transactions USING btree (token_id);

-- +migrate Down
DROP TABLE IF EXISTS transactions;

DROP TYPE IF EXISTS tx_type;

DROP TYPE IF EXISTS business_type;

DROP TYPE IF EXISTS transfer_direction;

DROP TYPE IF EXISTS transaction_status;

