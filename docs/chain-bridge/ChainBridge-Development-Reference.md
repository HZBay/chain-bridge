# ChainBridge 开发参考手册

## 项目概述

**ChainBridge** - 基于CPOP账户抽象技术栈的通用钱包Relayer服务，为Web2开发者提供托管式区块链资产管理的桥接服务。集成Alchemy API提供高性能的多链数据服务。

### 技术栈选择
- **后端框架**: Go + [allaboutapps/go-starter](https://github.com/allaboutapps/go-starter)
- **API设计**: Swagger-First开发模式
- **数据库**: PostgreSQL + Redis
- **区块链**: 基于CPOP账户抽象系统
- **数据服务**: Alchemy API（替代自建RPC和数据同步）
- **容器化**: Docker + Kubernetes

## 核心设计文档

### 1. 主要设计文档
- `CustodialBridge-Design.md` - 完整系统设计文档（已更新为ChainBridge）
- `CustodialBridge-API-Design.md` - API设计和go-starter集成方案
- ~~`Chain-Sync-Strategy.md`~~ - ~~链上链下数据同步策略~~（已被Alchemy API替代）

### 2. 现有CPOP合约集成点

#### CPOP Token合约关键功能
```solidity
// contracts/cpop/interfaces/ICPOPToken.sol
interface ICPOPToken {
    // 批量铸造 - CustodialBridge积分发放
    function batchMint(address[] calldata recipients, uint256[] calldata amounts) external;
    
    // 批量销毁 - CustodialBridge积分扣除  
    function batchBurn(address[] calldata accounts, uint256[] calldata amounts) external;
    
    // 余额查询
    function balanceOf(address account) external view returns (uint256);
    
    // 转账授权检查
    function isAuthorizedTransfer(address from, address to) external view returns (bool);
}
```

#### AAWallet合约关键功能
```solidity
// contracts/cpop/AAWallet.sol
contract AAWallet {
    // 钱包部署和初始化
    function initialize(address _owner) external;
    function initializeWithMasterSigner(address _owner, address _masterSigner) external;
    
    // 余额管理
    function getDeposit() external view returns (uint256);
    function addDeposit() external payable;
    function withdrawDepositTo(address payable withdrawAddress, uint256 amount) external;
    
    // Session Key管理
    function addSessionKey(address sessionKey, uint48 validAfter, uint48 validUntil, bytes32 permissions) external;
    function revokeSessionKey(address sessionKey) external;
    
    // 聚合器支持
    function setAggregator(address _aggregator) external;
    function getAggregator() public view returns (address);
}
```

#### Gas Paymaster集成
```solidity
// contracts/cpop/GasPaymaster.sol
contract GasPaymaster {
    // 检查用户是否可以支付gas
    function canPayForGas(address user, uint256 gasAmount) external view returns (bool);
    
    // 估算Gas费用
    function estimateCost(uint256 gasAmount) external view returns (uint256);
    
    // 获取每日使用限额
    function getDailyUsage(address user) external view returns (uint256);
    function getDailyLimit(address user) external view returns (uint256);
}
```

## 项目结构规划

### 目录结构
```
chain-bridge/                    # 新项目根目录
├── README.md
├── Makefile
├── docker-compose.yml
├── Dockerfile
├── go.mod
├── go.sum
├── api/                            # Swagger API定义
│   ├── swagger.yml                 # 主Swagger文件
│   ├── definitions/                # 数据模型定义
│   │   ├── common.yml             # 通用响应模型
│   │   ├── wallet.yml             # 钱包相关模型
│   │   ├── transfer.yml           # 转账相关模型
│   │   ├── assets.yml             # 资产相关模型
│   │   ├── nft.yml                # NFT相关模型
│   │   └── errors.yml             # 错误模型
│   └── paths/                      # API路径定义
│       ├── wallet.yml             # 钱包管理API
│       ├── transfer.yml           # 转账操作API
│       ├── assets.yml             # 资产查询API
│       ├── nft.yml                # NFT操作API
│       └── system.yml             # 系统管理API
├── cmd/
│   ├── api-server/                # API服务器
│   │   └── main.go
│   ├── batch-processor/           # 批量处理器
│   │   └── main.go
│   ├── event-listener/            # 事件监听器
│   │   └── main.go
│   └── migration/                 # 数据库迁移工具
│       └── main.go
├── internal/
│   ├── api/                       # 生成的API代码
│   │   ├── handlers/              # HTTP处理器实现
│   │   ├── middleware/            # 中间件
│   │   ├── models/                # 从Swagger生成的数据模型
│   │   └── restapi/               # REST API配置
│   ├── service/                   # 业务逻辑层
│   │   ├── wallet/                # 钱包管理服务
│   │   ├── transfer/              # 转账服务
│   │   ├── asset/                 # 资产管理服务（Alchemy集成）
│   │   ├── batch/                 # 批量处理服务
│   │   ├── application/           # 应用层服务（新增）
│   │   └── alchemy/               # Alchemy API集成服务（新增）
│   ├── repository/                # 数据访问层
│   │   ├── user_wallet.go
│   │   ├── user_balance.go
│   │   ├── transaction.go
│   │   └── batch.go
│   ├── blockchain/                # 区块链交互
│   │   ├── client/                # 区块链客户端
│   │   ├── contracts/             # 智能合约绑定
│   │   └── events/                # 事件监听
│   ├── config/                    # 配置管理
│   ├── auth/                      # 认证模块
│   └── monitor/                   # 监控模块
├── pkg/                           # 公共库
│   ├── database/                  # 数据库操作
│   ├── crypto/                    # 加密工具
│   ├── logger/                    # 日志工具
│   ├── cache/                     # 缓存工具
│   └── utils/                     # 通用工具
├── migrations/                    # 数据库迁移文件
│   ├── 001_create_chains.sql
│   ├── 002_create_user_wallets.sql
│   ├── 003_create_supported_tokens.sql
│   ├── 004_create_user_balances.sql
│   ├── 005_create_transactions.sql
│   └── 006_create_batches.sql
├── deployments/                   # 部署配置
│   ├── k8s/                       # Kubernetes配置
│   ├── docker/                    # Docker配置
│   └── helm/                      # Helm charts
├── config/                        # 环境配置
│   ├── development.yaml
│   ├── staging.yaml
│   └── production.yaml
├── docs/                          # 项目文档
│   ├── api/                       # API文档
│   ├── deployment/                # 部署文档
│   └── development/               # 开发文档
└── tests/                         # 测试文件
    ├── integration/               # 集成测试
    ├── unit/                      # 单元测试
    └── fixtures/                  # 测试数据
```

## 开发阶段规划

### Phase 1: 基础框架搭建 (1-2周)
```bash
# 1. 初始化go-starter项目
git clone https://github.com/allaboutapps/go-starter.git chain-bridge
cd chain-bridge

# 2. 设置基础配置
# - 修改go.mod模块名
# - 配置数据库连接
# - 设置基础中间件

# 3. 创建核心数据表
# - 执行migration脚本
# - 设置数据库索引
# - 配置Redis连接
```

### Phase 2: CPOP集成 + Alchemy API (2-3周) 
```bash
# 1. 智能合约绑定生成
abigen --abi contracts/cpop/interfaces/ICPOPToken.sol --pkg contracts --out internal/blockchain/contracts/cpop_token.go

# 2. Alchemy API集成
# - 集成Alchemy SDK
# - 实现AlchemyService包装器
# - 配置多链连接

# 3. 实现核心服务
# - WalletService: AA钱包管理
# - BalanceService: CPOP余额管理（集成Alchemy）  
# - BatchService: 批量处理引擎

# 4. API实现
# - 钱包创建/查询API
# - CPOP积分转账API
# - 余额查询API（Alchemy增强）
```

### Phase 3: 应用层功能 (2-3周)
```bash
# 1. 应用层服务开发
# - ActivityManager: 任务活动管理
# - BatchRewardEngine: 积分批量发放
# - UCardManager: U卡记录管理

# 2. 高级资产管理
# - ERC20代币支持（基于Alchemy API）
# - NFT资产管理（自动过滤垃圾NFT）
# - 跨链余额统计（10-100x性能提升）

# 3. 实时数据功能
# - WebSocket事件推送
# - 交易状态实时更新
# - 价格数据集成
```

## 关键实现要点

### 1. Swagger-First开发流程

#### 开发命令
```bash
# 验证Swagger规范
make validate-swagger

# 生成API代码
make generate-api

# 启动开发服务器
make dev

# 启动文档服务器
make serve-docs
```

#### API设计原则
- **一致性**: 统一的错误响应格式
- **版本控制**: `/api/v1/` 路径前缀
- **安全认证**: API Key + JWT Token
- **限流保护**: 每用户/每API的速率限制

### 2. 批量处理核心算法

#### 触发条件
```go
type BatchTrigger struct {
    MaxBatchSize    int           // 最大批次大小: 50
    MaxWaitTime     time.Duration // 最大等待时间: 15秒
    HighPrioritySize int          // 高优先级立即处理: 10
}

// 批量处理逻辑
func (bp *BatchProcessor) shouldTrigger(chainID int64) bool {
    pending := bp.getPendingCount(chainID)
    
    // 条件1: 达到批次大小
    if pending >= bp.MaxBatchSize {
        return true
    }
    
    // 条件2: 有高优先级任务且达到小批次
    if bp.hasHighPriority(chainID) && pending >= bp.HighPrioritySize {
        return true
    }
    
    // 条件3: 超时触发
    if bp.getOldestPendingTime(chainID) > bp.MaxWaitTime {
        return true
    }
    
    return false
}
```

#### Gas优化策略
```go
// Gas节省计算
func calculateGasSavings(batchSize int) GasSavings {
    // 单笔交易成本
    singleTxGas := 21000 + 35000 // base + operation
    totalSingle := singleTxGas * batchSize
    
    // 批量交易成本  
    batchOverhead := 50000
    batchOperations := 25000 * batchSize
    totalBatch := batchOverhead + batchOperations
    
    saved := totalSingle - totalBatch
    percentage := float64(saved) / float64(totalSingle) * 100
    
    return GasSavings{
        SingleGas:    totalSingle,
        BatchGas:     totalBatch, 
        SavedGas:     saved,
        SavedPercent: percentage, // 通常75-85%
    }
}
```

### 3. 数据同步策略

#### 同步频率配置
```yaml
# config/sync.yaml
balance_sync:
  cpop_tokens: 0s          # 实时同步
  major_tokens: 5m         # 主流代币5分钟
  other_tokens: 10m        # 其他代币10分钟
  nft_assets: 15m          # NFT资产15分钟

reconciliation:
  balance_check: 1h        # 余额对账每小时
  transaction_check: 6h    # 交易对账每6小时
  auto_repair_threshold: "1000000"  # 自动修复阈值
```

#### 事件监听配置
```go
// 关键事件监听
var WatchEvents = []string{
    "Transfer",              // ERC20转账
    "TransferSingle",        // ERC1155转账  
    "UserOperationEvent",    // EntryPoint事件
    "Mint",                  // CPOP铸造
    "Burn",                  // CPOP销毁
    "SessionKeyAdded",       // Session Key添加
    "MasterSignerUpdated",   // Master Signer更新
}
```

### 4. 错误处理和重试

#### 错误分类处理
```go
type ErrorHandler struct {
    MaxRetries map[ErrorType]int
    RetryDelay map[ErrorType]time.Duration
}

var DefaultErrorConfig = ErrorHandler{
    MaxRetries: map[ErrorType]int{
        NetworkError:     3,
        GasPriceError:   2, 
        NonceError:      5,
        InsufficientFunds: 0, // 不重试
    },
    RetryDelay: map[ErrorType]time.Duration{
        NetworkError:     5 * time.Second,
        GasPriceError:   10 * time.Second,
        NonceError:      2 * time.Second,
    },
}
```

### 5. 监控和告警

#### 关键指标
```yaml
# prometheus指标
chain_bridge_api_requests_total{endpoint, method, status}
chain_bridge_batch_processing_duration_seconds{chain}
chain_bridge_gas_saved_percentage{chain}
chain_bridge_alchemy_api_latency_seconds{endpoint}
chain_bridge_transaction_success_rate{chain}
chain_bridge_database_connection_pool{state}
```

#### 告警规则
```yaml
# alerting规则
- alert: HighAPILatency
  expr: histogram_quantile(0.95, chain_bridge_api_duration_seconds) > 1
  
- alert: LowGasSavings  
  expr: chain_bridge_gas_saved_percentage < 70
  
- alert: AlchemyAPILatency
  expr: chain_bridge_alchemy_api_latency_seconds > 2
  
- alert: TransactionFailureRate
  expr: rate(chain_bridge_transaction_failures_total[5m]) > 0.05
```

## 集成测试策略

### 1. 单元测试
```bash
# 测试覆盖率要求
go test -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
# 目标覆盖率: >80%
```

### 2. 集成测试
```go
// 测试环境设置
func SetupIntegrationTest() *TestEnvironment {
    // 1. 启动测试数据库
    testDB := setupTestPostgreSQL()
    
    // 2. 启动模拟区块链
    mockChain := setupMockBlockchain()
    
    // 3. 部署测试合约
    testContracts := deployTestContracts(mockChain)
    
    return &TestEnvironment{
        DB:        testDB,
        Chain:     mockChain, 
        Contracts: testContracts,
    }
}
```

### 3. 性能测试
```bash
# API性能测试
k6 run tests/performance/api_load_test.js

# 批量处理性能测试  
k6 run tests/performance/batch_processing_test.js
```

## 部署和运维

### 1. Docker化
```dockerfile
# 多阶段构建
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o chain-bridge cmd/api-server/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/
COPY --from=builder /app/chain-bridge .
COPY --from=builder /app/config ./config
CMD ["./chain-bridge"]
```

### 2. Kubernetes部署
```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: chain-bridge-api
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
  template:
    spec:
      containers:
      - name: api
        image: chain-bridge:latest
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"  
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
```

### 3. 监控部署
```yaml
# monitoring/docker-compose.yml
version: '3.8'
services:
  prometheus:
    image: prom/prometheus:latest
    ports: ["9090:9090"]
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      
  grafana:
    image: grafana/grafana:latest
    ports: ["3000:3000"] 
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - ./dashboards:/var/lib/grafana/dashboards
      
  alertmanager:
    image: prom/alertmanager:latest
    ports: ["9093:9093"]
    volumes:
      - ./alertmanager.yml:/etc/alertmanager/alertmanager.yml
```

## 快速开始命令

### 项目初始化
```bash
# 1. 克隆并初始化项目
git clone https://github.com/allaboutapps/go-starter.git chain-bridge
cd chain-bridge

# 2. 修改模块名和基础配置
find . -name "*.go" -exec sed -i 's|allaboutapps/go-starter|your-org/chain-bridge|g' {} \;

# 3. 安装依赖
go mod tidy

# 4. 启动开发环境
make build
make dev
```

### API开发
```bash
# 1. 编辑API定义
vim api/swagger.yml

# 2. 验证和生成
make validate-swagger
make generate-api

# 3. 实现处理器
vim internal/api/handlers/

# 4. 测试API
curl -X POST http://localhost:8080/api/v1/transfer \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{"from_user_id":"user123","to_address":"0x...","amount":"100"}'
```

## 注意事项

### 1. 安全考虑
- API Key轮换策略
- 私钥安全存储(HSM/KMS)
- 数据库加密
- 网络安全(VPC/防火墙)

### 2. 性能优化
- 数据库连接池配置
- Redis缓存策略
- 批量操作优化
- 链接复用

### 3. 可靠性保障  
- 幂等性设计
- 事务一致性
- 故障恢复
- 数据备份

这份开发参考手册包含了ChainBridge项目的完整技术栈、实施计划和关键实现细节，结合Alchemy API集成的性能优势，可以作为正式开发时的完整指南。