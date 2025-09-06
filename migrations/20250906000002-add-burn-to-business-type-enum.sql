-- +migrate Up
-- 添加 "burn" 和 "open_box" 到 business_type 枚举中
ALTER TYPE business_type
    ADD VALUE 'burn';

ALTER TYPE business_type
    ADD VALUE 'open_box';

-- +migrate Down
-- 注意：PostgreSQL 不支持从枚举中删除值，所以这里无法回滚
-- 如果需要回滚，需要重新创建整个枚举类型
-- 这里只添加注释说明
-- ALTER TYPE business_type DROP VALUE 'burn'; -- 不支持的操作
