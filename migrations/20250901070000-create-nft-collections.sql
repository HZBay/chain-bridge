-- +migrate Up
CREATE TABLE nft_collections (
    id serial PRIMARY KEY,
    collection_id varchar(100) UNIQUE NOT NULL,
    chain_id bigint NOT NULL,
    contract_address char(42) NOT NULL,
    name varchar(255) NOT NULL,
    symbol varchar(50) NOT NULL,
    contract_type varchar(20) NOT NULL CHECK (contract_type IN ('ERC721', 'ERC1155')),
    base_uri text,
    is_enabled boolean DEFAULT TRUE,
    created_at timestamptz DEFAULT NOW(),
    FOREIGN KEY (chain_id) REFERENCES chains (chain_id),
    UNIQUE (chain_id, contract_address)
);

CREATE INDEX idx_nft_collections_chain_id ON nft_collections USING btree (chain_id);

CREATE INDEX idx_nft_collections_contract_address ON nft_collections USING btree (contract_address);

-- +migrate Down
DROP TABLE IF EXISTS nft_collections;

