-- +migrate Up
ALTER TABLE nft_assets
    ADD COLUMN individual_operation_id UUID NOT NULL DEFAULT uuid_generate_v4 ();

-- Remove the default after adding the column
ALTER TABLE nft_assets
    ALTER COLUMN individual_operation_id DROP DEFAULT;

-- +migrate Down
ALTER TABLE nft_assets
    DROP COLUMN individual_operation_id;

