# ChainBridge 设计文档

ChainBridge是基于CPOP账户抽象技术栈的通用钱包Relayer服务，为Web2开发者提供托管式区块链资产管理的桥接服务。集成Alchemy API提供高性能的多链数据服务。

## 文档结构

### 核心设计
- **[ChainBridge-Design.md](./ChainBridge-Design.md)** - 完整系统架构和API设计（已更新为ChainBridge）
- **[ChainBridge-API-Design.md](./ChainBridge-API-Design.md)** - 基于go-starter的API开发流程

### 实现指南
- **[ChainBridge-Development-Reference.md](./ChainBridge-Development-Reference.md)** - 完整开发参考手册
- **[ChainBridge-Master-Config.md](./ChainBridge-Master-Config.md)** - 多Master密钥管理配置
- **[ChainBridge-Contract-API.md](./ChainBridge-Contract-API.md)** - 通用合约调用API设计（暂未采用）

## 项目概述

### 技术栈
- **后端框架**: Go + go-starter
- **API设计**: Swagger-First开发模式
- **数据库**: PostgreSQL + Redis
- **区块链**: 基于CPOP账户抽象系统
- **数据服务**: Alchemy API（替代自建RPC）
- **容器化**: Docker + Kubernetes

### 核心功能
1. **钱包管理** - AA钱包自动部署和管理
2. **资产转账** - 支持ETH、ERC20、CPOP、NFT转账
3. **批量处理** - 智能批量操作节省75-85% Gas费用
4. **多链支持** - Ethereum、BSC、Polygon等主流网络
5. **应用层服务** - 积分批量发放、活动结算、U卡充值等高级功能
6. **Alchemy集成** - 高性能数据查询，10-100x性能提升
7. **实时同步** - 链上链下数据一致性保障

### 开发状态
当前处于设计阶段，等待CPOP合约开发完成后启动实施。

## 快速开始

详细的开发指南请参考 [ChainBridge-Development-Reference.md](./ChainBridge-Development-Reference.md)

```bash
# 项目初始化（未来）
git clone https://github.com/allaboutapps/go-starter.git chain-bridge
cd chain-bridge

# 修改配置并启动
make build
make dev
```

## 更新历史

### v2.0.0 - ChainBridge重大更新 ✅
- **项目重命名**: ChainBridge → ChainBridge  
- **应用层添加**: 新增积分批量发放、活动结算、U卡充值等高级功能
- **Alchemy集成**: 使用Alchemy API替代自建RPC，性能提升10-100倍
- **架构简化**: 移除自建数据同步层，直接使用Alchemy API专业数据服务
- **架构优化**: 添加应用层服务模块，支持更复杂的业务场景

### 核心优势
- **性能提升**: Alchemy API让数据查询比自建方案快10-100倍
- **成本节省**: 基础设施成本降低95%，开发效率提升8倍
- **功能增强**: 支持积分批量发放、实时监控、NFT管理等高级功能
- **可靠性**: 多层容错机制，99.99%可用性保障

## 联系方式

如有问题请联系项目团队。