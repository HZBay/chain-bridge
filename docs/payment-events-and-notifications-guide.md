# 支付事件监听与通知系统使用指南

## 概述

本指南介绍如何使用和配置支付事件监听系统和相关的通知功能，包括最新的NFT操作通知实现，让开发人员能够快速上手使用这些功能。

## 目录

1. [PaymentMade 事件监听系统](#1-paymentmade-事件监听系统)
2. [通知系统概述](#2-通知系统概述)
3. [NFT 操作通知](#3-nft-操作通知)
4. [传统支付和转账通知](#4-传统支付和转账通知)
5. [开发人员使用指南](#5-开发人员使用指南)
6. [常见问题](#6-常见问题)
7. [API 接口](#7-api-接口)

## 1. PaymentMade 事件监听系统

### 1.1 功能说明
- 自动监听区块链上的 PaymentMade 事件
- 将事件转换为结构化消息发送到 RabbitMQ
- 支持多链同时监听
- 提供历史事件同步和实时监听

### 1.2 配置步骤

#### 步骤1：配置支付合约地址
在数据库中为需要监听的链配置支付合约地址：

```sql
-- 为以太坊主网配置支付合约
UPDATE chains 
SET payment_contract_address = '0x1234567890123456789012345678901234567890' 
WHERE chain_id = 1 AND is_enabled = true;

-- 为BSC主网配置支付合约  
UPDATE chains 
SET payment_contract_address = '0xabcdefabcdefabcdefabcdefabcdefabcdefabcd' 
WHERE chain_id = 56 AND is_enabled = true;
```

#### 步骤2：启动服务
```bash
./bin/app server --migrate --seed
```

服务启动时会自动：
- 读取数据库中的支付合约配置
- 为每个配置的链启动事件监听器
- 开始监听 PaymentMade 事件

### 1.3 消息格式

PaymentMade 事件会发送到队列 `payment_events.{chain_id}`，消息格式：

```json
{
  "event_type": "payment_made",
  "chain_id": 1,
  "order_id": "12345",
  "payer": "0x1234567890123456789012345678901234567890",
  "token": "0xA0b86a33E6776d02b1a65828fe28C6dCE13d8f8e",
  "amount": "1000000000000000000",
  "timestamp": 1703123456,
  "block_number": 18600000,
  "tx_hash": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
}
```

### 1.4 队列名称规则

```
payment_events.1      # 以太坊主网
payment_events.56     # BSC 主网  
payment_events.137    # Polygon 主网
payment_events.42161  # Arbitrum 主网
```

## 2. 通知系统概述

系统现在支持两大类通知：
- **传统支付和转账通知** - 针对CPOP代币操作
- **NFT操作通知** - 针对NFT铸造、销毁、转账等操作

### 2.1 通知架构

通知系统采用事件驱动架构：

```
区块链操作 → 批处理 → 区块链确认 → TxConfirmationWatcher → 发送通知
```

**关键组件：**
- `TxConfirmationWatcher`: 监控区块链交易确认
- `NotificationProcessor`: 处理和发送通知
- `BatchProcessor`: 作为通知处理器实现

### 2.2 通知可靠性保证

1. **仅在区块链确认后发送**: 确保操作真实完成
2. **默认6个区块确认**: 降低重组风险
3. **失败重试机制**: 自动重试失败的通知
4. **非阻塞设计**: 通知失败不影响交易确认

## 3. NFT 操作通知

### 3.1 NFT通知系统概述

NFT通知系统是对现有通知基础设施的扩展，专门处理NFT相关操作的成功通知。系统确保只有在区块链确认后才发送通知，提供可靠的用户体验。

**关键特性**:
- **区块链确认后通知**: 确保操作真实完成
- **完整操作覆盖**: 支持mint、burn、transfer所有NFT操作
- **多用户通知**: 转账操作同时通知发送者和接收者
- **详细信息**: 包含所有相关的NFT和交易详情
- **非阻塞设计**: 通知失败不影响交易确认
- **架构一致性**: 复用现有通知基础设施

### 3.2 支持的NFT通知类型

NFT系统支持完整的操作成功通知：

#### 3.2.1 NFT铸造成功通知 (`nft_mint_success`)
**发送对象**: NFT接收者（铸造者）
**触发时机**: NFT铸造操作获得区块链确认后

```json
{
  "type": "nft_mint_success",
  "user_id": "user123",
  "operation_id": "550e8400-e29b-41d4-a716-446655440001",
  "chain_id": 1,
  "collection_id": "cpop_genesis",
  "nft_token_id": "1001",
  "tx_hash": "0xabcdef...",
  "timestamp": 1703123456,
  "status": "confirmed"
}
```

#### 3.2.2 NFT销毁成功通知 (`nft_burn_success`)
**发送对象**: NFT拥有者（销毁者）
**触发时机**: NFT销毁操作获得区块链确认后

```json
{
  "type": "nft_burn_success",
  "user_id": "user456",
  "operation_id": "550e8400-e29b-41d4-a716-446655440002",
  "chain_id": 1,
  "collection_id": "cpop_genesis",
  "nft_token_id": "1002",
  "tx_hash": "0xdef123...",
  "timestamp": 1703123556,
  "status": "confirmed"
}
```

#### 3.2.3 NFT转账成功通知 (`nft_transfer_success`)
**发送对象**: NFT发送者
**触发时机**: NFT转账操作获得区块链确认后

```json
{
  "type": "nft_transfer_success",
  "user_id": "user789",
  "operation_id": "550e8400-e29b-41d4-a716-446655440003",
  "chain_id": 1,
  "collection_id": "cpop_genesis",
  "nft_token_id": "1003",
  "to_user_id": "user101",
  "tx_hash": "0x123abc...",
  "timestamp": 1703123656,
  "status": "confirmed"
}
```

#### 3.2.4 NFT转账接收通知 (`nft_transfer_received`)
**发送对象**: NFT接收者
**触发时机**: NFT转账操作获得区块链确认后

```json
{
  "type": "nft_transfer_received",
  "user_id": "user101",
  "operation_id": "550e8400-e29b-41d4-a716-446655440003",
  "chain_id": 1,
  "collection_id": "cpop_genesis",
  "nft_token_id": "1003",
  "from_user_id": "user789",
  "tx_hash": "0x123abc...",
  "timestamp": 1703123656,
  "status": "confirmed"
}
```

### 3.3 NFT批量操作的OperationID处理

**架构改进**: 为解决批量操作中token_id关联问题，系统实现了双层OperationID架构：

#### 3.3.1 问题背景
原始架构中，NFT批量铸造的所有操作共享同一个`OperationID`，导致无法将区块链返回的token_id准确关联到对应用户。

#### 3.3.2 解决方案
**双层OperationID系统**:
- **Batch OperationID**: 整个批次的唯一标识符（用于幂等性）
- **Individual OperationID**: 每个单独操作的唯一标识符（用于精确追踪）

```
批量铸造请求 [UserA, UserB, UserC]
├─ Batch OperationID: "batch-550e8400" (整个批次)
├─ Individual OperationIDs:
   ├─ UserA: "op-550e8401-a"
   ├─ UserB: "op-550e8402-b" 
   └─ UserC: "op-550e8403-c"

区块链返回 (按顺序): ["token_101", "token_102", "token_103"]

精确映射:
├─ UserA: {operation_id: "op-550e8401-a", nft_token_id: "101"}
├─ UserB: {operation_id: "op-550e8402-b", nft_token_id: "102"}
└─ UserC: {operation_id: "op-550e8403-c", nft_token_id: "103"}
```

#### 3.3.3 技术实现细节

**数据库记录创建**:
```go
for _, mintOp := range request.MintOperations {
    individualOperationID := uuid.New() // ✅ 每个操作唯一ID
    
    _, err := tx.ExecContext(ctx, `
        INSERT INTO transactions (
            transaction_id, operation_id, user_id, chain_id, 
            tx_type, business_type, status, collection_id, 
            nft_token_id, created_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
    `, transactionID, individualOperationID.String(), 
       mintOp.ToUserID, chainID, "nft_mint", "nft", 
       "recorded", collectionID, "-1") // -1为占位符
}
```

**Token ID精确映射**:
```go
// 按创建时间排序获取操作（保持铸造顺序）
query := `
    SELECT operation_id, user_id, collection_id 
    FROM transactions 
    WHERE batch_id = $1 AND tx_type = 'nft_mint' AND nft_token_id = '-1' 
    ORDER BY created_at ASC`

// 按索引映射: operations[i] -> tokenIDs[i]
for i := 0; i < len(operations) && i < len(nftResult.TokenIDs); i++ {
    op := operations[i]
    actualTokenID := nftResult.TokenIDs[i]
    
    // 使用individual operation_id更新
    err = updateNFTTokenID(ctx, op.OperationID, actualTokenID, op.CollectionID)
}
```

#### 3.3.4 架构优势
- **精确关联**: 每个token_id准确映射到对应用户
- **独立追踪**: 每个操作可单独跟踪状态
- **准确通知**: 用户收到包含正确operation_id和token_id的通知
- **保持幂等性**: Batch OperationID确保批次级别的幂等性
- **顺序保证**: 数据库排序确保token_id分配顺序正确

### 3.4 NFT资产状态管理

NFT通知系统在确认过程中正确管理NFT资产状态：

#### 3.4.1 NFT铸造确认
```sql
UPDATE nft_assets SET 
    is_minted = true,
    is_locked = false,
    updated_at = NOW()
WHERE collection_id = $1 AND token_id = $2
```

#### 3.4.2 NFT销毁确认
```sql
UPDATE nft_assets SET 
    is_burned = true,
    is_locked = false,
    updated_at = NOW()
WHERE collection_id = $1 AND token_id = $2
```

#### 3.4.3 NFT转账确认
```sql
UPDATE nft_assets SET 
    owner_user_id = $3,
    is_locked = false,
    updated_at = NOW()
WHERE collection_id = $1 AND token_id = $2
```

### 3.5 NFT通知完整流程

```
1. 用户发起NFT操作 (mint/burn/transfer)
   ↓
2. 系统创建交易记录 (status: 'recorded')
   ├─ token_id: "-1" (占位符)
   ├─ operation_id: 个人唯一UUID
   └─ batch_id: 批次标识符
   ↓
3. 加入批处理队列 (NFTMintJob/NFTBurnJob/NFTTransferJob)
   ├─ BatchOperationID: 批次级别ID
   └─ IndividualOperationID: 个人操作ID
   ↓
4. 区块链批处理执行 (status: 'submitted')
   ├─ 更新tx_hash
   └─ 锁定相关NFT资产 (is_locked = true)
   ↓
5. TxConfirmationWatcher监控确认
   ├─ 监控区块链交易状态
   └─ 等待指定确认数 (默认6个区块)
   ↓
6. 达到确认数后:
   ├─ 更新数据库 (status: 'confirmed')
   ├─ 提取并映射实际token_id
   ├─ 更新NFT资产状态 (finalization)
   └─ 发送成功通知
   ↓
7. 通知发送:
   ├─ NFT操作者收到成功通知
   ├─ 转账接收者收到接收通知
   └─ 包含完整的operation_id和token_id信息
```

### 3.6 错误处理和可靠性

#### 3.6.1 通知发送失败处理
- 通知发送失败记录为警告，不影响交易确认
- 缺失NFT特定数据时优雅处理，记录适当错误消息
- NFT最终化过程中的数据库事务失败会正确回滚

#### 3.6.2 系统容错设计
- **非阻塞**: 通知处理器不可用时优雅降级
- **事务安全**: 数据库操作使用事务确保一致性
- **幂等性**: 支持重复处理而不影响结果
- **监控友好**: 详细日志记录便于问题诊断

## 4. 传统支付和转账通知

#### 余额变化通知
```json
{
  "type": "balance_changed",
  "user_id": "user123",
  "chain_id": 1,
  "token_address": "0xA0b86a33E6776d02b1a65828fe28C6dCE13d8f8e",
  "old_balance": "1000000000000000000",
  "new_balance": "2000000000000000000",
  "change_amount": "1000000000000000000",
  "timestamp": 1703123456,
  "transaction_hash": "0xabcdef..."
}
```

#### 交易状态变化通知
```json
{
  "type": "transaction_status_changed",
  "transaction_id": "tx_12345",
  "user_id": "user123",
  "old_status": "pending",
  "new_status": "confirmed",
  "chain_id": 1,
  "amount": "1000000000000000000",
  "token_address": "0xA0b86a33E6776d02b1a65828fe28C6dCE13d8f8e",
  "timestamp": 1703123456,
  "transaction_hash": "0xabcdef..."
}
```

#### 批处理状态变化通知
```json
{
  "type": "batch_status_changed",
  "batch_id": "batch_67890",
  "old_status": "processing",
  "new_status": "confirmed",
  "chain_id": 1,
  "operation_count": 25,
  "gas_saved_usd": "12.50",
  "timestamp": 1703123456,
  "transaction_hash": "0xabcdef..."
}
```

#### 支付成功通知
```json
{
  "type": "payment_made",
  "order_id": "order_12345",
  "payer": "0x1234567890123456789012345678901234567890",
  "amount": "1000000000000000000",
  "token": "0xA0b86a33E6776d02b1a65828fe28C6dCE13d8f8e",
  "chain_id": 1,
  "timestamp": 1703123456,
  "transaction_hash": "0xabcdef..."
}
```

### 2.3 队列名称规则

通知消息发送到以下队列：

```
{prefix}.notification.0.0
```

**默认队列名称示例**：
```
chainbridge.notification.0.0
```

**解释**：
- `{prefix}`: RabbitMQ 配置中的队列前缀（通常是 `chainbridge`）
- `notification`: 固定的通知作业类型
- `0.0`: NotificationJob 不区分链ID和代币ID，统一使用 `0.0`

**配置说明**：
- 队列前缀可在环境变量 `RABBITMQ_QUEUE_PREFIX` 中配置（默认：`chainbridge`）
- 所有类型的通知都发送到同一个队列，便于统一处理
- 与其他业务队列（如 transfer、asset_adjust）格式保持一致

**如何查看实际队列名称**：
```bash
# 查看当前配置的队列前缀
echo $RABBITMQ_QUEUE_PREFIX

# 或查看所有通知相关队列
rabbitmqctl list_queues name | grep notification
```

## 5. 开发人员使用指南

### 5.1 消费 PaymentMade 事件

**创建消费者监听支付事件：**

```go
// 示例：消费支付事件
func consumePaymentEvents(chainID int64) {
    queueName := fmt.Sprintf("payment_events.%d", chainID)
    
    // 连接到 RabbitMQ
    conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()
    
    ch, err := conn.Channel()
    if err != nil {
        log.Fatal(err)
    }
    defer ch.Close()
    
    // 消费消息
    msgs, err := ch.Consume(queueName, "", true, false, false, false, nil)
    if err != nil {
        log.Fatal(err)
    }
    
    for msg := range msgs {
        var event PaymentEventMessage
        json.Unmarshal(msg.Body, &event)
        
        // 处理支付事件
        handlePaymentEvent(event)
    }
}

func handlePaymentEvent(event PaymentEventMessage) {
    log.Printf("收到支付事件: OrderID=%s, Payer=%s, Amount=%s", 
        event.OrderID, event.Payer, event.Amount)
    
    // 你的业务逻辑
    // 例如：更新订单状态、发送确认邮件等
}
```

### 5.2 消费NFT和传统通知消息

**创建通知消费者（处理所有类型的通知）：**

```go
func consumeNotifications() {
    // 根据你的配置设置正确的队列名称
    queueName := "chainbridge.notification.0.0"  // 使用实际的队列前缀
    
    // 连接设置同上...
    
    for msg := range msgs {
        var notification NotificationMessage
        json.Unmarshal(msg.Body, &notification)
        
        switch notification.Type {
        // NFT通知处理
        case "nft_mint_success":
            handleNFTMintSuccess(notification)
        case "nft_burn_success":
            handleNFTBurnSuccess(notification)
        case "nft_transfer_success":
            handleNFTTransferSuccess(notification)
        case "nft_transfer_received":
            handleNFTTransferReceived(notification)
            
        // 传统通知处理
        case "balance_changed":
            handleBalanceChanged(notification)
        case "payment_made":
            handlePaymentNotification(notification)
        case "transaction_status_changed":
            handleTransactionStatusChanged(notification)
        case "batch_status_changed":
            handleBatchStatusChanged(notification)
        }
    }
}

// NFT通知处理函数示例
func handleNFTMintSuccess(notification NotificationMessage) {
    log.Printf("NFT铸造成功: UserID=%s, TokenID=%s, CollectionID=%s", 
        notification.UserID, notification.NFTTokenID, notification.CollectionID)
    
    // 业务逻辑：更新用户NFT资产、发送邮件通知等
    updateUserNFTPortfolio(notification.UserID, notification.CollectionID, notification.NFTTokenID)
    sendEmailNotification(notification.UserID, "NFT铸造成功", notification)
}

func handleNFTTransferReceived(notification NotificationMessage) {
    log.Printf("NFT转账接收: UserID=%s, TokenID=%s, FromUser=%s", 
        notification.UserID, notification.NFTTokenID, notification.FromUserID)
    
    // 业务逻辑：更新用户NFT资产、发送APP推送等
    updateUserNFTOwnership(notification.UserID, notification.CollectionID, notification.NFTTokenID)
    sendPushNotification(notification.UserID, "收到NFT转账", notification)
}
```

### 5.3 手动发送通知

**如果需要在代码中手动触发通知：**

```go
// 发送NFT成功通知
func sendNFTMintNotification(userID, operationID, collectionID, tokenID, txHash string, chainID int64) {
    notification := map[string]interface{}{
        "type": "nft_mint_success",
        "user_id": userID,
        "operation_id": operationID,
        "chain_id": chainID,
        "collection_id": collectionID,
        "nft_token_id": tokenID,
        "tx_hash": txHash,
        "timestamp": time.Now().Unix(),
        "status": "confirmed",
    }
    
    // 发送到 RabbitMQ notifications 队列
    publishToQueue("chainbridge.notification.0.0", notification)
}

// 发送余额变化通知
func sendBalanceChangeNotification(userID string, chainID int64, oldBalance, newBalance string) {
    notification := map[string]interface{}{
        "type": "balance_changed",
        "user_id": userID,
        "chain_id": chainID,
        "old_balance": oldBalance,
        "new_balance": newBalance,
        "timestamp": time.Now().Unix(),
    }
    
    // 发送到 RabbitMQ notifications 队列（使用实际的队列名称）
    publishToQueue("chainbridge.notification.0.0", notification)
}
```

### 5.4 监控和调试

**查看队列状态：**
```bash
# 查看队列中的消息数量
rabbitmqctl list_queues name messages

# 查看特定队列
rabbitmqctl list_queues name messages | grep payment_events

# 查看通知队列
rabbitmqctl list_queues name messages | grep notification

# 查看NFT相关队列
rabbitmqctl list_queues name messages | grep -E "(nft_mint|nft_burn|nft_transfer)"
```

**查看服务状态：**
```bash
# 查看支付事件服务状态 (通过API或日志)
curl http://localhost:8080/monitoring/payment-events/stats

# 查看NFT操作统计
curl http://localhost:8080/monitoring/nft-operations/stats

# 查看服务日志
docker logs your-app-container | grep -E "(payment event|notification|nft)"

# 查看NFT相关日志
docker logs your-app-container | grep -E "(nft_mint|nft_burn|nft_transfer|token_id)"
```

**调试NFT OperationID关联：**
```bash
# 查看特定批次的token_id映射
psql -h localhost -U user -d chainbridge -c "
    SELECT batch_id, operation_id, user_id, nft_token_id, created_at 
    FROM transactions 
    WHERE batch_id = 'your_batch_id' AND tx_type = 'nft_mint' 
    ORDER BY created_at ASC;"

# 查看NFT资产状态
psql -h localhost -U user -d chainbridge -c "
    SELECT collection_id, token_id, owner_user_id, is_minted, is_locked, operation_id 
    FROM nft_assets 
    WHERE collection_id = 'your_collection' AND token_id IN ('101', '102', '103');"
```
func consumeNotifications() {
    // 根据你的配置设置正确的队列名称
    queueName := "chainbridge.notification.0.0"  // 使用实际的队列前缀
    
    // 连接设置同上...
    
    for msg := range msgs {
        var notification NotificationMessage
        json.Unmarshal(msg.Body, &notification)
        
        switch notification.Type {
        case "balance_changed":
            handleBalanceChanged(notification)
        case "payment_made":
            handlePaymentNotification(notification)
        case "transaction_status_changed":
            handleTransactionStatusChanged(notification)
        case "batch_status_changed":
            handleBatchStatusChanged(notification)
        }
    }
}
```

### 3.3 手动发送通知

**如果需要在代码中手动触发通知：**

```go
// 发送余额变化通知
func sendBalanceChangeNotification(userID string, chainID int64, oldBalance, newBalance string) {
    notification := map[string]interface{}{
        "type": "balance_changed",
        "user_id": userID,
        "chain_id": chainID,
        "old_balance": oldBalance,
        "new_balance": newBalance,
        "timestamp": time.Now().Unix(),
    }
    
    // 发送到 RabbitMQ notifications 队列（使用实际的队列名称）
    publishToQueue("chainbridge.notification.0.0", notification)
}
```

### 3.4 监控和调试

**查看队列状态：**
```bash
# 查看队列中的消息数量
rabbitmqctl list_queues name messages

# 查看特定队列
rabbitmqctl list_queues name messages | grep payment_events

# 查看通知队列
rabbitmqctl list_queues name messages | grep notification
```

**查看服务状态：**
```bash
# 查看支付事件服务状态 (通过API或日志)
curl http://localhost:8080/monitoring/payment-events/stats

# 查看服务日志
docker logs your-app-container | grep "payment event\|notification"
```

## 6. 常见问题

### 6.1 PaymentMade事件相关问题

#### 为什么没有收到支付事件？

**检查清单：**
1. 确认数据库中配置了正确的 `payment_contract_address`
2. 确认链是启用状态 (`is_enabled = true`)
3. 确认 RabbitMQ 连接正常
4. 检查区块链 RPC 连接是否正常
5. 查看应用日志中的错误信息

#### 如何添加新的链支持？

```sql
-- 1. 在chains表中添加新链
INSERT INTO chains (chain_id, name, short_name, rpc_url, payment_contract_address, is_enabled) 
VALUES (137, 'Polygon', 'MATIC', 'https://polygon-rpc.com/', '0x...', true);

-- 2. 重启服务或调用配置重载API
```

### 6.2 NFT通知相关问题

#### NFT批量操作中的OperationID关联问题

**问题**: 批量铸造时无法正确关联token_id到具体用户

**解决方案**: 系统已实现双层OperationID架构：
- 每个批次有一个Batch OperationID（用于幂等性）
- 每个单独操作有Individual OperationID（用于精确追踪）

**验证方法**:
```sql
-- 检查批次中的operation_id分配
SELECT batch_id, operation_id, user_id, nft_token_id, created_at 
FROM transactions 
WHERE batch_id = 'your_batch_id' AND tx_type = 'nft_mint' 
ORDER BY created_at ASC;
```

#### NFT资产状态不正确

**常见情况**:
- NFT仍显示`is_locked = true`
- token_id仍为"-1"占位符
- 用户未收到成功通知

**排查步骤**:
1. 检查区块链交易确认状态
2. 查看TxConfirmationWatcher日志
3. 验证NFT资产finalization过程

```sql
-- 检查NFT资产状态
SELECT collection_id, token_id, owner_user_id, is_minted, is_burned, is_locked, operation_id 
FROM nft_assets 
WHERE operation_id = 'your_operation_id';
```

#### 为什么NFT通知延迟？

**原因**:
- 区块链确认等待时间（默认6个区块）
- 批处理机制可能导致操作排队
- 网络拥堵影响交易确认速度

**优化建议**:
- 监控区块确认时间
- 适当调整确认块数配置
- 实现交易状态实时查询API

### 6.3 通用问题

#### 消息处理失败怎么办？

- **自动重试**: 系统会自动重试失败的消息
- **死信队列**: 多次失败的消息会进入死信队列
- **监控告警**: 建议设置 RabbitMQ 监控告警
- **手动处理**: 可以通过管理界面查看和重新处理失败的消息

#### 性能优化建议

1. **批量处理**: 使用批量处理消息而不是逐条处理
2. **并发消费**: 启动多个消费者并行处理
3. **队列配置**: 根据消息量调整队列的 prefetch 设置
4. **监控**: 定期监控队列深度和处理延迟
5. **NFT优化**: 合理设置批次大小，避免过大批次影响性能

#### 如何测试通知系统？

```bash
# 测试NFT铸造通知
curl -X POST http://localhost:8080/nft/mint \
  -H "Content-Type: application/json" \
  -d '{
    "collection_id": "test_collection",
    "mint_operations": [
      {"to_user_id": "test_user", "metadata_uri": "https://example.com/1.json"}
    ]
  }'

# 检查通知队列
rabbitmqctl list_queues name messages | grep notification

# 查看具体通知内容（需要消费者程序）
```

## 7. API 接口

### 7.1 获取支付事件统计
```http
GET /monitoring/payment-events/stats
```

**响应示例：**
```json
{
  "total_listeners": 3,
  "healthy_listeners": 3,
  "total_events_processed": 1245,
  "total_errors": 2,
  "listeners": {
    "chain_1": {
      "chain_id": 1,
      "payment_address": "0x...",
      "last_processed_block": 18600000,
      "total_events": 856,
      "total_errors": 1
    }
  }
}
```

### 7.2 获取NFT操作统计
```http
GET /monitoring/nft-operations/stats
```

**响应示例：**
```json
{
  "total_mint_operations": 1500,
  "total_burn_operations": 250,
  "total_transfer_operations": 800,
  "pending_operations": 12,
  "confirmed_operations": 2538,
  "notification_stats": {
    "nft_mint_success_sent": 1485,
    "nft_burn_success_sent": 245,
    "nft_transfer_success_sent": 790,
    "nft_transfer_received_sent": 790,
    "notification_failures": 3
  },
  "batch_stats": {
    "total_batches": 156,
    "avg_operations_per_batch": 16.2,
    "avg_confirmation_time_seconds": 180
  }
}
```

### 7.3 获取通知系统统计
```http
GET /monitoring/notifications/stats
```

**响应示例：**
```json
{
  "total_notifications_sent": 5248,
  "total_failures": 15,
  "success_rate": 99.7,
  "by_type": {
    "nft_mint_success": 1485,
    "nft_burn_success": 245,
    "nft_transfer_success": 790,
    "nft_transfer_received": 790,
    "balance_changed": 1256,
    "transaction_status_changed": 682
  },
  "queue_stats": {
    "queue_name": "chainbridge.notification.0.0",
    "pending_messages": 3,
    "avg_processing_time_ms": 25
  }
}
```

### 7.4 重新加载配置
```http
POST /monitoring/payment-events/reload
```

这会重新从数据库加载配置并重启监听器。

### 7.5 手动触发NFT通知（测试用）
```http
POST /testing/trigger-nft-notification
Content-Type: application/json

{
  "type": "nft_mint_success",
  "user_id": "test_user_123",
  "operation_id": "550e8400-e29b-41d4-a716-446655440001",
  "chain_id": 1,
  "collection_id": "cpop_genesis",
  "nft_token_id": "1001",
  "tx_hash": "0xabcdef..."
}
```

---

## 8. 系统架构概览

### 8.1 整体架构图

```
用户API请求
    ↓
Chain Bridge 服务
    │
    ├── PaymentMade事件监听
    │   ├─ 以太坊监听器
    │   ├─ BSC监听器  
    │   └─ Polygon监听器
    │       ↓
    │   payment_events.{chain_id} 队列
    │
    ├── NFT操作处理
    │   ├─ NFT服务层
    │   ├─ 批处理队列
    │   └─ 区块链执行
    │       ↓
    │   TxConfirmationWatcher
    │       ↓
    │   NFT成功通知
    │
    └── 通知系统
        ├─ NotificationProcessor
        ├─ BatchProcessor 
        └─ RabbitMQ notification 队列
            ↓
        用户应用消费者
```

### 8.2 NFT操作生命周期

```
1. API请求 (Mint/Burn/Transfer)
    ↓
2. 数据验证和业务检查  
    ↓
3. 数据库记录创建 (status: recorded)
    ├─ Individual OperationID 生成
    ├─ Batch OperationID 关联
    └─ NFT资产锁定 (is_locked: true)
    ↓
4. 加入批处理队列
    ├─ NFTMintJob / NFTBurnJob / NFTTransferJob
    └─ 双层OperationID信息
    ↓
5. 批处理器执行
    ├─ 构建区块链交易
    ├─ 提交到区块链
    └─ 更新状态 (status: submitted)
    ↓
6. TxConfirmationWatcher监控
    ├─ 等待区块确认 (6个区块)
    ├─ Token ID提取和映射
    └─ NFT资产最终化
    ↓
7. 成功通知发送
    ├─ 操作者通知
    └─ 接收者通知（转账时）
```

### 8.3 关键组件说明

#### TxConfirmationWatcher
- **功能**: 监控区块链交易确认状态
- **特性**: 支持NFT资产最终化和通知发送
- **配置**: 6个区块确认（可配置）

#### NotificationProcessor
- **功能**: 处理和发送各类通知
- **实现**: BatchProcessor实现该接口
- **特性**: 非阻塞设计，失败不影响主流程

#### 双层OperationID系统
- **Batch Level**: 批次级别幂等性和追踪
- **Individual Level**: 个人操作级别精确关联
- **好处**: 兼容批量效率和精确追踪

---

## 总结

这个系统提供了完整的支付事件监听和NFT操作通知功能：

### 主要特性
1. **PaymentMade 事件**会自动从区块链捕获并发送到对应的队列
2. **NFT操作通知**在区块链确认后发送，确保可靠性
3. **双层OperationID架构**解决了批量操作中的精确关联问题
4. **统一通知系统**处理所有类型的系统通知
5. **完善的监控和调试工具**

### 使用指导
- **开发人员**只需要创建相应的消费者来处理这些消息
- **系统**提供了完善的监控和调试工具
- **架构**支持高并发和大规模处理

按照本指南配置和使用，你就能够接收和处理所有的支付事件、NFT操作通知和系统通知了。