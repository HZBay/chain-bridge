-- +migrate Up
CREATE TYPE token_type AS ENUM (
    'native',
    'erc20'
);

CREATE TABLE supported_tokens (
    id serial PRIMARY KEY,
    chain_id bigint NOT NULL,
    contract_address char(42),
    symbol varchar(20) NOT NULL,
    name varchar(100) NOT NULL,
    decimals int NOT NULL,
    -- CPOP功能支持
    token_type token_type NOT NULL,
    supports_batch_operations boolean DEFAULT FALSE,
    batch_operations jsonb,
    is_enabled boolean DEFAULT TRUE,
    created_at timestamptz DEFAULT NOW(),
    UNIQUE (chain_id, contract_address),
    FOREIGN KEY (chain_id) REFERENCES chains (chain_id)
);

-- +migrate Down
DROP TABLE IF EXISTS supported_tokens;

DROP TYPE IF EXISTS token_type;

