# Assets Adjust 批处理实现文档

## 概述

实现了 `/assets/adjust` API 端点的批处理功能，支持批量调整用户资产余额（铸造/销毁操作），与现有的 RabbitMQ 批处理系统完全集成。

## API 端点

### POST /assets/adjust

批量调整用户资产余额，支持智能批处理和 CPOP 优化。

#### 请求格式

```json
{
  "operation_id": "op_daily_rewards_001",
  "adjustments": [
    {
      "user_id": "user_123",
      "amount": "+100.0",
      "chain_id": 56,
      "token_symbol": "CPOP",
      "business_type": "reward",
      "reason_type": "daily_checkin",
      "reason_detail": "Daily check-in reward"
    },
    {
      "user_id": "user_456", 
      "amount": "-50.0",
      "chain_id": 56,
      "token_symbol": "CPOP",
      "business_type": "consumption",
      "reason_type": "marketplace_purchase",
      "reason_detail": "NFT purchase"
    }
  ],
  "batch_preference": {
    "priority": "normal",
    "max_delay_seconds": 300
  }
}
```

#### 响应格式

```json
{
  "data": {
    "operation_id": "op_daily_rewards_001",
    "processed_count": 2,
    "status": "recorded"
  },
  "batch_info": {
    "will_be_batched": true,
    "batch_type": "batchAdjustAssets",
    "current_batch_size": 12,
    "optimal_batch_size": 25,
    "expected_efficiency": "74-76%"
  }
}
```

## 实现架构

### 1. 服务层 (`/app/internal/services/assets/service.go`)

```go
type Service interface {
    AdjustAssets(ctx context.Context, req *types.AssetAdjustRequest) (*types.AssetAdjustResponse, *types.BatchInfo, error)
}
```

**主要功能:**
- 请求验证和参数校验
- 批量事务记录到数据库
- 发布到 RabbitMQ 批处理队列
- 集成批处理优化器
- 支持多种业务类型和优先级

**关键特性:**
- 每个调整操作创建独立的事务记录
- 自动检测调整类型（mint/burn）基于金额符号
- 根据业务类型设置优先级
- 事务原子性保证

### 2. Handler 层 (`/app/internal/api/handlers/assets/`)

#### 文件结构
```
internal/api/handlers/assets/
├── adjust_assets.go     # POST /assets/adjust 处理器
├── common.go           # 共享常量和工具函数 
└── handler_test.go     # 单元测试
```

#### 关键组件

**AdjustAssetsHandler** (`adjust_assets.go`)
- 参数绑定和验证
- 详细的审计日志
- 错误处理和响应格式化
- 集成 assets 服务层

**常量定义** (`common.go`)
```go
const (
    // 调整类型
    AdjustmentTypeMint = "mint"
    AdjustmentTypeBurn = "burn"
    
    // 业务类型
    BusinessTypeReward      = "reward"
    BusinessTypeGasFee      = "gas_fee" 
    BusinessTypeConsumption = "consumption"
    BusinessTypeRefund      = "refund"
    
    // 批处理限制
    MaxAdjustmentsPerRequest = 100
)
```

### 3. 队列集成

#### AssetAdjustJob 类型
```go
type AssetAdjustJob struct {
    ID              string    `json:"id"`
    TransactionID   uuid.UUID `json:"transaction_id"`
    ChainID         int64     `json:"chain_id"`
    TokenID         int       `json:"token_id"`
    UserID          string    `json:"user_id"`
    Amount          string    `json:"amount"`
    AdjustmentType  string    `json:"adjustment_type"` // "mint" or "burn"
    BusinessType    string    `json:"business_type"`
    ReasonType      string    `json:"reason_type"`
    ReasonDetail    string    `json:"reason_detail,omitempty"`
    Priority        Priority  `json:"priority"`
    CreatedAt       time.Time `json:"created_at"`
}
```

#### 批处理器集成
- 使用现有的 `BatchProcessor.PublishAssetAdjust()` 方法
- 集成 `BatchOptimizer` 获取最优批次大小
- 支持 RabbitMQ 和内存处理器的混合模式

## 业务逻辑

### 1. 调整类型检测
```go
adjustmentType := "mint"
amount := *adjustment.Amount
if len(amount) > 0 && amount[0] == '-' {
    adjustmentType = "burn"
}
```

### 2. 优先级设置
```go
func (s *service) determinePriority(businessType string) queue.Priority {
    switch businessType {
    case "gas_fee":
        return queue.PriorityHigh // Gas 费用需要快速处理
    case "reward":
        return queue.PriorityNormal // 奖励是标准优先级
    case "consumption":
        return queue.PriorityNormal // 消费是标准优先级
    case "refund":
        return queue.PriorityHigh // 退款需要快速处理
    default:
        return queue.PriorityNormal
    }
}
```

### 3. 数据库事务管理
- 所有调整操作在单个事务中执行
- 事务失败时自动回滚
- 批处理器发布失败不影响事务记录

## 验证和安全

### 1. 请求验证
- 操作 ID 必填且不能为空
- 调整数组不能为空，最多 100 条
- 每个调整项的必填字段验证
- 业务类型枚举值验证
- 金额格式验证（必须是有效的十进制数）

### 2. 业务规则
```go
// 支持的业务类型
validBusinessTypes := []string{"reward", "gas_fee", "consumption", "refund"}

// 金额格式验证
if _, ok := decimal.New(0, 0).SetString(*adjustment.Amount); !ok {
    return fmt.Errorf("invalid amount format")
}
```

## 使用示例

### 1. 单个用户奖励
```bash
curl -X POST /assets/adjust \
  -H "Content-Type: application/json" \
  -d '{
    "operation_id": "daily_reward_20241222_001",
    "adjustments": [{
      "user_id": "user_123",
      "amount": "+50.0",
      "chain_id": 56,
      "token_symbol": "CPOP", 
      "business_type": "reward",
      "reason_type": "daily_checkin",
      "reason_detail": "Daily check-in bonus"
    }]
  }'
```

### 2. 批量用户奖励分发
```bash
curl -X POST /assets/adjust \
  -H "Content-Type: application/json" \
  -d '{
    "operation_id": "weekly_rewards_20241222",
    "adjustments": [
      {
        "user_id": "user_001",
        "amount": "+100.0",
        "chain_id": 56,
        "token_symbol": "CPOP",
        "business_type": "reward",
        "reason_type": "weekly_active",
        "reason_detail": "Weekly activity reward"
      },
      {
        "user_id": "user_002", 
        "amount": "+75.0",
        "chain_id": 56,
        "token_symbol": "CPOP",
        "business_type": "reward",
        "reason_type": "weekly_active",
        "reason_detail": "Weekly activity reward"
      }
    ]
  }'
```

### 3. Gas 费用扣除
```bash
curl -X POST /assets/adjust \
  -H "Content-Type: application/json" \
  -d '{
    "operation_id": "gas_deduction_20241222_001",
    "adjustments": [{
      "user_id": "user_456",
      "amount": "-2.5",
      "chain_id": 56,
      "token_symbol": "CPOP",
      "business_type": "gas_fee", 
      "reason_type": "transaction_fee",
      "reason_detail": "Smart contract interaction fee"
    }]
  }'
```

## 监控和日志

### 1. 审计日志
```go
log.Info().
    Str("operation_id", *req.OperationID).
    Int("adjustment_count", len(req.Adjustments)).
    Msg("Processing asset adjustment request")

log.Info().
    Str("operation_id", *adjustResponse.OperationID).
    Str("status", *adjustResponse.Status).
    Int64("processed_count", *adjustResponse.ProcessedCount).
    Bool("will_be_batched", batchInfo.WillBeBatched).
    Msg("Asset adjustment request completed successfully")
```

### 2. 错误处理
- 详细的验证错误信息
- 数据库操作错误处理
- 批处理器发布失败处理
- HTTP 错误状态码规范

## 测试覆盖

### 1. 单元测试 (`handler_test.go`)
- Handler 成功处理测试
- 参数验证错误测试
- Mock 服务集成测试
- 常量定义验证

### 2. 集成测试建议
- 数据库事务测试
- RabbitMQ 队列集成测试
- 批处理器优化测试
- 端到端 API 测试

## 性能考虑

### 1. 批处理优化
- 集成现有的 `BatchOptimizer`
- 动态调整批次大小
- 支持优先级队列

### 2. 限制和约束
- 单次请求最多 100 个调整
- 事务超时保护
- 内存使用优化

## 总结

Assets Adjust API 的实现特点：

✅ **完整的批处理集成** - 与现有 RabbitMQ 系统无缝集成  
✅ **灵活的业务类型支持** - 支持奖励、消费、退款、Gas 费用等场景  
✅ **智能优化** - 集成批处理优化器，动态调整批次大小  
✅ **安全可靠** - 完整的参数验证和事务管理  
✅ **监控友好** - 详细的日志记录和错误处理  
✅ **可测试** - 完整的单元测试覆盖  

这个实现为 CPOP 生态系统提供了强大的资产管理能力，支持高效的批量操作和智能优化。