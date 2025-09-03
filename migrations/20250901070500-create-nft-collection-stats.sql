-- +migrate Up
CREATE TABLE nft_collection_stats (
    id serial PRIMARY KEY,
    collection_id varchar(100) UNIQUE NOT NULL,
    total_supply integer DEFAULT 0,
    total_owners integer DEFAULT 0,
    floor_price_usd numeric(18, 2),
    market_cap_usd numeric(18, 2),
    volume_24h_usd numeric(18, 2),
    volume_7d_usd numeric(18, 2),
    average_price_24h_usd numeric(18, 2),
    count_24h integer DEFAULT 0,
    count_7d integer DEFAULT 0,
    updated_at timestamptz DEFAULT NOW(),
    FOREIGN KEY (collection_id) REFERENCES nft_collections (collection_id)
);

CREATE INDEX idx_nft_collection_stats_collection_id ON nft_collection_stats USING btree (collection_id);

CREATE INDEX idx_nft_collection_stats_floor_price ON nft_collection_stats USING btree (floor_price_usd);

CREATE INDEX idx_nft_collection_stats_market_cap ON nft_collection_stats USING btree (market_cap_usd);

-- +migrate Down
DROP TABLE IF EXISTS nft_collection_stats;

