-- +migrate Up
ALTER TABLE transactions
    ALTER COLUMN individual_operation_id SET DEFAULT uuid_generate_v4 ();

ALTER TABLE nft_assets
    ALTER COLUMN individual_operation_id SET DEFAULT uuid_generate_v4 ();

-- +migrate Down
ALTER TABLE transactions
    ALTER COLUMN individual_operation_id DROP DEFAULT;

ALTER TABLE nft_assets
    ALTER COLUMN individual_operation_id DROP DEFAULT;

