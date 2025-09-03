-- +migrate Up
CREATE TABLE user_balances (
    id serial PRIMARY KEY,
    user_id uuid NOT NULL,
    chain_id bigint NOT NULL,
    token_id int NOT NULL,
    -- 余额状态
    confirmed_balance numeric(36, 18) DEFAULT 0,
    pending_balance numeric(36, 18) DEFAULT 0,
    locked_balance numeric(36, 18) DEFAULT 0,
    -- 同步状态
    last_sync_time timestamptz,
    last_change_time timestamptz DEFAULT NOW(),
    created_at timestamptz DEFAULT NOW(),
    updated_at timestamptz DEFAULT NOW(),
    UNIQUE (user_id, chain_id, token_id),
    FOREIGN KEY (chain_id) REFERENCES chains (chain_id),
    FOREIGN KEY (token_id) REFERENCES supported_tokens (id),
    CHECK (confirmed_balance >= 0),
    CHECK (pending_balance >= 0)
);

CREATE INDEX idx_user_balances_fk_chain_id ON user_balances USING btree (chain_id);

CREATE INDEX idx_user_balances_fk_token_id ON user_balances USING btree (token_id);

-- +migrate Down
DROP TABLE IF EXISTS user_balances;

