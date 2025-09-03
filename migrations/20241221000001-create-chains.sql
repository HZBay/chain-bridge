-- +migrate Up
CREATE TABLE chains (
    chain_id bigint PRIMARY KEY,
    name varchar(50) NOT NULL,
    short_name varchar(10) NOT NULL,
    rpc_url varchar(255) NOT NULL,
    explorer_url varchar(255),
    -- CPOP相关配置
    entry_point_address char(42),
    cpop_token_address char(42),
    master_aggregator_address char(42),
    account_manager_address char(42),
    -- 批量优化配置
    optimal_batch_size int DEFAULT 25,
    max_batch_size int DEFAULT 40,
    min_batch_size int DEFAULT 10,
    is_enabled boolean DEFAULT TRUE,
    created_at timestamptz DEFAULT NOW()
);

-- +migrate Down
DROP TABLE IF EXISTS chains;

