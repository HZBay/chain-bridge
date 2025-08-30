# PaymentMade 事件监听与通知系统使用指南

## 概述

本指南介绍如何使用和配置 PaymentMade 事件监听系统和相关的通知功能，让开发人员能够快速上手使用这些功能。

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

## 2. 通知系统 (NotificationJob)

### 2.1 支持的通知类型

系统支持以下几种通知：

1. **用户余额变化通知** (`balance_changed`)
2. **交易状态变化通知** (`transaction_status_changed`)
3. **批处理状态变化通知** (`batch_status_changed`)
4. **支付成功通知** (`payment_made`)

### 2.2 通知消息格式

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

### 2.3 队列名称
所有通知都发送到：`notifications`

## 3. 开发人员使用指南

### 3.1 消费 PaymentMade 事件

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

### 3.2 消费通知消息

**创建通知消费者：**

```go
func consumeNotifications() {
    queueName := "notifications"
    
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
    
    // 发送到 RabbitMQ notifications 队列
    publishToQueue("notifications", notification)
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
rabbitmqctl list_queues name messages | grep notifications
```

**查看服务状态：**
```bash
# 查看支付事件服务状态 (通过API或日志)
curl http://localhost:8080/monitoring/payment-events/stats

# 查看服务日志
docker logs your-app-container | grep "payment event\|notification"
```

## 4. 常见问题

### 4.1 为什么没有收到支付事件？

**检查清单：**
1. 确认数据库中配置了正确的 `payment_contract_address`
2. 确认链是启用状态 (`is_enabled = true`)
3. 确认 RabbitMQ 连接正常
4. 检查区块链 RPC 连接是否正常
5. 查看应用日志中的错误信息

### 4.2 如何添加新的链支持？

```sql
-- 1. 在chains表中添加新链
INSERT INTO chains (chain_id, name, short_name, rpc_url, payment_contract_address, is_enabled) 
VALUES (137, 'Polygon', 'MATIC', 'https://polygon-rpc.com/', '0x...', true);

-- 2. 重启服务或调用配置重载API
```

### 4.3 消息处理失败怎么办？

- **自动重试**: 系统会自动重试失败的消息
- **死信队列**: 多次失败的消息会进入死信队列
- **监控告警**: 建议设置 RabbitMQ 监控告警
- **手动处理**: 可以通过管理界面查看和重新处理失败的消息

### 4.4 性能优化建议

1. **批量处理**: 使用批量处理消息而不是逐条处理
2. **并发消费**: 启动多个消费者并行处理
3. **队列配置**: 根据消息量调整队列的 prefetch 设置
4. **监控**: 定期监控队列深度和处理延迟

## 5. API 接口

### 5.1 获取支付事件统计
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

### 5.2 重新加载配置
```http
POST /monitoring/payment-events/reload
```

这会重新从数据库加载配置并重启监听器。

---

## 总结

这个系统提供了完整的支付事件监听和通知功能：

1. **PaymentMade 事件**会自动从区块链捕获并发送到对应的队列
2. **通知系统**会在关键状态变化时发送通知消息
3. **开发人员**只需要创建相应的消费者来处理这些消息
4. **系统**提供了完善的监控和调试工具

按照本指南配置和使用，你就能够接收和处理所有的支付事件和系统通知了。