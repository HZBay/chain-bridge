-- +migrate Up
ALTER TABLE transactions
    ADD COLUMN individual_operation_id UUID NOT NULL DEFAULT uuid_generate_v4 ();

-- Remove the default after adding the column
ALTER TABLE transactions
    ALTER COLUMN individual_operation_id DROP DEFAULT;

-- +migrate Down
ALTER TABLE transactions
    DROP COLUMN individual_operation_id;

