# ChainBridge 系统架构总览

## 📋 目录

- [系统概述](#系统概述)
- [整体架构](#整体架构)
- [核心模块](#核心模块)
- [数据流](#数据流)
- [技术栈](#技术栈)
- [部署架构](#部署架构)

---

## 🎯 系统概述

**ChainBridge** 是基于 CPOP 账户抽象技术栈构建的通用钱包 Relayer 服务，为 Web2 开发者提供托管式区块链资产管理的桥接服务。

### 核心价值

1. **抽象化区块链复杂性**: 将复杂的区块链操作封装为简单的 RESTful API
2. **多链统一管理**: 支持 Ethereum、BSC、Polygon 等主流区块链
3. **极致 Gas 优化**: 通过批量处理实现 75-85% 的 Gas 费用节省
4. **开发者友好**: 让 Web2 开发者像调用传统 API 一样使用区块链功能
5. **企业级可靠性**: 99.99% 可用性保障，完善的监控和告警

---

## 🏗️ 整体架构

### 架构图

```mermaid
graph TB
    subgraph "客户端层"
        WEB[Web 前端]
        MOBILE[移动应用]
        API_CLIENT[API 客户端]
    end
    
    subgraph "ChainBridge 服务层"
        subgraph "API Gateway"
            AUTH[认证中间件]
            RATE_LIMIT[限流中间件]
            LOGGING[日志中间件]
        end
        
        subgraph "业务服务层"
            ACCOUNT_SVC[账户服务]
            ASSET_SVC[资产服务]
            TRANSFER_SVC[转账服务]
            BATCH_SVC[批处理服务]
            NFT_SVC[NFT 服务]
            MONITOR_SVC[监控服务]
        end
        
        subgraph "批处理引擎"
            QUEUE_MGR[队列管理器]
            BATCH_OPT[批量优化器]
            CONSUMER_MGR[消费者管理器]
        end
        
        subgraph "区块链集成层"
            CPOP_CALLER[CPOP 调用器]
            WALLET_MGR[钱包管理器]
            GAS_OPT[Gas 优化器]
        end
    end
    
    subgraph "数据存储层"
        POSTGRES[(PostgreSQL)]
        REDIS[(Redis)]
        RABBITMQ[(RabbitMQ)]
    end
    
    subgraph "外部服务"
        ALCHEMY[Alchemy API]
        BLOCKCHAIN[区块链网络]
    end
    
    WEB --> AUTH
    MOBILE --> AUTH
    API_CLIENT --> AUTH
    
    AUTH --> ACCOUNT_SVC
    AUTH --> ASSET_SVC
    AUTH --> TRANSFER_SVC
    AUTH --> NFT_SVC
    AUTH --> MONITOR_SVC
    
    ACCOUNT_SVC --> BATCH_SVC
    ASSET_SVC --> BATCH_SVC
    TRANSFER_SVC --> BATCH_SVC
    NFT_SVC --> BATCH_SVC
    
    BATCH_SVC --> QUEUE_MGR
    QUEUE_MGR --> BATCH_OPT
    BATCH_OPT --> CONSUMER_MGR
    CONSUMER_MGR --> CPOP_CALLER
    
    CPOP_CALLER --> WALLET_MGR
    WALLET_MGR --> GAS_OPT
    GAS_OPT --> BLOCKCHAIN
    
    ASSET_SVC --> ALCHEMY
    ACCOUNT_SVC --> ALCHEMY
    
    ACCOUNT_SVC --> POSTGRES
    ASSET_SVC --> POSTGRES
    TRANSFER_SVC --> POSTGRES
    BATCH_SVC --> POSTGRES
    
    BATCH_SVC --> REDIS
    MONITOR_SVC --> REDIS
    
    QUEUE_MGR --> RABBITMQ
    CONSUMER_MGR --> RABBITMQ
```

### 架构特点

1. **分层架构**: 清晰的分层设计，职责分离
2. **微服务化**: 按业务功能模块化，便于扩展和维护
3. **异步处理**: 通过消息队列实现异步批处理
4. **缓存优化**: 多级缓存提升性能
5. **外部集成**: 集成 Alchemy API 提供高性能数据服务

---

## 🧩 核心模块

### 1. API Gateway 层

#### 认证中间件
- **JWT Token 验证**: 基于 JWT 的用户身份验证
- **权限控制**: 基于角色的访问控制 (RBAC)
- **API Key 管理**: 支持 API Key 认证方式

#### 限流中间件
- **请求频率限制**: 防止 API 滥用
- **用户级别限流**: 基于用户 ID 的个性化限流
- **IP 级别限流**: 防止恶意攻击

#### 日志中间件
- **结构化日志**: 使用 JSON 格式记录日志
- **请求追踪**: 每个请求分配唯一 ID
- **性能监控**: 记录请求处理时间

### 2. 业务服务层

#### 账户服务 (Account Service)
```go
type AccountService interface {
    CreateUserAccount(ctx context.Context, req *CreateAccountRequest) (*CreateAccountResponse, error)
    GetUserAccount(ctx context.Context, userID string, chainID int64) (*AccountResponse, error)
    DeployUserAccount(ctx context.Context, userID string, chainID int64) (*DeployAccountResponse, error)
}
```

**核心功能**:
- AA 钱包创建和部署
- 钱包地址预测
- 钱包状态管理
- 多链钱包统一管理

#### 资产服务 (Asset Service)
```go
type AssetService interface {
    GetUserAssets(ctx context.Context, userID string) (*AssetsResponse, error)
    AdjustAssets(ctx context.Context, req *AssetAdjustRequest) (*AssetAdjustResponse, error)
    GetAssetHistory(ctx context.Context, userID string, assetID string) (*AssetHistoryResponse, error)
}
```

**核心功能**:
- 用户资产查询
- 资产余额调整 (铸造/销毁)
- 资产历史记录
- 多链资产聚合

#### 转账服务 (Transfer Service)
```go
type TransferService interface {
    TransferAssets(ctx context.Context, req *TransferRequest) (*TransferResponse, error)
    GetTransferHistory(ctx context.Context, userID string) (*TransferHistoryResponse, error)
    GetTransferStatus(ctx context.Context, transferID string) (*TransferStatusResponse, error)
}
```

**核心功能**:
- 用户间资产转账
- 转账状态跟踪
- 转账历史查询
- 批量转账支持

#### NFT 服务 (NFT Service)
```go
type NFTService interface {
    BatchMintNFT(ctx context.Context, req *BatchMintNFTRequest) (*BatchMintNFTResponse, error)
    BatchTransferNFT(ctx context.Context, req *BatchTransferNFTRequest) (*BatchTransferNFTResponse, error)
    GetNFTMetadata(ctx context.Context, tokenID string) (*NFTMetadataResponse, error)
}
```

**核心功能**:
- NFT 批量铸造
- NFT 批量转账
- NFT 元数据管理
- NFT 集合管理

### 3. 批处理引擎

#### 队列管理器 (Queue Manager)
```go
type QueueManager interface {
    PublishJob(ctx context.Context, job Job) error
    GetQueueStatus(ctx context.Context) (*QueueStatusResponse, error)
    PauseQueue(ctx context.Context, queueName string) error
    ResumeQueue(ctx context.Context, queueName string) error
}
```

**核心功能**:
- 作业发布和调度
- 队列状态监控
- 队列生命周期管理
- 按链分队列架构

#### 批量优化器 (Batch Optimizer)
```go
type BatchOptimizer interface {
    CalculateOptimalBatchSize(chainID int64, tokenID int, jobType JobType) (int, error)
    EstimateGasSavings(batchSize int, chainID int64) (*GasSavings, error)
    GetOptimizationRecommendation(ctx context.Context) (*OptimizationRecommendation, error)
}
```

**核心功能**:
- 动态批量大小计算
- Gas 费用优化建议
- 性能指标分析
- 智能批处理策略

#### 消费者管理器 (Consumer Manager)
```go
type ConsumerManager interface {
    StartConsumers(ctx context.Context) error
    StopConsumers(ctx context.Context) error
    GetConsumerStatus(ctx context.Context) (*ConsumerStatusResponse, error)
    ScaleConsumers(ctx context.Context, chainID int64, workerCount int) error
}
```

**核心功能**:
- 按链消费者管理
- 动态扩缩容
- 消费者健康检查
- 故障自动恢复

### 4. 区块链集成层

#### CPOP 调用器 (CPOP Caller)
```go
type CPOPCaller interface {
    BatchMint(ctx context.Context, recipients []common.Address, amounts []*big.Int) (*BatchResult, error)
    BatchTransfer(ctx context.Context, from, to []common.Address, amounts []*big.Int) (*BatchResult, error)
    BatchBurn(ctx context.Context, accounts []common.Address, amounts []*big.Int) (*BatchResult, error)
}
```

**核心功能**:
- CPOP 合约调用封装
- 批量操作执行
- 交易状态跟踪
- 错误处理和重试

#### 钱包管理器 (Wallet Manager)
```go
type WalletManager interface {
    CreateWallet(ctx context.Context, owner common.Address, masterSigner common.Address) (*WalletInfo, error)
    GetWalletAddress(ctx context.Context, owner common.Address, masterSigner common.Address) (common.Address, error)
    IsWalletDeployed(ctx context.Context, owner common.Address, masterSigner common.Address) (bool, error)
}
```

**核心功能**:
- AA 钱包创建
- 钱包地址预测
- 钱包部署状态检查
- 多链钱包管理

---

## 📊 数据流

### 1. 用户资产查询流程

```mermaid
sequenceDiagram
    participant Client
    participant API
    participant AssetService
    participant AlchemyAPI
    participant Cache
    participant DB
    
    Client->>API: GET /api/v1/assets/{user_id}
    API->>AssetService: GetUserAssets(userID)
    
    AssetService->>Cache: 检查缓存
    alt 缓存命中
        Cache-->>AssetService: 返回缓存数据
    else 缓存未命中
        AssetService->>AlchemyAPI: 查询链上余额
        AlchemyAPI-->>AssetService: 返回余额数据
        AssetService->>DB: 查询本地数据
        DB-->>AssetService: 返回本地数据
        AssetService->>Cache: 更新缓存
    end
    
    AssetService-->>API: 返回聚合数据
    API-->>Client: 返回资产概览
```

### 2. 资产调整批处理流程

```mermaid
sequenceDiagram
    participant Client
    participant API
    participant AssetService
    participant BatchService
    participant Queue
    participant Consumer
    participant CPOPCaller
    participant Blockchain
    
    Client->>API: POST /api/v1/assets/adjust
    API->>AssetService: AdjustAssets(request)
    
    AssetService->>AssetService: 验证请求
    AssetService->>BatchService: 创建批处理作业
    BatchService->>Queue: 发布作业到队列
    
    Queue->>Consumer: 消费作业
    Consumer->>Consumer: 聚合同类型作业
    Consumer->>CPOPCaller: 执行批量操作
    CPOPCaller->>Blockchain: 调用 CPOP 合约
    
    Blockchain-->>CPOPCaller: 返回交易哈希
    CPOPCaller-->>Consumer: 返回执行结果
    Consumer->>Consumer: 更新数据库
    Consumer->>Queue: 确认消息处理
    
    AssetService-->>API: 返回处理结果
    API-->>Client: 返回响应
```

### 3. 钱包创建流程

```mermaid
sequenceDiagram
    participant Client
    participant API
    participant AccountService
    participant WalletManager
    participant CPOPCaller
    participant Blockchain
    
    Client->>API: POST /api/v1/account/{user_id}/create
    API->>AccountService: CreateUserAccount(request)
    
    AccountService->>WalletManager: 预测钱包地址
    WalletManager-->>AccountService: 返回预测地址
    
    AccountService->>AccountService: 保存钱包信息到数据库
    
    AccountService->>CPOPCaller: 部署钱包合约
    CPOPCaller->>Blockchain: 调用 WalletManager.CreateUserAccount
    
    Blockchain-->>CPOPCaller: 返回部署交易
    CPOPCaller-->>AccountService: 返回交易哈希
    
    AccountService->>AccountService: 更新钱包状态
    
    AccountService-->>API: 返回创建结果
    API-->>Client: 返回钱包信息
```

---

## 🛠️ 技术栈

### 后端技术栈

| 技术 | 版本 | 用途 | 说明 |
|------|------|------|------|
| Go | 1.23+ | 主要开发语言 | 高性能、并发友好的系统编程语言 |
| Echo | v4.x | Web 框架 | 轻量级、高性能的 HTTP 框架 |
| PostgreSQL | 15+ | 主数据库 | 可靠的关系型数据库 |
| Redis | 7+ | 缓存和会话存储 | 高性能的内存数据库 |
| RabbitMQ | 3.12+ | 消息队列 | 可靠的消息传递系统 |
| SQLBoiler | v4.x | ORM | 代码生成的 ORM 工具 |
| go-swagger | v0.30+ | API 文档生成 | Swagger 代码生成工具 |

### 区块链技术栈

| 技术 | 版本 | 用途 | 说明 |
|------|------|------|------|
| CPOP-ABIs | v0.0.0-20250822024504 | 合约交互 | CPOP 账户抽象合约 ABI |
| go-ethereum | v1.16.2+ | 以太坊客户端 | 以太坊 Go 客户端库 |
| Alchemy API | Latest | 区块链数据服务 | 高性能的区块链数据 API |

### 开发工具

| 工具 | 用途 | 说明 |
|------|------|------|
| Docker | 容器化 | 开发和生产环境容器化 |
| Docker Compose | 本地开发 | 多服务本地开发环境 |
| Make | 构建工具 | 项目构建和任务管理 |
| golangci-lint | 代码检查 | Go 代码质量检查工具 |
| sql-migrate | 数据库迁移 | 数据库版本管理工具 |

### 监控和运维

| 工具 | 用途 | 说明 |
|------|------|------|
| Prometheus | 指标收集 | 系统指标监控 |
| Grafana | 可视化 | 监控数据可视化 |
| ELK Stack | 日志分析 | 集中化日志管理 |
| Trivy | 安全扫描 | 容器安全漏洞扫描 |

---

## 🚀 部署架构

### 开发环境

```yaml
# docker-compose.yml
version: '3.8'
services:
  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=postgres://user:pass@postgres:5432/chainbridge
      - REDIS_URL=redis://redis:6379
      - RABBITMQ_URL=amqp://guest:guest@rabbitmq:5672
    depends_on:
      - postgres
      - redis
      - rabbitmq
  
  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: chainbridge
      POSTGRES_USER: user
      POSTGRES_PASSWORD: pass
    volumes:
      - postgres_data:/var/lib/postgresql/data
  
  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data
  
  rabbitmq:
    image: rabbitmq:3.12-management
    environment:
      RABBITMQ_DEFAULT_USER: guest
      RABBITMQ_DEFAULT_PASS: guest
    volumes:
      - rabbitmq_data:/var/lib/rabbitmq
```

### 生产环境

#### Kubernetes 部署

```yaml
# k8s-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: chainbridge
spec:
  replicas: 3
  selector:
    matchLabels:
      app: chainbridge
  template:
    metadata:
      labels:
        app: chainbridge
    spec:
      containers:
      - name: chainbridge
        image: chainbridge:latest
        ports:
        - containerPort: 8080
        env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: chainbridge-secrets
              key: database-url
        - name: REDIS_URL
          valueFrom:
            secretKeyRef:
              name: chainbridge-secrets
              key: redis-url
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /api/v1/health/healthy
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /api/v1/health/ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
```

#### 服务配置

```yaml
# k8s-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: chainbridge-service
spec:
  selector:
    app: chainbridge
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
  type: LoadBalancer
```

### 高可用架构

```mermaid
graph TB
    subgraph "负载均衡层"
        LB[Load Balancer]
    end
    
    subgraph "应用层"
        APP1[ChainBridge Pod 1]
        APP2[ChainBridge Pod 2]
        APP3[ChainBridge Pod 3]
    end
    
    subgraph "数据层"
        POSTGRES_MASTER[(PostgreSQL Master)]
        POSTGRES_SLAVE[(PostgreSQL Slave)]
        REDIS_CLUSTER[(Redis Cluster)]
        RABBITMQ_CLUSTER[(RabbitMQ Cluster)]
    end
    
    subgraph "外部服务"
        ALCHEMY[Alchemy API]
        BLOCKCHAIN[Blockchain Networks]
    end
    
    LB --> APP1
    LB --> APP2
    LB --> APP3
    
    APP1 --> POSTGRES_MASTER
    APP2 --> POSTGRES_MASTER
    APP3 --> POSTGRES_MASTER
    
    POSTGRES_MASTER --> POSTGRES_SLAVE
    
    APP1 --> REDIS_CLUSTER
    APP2 --> REDIS_CLUSTER
    APP3 --> REDIS_CLUSTER
    
    APP1 --> RABBITMQ_CLUSTER
    APP2 --> RABBITMQ_CLUSTER
    APP3 --> RABBITMQ_CLUSTER
    
    APP1 --> ALCHEMY
    APP2 --> ALCHEMY
    APP3 --> ALCHEMY
    
    APP1 --> BLOCKCHAIN
    APP2 --> BLOCKCHAIN
    APP3 --> BLOCKCHAIN
```

---

## 📈 性能指标

### 系统性能目标

| 指标 | 目标值 | 说明 |
|------|--------|------|
| API 响应时间 | < 200ms | 95% 的 API 请求响应时间 |
| 批处理吞吐量 | > 1000 TPS | 每秒处理的交易数量 |
| 系统可用性 | 99.99% | 年度系统可用时间 |
| 数据库连接池 | 100 | 最大并发数据库连接数 |
| 缓存命中率 | > 90% | Redis 缓存命中率 |
| 队列处理延迟 | < 5s | 消息从入队到处理完成的时间 |

### 监控指标

#### 应用层指标
- HTTP 请求数量和响应时间
- 错误率和状态码分布
- 内存和 CPU 使用率
- Goroutine 数量

#### 业务层指标
- 用户注册和活跃度
- 资产操作频率
- 批处理效率
- Gas 费用节省

#### 基础设施指标
- 数据库连接数和查询性能
- Redis 内存使用和命中率
- RabbitMQ 队列长度和处理速度
- 网络延迟和带宽使用

---

## 🔒 安全架构

### 认证和授权

1. **JWT Token 认证**: 基于 JWT 的无状态认证
2. **API Key 认证**: 支持 API Key 方式认证
3. **角色权限控制**: 基于 RBAC 的权限管理
4. **请求签名验证**: 重要操作需要请求签名

### 数据安全

1. **数据加密**: 敏感数据加密存储
2. **传输安全**: 全链路 HTTPS 加密
3. **数据库安全**: 数据库连接加密和访问控制
4. **密钥管理**: 安全的密钥存储和轮换

### 网络安全

1. **防火墙配置**: 严格的网络访问控制
2. **DDoS 防护**: 分布式拒绝服务攻击防护
3. **IP 白名单**: 限制访问来源
4. **速率限制**: API 调用频率限制

---

## 📚 总结

ChainBridge 系统采用现代化的微服务架构，通过清晰的分层设计、完善的批处理引擎、以及强大的区块链集成能力，为 Web2 开发者提供了简单易用的区块链资产管理服务。

**核心优势**:
1. **高性能**: 通过批处理和缓存优化实现高性能
2. **高可用**: 分布式架构和故障自动恢复
3. **易扩展**: 微服务架构支持水平扩展
4. **易维护**: 清晰的代码结构和完善的文档
5. **安全可靠**: 多层次的安全防护机制

通过持续的技术创新和架构优化，ChainBridge 将为区块链应用开发提供更加便捷和可靠的解决方案。