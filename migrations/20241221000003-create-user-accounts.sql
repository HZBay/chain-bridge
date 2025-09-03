-- +migrate Up
CREATE TABLE user_accounts (
    id serial PRIMARY KEY,
    user_id uuid NOT NULL,
    chain_id bigint NOT NULL,
    aa_address char(42) NOT NULL,
    owner CHAR(42) NOT NULL,
    is_deployed boolean DEFAULT FALSE,
    deployment_tx_hash char(66),
    master_signer char(42),
    created_at timestamptz DEFAULT NOW(),
    UNIQUE (user_id, chain_id),
    FOREIGN KEY (chain_id) REFERENCES chains (chain_id)
);

CREATE INDEX idx_user_chain ON user_accounts USING btree (user_id, chain_id);

CREATE INDEX idx_user_accounts_fk_chain_id ON user_accounts USING btree (chain_id);

-- +migrate Down
DROP TABLE IF EXISTS user_accounts;

