# ChainBridge 数据库设计文档

## 📋 目录

- [数据库概述](#数据库概述)
- [表结构设计](#表结构设计)
- [索引设计](#索引设计)
- [关系设计](#关系设计)
- [数据迁移](#数据迁移)
- [性能优化](#性能优化)
- [备份和恢复](#备份和恢复)

---

## 🎯 数据库概述

### 技术选型

- **主数据库**: PostgreSQL 15+
- **缓存数据库**: Redis 7+
- **消息队列**: RabbitMQ 3.12+
- **ORM 工具**: SQLBoiler v4.x
- **迁移工具**: sql-migrate

### 设计原则

1. **规范化设计**: 遵循第三范式，减少数据冗余
2. **性能优先**: 合理设计索引，优化查询性能
3. **扩展性**: 支持水平扩展和分片
4. **一致性**: 保证数据一致性和完整性
5. **安全性**: 敏感数据加密存储

---

## 🗄️ 表结构设计

### 核心业务表

#### 1. 用户表 (users)
```sql
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    phone VARCHAR(20),
    is_active BOOLEAN DEFAULT TRUE,
    is_verified BOOLEAN DEFAULT FALSE,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 索引
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_created_at ON users(created_at);
CREATE INDEX idx_users_is_active ON users(is_active);
```

#### 2. 区块链配置表 (chains)
```sql
CREATE TABLE chains (
    id SERIAL PRIMARY KEY,
    chain_id BIGINT UNIQUE NOT NULL,
    name VARCHAR(100) NOT NULL,
    display_name VARCHAR(100) NOT NULL,
    rpc_url VARCHAR(500) NOT NULL,
    explorer_url VARCHAR(500),
    native_currency VARCHAR(10) DEFAULT 'ETH',
    block_time INTEGER DEFAULT 12, -- 秒
    confirmation_blocks INTEGER DEFAULT 12,
    is_enabled BOOLEAN DEFAULT TRUE,
    is_testnet BOOLEAN DEFAULT FALSE,
    
    -- CPOP 合约配置
    cpop_token_address CHAR(42),
    wallet_manager_address CHAR(42),
    master_aggregator_address CHAR(42),
    gas_paymaster_address CHAR(42),
    gas_oracle_address CHAR(42),
    session_key_manager_address CHAR(42),
    
    -- NFT 合约配置
    official_nft_contract_address CHAR(42),
    
    -- 批处理配置
    optimal_batch_size INTEGER DEFAULT 25,
    max_batch_size INTEGER DEFAULT 50,
    batch_timeout_seconds INTEGER DEFAULT 300,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 索引
CREATE INDEX idx_chains_chain_id ON chains(chain_id);
CREATE INDEX idx_chains_is_enabled ON chains(is_enabled);
CREATE INDEX idx_chains_is_testnet ON chains(is_testnet);
```

#### 3. 支持的代币表 (supported_tokens)
```sql
CREATE TABLE supported_tokens (
    id SERIAL PRIMARY KEY,
    chain_id BIGINT NOT NULL REFERENCES chains(chain_id),
    symbol VARCHAR(20) NOT NULL,
    name VARCHAR(100) NOT NULL,
    contract_address CHAR(42) NOT NULL,
    decimals INTEGER NOT NULL DEFAULT 18,
    logo_url VARCHAR(500),
    is_native BOOLEAN DEFAULT FALSE,
    is_enabled BOOLEAN DEFAULT TRUE,
    price_usd DECIMAL(20, 8),
    price_updated_at TIMESTAMPTZ,
    
    -- 批处理配置
    optimal_batch_size INTEGER DEFAULT 25,
    max_batch_size INTEGER DEFAULT 50,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    UNIQUE(chain_id, symbol),
    UNIQUE(chain_id, contract_address)
);

-- 索引
CREATE INDEX idx_supported_tokens_chain_id ON supported_tokens(chain_id);
CREATE INDEX idx_supported_tokens_symbol ON supported_tokens(symbol);
CREATE INDEX idx_supported_tokens_is_enabled ON supported_tokens(is_enabled);
CREATE INDEX idx_supported_tokens_is_native ON supported_tokens(is_native);
```

#### 4. 用户账户表 (user_accounts)
```sql
CREATE TABLE user_accounts (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    chain_id BIGINT NOT NULL REFERENCES chains(chain_id),
    aa_address CHAR(42) NOT NULL,
    owner CHAR(42) NOT NULL,
    is_deployed BOOLEAN DEFAULT FALSE,
    deployment_tx_hash CHAR(66),
    master_signer CHAR(42),
    deployment_gas_used BIGINT,
    deployment_gas_price BIGINT,
    deployment_cost_eth DECIMAL(20, 8),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    UNIQUE(user_id, chain_id),
    UNIQUE(chain_id, aa_address)
);

-- 索引
CREATE INDEX idx_user_accounts_user_id ON user_accounts(user_id);
CREATE INDEX idx_user_accounts_chain_id ON user_accounts(chain_id);
CREATE INDEX idx_user_accounts_aa_address ON user_accounts(aa_address);
CREATE INDEX idx_user_accounts_is_deployed ON user_accounts(is_deployed);
CREATE INDEX idx_user_accounts_owner ON user_accounts(owner);
```

#### 5. 用户余额表 (user_balances)
```sql
CREATE TABLE user_balances (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    chain_id BIGINT NOT NULL REFERENCES chains(chain_id),
    token_id INTEGER NOT NULL REFERENCES supported_tokens(id),
    
    -- 余额信息
    confirmed_balance DECIMAL(30, 18) DEFAULT 0,
    pending_balance DECIMAL(30, 18) DEFAULT 0,
    locked_balance DECIMAL(30, 18) DEFAULT 0,
    
    -- 同步信息
    last_sync_at TIMESTAMPTZ,
    sync_status VARCHAR(20) DEFAULT 'synced', -- synced, syncing, pending, error
    sync_error TEXT,
    
    -- 价格信息
    balance_usd DECIMAL(20, 8) DEFAULT 0,
    price_usd DECIMAL(20, 8),
    price_updated_at TIMESTAMPTZ,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    UNIQUE(user_id, chain_id, token_id)
);

-- 索引
CREATE INDEX idx_user_balances_user_id ON user_balances(user_id);
CREATE INDEX idx_user_balances_chain_id ON user_balances(chain_id);
CREATE INDEX idx_user_balances_token_id ON user_balances(token_id);
CREATE INDEX idx_user_balances_sync_status ON user_balances(sync_status);
CREATE INDEX idx_user_balances_last_sync_at ON user_balances(last_sync_at);
```

#### 6. 交易记录表 (transactions)
```sql
CREATE TABLE transactions (
    id SERIAL PRIMARY KEY,
    tx_id VARCHAR(100) UNIQUE NOT NULL,
    operation_id VARCHAR(100) UNIQUE NOT NULL,
    
    -- 用户信息
    user_id UUID NOT NULL REFERENCES users(id),
    chain_id BIGINT NOT NULL REFERENCES chains(chain_id),
    token_id INTEGER NOT NULL REFERENCES supported_tokens(id),
    
    -- 交易信息
    tx_type VARCHAR(50) NOT NULL, -- transfer, mint, burn, deploy
    from_user_id UUID REFERENCES users(id),
    to_user_id UUID REFERENCES users(id),
    amount DECIMAL(30, 18) NOT NULL,
    
    -- 区块链信息
    tx_hash CHAR(66),
    block_number BIGINT,
    block_hash CHAR(66),
    gas_used BIGINT,
    gas_price BIGINT,
    gas_cost_eth DECIMAL(20, 8),
    
    -- 状态信息
    status VARCHAR(20) DEFAULT 'pending', -- pending, processing, completed, failed
    confirmation_count INTEGER DEFAULT 0,
    required_confirmations INTEGER DEFAULT 12,
    
    -- 业务信息
    business_type VARCHAR(50), -- reward, gas_fee, consumption, refund
    reason_type VARCHAR(100),
    reason_detail TEXT,
    
    -- 错误信息
    failure_reason TEXT,
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    
    -- 时间信息
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ
);

-- 索引
CREATE INDEX idx_transactions_tx_id ON transactions(tx_id);
CREATE INDEX idx_transactions_operation_id ON transactions(operation_id);
CREATE INDEX idx_transactions_user_id ON transactions(user_id);
CREATE INDEX idx_transactions_chain_id ON transactions(chain_id);
CREATE INDEX idx_transactions_token_id ON transactions(token_id);
CREATE INDEX idx_transactions_tx_type ON transactions(tx_type);
CREATE INDEX idx_transactions_status ON transactions(status);
CREATE INDEX idx_transactions_created_at ON transactions(created_at);
CREATE INDEX idx_transactions_tx_hash ON transactions(tx_hash);
CREATE INDEX idx_transactions_business_type ON transactions(business_type);
```

#### 7. 批处理表 (batches)
```sql
CREATE TABLE batches (
    id SERIAL PRIMARY KEY,
    batch_id VARCHAR(100) UNIQUE NOT NULL,
    chain_id BIGINT NOT NULL REFERENCES chains(chain_id),
    token_id INTEGER NOT NULL REFERENCES supported_tokens(id),
    
    -- 批处理信息
    batch_type VARCHAR(50) NOT NULL, -- batchMint, batchBurn, batchTransfer
    transaction_count INTEGER NOT NULL,
    total_amount DECIMAL(30, 18) NOT NULL,
    
    -- 区块链信息
    tx_hash CHAR(66),
    block_number BIGINT,
    gas_used BIGINT,
    gas_price BIGINT,
    gas_cost_eth DECIMAL(20, 8),
    
    -- 状态信息
    status VARCHAR(20) DEFAULT 'pending', -- pending, processing, completed, failed
    confirmation_count INTEGER DEFAULT 0,
    required_confirmations INTEGER DEFAULT 12,
    
    -- 性能信息
    gas_savings_percentage DECIMAL(5, 2),
    gas_savings_usd DECIMAL(20, 8),
    processing_time_ms INTEGER,
    
    -- 错误信息
    failure_reason TEXT,
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    
    -- 时间信息
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ
);

-- 索引
CREATE INDEX idx_batches_batch_id ON batches(batch_id);
CREATE INDEX idx_batches_chain_id ON batches(chain_id);
CREATE INDEX idx_batches_token_id ON batches(token_id);
CREATE INDEX idx_batches_batch_type ON batches(batch_type);
CREATE INDEX idx_batches_status ON batches(status);
CREATE INDEX idx_batches_created_at ON batches(created_at);
CREATE INDEX idx_batches_tx_hash ON batches(tx_hash);
```

### NFT 相关表

#### 8. NFT 集合表 (nft_collections)
```sql
CREATE TABLE nft_collections (
    id SERIAL PRIMARY KEY,
    chain_id BIGINT NOT NULL REFERENCES chains(chain_id),
    contract_address CHAR(42) NOT NULL,
    name VARCHAR(200) NOT NULL,
    symbol VARCHAR(50) NOT NULL,
    description TEXT,
    image_url VARCHAR(500),
    banner_url VARCHAR(500),
    website_url VARCHAR(500),
    twitter_url VARCHAR(500),
    discord_url VARCHAR(500),
    
    -- 集合统计
    total_supply BIGINT DEFAULT 0,
    minted_count BIGINT DEFAULT 0,
    floor_price_eth DECIMAL(20, 8),
    floor_price_usd DECIMAL(20, 8),
    volume_24h_eth DECIMAL(20, 8),
    volume_24h_usd DECIMAL(20, 8),
    
    -- 状态信息
    is_verified BOOLEAN DEFAULT FALSE,
    is_enabled BOOLEAN DEFAULT TRUE,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    UNIQUE(chain_id, contract_address)
);

-- 索引
CREATE INDEX idx_nft_collections_chain_id ON nft_collections(chain_id);
CREATE INDEX idx_nft_collections_contract_address ON nft_collections(contract_address);
CREATE INDEX idx_nft_collections_is_verified ON nft_collections(is_verified);
CREATE INDEX idx_nft_collections_is_enabled ON nft_collections(is_enabled);
```

#### 9. NFT 资产表 (nft_assets)
```sql
CREATE TABLE nft_assets (
    id SERIAL PRIMARY KEY,
    collection_id INTEGER NOT NULL REFERENCES nft_collections(id),
    token_id VARCHAR(100) NOT NULL,
    owner_user_id UUID REFERENCES users(id),
    owner_address CHAR(42),
    
    -- 元数据
    name VARCHAR(200),
    description TEXT,
    image_url VARCHAR(500),
    animation_url VARCHAR(500),
    external_url VARCHAR(500),
    attributes JSONB,
    
    -- 价格信息
    last_sale_price_eth DECIMAL(20, 8),
    last_sale_price_usd DECIMAL(20, 8),
    last_sale_at TIMESTAMPTZ,
    
    -- 状态信息
    is_burned BOOLEAN DEFAULT FALSE,
    is_listed BOOLEAN DEFAULT FALSE,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    UNIQUE(collection_id, token_id)
);

-- 索引
CREATE INDEX idx_nft_assets_collection_id ON nft_assets(collection_id);
CREATE INDEX idx_nft_assets_token_id ON nft_assets(token_id);
CREATE INDEX idx_nft_assets_owner_user_id ON nft_assets(owner_user_id);
CREATE INDEX idx_nft_assets_owner_address ON nft_assets(owner_address);
CREATE INDEX idx_nft_assets_is_burned ON nft_assets(is_burned);
CREATE INDEX idx_nft_assets_is_listed ON nft_assets(is_listed);
```

### 系统管理表

#### 10. 访问令牌表 (access_tokens)
```sql
CREATE TABLE access_tokens (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    token VARCHAR(255) UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    is_revoked BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);

-- 索引
CREATE INDEX idx_access_tokens_user_id ON access_tokens(user_id);
CREATE INDEX idx_access_tokens_token ON access_tokens(token);
CREATE INDEX idx_access_tokens_expires_at ON access_tokens(expires_at);
CREATE INDEX idx_access_tokens_is_revoked ON access_tokens(is_revoked);
```

#### 11. 刷新令牌表 (refresh_tokens)
```sql
CREATE TABLE refresh_tokens (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    token VARCHAR(255) UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    is_revoked BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 索引
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_token ON refresh_tokens(token);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);
CREATE INDEX idx_refresh_tokens_is_revoked ON refresh_tokens(is_revoked);
```

---

## 🔍 索引设计

### 主键索引
所有表都使用主键索引，通常为自增 ID 或 UUID。

### 唯一索引
```sql
-- 用户邮箱唯一索引
CREATE UNIQUE INDEX idx_users_email_unique ON users(email);

-- 链 ID 唯一索引
CREATE UNIQUE INDEX idx_chains_chain_id_unique ON chains(chain_id);

-- 用户账户唯一索引
CREATE UNIQUE INDEX idx_user_accounts_user_chain_unique ON user_accounts(user_id, chain_id);

-- 用户余额唯一索引
CREATE UNIQUE INDEX idx_user_balances_user_chain_token_unique ON user_balances(user_id, chain_id, token_id);
```

### 复合索引
```sql
-- 交易查询复合索引
CREATE INDEX idx_transactions_user_chain_status ON transactions(user_id, chain_id, status);
CREATE INDEX idx_transactions_chain_token_created ON transactions(chain_id, token_id, created_at);
CREATE INDEX idx_transactions_business_type_created ON transactions(business_type, created_at);

-- 批处理查询复合索引
CREATE INDEX idx_batches_chain_token_status ON batches(chain_id, token_id, status);
CREATE INDEX idx_batches_chain_created ON batches(chain_id, created_at);

-- NFT 资产查询复合索引
CREATE INDEX idx_nft_assets_collection_owner ON nft_assets(collection_id, owner_user_id);
CREATE INDEX idx_nft_assets_owner_burned ON nft_assets(owner_user_id, is_burned);
```

### 部分索引
```sql
-- 只对活跃用户建立索引
CREATE INDEX idx_users_active ON users(id) WHERE is_active = TRUE;

-- 只对已部署的账户建立索引
CREATE INDEX idx_user_accounts_deployed ON user_accounts(id) WHERE is_deployed = TRUE;

-- 只对未完成的交易建立索引
CREATE INDEX idx_transactions_pending ON transactions(id) WHERE status IN ('pending', 'processing');
```

---

## 🔗 关系设计

### 外键关系
```sql
-- 用户相关外键
ALTER TABLE user_accounts ADD CONSTRAINT fk_user_accounts_user_id 
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE user_balances ADD CONSTRAINT fk_user_balances_user_id 
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE transactions ADD CONSTRAINT fk_transactions_user_id 
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- 链相关外键
ALTER TABLE supported_tokens ADD CONSTRAINT fk_supported_tokens_chain_id 
    FOREIGN KEY (chain_id) REFERENCES chains(chain_id) ON DELETE CASCADE;

ALTER TABLE user_accounts ADD CONSTRAINT fk_user_accounts_chain_id 
    FOREIGN KEY (chain_id) REFERENCES chains(chain_id) ON DELETE CASCADE;

ALTER TABLE user_balances ADD CONSTRAINT fk_user_balances_chain_id 
    FOREIGN KEY (chain_id) REFERENCES chains(chain_id) ON DELETE CASCADE;

-- 代币相关外键
ALTER TABLE user_balances ADD CONSTRAINT fk_user_balances_token_id 
    FOREIGN KEY (token_id) REFERENCES supported_tokens(id) ON DELETE CASCADE;

ALTER TABLE transactions ADD CONSTRAINT fk_transactions_token_id 
    FOREIGN KEY (token_id) REFERENCES supported_tokens(id) ON DELETE CASCADE;

-- NFT 相关外键
ALTER TABLE nft_assets ADD CONSTRAINT fk_nft_assets_collection_id 
    FOREIGN KEY (collection_id) REFERENCES nft_collections(id) ON DELETE CASCADE;
```

### 级联删除策略
- **用户删除**: 级联删除用户账户、余额、交易记录
- **链删除**: 级联删除该链的所有代币、账户、余额
- **代币删除**: 级联删除该代币的所有余额、交易记录
- **NFT 集合删除**: 级联删除该集合的所有 NFT 资产

---

## 📦 数据迁移

### 迁移文件命名规范
```
{timestamp}-{description}.sql
```

示例：
```
20241221000001-create-chains.sql
20241221000002-create-supported-tokens.sql
20241221000003-create-user-accounts.sql
```

### 迁移文件结构
```sql
-- +migrate Up
-- 创建表或添加字段
CREATE TABLE example_table (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- +migrate Down
-- 回滚操作
DROP TABLE example_table;
```

### 常用迁移操作

#### 创建表
```sql
-- +migrate Up
CREATE TABLE new_table (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- +migrate Down
DROP TABLE new_table;
```

#### 添加字段
```sql
-- +migrate Up
ALTER TABLE existing_table ADD COLUMN new_field VARCHAR(100);

-- +migrate Down
ALTER TABLE existing_table DROP COLUMN new_field;
```

#### 添加索引
```sql
-- +migrate Up
CREATE INDEX idx_existing_table_new_field ON existing_table(new_field);

-- +migrate Down
DROP INDEX idx_existing_table_new_field;
```

#### 数据迁移
```sql
-- +migrate Up
INSERT INTO new_table (name, created_at)
SELECT old_name, created_at FROM old_table;

-- +migrate Down
DELETE FROM new_table WHERE name IN (SELECT old_name FROM old_table);
```

### 迁移管理命令
```bash
# 创建新迁移
sql-migrate new create_new_table

# 应用迁移
sql-migrate up

# 回滚迁移
sql-migrate down 1

# 查看迁移状态
sql-migrate status
```

---

## ⚡ 性能优化

### 查询优化

#### 1. 使用适当的索引
```sql
-- 为常用查询字段建立索引
CREATE INDEX idx_transactions_user_status ON transactions(user_id, status);
CREATE INDEX idx_user_balances_user_chain ON user_balances(user_id, chain_id);
```

#### 2. 避免全表扫描
```sql
-- ✅ 好的查询 - 使用索引
SELECT * FROM transactions WHERE user_id = 'user_123' AND status = 'completed';

-- ❌ 不好的查询 - 全表扫描
SELECT * FROM transactions WHERE amount > 100;
```

#### 3. 使用 LIMIT 限制结果集
```sql
-- 分页查询
SELECT * FROM transactions 
WHERE user_id = 'user_123' 
ORDER BY created_at DESC 
LIMIT 20 OFFSET 0;
```

#### 4. 使用 EXPLAIN 分析查询计划
```sql
EXPLAIN (ANALYZE, BUFFERS) 
SELECT * FROM transactions 
WHERE user_id = 'user_123' AND status = 'completed';
```

### 连接优化

#### 1. 使用适当的连接类型
```sql
-- 内连接 - 只返回匹配的记录
SELECT u.email, ub.confirmed_balance 
FROM users u 
INNER JOIN user_balances ub ON u.id = ub.user_id 
WHERE ub.chain_id = 1;

-- 左连接 - 返回所有用户，即使没有余额记录
SELECT u.email, COALESCE(ub.confirmed_balance, 0) as balance 
FROM users u 
LEFT JOIN user_balances ub ON u.id = ub.user_id AND ub.chain_id = 1;
```

#### 2. 避免 N+1 查询
```go
// ❌ 不好的做法 - N+1 查询
func GetUserAssets(userID string) ([]Asset, error) {
    user, err := getUser(userID)
    if err != nil {
        return nil, err
    }
    
    var assets []Asset
    for _, chainID := range user.ChainIDs {
        balance, err := getUserBalance(userID, chainID) // N+1 查询
        if err != nil {
            return nil, err
        }
        assets = append(assets, balance)
    }
    return assets, nil
}

// ✅ 好的做法 - 单次查询
func GetUserAssets(userID string) ([]Asset, error) {
    query := `
        SELECT ub.chain_id, ub.confirmed_balance, st.symbol, st.name
        FROM user_balances ub
        JOIN supported_tokens st ON ub.token_id = st.id
        WHERE ub.user_id = $1
    `
    rows, err := db.Query(query, userID)
    // 处理结果...
}
```

### 缓存策略

#### 1. 应用层缓存
```go
// 用户资产缓存
func (s *service) GetUserAssets(ctx context.Context, userID string) (*types.AssetsResponse, error) {
    cacheKey := fmt.Sprintf("user_assets:%s", userID)
    
    // 尝试从缓存获取
    if cached, err := s.redis.Get(ctx, cacheKey).Result(); err == nil {
        var response types.AssetsResponse
        if err := json.Unmarshal([]byte(cached), &response); err == nil {
            return &response, nil
        }
    }
    
    // 从数据库获取
    response, err := s.getUserAssetsFromDB(ctx, userID)
    if err != nil {
        return nil, err
    }
    
    // 缓存结果
    if data, err := json.Marshal(response); err == nil {
        s.redis.Set(ctx, cacheKey, data, 5*time.Minute)
    }
    
    return response, nil
}
```

#### 2. 数据库查询缓存
```sql
-- 使用物化视图缓存复杂查询结果
CREATE MATERIALIZED VIEW user_asset_summary AS
SELECT 
    u.id as user_id,
    u.email,
    COUNT(ub.id) as asset_count,
    SUM(ub.balance_usd) as total_value_usd
FROM users u
LEFT JOIN user_balances ub ON u.id = ub.user_id
GROUP BY u.id, u.email;

-- 定期刷新物化视图
REFRESH MATERIALIZED VIEW user_asset_summary;
```

### 分区策略

#### 1. 按时间分区
```sql
-- 交易表按月份分区
CREATE TABLE transactions (
    id SERIAL,
    user_id UUID NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    -- 其他字段...
) PARTITION BY RANGE (created_at);

-- 创建分区表
CREATE TABLE transactions_2024_01 PARTITION OF transactions
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');

CREATE TABLE transactions_2024_02 PARTITION OF transactions
    FOR VALUES FROM ('2024-02-01') TO ('2024-03-01');
```

#### 2. 按用户分区
```sql
-- 用户余额表按用户 ID 哈希分区
CREATE TABLE user_balances (
    id SERIAL,
    user_id UUID NOT NULL,
    -- 其他字段...
) PARTITION BY HASH (user_id);

-- 创建分区表
CREATE TABLE user_balances_0 PARTITION OF user_balances
    FOR VALUES WITH (modulus 4, remainder 0);

CREATE TABLE user_balances_1 PARTITION OF user_balances
    FOR VALUES WITH (modulus 4, remainder 1);
```

---

## 💾 备份和恢复

### 备份策略

#### 1. 全量备份
```bash
# 创建全量备份
pg_dump -h localhost -U postgres -d chainbridge > chainbridge_full_backup_$(date +%Y%m%d_%H%M%S).sql

# 压缩备份
pg_dump -h localhost -U postgres -d chainbridge | gzip > chainbridge_full_backup_$(date +%Y%m%d_%H%M%S).sql.gz
```

#### 2. 增量备份
```bash
# 使用 WAL 归档进行增量备份
# 在 postgresql.conf 中配置
wal_level = replica
archive_mode = on
archive_command = 'cp %p /backup/wal_archive/%f'

# 创建基础备份
pg_basebackup -h localhost -U postgres -D /backup/base_backup -Ft -z -P
```

#### 3. 自动化备份脚本
```bash
#!/bin/bash
# backup.sh

BACKUP_DIR="/backup/chainbridge"
DATE=$(date +%Y%m%d_%H%M%S)
DB_NAME="chainbridge"
DB_USER="postgres"
DB_HOST="localhost"

# 创建备份目录
mkdir -p $BACKUP_DIR

# 全量备份
pg_dump -h $DB_HOST -U $DB_USER -d $DB_NAME | gzip > $BACKUP_DIR/chainbridge_full_$DATE.sql.gz

# 清理旧备份（保留 7 天）
find $BACKUP_DIR -name "chainbridge_full_*.sql.gz" -mtime +7 -delete

echo "Backup completed: chainbridge_full_$DATE.sql.gz"
```

### 恢复策略

#### 1. 从全量备份恢复
```bash
# 恢复数据库
gunzip -c chainbridge_full_backup_20241215_120000.sql.gz | psql -h localhost -U postgres -d chainbridge

# 或者先删除数据库再创建
dropdb -h localhost -U postgres chainbridge
createdb -h localhost -U postgres chainbridge
gunzip -c chainbridge_full_backup_20241215_120000.sql.gz | psql -h localhost -U postgres -d chainbridge
```

#### 2. 从增量备份恢复
```bash
# 恢复基础备份
tar -xzf base_backup.tar.gz -C /var/lib/postgresql/data/

# 恢复 WAL 日志
pg_receivewal -h localhost -U postgres -D /var/lib/postgresql/data/pg_wal/
```

### 监控和告警

#### 1. 备份监控
```sql
-- 检查最后备份时间
SELECT 
    schemaname,
    tablename,
    last_vacuum,
    last_autovacuum,
    last_analyze,
    last_autoanalyze
FROM pg_stat_user_tables
WHERE schemaname = 'public';
```

#### 2. 磁盘空间监控
```bash
# 检查数据库大小
psql -h localhost -U postgres -d chainbridge -c "
SELECT 
    pg_size_pretty(pg_database_size('chainbridge')) as database_size,
    pg_size_pretty(pg_total_relation_size('transactions')) as transactions_size,
    pg_size_pretty(pg_total_relation_size('user_balances')) as balances_size;
"
```

#### 3. 性能监控
```sql
-- 检查慢查询
SELECT 
    query,
    calls,
    total_time,
    mean_time,
    rows
FROM pg_stat_statements
ORDER BY mean_time DESC
LIMIT 10;
```

---

## 📊 数据统计和分析

### 业务指标查询

#### 1. 用户统计
```sql
-- 用户注册趋势
SELECT 
    DATE(created_at) as date,
    COUNT(*) as new_users
FROM users
WHERE created_at >= NOW() - INTERVAL '30 days'
GROUP BY DATE(created_at)
ORDER BY date;

-- 活跃用户统计
SELECT 
    COUNT(*) as total_users,
    COUNT(*) FILTER (WHERE last_login_at >= NOW() - INTERVAL '7 days') as active_7d,
    COUNT(*) FILTER (WHERE last_login_at >= NOW() - INTERVAL '30 days') as active_30d
FROM users
WHERE is_active = TRUE;
```

#### 2. 资产统计
```sql
-- 总资产价值统计
SELECT 
    c.name as chain_name,
    st.symbol,
    COUNT(ub.id) as user_count,
    SUM(ub.confirmed_balance) as total_balance,
    SUM(ub.balance_usd) as total_value_usd
FROM user_balances ub
JOIN chains c ON ub.chain_id = c.chain_id
JOIN supported_tokens st ON ub.token_id = st.id
WHERE ub.confirmed_balance > 0
GROUP BY c.name, st.symbol
ORDER BY total_value_usd DESC;
```

#### 3. 交易统计
```sql
-- 交易量统计
SELECT 
    DATE(created_at) as date,
    tx_type,
    COUNT(*) as transaction_count,
    SUM(amount) as total_amount,
    AVG(gas_cost_eth) as avg_gas_cost
FROM transactions
WHERE created_at >= NOW() - INTERVAL '30 days'
GROUP BY DATE(created_at), tx_type
ORDER BY date, tx_type;
```

#### 4. 批处理效率统计
```sql
-- 批处理效率统计
SELECT 
    c.name as chain_name,
    st.symbol,
    COUNT(*) as batch_count,
    AVG(transaction_count) as avg_batch_size,
    AVG(gas_savings_percentage) as avg_gas_savings,
    SUM(gas_savings_usd) as total_gas_savings_usd
FROM batches b
JOIN chains c ON b.chain_id = c.chain_id
JOIN supported_tokens st ON b.token_id = st.id
WHERE b.status = 'completed'
GROUP BY c.name, st.symbol
ORDER BY total_gas_savings_usd DESC;
```

---

## 📋 总结

ChainBridge 数据库设计遵循规范化原则，通过合理的表结构设计、索引优化、关系管理，以及完善的备份恢复策略，为系统提供了稳定、高效的数据存储解决方案。

**核心特点**:
1. **规范化设计**: 遵循第三范式，减少数据冗余
2. **性能优化**: 合理的索引设计和查询优化
3. **扩展性**: 支持分区和水平扩展
4. **一致性**: 完善的外键约束和数据完整性
5. **可维护性**: 清晰的表结构和命名规范
6. **安全性**: 敏感数据加密和访问控制
7. **监控性**: 完善的统计查询和性能监控

通过持续的数据优化和监控，ChainBridge 数据库将为区块链应用提供可靠、高效的数据服务。