-- +migrate Up
CREATE TABLE nft_price_history (
    id serial PRIMARY KEY,
    collection_id varchar(100) NOT NULL,
    token_id varchar(78),
    price_usd numeric(18, 2),
    price_source varchar(50),
    recorded_at timestamptz DEFAULT NOW(),
    FOREIGN KEY (collection_id) REFERENCES nft_collections (collection_id)
);

CREATE INDEX idx_nft_price_history_collection_id ON nft_price_history USING btree (collection_id);

CREATE INDEX idx_nft_price_history_token_id ON nft_price_history USING btree (token_id);

CREATE INDEX idx_nft_price_history_recorded_at ON nft_price_history USING btree (recorded_at);

-- +migrate Down
DROP TABLE IF EXISTS nft_price_history;

