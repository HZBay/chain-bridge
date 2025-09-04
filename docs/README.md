# ChainBridge 设计文档

ChainBridge 是基于 CPOP 账户抽象技术栈的通用钱包 Relayer 服务，为 Web2 开发者提供托管式区块链资产管理的桥接服务。

## 📚 文档结构

### 🏗️ 系统架构
- **[系统架构总览](./system-architecture-overview.md)** - 完整的系统架构设计和组件说明
- **[批处理系统架构](./batch-processing-architecture.md)** - 按链分队列的批处理系统设计
- **[配置管理架构](./configuration-management-architecture.md)** - 分层配置管理架构设计

### 📡 API 设计
- **[API 设计规范](./api-design-standards.md)** - RESTful API 设计规范和最佳实践
- **[ChainBridge API 设计](./chain-bridge/ChainBridge-API-Design.md)** - 基于 go-starter 的 API 开发流程

### 🗄️ 数据库设计
- **[数据库设计文档](./database-design.md)** - 完整的数据库表结构设计和优化策略

### 🔧 开发指南
- **[开发参考手册](./chain-bridge/ChainBridge-Development-Reference.md)** - 完整的开发参考手册
- **[主配置管理](./chain-bridge/ChainBridge-Master-Config.md)** - 多 Master 密钥管理配置

### 📊 业务功能
- **[资产调整实现](./assets-adjust-implementation.md)** - 资产调整功能的详细实现
- **[NFT 批处理方案](./NFT-Batch-Processing-Solution.md)** - NFT 批量处理解决方案
- **[NFT 批处理任务清单](./nft-batch-processing-todo.md)** - NFT 批处理功能实现任务清单
- **[支付事件和通知指南](./payment-events-and-notifications-guide.md)** - 支付事件和通知系统

### 🔄 集成方案
- **[RabbitMQ 集成总结](./rabbitmq-integration-summary.md)** - RabbitMQ 消息队列集成
- **[RabbitMQ 批处理消费者配置](./rabbitmq-batch-consumer-config.md)** - 批处理消费者配置
- **[链数据库集成总结](./chains-database-integration-summary.md)** - 链配置数据库集成

### 📈 监控和运维
- **[队列监控指标](./monitoring/queue-metrics.md)** - 队列性能监控指标
- **[系统健康检查](./monitoring/health-checks.md)** - 系统健康检查机制

## 🚀 快速开始

### 1. 了解系统架构
建议按以下顺序阅读文档：
1. [系统架构总览](./system-architecture-overview.md) - 了解整体架构
2. [API 设计规范](./api-design-standards.md) - 了解 API 设计原则
3. [数据库设计文档](./database-design.md) - 了解数据存储设计

### 2. 开发环境搭建
参考 [开发参考手册](./chain-bridge/ChainBridge-Development-Reference.md) 搭建开发环境。

### 3. 功能开发
- **API 开发**: 参考 [API 设计规范](./api-design-standards.md)
- **数据库开发**: 参考 [数据库设计文档](./database-design.md)
- **批处理开发**: 参考 [批处理系统架构](./batch-processing-architecture.md)

## 📋 文档维护

### 文档更新原则
1. **及时更新**: 代码变更时同步更新相关文档
2. **版本控制**: 重要变更需要版本标记
3. **示例完整**: 提供完整的代码示例和配置示例
4. **结构清晰**: 保持文档结构的一致性和可读性

### 文档贡献
1. 遵循现有的文档结构和格式
2. 使用 Markdown 格式编写
3. 包含必要的代码示例和图表
4. 确保文档的准确性和完整性

## 🔗 相关链接

- [项目主页](../README.md)
- [开发规范](../CLAUDE.md)
- [API 文档](../api/swagger.yml)
- [数据库迁移](../migrations/)

## 📞 联系方式

如有文档相关问题，请联系项目团队或提交 Issue。aster/docs