-- +migrate Up
ALTER TABLE nft_assets
    DROP CONSTRAINT nft_assets_collection_id_token_id_key;

-- +migrate Down
ALTER TABLE nft_assets
    ADD CONSTRAINT nft_assets_collection_id_token_id_key UNIQUE (collection_id, token_id);

