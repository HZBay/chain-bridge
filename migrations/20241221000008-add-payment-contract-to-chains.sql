-- +migrate Up
-- Add payment contract address to chains table
ALTER TABLE chains
    ADD COLUMN payment_contract_address char(42);

-- Add comment for the new field
COMMENT ON COLUMN chains.payment_contract_address IS 'Payment contract address for PaymentMade event listening';

-- +migrate Down
-- Remove payment contract address column
ALTER TABLE chains
    DROP COLUMN IF EXISTS payment_contract_address;

