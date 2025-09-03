-- +migrate Up
-- Add NFT transaction types
ALTER TYPE tx_type
    ADD VALUE 'nft_mint';

ALTER TYPE tx_type
    ADD VALUE 'nft_burn';

ALTER TYPE tx_type
    ADD VALUE 'nft_transfer';

-- Add NFT batch types
ALTER TYPE batch_type
    ADD VALUE 'nft_mint';

ALTER TYPE batch_type
    ADD VALUE 'nft_burn';

ALTER TYPE batch_type
    ADD VALUE 'nft_transfer';

-- Add NFT operation types
ALTER TYPE cpop_operation_type
    ADD VALUE 'batch_nft_mint';

ALTER TYPE cpop_operation_type
    ADD VALUE 'batch_nft_burn';

ALTER TYPE cpop_operation_type
    ADD VALUE 'batch_nft_transfer';

-- +migrate Down
-- Note: PostgreSQL does not support removing values from ENUM types
-- The down migration is intentionally left empty
