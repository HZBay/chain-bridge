-- +migrate Up
CREATE TABLE nft_assets (
    id serial PRIMARY KEY,
    collection_id varchar(100) NOT NULL,
    token_id varchar(78) NOT NULL,
    owner_user_id text NOT NULL,
    chain_id bigint NOT NULL,
    metadata_uri text,
    name varchar(255),
    description text,
    image_url text,
    attributes jsonb,
    -- 是否已经销毁
    is_burned boolean DEFAULT FALSE,
    -- 是否已经上链
    is_minted boolean DEFAULT FALSE,
    -- 是否被锁住，burn、transfer过程中要锁住
    is_locked boolean DEFAULT FALSE,
    created_at timestamptz DEFAULT NOW(),
    updated_at timestamptz DEFAULT NOW(),
    FOREIGN KEY (collection_id) REFERENCES nft_collections (collection_id),
    UNIQUE (collection_id, token_id)
);

CREATE INDEX idx_nft_assets_collection_id ON nft_assets USING btree (collection_id);

CREATE INDEX idx_nft_assets_owner_user_id ON nft_assets USING btree (owner_user_id);

CREATE INDEX idx_nft_assets_chain_id ON nft_assets USING btree (chain_id);

CREATE INDEX idx_nft_assets_is_burned ON nft_assets USING btree (is_burned);

CREATE INDEX idx_nft_assets_token_id ON nft_assets USING btree (token_id);

-- +migrate Down
DROP TABLE IF EXISTS nft_assets;

