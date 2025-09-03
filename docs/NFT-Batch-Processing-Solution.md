# NFT批处理功能方案设计文档

## 1. 项目概述

基于现有CPOP Token批处理功能，为项目添加NFT（ERC721）的批处理支持，实现NFT的批量铸造、销毁和转账功能。

**目标功能**: batchMint、batchBurn、batchTransferFrom

## 2. 数据库设计

### 2.1 新增表结构

#### NFT Collections 表
```sql
CREATE TABLE nft_collections (
    id SERIAL PRIMARY KEY,
    collection_id VARCHAR(100) UNIQUE NOT NULL,
    chain_id BIGINT NOT NULL,
    contract_address CHAR(42) NOT NULL,
    name VARCHAR(255) NOT NULL,
    symbol VARCHAR(50) NOT NULL,
    contract_type VARCHAR(20) NOT NULL CHECK (contract_type IN ('ERC721', 'ERC1155')),
    base_uri TEXT,
    is_enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    FOREIGN KEY (chain_id) REFERENCES chains (chain_id),
    UNIQUE(chain_id, contract_address)
);
```

#### NFT Assets 表
```sql
CREATE TABLE nft_assets (
    id SERIAL PRIMARY KEY,
    collection_id VARCHAR(100) NOT NULL,
    token_id VARCHAR(78) NOT NULL,
    owner_user_id TEXT NOT NULL,
    chain_id BIGINT NOT NULL,
    metadata_uri TEXT,
    name VARCHAR(255),
    description TEXT,
    image_url TEXT,
    attributes JSONB,
    -- 是否已经销毁
    is_burned BOOLEAN DEFAULT FALSE,
    -- 是否已经上链
    is_minted BOOLEAN DEFAULT FALSE,
    -- 是否被锁住，burn、transfer过程中要锁住
    is_locked BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    FOREIGN KEY (collection_id) REFERENCES nft_collections (collection_id),
    UNIQUE(collection_id, token_id)
);
```

### 2.2 扩展现有表

#### 扩展 transactions 表
```sql
ALTER TABLE transactions ADD COLUMN collection_id VARCHAR(100);
ALTER TABLE transactions ADD COLUMN nft_token_id VARCHAR(78);
ALTER TABLE transactions ADD COLUMN nft_metadata JSONB;
```

#### 扩展枚举类型
```sql
-- 添加NFT交易类型
ALTER TYPE tx_type ADD VALUE 'nft_mint';
ALTER TYPE tx_type ADD VALUE 'nft_burn';  
ALTER TYPE tx_type ADD VALUE 'nft_transfer';

-- 添加NFT批处理类型
ALTER TYPE batch_type ADD VALUE 'nft_mint';
ALTER TYPE batch_type ADD VALUE 'nft_burn';
ALTER TYPE batch_type ADD VALUE 'nft_transfer';

ALTER TYPE cpop_operation_type ADD VALUE 'batch_nft_mint';
ALTER TYPE cpop_operation_type ADD VALUE 'batch_nft_burn';
ALTER TYPE cpop_operation_type ADD VALUE 'batch_nft_transfer';
```

## 3. API设计

### 3.1 集成现有用户资产API

#### 扩展 `/api/v1/assets/{user_id}` 接口
现有接口返回用户的代币资产，需要扩展以包含NFT资产。

**扩展后的响应格式**:
```json
{
  "assets": [
    {
      "balance_usd": 250,
      "chain_id": 56,
      "chain_name": "BSC",
      "confirmed_balance": "5000.0",
      "contract_address": "6.6325114945411165e+47",
      "decimals": 18,
      "locked_balance": "0.0",
      "name": "ChainBridge PoP Token",
      "pending_balance": "5050.0",
      "symbol": "CPOP",
      "sync_status": "synced",
      "asset_type": "token"
    }
  ],
  "nft_assets": [
    {
      "collection_id": "cpop_nft_collection_1",
      "collection_name": "CPOP Genesis NFTs",
      "contract_address": "0x1234567890123456789012345678901234567890",
      "chain_id": 56,
      "chain_name": "BSC",
      "total_count": 5,
      "floor_price_usd": 100,
      "total_value_usd": 500,
      "items": [
        {
          "token_id": "1",
          "meta": {
            "name": "Token Name",
            "description": "Token Description",
            "image": "https://example.com/token-image.png",
            "external_url": "https://example.com/token/1",
            "attributes": [
              {
                "trait_type": "Background",
                "value": "Blue"
              },
              {
                "trait_type": "Rarity",
                "value": "Legendary"
              }
            ]
          }
        }
      ]
    }
  ],
  "total_value_usd": 1750.5,
  "token_value_usd": 1250.5,
  "nft_value_usd": 500.0,
  "user_id": "user_123"
}
```

**新增字段说明**:
- `nft_assets`: NFT资产数组，按集合分组
- `token_value_usd`: 仅代币资产的总价值
- `nft_value_usd`: 仅NFT资产的总价值
- `total_value_usd`: 代币 + NFT的总价值

### 3.2 NFT批量操作API

#### 3.2.1 NFT批量铸造
```http
POST /api/v1/assets/nft/mint
```

**请求示例**:
```json
{
  "operation_id": "550e8400-e29b-41d4-a716-446655440001",
  "collection_id": "cpop_nft_collection_1",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "user123",
      "business_type": "reward",
      "reason_type": "achievement_unlock",
      "reason_detail": "Level 10 achievement",
      "meta": {
        "name": "Token Name",
        "description": "Token Description",
        "image": "https://example.com/token-image.png",
        "external_url": "https://example.com/token/1",
        "attributes": [
          {
            "trait_type": "Background",
            "value": "Blue"
          },
          {
            "trait_type": "Rarity",
            "value": "Legendary"
          }
        ]
      }
    }
  ],
  "batch_preferences": {
    "max_batch_size": 50,
    "timeout_seconds": 300
  }
}
```

### 3.2 NFT批量销毁
```http
POST /api/v1/assets/nft/burn
```

### 3.3 NFT批量转账
```http
POST /api/v1/assets/nft/transfer
```

## 4. 代码实现

### 4.1 区块链集成层

#### NFT批量调用器
```go
// internal/blockchain/nft_caller.go
type NFTBatchCaller struct {
    client      *ethclient.Client
    nftContract *cpop.CPNFT
    auth        *bind.TransactOpts
}

func (n *NFTBatchCaller) BatchMint(ctx context.Context, recipients []common.Address, tokenIds []*big.Int, metadataURIs []string) (*BatchResult, error) {
    tx, err := n.nftContract.BatchMint(n.auth, recipients, tokenIds, metadataURIs)
    if err != nil {
        return nil, fmt.Errorf("batch NFT mint failed: %w", err)
    }
    
    receipt, err := n.waitForConfirmation(ctx, tx.Hash())
    if err != nil {
        return &BatchResult{TxHash: tx.Hash().Hex(), Status: "submitted"}, nil
    }
    
    return &BatchResult{
        TxHash:      tx.Hash().Hex(),
        BlockNumber: receipt.BlockNumber,
        GasUsed:     receipt.GasUsed,
        Status:      "confirmed",
    }, nil
}

func (n *NFTBatchCaller) BatchBurn(ctx context.Context, tokenIds []*big.Int) (*BatchResult, error) {
    // 类似实现
}

func (n *NFTBatchCaller) BatchTransferFrom(ctx context.Context, fromAddresses, toAddresses []common.Address, tokenIds []*big.Int) (*BatchResult, error) {
    // 类似实现
}
```

### 4.2 队列系统扩展

#### Job类型定义
```go
// internal/queue/types.go
const (
    JobTypeNFTMint     JobType = "nft_mint"
    JobTypeNFTBurn     JobType = "nft_burn"  
    JobTypeNFTTransfer JobType = "nft_transfer"
)

type NFTMintJob struct {
    ID           string    `json:"id"`
    JobType      JobType   `json:"job_type"`
    TransactionID uuid.UUID `json:"transaction_id"`
    ChainID      int64     `json:"chain_id"`
    CollectionID string    `json:"collection_id"`
    ToUserID     string    `json:"to_user_id"`
    TokenID      string    `json:"token_id"`
    MetadataURI  string    `json:"metadata_uri"`
    BusinessType string    `json:"business_type"`
    ReasonType   string    `json:"reason_type"`
    Priority     Priority  `json:"priority"`
    CreatedAt    time.Time `json:"created_at"`
}
```

#### 扩展BatchProcessor接口
```go
type BatchProcessor interface {
    // 现有方法...
    PublishTransfer(ctx context.Context, job TransferJob) error
    PublishAssetAdjust(ctx context.Context, job AssetAdjustJob) error
    
    // 新增NFT方法
    PublishNFTMint(ctx context.Context, job NFTMintJob) error
    PublishNFTBurn(ctx context.Context, job NFTBurnJob) error
    PublishNFTTransfer(ctx context.Context, job NFTTransferJob) error
}
```

### 4.3 扩展现有资产服务

#### 修改 assets 服务支持NFT查询
```go
// internal/services/assets/service.go
type UserAssetsResponse struct {
    Assets        []AssetInfo `json:"assets"`
    NFTAssets     []NFTCollectionInfo `json:"nft_assets"`
    TotalValueUSD float64 `json:"total_value_usd"`
    TokenValueUSD float64 `json:"token_value_usd"`
    NFTValueUSD   float64 `json:"nft_value_usd"`
    UserID        string  `json:"user_id"`
}

type AssetInfo struct {
    BalanceUSD       float64 `json:"balance_usd"`
    ChainID          int64   `json:"chain_id"`
    ChainName        string  `json:"chain_name"`
    ConfirmedBalance string  `json:"confirmed_balance"`
    ContractAddress  string  `json:"contract_address"`
    Decimals         int     `json:"decimals"`
    LockedBalance    string  `json:"locked_balance"`
    Name             string  `json:"name"`
    PendingBalance   string  `json:"pending_balance"`
    Symbol           string  `json:"symbol"`
    SyncStatus       string  `json:"sync_status"`
    AssetType        string  `json:"asset_type"` // "token"
}

type NFTCollectionInfo struct {
    CollectionID    string    `json:"collection_id"`
    CollectionName  string    `json:"collection_name"`
    ContractAddress string    `json:"contract_address"`
    ChainID         int64     `json:"chain_id"`
    ChainName       string    `json:"chain_name"`
    TotalCount      int       `json:"total_count"`
    FloorPriceUSD   float64   `json:"floor_price_usd"`
    TotalValueUSD   float64   `json:"total_value_usd"`
    Items           []NFTItem `json:"items"`
}

type NFTItem struct {
    TokenID           string        `json:"token_id"`
    Name              string        `json:"name"`
    Description       string        `json:"description"`
    ImageURL          string        `json:"image_url"`
    MetadataURI       string        `json:"metadata_uri"`
    Rarity            string        `json:"rarity"`
    EstimatedValueUSD float64       `json:"estimated_value_usd"`
    LastTransferDate  time.Time     `json:"last_transfer_date"`
    Attributes        []NFTAttribute `json:"attributes"`
}

type NFTAttribute struct {
    TraitType         string  `json:"trait_type"`
    Value             string  `json:"value"`
    RarityPercentage  float64 `json:"rarity_percentage"`
}

// 扩展GetUserAssets方法
func (s *service) GetUserAssets(ctx context.Context, userID string, chainID *int64) (*UserAssetsResponse, error) {
    // 获取代币资产(现有逻辑)
    tokenAssets, tokenValueUSD, err := s.getUserTokenAssets(ctx, userID, chainID)
    if err != nil {
        return nil, fmt.Errorf("failed to get token assets: %w", err)
    }
    
    // 获取NFT资产(新增)
    nftAssets, nftValueUSD, err := s.getUserNFTAssets(ctx, userID, chainID)
    if err != nil {
        return nil, fmt.Errorf("failed to get NFT assets: %w", err)
    }
    
    return &UserAssetsResponse{
        Assets:        tokenAssets,
        NFTAssets:     nftAssets,
        TotalValueUSD: tokenValueUSD + nftValueUSD,
        TokenValueUSD: tokenValueUSD,
        NFTValueUSD:   nftValueUSD,
        UserID:        userID,
    }, nil
}

// 新增方法: 获取用户NFT资产
func (s *service) getUserNFTAssets(ctx context.Context, userID string, chainID *int64) ([]NFTCollectionInfo, float64, error) {
    query := `
        SELECT 
            nc.collection_id,
            nc.name as collection_name,
            nc.contract_address,
            nc.chain_id,
            c.name as chain_name,
            COUNT(na.id) as total_count,
            COALESCE(AVG(na_price.floor_price), 0) as floor_price_usd
        FROM nft_collections nc
        JOIN chains c ON nc.chain_id = c.chain_id
        LEFT JOIN nft_assets na ON nc.collection_id = na.collection_id 
            AND na.owner_user_id = $1 
            AND na.is_burned = false
        LEFT JOIN (
            -- 假设有价格表，这里使用模拟数据
            SELECT collection_id, 100.0 as floor_price
        ) na_price ON nc.collection_id = na_price.collection_id
        WHERE ($2::bigint IS NULL OR nc.chain_id = $2)
            AND na.id IS NOT NULL
        GROUP BY nc.collection_id, nc.name, nc.contract_address, nc.chain_id, c.name
        ORDER BY nc.collection_id`
    
    rows, err := s.db.QueryContext(ctx, query, userID, chainID)
    if err != nil {
        return nil, 0, fmt.Errorf("failed to query NFT collections: %w", err)
    }
    defer rows.Close()
    
    var collections []NFTCollectionInfo
    var totalNFTValue float64
    
    for rows.Next() {
        var collection NFTCollectionInfo
        err := rows.Scan(
            &collection.CollectionID,
            &collection.CollectionName,
            &collection.ContractAddress,
            &collection.ChainID,
            &collection.ChainName,
            &collection.TotalCount,
            &collection.FloorPriceUSD,
        )
        if err != nil {
            return nil, 0, fmt.Errorf("failed to scan NFT collection: %w", err)
        }
        
        // 获取该集合下的NFT物品
        items, err := s.getNFTItems(ctx, userID, collection.CollectionID)
        if err != nil {
            return nil, 0, fmt.Errorf("failed to get NFT items: %w", err)
        }
        
        collection.Items = items
        collection.TotalValueUSD = collection.FloorPriceUSD * float64(collection.TotalCount)
        totalNFTValue += collection.TotalValueUSD
        
        collections = append(collections, collection)
    }
    
    return collections, totalNFTValue, nil
}

// 获取NFT物品详情
func (s *service) getNFTItems(ctx context.Context, userID, collectionID string) ([]NFTItem, error) {
    query := `
        SELECT 
            token_id,
            COALESCE(name, '') as name,
            COALESCE(description, '') as description,
            COALESCE(image_url, '') as image_url,
            COALESCE(metadata_uri, '') as metadata_uri,
            COALESCE(attributes, '{}') as attributes,
            updated_at
        FROM nft_assets
        WHERE owner_user_id = $1 
            AND collection_id = $2 
            AND is_burned = false
        ORDER BY token_id`
    
    rows, err := s.db.QueryContext(ctx, query, userID, collectionID)
    if err != nil {
        return nil, fmt.Errorf("failed to query NFT items: %w", err)
    }
    defer rows.Close()
    
    var items []NFTItem
    for rows.Next() {
        var item NFTItem
        var attributesJSON string
        
        err := rows.Scan(
            &item.TokenID,
            &item.Name,
            &item.Description,
            &item.ImageURL,
            &item.MetadataURI,
            &attributesJSON,
            &item.LastTransferDate,
        )
        if err != nil {
            return nil, fmt.Errorf("failed to scan NFT item: %w", err)
        }
        
        // 解析attributes JSON
        if attributesJSON != "{}" {
            var attributes []NFTAttribute
            if err := json.Unmarshal([]byte(attributesJSON), &attributes); err == nil {
                item.Attributes = attributes
            }
        }
        
        // 设置估值（可以根据稀有度等因素计算）
        item.EstimatedValueUSD = 100.0 // 默认值，实际可根据稀有度计算
        item.Rarity = s.calculateRarity(item.Attributes)
        
        items = append(items, item)
    }
    
    return items, nil
}

// 计算NFT稀有度
func (s *service) calculateRarity(attributes []NFTAttribute) string {
    if len(attributes) == 0 {
        return "common"
    }
    
    var avgRarity float64
    for _, attr := range attributes {
        avgRarity += attr.RarityPercentage
    }
    avgRarity /= float64(len(attributes))
    
    switch {
    case avgRarity <= 1.0:
        return "legendary"
    case avgRarity <= 5.0:
        return "epic"
    case avgRarity <= 15.0:
        return "rare"
    case avgRarity <= 50.0:
        return "uncommon"
    default:
        return "common"
    }
}
```

## 5. API处理器修改

### 5.1 修改现有assets处理器
```go
// internal/api/handlers/assets/handler.go

// 修改GetUserAssets处理器以支持NFT
func (h *Handler) GetUserAssets(c echo.Context) error {
    userID := c.Param("user_id")
    
    // 获取可选的chain_id参数
    var chainID *int64
    if chainIDStr := c.QueryParam("chain_id"); chainIDStr != "" {
        if cid, err := strconv.ParseInt(chainIDStr, 10, 64); err == nil {
            chainID = &cid
        }
    }
    
    // 获取是否包含NFT的参数
    includeNFT := c.QueryParam("include_nft") != "false" // 默认包含
    
    if includeNFT {
        // 返回包含NFT的完整响应
        resp, err := h.assetsService.GetUserAssets(c.Request().Context(), userID, chainID)
        if err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{
                "error": err.Error(),
            })
        }
        return c.JSON(http.StatusOK, resp)
    } else {
        // 返回仅代币的响应（兼容现有客户端）
        tokenAssets, tokenValueUSD, err := h.assetsService.GetUserTokenAssets(c.Request().Context(), userID, chainID)
        if err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{
                "error": err.Error(),
            })
        }
        
        // 返回原有格式
        resp := map[string]interface{}{
            "assets":          tokenAssets,
            "total_value_usd": tokenValueUSD,
            "user_id":         userID,
        }
        return c.JSON(http.StatusOK, resp)
    }
}

// 新增NFT专用端点
func (h *Handler) GetUserNFTs(c echo.Context) error {
    userID := c.Param("user_id")
    
    var chainID *int64
    if chainIDStr := c.QueryParam("chain_id"); chainIDStr != "" {
        if cid, err := strconv.ParseInt(chainIDStr, 10, 64); err == nil {
            chainID = &cid
        }
    }
    
    collectionID := c.QueryParam("collection_id")
    
    nftAssets, totalValue, err := h.assetsService.GetUserNFTAssets(c.Request().Context(), userID, chainID, collectionID)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{
            "error": err.Error(),
        })
    }
    
    resp := map[string]interface{}{
        "nft_assets":     nftAssets,
        "total_value_usd": totalValue,
        "user_id":        userID,
    }
    
    return c.JSON(http.StatusOK, resp)
}
```

### 5.2 新增NFT批处理处理器
``go
// internal/api/handlers/nft/batch_handler.go
package nft

import (
    "net/http"
    
    "github.com/labstack/echo/v4"
    "github.com/hzbay/chain-bridge/internal/services/nft"
    "github.com/hzbay/chain-bridge/internal/types"
)

type BatchHandler struct {
    nftService nft.Service
}

func NewBatchHandler(nftService nft.Service) *BatchHandler {
    return &BatchHandler{
        nftService: nftService,
    }
}

// NFT批量铸造
func (h *BatchHandler) BatchMint(c echo.Context) error {
    var req types.NFTBatchMintRequest
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]interface{}{
            "status": 400,
            "title":  "Bad Request",
            "type":   "validation_error",
            "validationErrors": []map[string]string{{
                "error": "Invalid request format",
                "field": "body",
            }},
        })
    }
    
    // 验证请求
    if err := req.Validate(); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]interface{}{
            "status": 400,
            "title":  "Validation Error", 
            "type":   "validation_error",
            "validationErrors": []map[string]string{{
                "error": err.Error(),
                "field": "body",
            }},
        })
    }
    
    resp, err := h.nftService.BatchMint(c.Request().Context(), &req)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]interface{}{
            "status": 500,
            "title":  "Internal Server Error",
            "type":   "server_error",
            "detail": err.Error(),
        })
    }
    
    return c.JSON(http.StatusOK, resp)
}

// NFT批量销毁
func (h *BatchHandler) BatchBurn(c echo.Context) error {
    var req types.NFTBatchBurnRequest
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{
            "error": "Invalid request format",
        })
    }
    
    if err := req.Validate(); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{
            "error": err.Error(),
        })
    }
    
    resp, err := h.nftService.BatchBurn(c.Request().Context(), &req)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{
            "error": err.Error(),
        })
    }
    
    return c.JSON(http.StatusOK, resp)
}

// NFT批量转账
func (h *BatchHandler) BatchTransfer(c echo.Context) error {
    var req types.NFTBatchTransferRequest
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{
            "error": "Invalid request format",
        })
    }
    
    if err := req.Validate(); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{
            "error": err.Error(),
        })
    }
    
    resp, err := h.nftService.BatchTransfer(c.Request().Context(), &req)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{
            "error": err.Error(),
        })
    }
    
    return c.JSON(http.StatusOK, resp)
}
```

#### NFT服务
```go
// internal/services/nft/service.go
type Service interface {
    BatchMint(ctx context.Context, req *types.NFTBatchMintRequest) (*types.NFTBatchMintResponse, error)
    BatchBurn(ctx context.Context, req *types.NFTBatchBurnRequest) (*types.NFTBatchBurnResponse, error)
    BatchTransfer(ctx context.Context, req *types.NFTBatchTransferRequest) (*types.NFTBatchTransferResponse, error)
}

func (s *service) BatchMint(ctx context.Context, req *types.NFTBatchMintRequest) (*types.NFTBatchMintResponse, error) {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer tx.Rollback()
    
    // 创建交易记录和NFT资产记录
    for _, mintOp := range req.MintOperations {
        txID := uuid.New()
        
        // 插入transactions表
        _, err = tx.ExecContext(ctx, `
            INSERT INTO transactions (
                tx_id, operation_id, user_id, chain_id, tx_type, 
                collection_id, nft_token_id, status, business_type
            ) VALUES ($1, $2, $3, $4, 'nft_mint', $5, $6, 'pending', $7)`,
            txID, req.OperationID, mintOp.ToUserID, req.ChainID,
            req.CollectionID, mintOp.TokenID, mintOp.BusinessType)
        
        // 发送到队列
        job := queue.NFTMintJob{
            ID:           uuid.New().String(),
            JobType:      queue.JobTypeNFTMint,
            TransactionID: txID,
            ChainID:      req.ChainID,
            CollectionID: req.CollectionID,
            ToUserID:     mintOp.ToUserID,
            TokenID:      mintOp.TokenID,
            MetadataURI:  mintOp.MetadataURI,
            BusinessType: mintOp.BusinessType,
        }
        
        err = s.batchProcessor.PublishNFTMint(ctx, job)
        if err != nil {
            return nil, fmt.Errorf("failed to publish NFT mint job: %w", err)
        }
    }
    
    tx.Commit()
    return &types.NFTBatchMintResponse{...}, nil
}
```

## 5. 队列处理逻辑

### 5.1 队列命名规范
```
格式: {prefix}.{operation}.{chain_id}.{collection_hash}
示例:
- chain-bridge.nft_mint.11155111.cpop_nft_1
- chain-bridge.nft_burn.11155111.cpop_nft_1  
- chain-bridge.nft_transfer.11155111.cpop_nft_1
```

### 5.2 批处理消费者扩展
```go
// 在RabbitMQBatchConsumer中添加NFT处理
func (c *RabbitMQBatchConsumer) executeBlockchainBatch(ctx context.Context, messages []*MessageWrapper, group BatchGroup) (*blockchain.BatchResult, error) {
    switch group.JobType {
    case JobTypeNFTMint:
        return c.processNFTMintBatch(ctx, group.ChainID, group.CollectionID, jobs)
    case JobTypeNFTBurn:
        return c.processNFTBurnBatch(ctx, group.ChainID, group.CollectionID, jobs)
    case JobTypeNFTTransfer:
        return c.processNFTTransferBatch(ctx, group.ChainID, group.CollectionID, jobs)
    }
}

func (c *RabbitMQBatchConsumer) processNFTMintBatch(ctx context.Context, chainID int64, collectionID string, jobs []BatchJob) (*blockchain.BatchResult, error) {
    nftCaller := c.getNFTCaller(chainID, collectionID)
    
    var mintJobs []NFTMintJob
    for _, job := range jobs {
        if nftJob, ok := job.(NFTMintJob); ok {
            mintJobs = append(mintJobs, nftJob)
        }
    }
    
    recipients, tokenIds, metadataURIs := c.prepareNFTMintParams(ctx, mintJobs)
    return nftCaller.BatchMint(ctx, recipients, tokenIds, metadataURIs)
}
```

## 6. 实施计划

### 6.1 Phase 1: 数据库Schema (1周)
- 创建NFT相关表
- 扩展现有表和枚举类型
- 编写和测试数据库迁移脚本

### 6.2 Phase 2: 区块链集成 (1周)  
- 实现NFTBatchCaller
- 集成cpop-abis中的NFT合约
- 测试合约调用功能

### 6.3 Phase 3: 队列系统扩展 (1周)
- 扩展队列类型和Job定义
- 实现NFT批处理消费者
- 测试队列处理逻辑

### 6.4 Phase 4: API和服务层 (1周)
- 实现NFT服务层
- 创建API处理器
- **修改现有 `/api/v1/assets/{user_id}` 接口以包含NFT资产**
- 创建NFT专用API端点
- 编写API文档和测试

### 6.5 Phase 5: 测试和优化 (1周)
- 端到端测试
- 性能优化
- 文档完善

## 7. 风险评估

### 7.1 技术风险
- **NFT合约兼容性**: 需确认cpop-abis中NFT合约接口完整性
- **Gas优化效果**: NFT批处理Gas节省可能低于ERC20
- **元数据处理**: IPFS元数据可能影响批处理性能

### 7.2 缓解措施
- 提前验证合约接口和方法
- 实施渐进式批处理大小优化
- 设计异步元数据处理机制

## 8. 监控和告警

### 8.1 关键指标
- NFT批处理成功率
- 批处理效率（Gas节省比例）
- 队列处理延迟
- 合约调用失败率

### 8.2 告警规则
- 批处理成功率 < 95%
- 队列积压 > 1000条消息
- Gas效率 < 预期的50%

---

**总结**: 本方案完全复用现有CPOP Token批处理架构，通过扩展数据库schema、添加NFT相关Job类型和实现NFT合约调用器，实现NFT批处理功能。**关键创新点是将NFT资产无缝集成到现有的 `/api/v1/assets/{user_id}` 接口中**，使用户可以在同一响应中查看代币和NFT资产。预计开发周期5周，风险可控.