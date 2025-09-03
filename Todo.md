基于现有 CPOP Token 批处理架构，扩展支持 NFT（ERC721）的批量操作功能，包括批量铸造、销毁和转账。新功能需与现有 Token 处理逻辑保持兼容，同时满足 NFT 的特殊性要求。

详细设计参考 `docs/NFT-Batch-Processing-Solution.md`， 设计规范参考`CLAUDE.md`
目前已经实现API的定义与数据库migrations的编写。

## 核心需求

### 1. API 实现
- 完成 `internal/api/handlers/nft/` 目录下所有 API 处理器的实现
- 实现以下API：
  - NFT 批量铸造
  - NFT 批量销毁
  - NFT 批量转账
- 每个端点应包含：
  - 请求验证与参数解析
  - 业务逻辑处理
  - 数据库事务管理
  - 适当的状态码和响应

### 2. 服务层实现
- 完成 `internal/services/nft/` 目录下的服务实现
- 实现 NFT 批处理的核心业务逻辑：
  - 请求验证与预处理
  - 数据库记录创建（事务、批处理记录等）
  - 任务入队处理
  - 状态管理

### 3. 批处理逻辑扩展
- 扩展现有批处理系统以支持 NFT 操作类型
- 修改 `internal/queue/hybrid_processor.go` 中的 `StartBatchConsumer`：
  - 添加对 NFT 操作类型（`nft_mint`, `nft_burn`, `nft_transfer`）的识别和处理
  - 调用相应的 NFT 批量方法
- 确保与现有 Token 批处理逻辑共存且互不干扰

### 4. 区块链交互
- 使用 `github.com/HzBay/account-abstraction/cpop-abis/cp_nft.go` 中的 ABI 方法：
  - `BatchMint` - NFT 批量铸造
  - `BatchBurn` - NFT 批量销毁
  - `BatchTransfer` - NFT 批量转账
- 实现 `internal/blockchain/nft_caller.go` 中的调用逻辑

### 5. 数据库模型与状态管理
- 使用已创建的 NFT 相关数据表：
  - `transactions` - NFT 操作记录
  - `batches` - NFT 批处理任务
  - `nft_assets` - NFT 所有权记录
- 实现适当的状态流转：
  - NFT 交易状态：`pending` → `batching` → `submitted` → `confirmed`/`failed`
  - NFT 批次状态：`preparing` → `submitted` → `confirmed`/`failed`

### 6. 并发与一致性保证
- 对 NFT 所有权更新操作使用行级锁（`SELECT ... FOR UPDATE`）
- 确保在高并发场景下 NFT 所有权的唯一性和一致性
- 实现适当的错误处理和重试机制

## 实现注意事项

### 与现有系统的兼容性
- 不要修改现有的 CPOP Token 处理逻辑
- 新功能应作为独立模块实现，与现有系统松耦合
- 共享基础设施（如消息队列、数据库连接池等），但保持处理逻辑分离

### NFT 特殊性考虑
- NFT 具有唯一性（每个 tokenId 唯一），处理时需特别注意：
  - 铸造时确保 tokenId 唯一性
  - 转账时验证所有权
  - 销毁时验证所有权并更新状态
- 批量操作中单个项目的失败不应导致整个批次失败，应实现部分成功处理

### 错误处理与回滚
- 实现完善的错误处理机制
- 对于失败操作，提供明确的状态和错误信息
- 确保数据库状态与链上状态的一致性
- 实现适当的回滚机制

### 监控与日志
- 添加适当的日志记录，便于调试和审计
- 实现监控指标，跟踪 NFT 批处理的性能和成功率

## 优先级与交付要求

1. **第一阶段**：完成 API 实现和服务层基础架构
2. **第二阶段**：实现批处理逻辑和区块链交互
3. **第三阶段**：完善错误处理、状态管理和监控
4. **第四阶段**：进行集成测试和性能优化

## 验收标准

- 所有 API 端点正常工作，返回正确的状态码和响应
- NFT 批量铸造、销毁和转账功能正常
- 数据库状态正确更新，与链上状态保持一致
- 高并发场景下系统稳定，数据一致
- 适当的错误处理和日志记录
- 与现有 CPOP Token 功能共存且互不干扰

请按照以上需求完成 NFT 批处理功能的实现，保持代码风格与现有系统一致，并确保代码质量和可维护性。