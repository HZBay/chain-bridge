-- +migrate Up
ALTER TABLE transactions
    ADD COLUMN source_record_id varchar(255);

-- 为 source_record_id 创建索引以便快速查询
CREATE INDEX idx_transactions_source_record_id ON transactions USING btree (source_record_id);

-- 添加注释
COMMENT ON COLUMN transactions.source_record_id IS 'Reference ID to the original record in customer database';

-- +migrate Down
ALTER TABLE transactions
    DROP COLUMN source_record_id;

