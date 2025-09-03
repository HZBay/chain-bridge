# ChainBridge 开发流程规范

## 项目概述

基于 [go-starter](https://github.com/allaboutapps/go-starter) 模板的生产级 RESTful JSON 后端服务，使用 Go + PostgreSQL + Docker 技术栈。

## 核心特性

- **Docker 开发环境**: VSCode DevContainers + Docker Compose
- **数据库**: PostgreSQL + sql-migrate + SQLBoiler
- **API**: go-swagger 代码生成 + Echo 框架  
- **测试**: IntegreSQL 集成测试
- **监控**: 健康检查、性能分析
- **区块链交互**: 使用 cpop-abis 库进行链上操作

## 区块链集成

### CPOP-ABI 库 (最新版本)
使用 `github.com/HzBay/account-abstraction/cpop-abis` 进行链上交互，版本：`v0.0.0-20250822024504-56bf13b63504`

#### 核心合约组件详解

**1. CPOPToken** - 增强型 ERC20 代币合约
- 基础功能：标准 ERC20 (Transfer, Approve, BalanceOf, TotalSupply)
- 批量操作：
  - `BatchTransfer(recipients, amounts)`: 批量转账
  - `BatchTransferFrom(from, to, amounts)`: 批量授权转账
  - `BatchMint(recipients, amounts)`: 批量铸造
  - `BatchBurn(accounts, amounts)`: 批量销毁
- 角色管理系统：
  - `ADMIN_ROLE`, `MINTER_ROLE`, `BURNER_ROLE`: 角色常量
  - `GrantRole(account, role)`, `RevokeRole(account, role)`: 角色授权
  - `HasRole(account, role)`: 角色检查
- 管理功能：
  - `AdminBurn(from, amount)`: 管理员销毁
  - `Mint(to, amount)`: 铸造代币

**2. WalletManager** - 账户抽象钱包工厂
- 钱包创建：
  - `CreateUserAccount(owner, masterSigner)`: 标准用户账户
  - `CreateAccountWithMasterSigner(owner, masterSigner)`: 带主签名者的账户
  - `CreateWallet(owner, masterSigner)`: 通用钱包创建
- 地址预测：
  - `GetAccountAddress(owner, masterSigner)`: 预计算钱包地址
  - `IsAccountDeployed(owner, masterSigner)`: 检查部署状态
- 配置管理：
  - `GetDefaultMasterSigner()`: 获取默认主签名者
  - `SetDefaultMasterSigner(masterSigner)`: 设置默认主签名者
  - `GetInitCode(owner, masterSigner)`: 获取初始化代码
- 权限控制：
  - `AuthorizeCreator(creator)`: 授权创建者
  - `RevokeCreator(creator)`: 撤销创建者权限
  - `IsAuthorizedCreator(creator)`: 检查创建者权限

**3. AAWallet** - EIP-4337 智能合约钱包
- 账户抽象功能完全支持
- 交易执行和签名验证
- 与 EntryPoint 合约集成

**4. GasPaymaster** - Gas 代付系统
- 代币 Gas 费用支付
- 多代币支持
- 价格预言机集成

**5. MasterAggregator** - 签名聚合优化
- 批量操作签名聚合
- Gas 费用优化
- 提高交易吞吐量

**6. GasPriceOracle** - Gas 价格预言机
- 实时 Gas 价格数据
- 多网络支持
- 价格趋势分析

**7. SessionKeyManager** - 会话密钥管理
- 临时授权机制
- 权限范围控制
- 会话过期管理

#### 完整使用示例
```go
package main

import (
    "context"
    "crypto/ecdsa"
    "log"
    "math/big"
    "os"
    
    "github.com/ethereum/go-ethereum/accounts/abi/bind"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/crypto"
    "github.com/ethereum/go-ethereum/ethclient"
    
    cpop "github.com/HzBay/account-abstraction/cpop-abis"
)

func main() {
    // 1. 连接区块链
    client, err := ethclient.Dial(os.Getenv("ETH_RPC_URL"))
    if err != nil {
        log.Fatal("连接失败:", err)
    }
    
    // 2. 准备交易授权
    privateKey, _ := crypto.HexToECDSA(os.Getenv("PRIVATE_KEY"))
    chainID, _ := client.NetworkID(context.Background())
    auth, _ := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
    
    // 3. CPOP Token 操作
    tokenAddr := common.HexToAddress(os.Getenv("CPOP_TOKEN_ADDRESS"))
    token, _ := cpop.NewCPOPToken(tokenAddr, client)
    
    // 查询代币信息
    name, _ := token.Name(&bind.CallOpts{})
    symbol, _ := token.Symbol(&bind.CallOpts{})
    balance, _ := token.BalanceOf(&bind.CallOpts{}, auth.From)
    log.Printf("代币: %s (%s), 余额: %s", name, symbol, balance.String())
    
    // 批量转账示例
    recipients := []common.Address{
        common.HexToAddress("0x1..."),
        common.HexToAddress("0x2..."),
    }
    amounts := []*big.Int{
        big.NewInt(1000),
        big.NewInt(2000),
    }
    tx, _ := token.BatchTransfer(auth, recipients, amounts)
    log.Printf("批量转账交易: %s", tx.Hash().Hex())
    
    // 4. 钱包管理操作
    managerAddr := common.HexToAddress(os.Getenv("WALLET_MANAGER_ADDRESS"))
    manager, _ := cpop.NewWalletManager(managerAddr, client)
    
    // 预测钱包地址
    masterSigner := common.HexToAddress("0x...")
    predictedAddr, _ := manager.GetAccountAddress(&bind.CallOpts{}, auth.From, masterSigner)
    log.Printf("预测钱包地址: %s", predictedAddr.Hex())
    
    // 检查部署状态
    isDeployed, _ := manager.IsAccountDeployed(&bind.CallOpts{}, auth.From, masterSigner)
    if !isDeployed {
        // 创建新钱包
        createTx, _ := manager.CreateUserAccount(auth, auth.From, masterSigner)
        log.Printf("创建钱包交易: %s", createTx.Hash().Hex())
    }
    
    // 5. 权限管理
    // 检查创建者权限
    isAuthorized, _ := manager.IsAuthorizedCreator(&bind.CallOpts{}, auth.From)
    log.Printf("创建者权限状态: %v", isAuthorized)
}
```

#### 环境变量配置
```bash
# .env 配置文件
ETH_RPC_URL=https://your-rpc-endpoint
PRIVATE_KEY=your-private-key-hex
CHAIN_ID=1

# 合约地址配置
CPOP_TOKEN_ADDRESS=0x...
WALLET_MANAGER_ADDRESS=0x...
MASTER_AGGREGATOR_ADDRESS=0x...
GAS_PAYMASTER_ADDRESS=0x...
GAS_ORACLE_ADDRESS=0x...
SESSION_KEY_MANAGER_ADDRESS=0x...
```

#### 技术要求
- Go 1.23+
- github.com/ethereum/go-ethereum v1.16.2+
- 兼容 EIP-4337 Account Abstraction 标准

## API 开发流程（正确方式）

### 1. 定义 API 规范
首先在 `api/paths/` 和 `api/definitions/` 中定义接口：

```bash
# 1. 在 api/paths/ 中定义路径和参数
# 例如：api/paths/monitoring.yml

# 2. 在 api/definitions/ 中定义请求/响应类型
# 例如：api/definitions/monitoring.yml

# 3. 在 api/config/main.yml 中添加引用
```

### 2. 生成代码
```bash
make swagger  # 根据 API 定义生成 Go 类型文件
```

#### 复合响应类型定义
对于包含多个字段的复合响应（如 `data` + `batch_info`），必须定义具体的类型名称：

```yaml
# ❌ 错误：匿名响应对象
responses:
  "200":
    description: Success
    schema:
      type: object
      properties:
        data:
          $ref: "#/definitions/AssetAdjustResponse"
        batch_info:
          $ref: "#/definitions/BatchInfo"

# ✅ 正确：定义具体类型名称
AssetAdjustCompleteResponse:
  type: object
  required: [data, batch_info]
  properties:
    data:
      $ref: "#/definitions/AssetAdjustResponse"
    batch_info:
      $ref: "#/definitions/BatchInfo"
```

然后在 `api/config/main.yml` 中添加引用：
```yaml
definitions:
  assetAdjustCompleteResponse:
    $ref: "../definitions/assets.yml#/definitions/AssetAdjustCompleteResponse"
```

### 3. 实现 Handler
在生成的类型基础上编写 handler 逻辑：

```go
// 场景1: 只有请求体 (POST /assets/adjust)
var request types.AssetAdjustRequest
if err := util.BindAndValidateBody(c, &request); err != nil {
    return err
}

// 场景2: 只有路径参数 (GET /chains/{chain_id})
params := cpop.NewGetChainConfigParams()
if err := util.BindAndValidatePathParams(c, &params); err != nil {
    return err
}
chainID := params.ChainID

// 场景3: 路径参数+请求体 (POST /account/{user_id}/create)
params := cpop.NewCreateUserAccountParams()
if err := params.BindRequest(c.Request(), nil); err != nil {
    return err  // 同时验证路径参数和请求体
}
userID := params.UserID
request := params.Request

// 返回响应 - 直接使用生成类型
response := &types.CreateAccountResponse{...}
return util.ValidateAndReturn(c, http.StatusOK, response)

// 复合响应 - 先定义具体类型
response := &types.AssetAdjustCompleteResponse{
    Data:      adjustResponse,
    BatchInfo: batchInfo,  
}
return util.ValidateAndReturn(c, http.StatusOK, response)
```

### 4. Handler 文件组织
每个接口单独一个文件，参考 account 模式：
- `handler.go` - 只包含 Handler 结构体和 NewHandler 函数
- `get_xxx.go` - GET 接口实现
- `post_xxx.go` - POST 接口实现
- `put_xxx.go` - PUT 接口实现

## 数据库开发流程

### 1. 编写 Migration
在 `migrations/` 目录下编写数据库迁移文件

### 2. 生成代码
```bash
make sql  # 根据 migrations 生成相应的 Go 文件
```

## 构建流程 & Make 目标

### 主要构建命令
```bash
# 默认目标：格式化、构建、检查
make build

# 完整流程：清理、初始化、构建、测试
make all

# 快速检查：sql、swagger、生成、格式化、构建、检查
make
```

### 关键 Make 目标分类

#### 🚀 构建相关
```bash
make build          # 默认构建目标
make all            # 完整构建 + 测试
make go-build       # 仅 Go 编译
make go-format      # Go 代码格式化
make go-lint        # 代码检查
```

#### 📊 SQL/数据库
```bash
make sql            # 格式化 + 检查 + 生成 models
make sql-regenerate # 重新生成数据库相关代码
make sql-boiler     # SQLBoiler 生成 internal/models/*.go
make sql-format     # 格式化 SQL 文件
make sql-reset      # 重置开发数据库
make sql-spec-migrate # 应用迁移到 spec 数据库
```

#### 📋 Swagger/API
```bash
make swagger        # 生成 API 代码
make swagger-concat # 合并 API 定义文件
make swagger-generate # 生成 internal/types/*.go
make swagger-validate # 验证 swagger.yml
```

#### 🧪 测试相关
```bash
make test           # 运行测试
make test-by-name   # 按测试名显示
make watch-tests    # 监听文件变化运行测试
```

#### 🔧 初始化
```bash
make init           # 初始化：modules + tools + tidy
make modules        # 下载依赖模块
make tools          # 安装工具
make clean          # 清理临时文件
```

#### 📝 辅助工具
```bash
make help           # 显示常用目标
make help-all       # 显示所有目标
make info           # 显示项目信息
make set-module-name # 设置模块名
```

### 构建流程详解

1. **预构建 (build-pre)**:
   - `make sql` - 数据库代码生成
   - `make swagger` - API 代码生成  
   - `make contracts` - 合约代码生成 (no-op)
   - `make go-generate` - Go 代码生成

2. **构建 (build)**:
   - `go fmt ./...` - 代码格式化
   - `go build` - 编译二进制文件

3. **检查 (lint)**:
   - `golangci-lint` - 代码质量检查
   - 各种结构检查 (check-*)

## 本地开发环境

### 启动开发环境
```bash
./docker-helper.sh --up  # 启动开发容器
make all                 # 完整构建和测试
```

### 运行服务
```bash
make build               # 构建应用
app probe readiness -v   # 检查环境
app db migrate          # 数据库迁移
app db seed             # 数据填充
app server              # 启动服务

# 或一键启动
app server --probe --migrate --seed
```

### 监听文件变化
```bash
make watch-sql      # 监听 SQL 文件变化
make watch-swagger  # 监听 API 文件变化
make watch-tests    # 监听 Go 文件运行测试
```

## 重要原则

### ❌ 错误做法
- 直接使用 `c.JSON(http.StatusOK, response)` 返回响应
- 先写 handler 再补 API 定义
- 使用 `map[string]interface{}` 而不是生成的类型
- 匿名响应结构不定义具体类型名称
- **在 Service 层重复进行参数验证** (如 `validateRequest`, `validateConfig` 等方法)

### ✅ 正确做法
- API 优先：先定义接口规范，再生成代码，最后实现逻辑
- 使用 `util.ValidateAndReturn()` 返回响应
- 使用生成的类型而不是通用类型
- 每个接口单独文件
- **为复合响应定义具体类型名称**
- **参数验证策略** (根据接口类型选择)：
  - **只有请求体**: `util.BindAndValidateBody(c, &body)`
  - **只有路径参数**: `util.BindAndValidatePathParams(c, &params)`
  - **只有查询参数**: `util.BindAndValidateQueryParams(c, &params)`
  - **路径+查询参数**: `util.BindAndValidatePathAndQueryParams(c, &params)`
  - **复合参数** (路径+请求体等): `params.BindRequest(c.Request(), nil)`
- **参数验证分层原则**：
  - ✅ **Handler 层**：统一处理所有参数验证和类型转换
  - ❌ **Service 层**：不应包含重复的参数验证逻辑

## 常用命令快速参考

```bash
# 开发流程
make build                    # 完整构建
make test                     # 运行测试
make watch-tests             # 监听测试

# API 开发
make swagger                 # 生成 API 代码
make watch-swagger           # 监听 API 文件

# 数据库开发
make sql                     # 生成数据库代码
make sql-reset               # 重置数据库
make watch-sql               # 监听 SQL 文件

# 调试工具
make info                    # 项目信息
make get-embedded-modules    # 查看依赖模块
make trivy                   # 安全扫描

# 响应类型处理流程
# 1. 在 api/definitions/ 中定义具体类型 (如 AssetAdjustCompleteResponse)
# 2. 在 api/config/main.yml 中添加类型引用
# 3. 运行 make swagger 生成 Go 类型
# 4. 在 handler 中使用 util.ValidateAndReturn() 返回类型化响应
```

## 项目结构

```
├── api/                     # API 定义
│   ├── config/main.yml      # 主配置，包含所有类型引用
│   ├── definitions/         # 类型定义
│   │   ├── assets.yml       # 资产相关类型 (含 AssetAdjustCompleteResponse)
│   │   ├── transfer.yml     # 转账相关类型 (含 TransferCompleteResponse)
│   │   ├── account.yml       # 钱包相关类型
│   │   ├── monitoring.yml   # 监控相关类型
│   │   └── errors.yml       # 错误类型定义
│   ├── paths/              # 路径定义
│   │   ├── assets.yml       # 资产接口路径
│   │   ├── transfer.yml     # 转账接口路径
│   │   ├── account.yml       # 钱包接口路径
│   │   ├── chains.yml       # 链配置接口路径
│   │   └── monitoring.yml   # 监控接口路径
│   └── swagger.yml         # 生成的完整 API
├── internal/
│   ├── api/handlers/       # API 处理器
│   │   ├── chains/
│   │   ├── monitoring/
│   │   └── account/
│   ├── models/            # 生成的数据库模型
│   ├── types/             # 生成的 API 类型
│   └── services/          # 业务逻辑
├── migrations/            # 数据库迁移文件
├── bin/                  # 编译后的二进制文件
└── docker-compose.yml    # 开发环境配置
```

## Handler 文件组织示例

```
internal/api/handlers/
├── chains/
│   ├── handler.go              # Handler 结构体
│   ├── get_chains.go           # GET /chains
│   ├── get_chain_config.go     # GET /chains/{id}
│   ├── update_batch_config.go  # PUT /chains/{id}/batch-config
│   └── refresh_cache.go        # POST /chains/refresh-cache
├── monitoring/
│   ├── handler.go              # Handler 结构体  
│   ├── get_queue_metrics.go    # GET /monitoring/queue/metrics
│   ├── get_queue_stats.go      # GET /monitoring/queue/stats
│   ├── check_queue_health.go   # GET /monitoring/queue/health
│   └── get_optimization_recommendation.go
└── account/
    ├── handler.go
    ├── create_user_account.go
    └── get_user_account.go
```