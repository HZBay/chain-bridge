# Assets Adjust 接口余额处理逻辑文档

## 概述

本文档详细说明了 `/api/v1/assets/adjust` 接口中余额字段的设计逻辑和处理流程，供开发人员参考和维护。

## 余额字段设计

### 1. confirmed_balance（链上确认余额）
- **数据源**：`user_balances.confirmed_balance` 字段
- **含义**：链上已确认的可用余额
- **特点**：用户可以直接使用的余额

### 2. pending_balance（待确认余额）
- **触发条件**：接口中 `amount` 为正数（如 `+100`）
- **操作逻辑**：设置 `pending_balance = 100`
- **链上确认后**：
  - `confirmed_balance += pending_balance`
  - `pending_balance = 0`

### 3. locked_balance（锁定余额）
- **触发条件**：接口中 `amount` 为负数（如 `-100`）
- **操作逻辑**：设置 `locked_balance = 100`
- **链上确认后**：
  - `confirmed_balance -= locked_balance`
  - `locked_balance = 0`

## 业务规则

### 1. 互斥规则
- **重要**：如果用户对某个token有 `pending_balance > 0` 或 `locked_balance > 0`，系统将拒绝该用户的新调整操作进入MQ队列
- **目的**：防止并发操作导致的数据不一致

### 2. 金额处理规则
- **正数**（如 `+100`）：操作 `pending_balance`
- **负数**（如 `-100`）：操作 `locked_balance`
- **符号处理**：系统会自动处理 `+` 和 `-` 符号

## 技术实现

### 1. API层验证
- **位置**：`internal/services/assets/service.go`
- **功能**：验证用户余额状态
- **逻辑**：检查用户是否有 `pending_balance > 0` 或 `locked_balance > 0`，如果有则拒绝操作

### 2. MQ处理流程
- **位置**：`internal/queue/rabbitmq_consumer.go`
- **流程**：分为三个阶段

#### Freeze阶段（操作进入MQ时）
- **正数**：设置 `pending_balance += amount`
- **负数**：设置 `locked_balance += amount`

#### Finalize阶段（链上确认成功）
- **正数**：`confirmed_balance += amount`，`pending_balance -= amount`
- **负数**：`confirmed_balance -= amount`，`locked_balance -= amount`

#### Unfreeze阶段（链上确认失败）
- **正数**：清除 `pending_balance -= amount`
- **负数**：清除 `locked_balance -= amount`

## 数据库表结构

### user_balances 表字段说明
- **id**：主键
- **user_id**：用户ID
- **chain_id**：链ID
- **token_id**：代币ID
- **confirmed_balance**：链上确认余额（numeric(36, 18)）
- **pending_balance**：待确认余额（numeric(36, 18)）
- **locked_balance**：锁定余额（numeric(36, 18)）
- **约束**：唯一索引 (user_id, chain_id, token_id)

## 使用示例

### 示例1：增加余额（+100）
**请求参数**：
- user_id: "user123"
- chain_id: 1
- token_symbol: "USDT"
- amount: "+100"
- business_type: "reward"
- reason_type: "daily_checkin"

**处理流程**：
1. 验证用户无pending/locked余额
2. 设置 `pending_balance = 100`
3. 链上确认后：`confirmed_balance += 100`, `pending_balance = 0`

### 示例2：减少余额（-50）
**请求参数**：
- user_id: "user123"
- chain_id: 1
- token_symbol: "USDT"
- amount: "-50"
- business_type: "consumption"
- reason_type: "purchase"

**处理流程**：
1. 验证用户无pending/locked余额
2. 设置 `locked_balance = 50`
3. 链上确认后：`confirmed_balance -= 50`, `locked_balance = 0`

## 错误处理

### 1. 余额冲突错误
**错误信息**：`user user123 has pending balance 100 for token 1 on chain 1, cannot process new adjustment`
**原因**：用户已有pending或locked余额，系统拒绝新操作

### 2. 余额不足错误
**错误信息**：`insufficient confirmed or locked balance to finalize`
**原因**：链上确认时余额不足，无法完成操作

## 注意事项

1. **并发控制**：系统通过检查pending/locked余额来防止并发操作
2. **事务安全**：所有余额操作都在数据库事务中执行
3. **幂等性**：相同operation_id的操作会被拒绝，确保幂等性
4. **错误恢复**：失败的操作会自动unfreeze，恢复余额状态

## 相关文件

- `internal/services/assets/service.go` - API层业务逻辑
- `internal/queue/rabbitmq_consumer.go` - MQ消费者处理逻辑
- `internal/queue/types.go` - 消息队列数据结构
- `migrations/20241221000004-create-user-balances.sql` - 数据库表结构

## 更新历史

- 2024-12-21: 初始版本，实现基本的余额处理逻辑
- 2024-12-21: 添加pending/locked余额互斥验证
- 2024-12-21: 优化MQ处理流程，根据amount正负号操作不同字段
