-- +migrate Up
-- 添加复合索引优化NFT token ID更新查询性能
-- 这个索引专门针对 WHERE tx_id = ? AND collection_id = ? AND token_id = '-1' 的查询条件
CREATE INDEX IF NOT EXISTS idx_nft_assets_update_lookup ON nft_assets (tx_id, collection_id, token_id)
WHERE
    token_id = '-1';

-- 添加类似的索引用于transactions表的更新
CREATE INDEX IF NOT EXISTS idx_transactions_nft_token_update ON transactions (tx_id, collection_id, nft_token_id)
WHERE
    nft_token_id = '-1';

-- +migrate Down
-- 删除添加的索引
DROP INDEX IF EXISTS idx_nft_assets_update_lookup;

DROP INDEX IF EXISTS idx_transactions_nft_token_update;

