-- +migrate Up
ALTER TABLE nft_assets ADD COLUMN operation_id varchar(36);

CREATE INDEX idx_nft_assets_operation_id ON nft_assets USING btree (operation_id);

-- +migrate Down
DROP INDEX IF EXISTS idx_nft_assets_operation_id;
ALTER TABLE nft_assets DROP COLUMN IF EXISTS operation_id;