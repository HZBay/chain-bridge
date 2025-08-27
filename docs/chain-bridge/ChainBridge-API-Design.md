# ChainBridge API设计 - 基于go-starter框架

## 项目结构

```
chain-bridge/
├── api/                          # API定义目录 (Swagger规范)
│   ├── swagger.yml               # 主Swagger文件
│   ├── definitions/              # 数据模型定义
│   │   ├── common.yml           # 通用模型
│   │   ├── account.yml           # 钱包相关模型
│   │   ├── transfer.yml         # 转账相关模型
│   │   ├── assets.yml           # 资产相关模型
│   │   └── errors.yml           # 错误模型
│   └── paths/                   # API路径定义
│       ├── account.yml           # 钱包管理API
│       ├── transfer.yml         # 转账操作API
│       ├── assets.yml           # 资产查询API（Alchemy增强）
│       ├── nft.yml              # NFT操作API
│       └── system.yml           # 系统管理API
├── internal/
│   ├── api/                     # 生成的API代码
│   │   ├── handlers/            # HTTP处理器
│   │   ├── models/              # 数据模型(从swagger生成)
│   │   └── restapi/             # REST API配置
│   ├── service/                 # 业务逻辑层
│   └── repository/              # 数据访问层
└── cmd/
    └── server/                  # 服务器入口
```

## go-starter 开发流程

### 1. API定义 (Swagger-First)

#### 主配置文件
```yaml
# api/swagger.yml
swagger: "2.0"
info:
  title: "ChainBridge API"
  version: "2.0.0"
  description: "Universal Account Relayer with Alchemy Integration for Web2 developers"
  contact:
    name: "ChainBridge Team"
    email: "tech@chain-bridge.com"

host: "api.chain-bridge.com"
basePath: "/api/v1"
schemes: ["https"]

consumes:
  - "application/json"
produces:
  - "application/json"

securityDefinitions:
  ApiKeyAuth:
    type: apiKey
    name: X-API-Key
    in: header
    description: "API Key for authentication"

security:
  - ApiKeyAuth: []

tags:
  - name: "account"
    description: "Account management operations"
  - name: "transfer"
    description: "Transfer operations"
  - name: "assets"
    description: "Asset management operations (Alchemy enhanced)"
  - name: "nft"
    description: "NFT operations"
  - name: "system"
    description: "System management operations"

paths:
  # 钱包管理
  /account/{user_id}:
    $ref: "./paths/account.yml#/account_get"
  /account/{user_id}/deploy:
    $ref: "./paths/account.yml#/account_deploy"
  
  # 转账操作
  /transfer:
    $ref: "./paths/transfer.yml#/transfer_create"
  /transfer/p2p:
    $ref: "./paths/transfer.yml#/transfer_p2p"
  /transfer/batch:
    $ref: "./paths/transfer.yml#/transfer_batch"
  
  # 资产查询
  /assets/{user_id}:
    $ref: "./paths/assets.yml#/assets_get"
  /assets/{user_id}/balance:
    $ref: "./paths/assets.yml#/balance_get"
  
  # 交易历史
  /transactions/{user_id}:
    $ref: "./paths/assets.yml#/transactions_get"
  /transaction/{transaction_id}:
    $ref: "./paths/assets.yml#/transaction_get"

definitions:
  # 通用模型
  $ref: "./definitions/common.yml"
  
  # 钱包模型
  $ref: "./definitions/account.yml"
  
  # 转账模型
  $ref: "./definitions/transfer.yml"
  
  # 资产模型
  $ref: "./definitions/assets.yml"
  
  # 错误模型
  $ref: "./definitions/errors.yml"
```

#### 路径定义示例
```yaml
# api/paths/transfer.yml
transfer_create:
  post:
    tags: ["transfer"]
    summary: "Create a new transfer"
    description: "Initiate a transfer operation that will be processed in batch"
    operationId: "postTransfer"
    parameters:
      - name: "payload"
        in: "body"
        required: true
        schema:
          $ref: "#/definitions/TransferRequest"
    responses:
      200:
        description: "Transfer created successfully"
        schema:
          $ref: "#/definitions/TransferResponse"
      400:
        description: "Invalid request"
        schema:
          $ref: "#/definitions/ErrorResponse"
      403:
        description: "Insufficient balance or permission denied"
        schema:
          $ref: "#/definitions/ErrorResponse"
      429:
        description: "Rate limit exceeded"
        schema:
          $ref: "#/definitions/ErrorResponse"
      500:
        description: "Internal server error"
        schema:
          $ref: "#/definitions/ErrorResponse"

transfer_p2p:
  post:
    tags: ["transfer"]
    summary: "Create P2P transfer between users"
    description: "Transfer assets between two users using their user IDs"
    operationId: "postTransferP2P"
    parameters:
      - name: "payload"
        in: "body"
        required: true
        schema:
          $ref: "#/definitions/TransferP2PRequest"
    responses:
      200:
        description: "P2P transfer created successfully"
        schema:
          $ref: "#/definitions/TransferResponse"
      400:
        description: "Invalid request"
        schema:
          $ref: "#/definitions/ErrorResponse"
      403:
        description: "Insufficient balance"
        schema:
          $ref: "#/definitions/ErrorResponse"

transfer_batch:
  post:
    tags: ["transfer"]
    summary: "Create batch transfers"
    description: "Create multiple transfers in a single request for gas optimization"
    operationId: "postTransferBatch"
    parameters:
      - name: "payload"
        in: "body"
        required: true
        schema:
          $ref: "#/definitions/TransferBatchRequest"
    responses:
      200:
        description: "Batch transfer created successfully"
        schema:
          $ref: "#/definitions/TransferBatchResponse"
      400:
        description: "Invalid request"
        schema:
          $ref: "#/definitions/ErrorResponse"
```

#### 数据模型定义
```yaml
# api/definitions/transfer.yml
TransferRequest:
  type: "object"
  required:
    - "from_user_id"
    - "to_address"
    - "chain_id"
    - "asset_type"
    - "amount"
  properties:
    from_user_id:
      type: "string"
      format: "uuid"
      description: "Source user ID"
      example: "550e8400-e29b-41d4-a716-446655440000"
    to_address:
      type: "string"
      pattern: "^0x[a-fA-F0-9]{40}$"
      description: "Destination address"
      example: "0x1234567890123456789012345678901234567890"
    chain_id:
      type: "integer"
      format: "int64"
      description: "Blockchain network ID"
      example: 1
      enum: [1, 56, 137, 42161]
    asset_type:
      type: "string"
      description: "Type of asset to transfer"
      example: "ERC20"
      enum: ["ETH", "ERC20", "CPOP", "NFT"]
    contract_address:
      type: "string"
      pattern: "^0x[a-fA-F0-9]{40}$"
      description: "Contract address (required for ERC20 and NFT)"
      example: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    amount:
      type: "string"
      pattern: "^[0-9]+\\.?[0-9]*$"
      description: "Amount to transfer (in token units)"
      example: "100.500000"
    memo:
      type: "string"
      maxLength: 256
      description: "Optional memo for the transfer"
      example: "Payment for services"
    gas_mode:
      type: "string"
      description: "Gas payment mode"
      example: "sponsored"
      enum: ["sponsored", "self"]
      default: "sponsored"
    priority:
      type: "string"
      description: "Transaction priority"
      example: "normal"
      enum: ["low", "normal", "high"]
      default: "normal"

TransferResponse:
  type: "object"
  required:
    - "transaction_id"
    - "status"
    - "estimated_confirmation"
  properties:
    transaction_id:
      type: "string"
      format: "uuid"
      description: "Unique transaction identifier"
      example: "tx_550e8400-e29b-41d4-a716-446655440000"
    status:
      type: "string"
      description: "Current transaction status"
      example: "pending"
      enum: ["pending", "submitted", "confirmed", "failed"]
    estimated_confirmation:
      type: "integer"
      description: "Estimated confirmation time in seconds"
      example: 45
    gas_fee_usd:
      type: "number"
      format: "float"
      description: "Estimated gas fee in USD"
      example: 2.50
    from_address:
      type: "string"
      pattern: "^0x[a-fA-F0-9]{40}$"
      description: "Source address"
      example: "0x5678901234567890123456789012345678901234"
    to_address:
      type: "string"
      pattern: "^0x[a-fA-F0-9]{40}$"
      description: "Destination address"
      example: "0x1234567890123456789012345678901234567890"
    explorer_url:
      type: "string"
      format: "uri"
      description: "Blockchain explorer URL (available after submission)"
      example: "https://etherscan.io/tx/"
    batch_info:
      $ref: "#/definitions/BatchInfo"

BatchInfo:
  type: "object"
  properties:
    will_be_batched:
      type: "boolean"
      description: "Whether this transaction will be batched"
      example: true
    estimated_batch_time:
      type: "integer"
      description: "Estimated time until batch processing in seconds"
      example: 15
    current_batch_size:
      type: "integer"
      description: "Current number of operations in the batch"
      example: 23

TransferP2PRequest:
  type: "object"
  required:
    - "from_user_id"
    - "to_user_id"
    - "chain_id"
    - "asset_type"
    - "amount"
  properties:
    from_user_id:
      type: "string"
      format: "uuid"
      description: "Source user ID"
    to_user_id:
      type: "string"
      format: "uuid"
      description: "Destination user ID"
    chain_id:
      type: "integer"
      format: "int64"
      description: "Blockchain network ID"
    asset_type:
      type: "string"
      enum: ["ETH", "ERC20", "CPOP", "NFT"]
    contract_address:
      type: "string"
      pattern: "^0x[a-fA-F0-9]{40}$"
      description: "Contract address (required for ERC20)"
    amount:
      type: "string"
      pattern: "^[0-9]+\\.?[0-9]*$"
      description: "Amount to transfer"
    memo:
      type: "string"
      maxLength: 256
      description: "Optional memo"

TransferBatchRequest:
  type: "object"
  required:
    - "from_user_id"
    - "transfers"
  properties:
    from_user_id:
      type: "string"
      format: "uuid"
      description: "Source user ID for all transfers"
    transfers:
      type: "array"
      minItems: 1
      maxItems: 100
      items:
        $ref: "#/definitions/BatchTransferItem"
    gas_mode:
      type: "string"
      enum: ["sponsored", "self"]
      default: "sponsored"

BatchTransferItem:
  type: "object"
  required:
    - "chain_id"
    - "asset_type"
    - "amount"
  properties:
    to_address:
      type: "string"
      pattern: "^0x[a-fA-F0-9]{40}$"
      description: "Destination address (use this OR to_user_id)"
    to_user_id:
      type: "string"
      format: "uuid"
      description: "Destination user ID (use this OR to_address)"
    chain_id:
      type: "integer"
      format: "int64"
    asset_type:
      type: "string"
      enum: ["ETH", "ERC20", "CPOP", "NFT"]
    contract_address:
      type: "string"
      pattern: "^0x[a-fA-F0-9]{40}$"
    amount:
      type: "string"
      pattern: "^[0-9]+\\.?[0-9]*$"
    memo:
      type: "string"
      maxLength: 256

TransferBatchResponse:
  type: "object"
  required:
    - "batch_id"
    - "transactions"
  properties:
    batch_id:
      type: "string"
      format: "uuid"
      description: "Batch identifier"
    transactions:
      type: "array"
      items:
        $ref: "#/definitions/TransferResponse"
    total_gas_fee_usd:
      type: "number"
      format: "float"
      description: "Total estimated gas fee for all transfers"
```

#### 错误模型定义
```yaml
# api/definitions/errors.yml
ErrorResponse:
  type: "object"
  required:
    - "error"
  properties:
    error:
      type: "string"
      description: "Error message"
      example: "Insufficient balance"
    error_code:
      type: "integer"
      description: "Numeric error code"
      example: 2001
    details:
      type: "object"
      description: "Additional error details"
      additionalProperties: true
    request_id:
      type: "string"
      format: "uuid"
      description: "Unique request identifier for tracking"

ValidationErrorResponse:
  type: "object"
  required:
    - "error"
    - "validation_errors"
  properties:
    error:
      type: "string"
      example: "Validation failed"
    error_code:
      type: "integer"
      example: 1000
    validation_errors:
      type: "array"
      items:
        $ref: "#/definitions/ValidationError"

ValidationError:
  type: "object"
  required:
    - "field"
    - "message"
  properties:
    field:
      type: "string"
      description: "Field name that failed validation"
      example: "amount"
    message:
      type: "string"
      description: "Validation error message"
      example: "Amount must be greater than 0"
    code:
      type: "string"
      description: "Validation error code"
      example: "INVALID_AMOUNT"
```

### 2. 代码生成 (go-swagger)

#### 安装和配置
```bash
# 安装go-swagger
go install github.com/go-swagger/go-swagger/cmd/swagger@latest

# 生成服务器代码
swagger generate server -f api/swagger.yml -A chain-bridge

# 生成客户端代码
swagger generate client -f api/swagger.yml -A chain-bridge
```

#### 生成的代码结构
```
internal/
├── api/
│   ├── models/              # 生成的数据模型
│   │   ├── transfer_request.go
│   │   ├── transfer_response.go
│   │   └── error_response.go
│   ├── operations/          # 生成的操作处理器
│   │   ├── transfer/
│   │   │   ├── post_transfer.go
│   │   │   ├── post_transfer_p2p.go
│   │   │   └── post_transfer_batch.go
│   │   └── assets/
│   └── restapi/            # REST API配置
│       ├── configure_chain_bridge.go
│       ├── server.go
│       └── embedded_spec.go
```

### 3. 处理器实现

#### 转账处理器示例
```go
// internal/api/handlers/transfer_handler.go
package handlers

import (
    "context"
    "github.com/go-openapi/runtime/middleware"
    "github.com/your-org/chain-bridge/internal/api/models"
    "github.com/your-org/chain-bridge/internal/api/operations/transfer"
    "github.com/your-org/chain-bridge/internal/service"
)

type TransferHandler struct {
    transferService *service.TransferService
    logger          *logger.Logger
}

func NewTransferHandler(transferService *service.TransferService, logger *logger.Logger) *TransferHandler {
    return &TransferHandler{
        transferService: transferService,
        logger:          logger,
    }
}

// PostTransfer handles POST /transfer
func (h *TransferHandler) PostTransfer(params transfer.PostTransferParams) middleware.Responder {
    ctx := context.Background()
    
    // 1. 验证请求
    if err := h.validateTransferRequest(params.Payload); err != nil {
        return transfer.NewPostTransferBadRequest().WithPayload(&models.ErrorResponse{
            Error:     err.Error(),
            ErrorCode: 1001,
        })
    }
    
    // 2. 调用业务逻辑
    result, err := h.transferService.CreateTransfer(ctx, &service.CreateTransferRequest{
        FromUserID:      *params.Payload.FromUserID,
        ToAddress:       *params.Payload.ToAddress,
        ChainID:         *params.Payload.ChainID,
        AssetType:       *params.Payload.AssetType,
        ContractAddress: params.Payload.ContractAddress,
        Amount:          *params.Payload.Amount,
        Memo:            params.Payload.Memo,
        GasMode:         params.Payload.GasMode,
        Priority:        params.Payload.Priority,
    })
    
    if err != nil {
        h.logger.Error("Failed to create transfer", "error", err)
        
        // 根据错误类型返回不同响应
        switch err {
        case service.ErrInsufficientBalance:
            return transfer.NewPostTransferForbidden().WithPayload(&models.ErrorResponse{
                Error:     "Insufficient balance",
                ErrorCode: 2001,
            })
        case service.ErrRateLimitExceeded:
            return transfer.NewPostTransferTooManyRequests().WithPayload(&models.ErrorResponse{
                Error:     "Rate limit exceeded",
                ErrorCode: 3002,
            })
        default:
            return transfer.NewPostTransferInternalServerError().WithPayload(&models.ErrorResponse{
                Error:     "Internal server error",
                ErrorCode: 5001,
            })
        }
    }
    
    // 3. 构造响应
    response := &models.TransferResponse{
        TransactionID:         result.TransactionID,
        Status:                result.Status,
        EstimatedConfirmation: result.EstimatedConfirmation,
        GasFeeUsd:            result.GasFeeUSD,
        FromAddress:          result.FromAddress,
        ToAddress:            result.ToAddress,
        ExplorerURL:          result.ExplorerURL,
        BatchInfo: &models.BatchInfo{
            WillBeBatched:      result.BatchInfo.WillBeBatched,
            EstimatedBatchTime: result.BatchInfo.EstimatedBatchTime,
            CurrentBatchSize:   result.BatchInfo.CurrentBatchSize,
        },
    }
    
    return transfer.NewPostTransferOK().WithPayload(response)
}

func (h *TransferHandler) validateTransferRequest(req *models.TransferRequest) error {
    // 自定义验证逻辑（补充Swagger验证）
    if req.AssetType != nil && *req.AssetType == "ERC20" && req.ContractAddress == nil {
        return errors.New("contract_address is required for ERC20 transfers")
    }
    
    // 验证金额格式
    if req.Amount != nil {
        if _, ok := new(big.Int).SetString(*req.Amount, 10); !ok {
            return errors.New("invalid amount format")
        }
    }
    
    return nil
}
```

### 4. 服务器配置

#### 主配置文件
```go
// internal/api/restapi/configure_chain_bridge.go
package restapi

import (
    "crypto/tls"
    "net/http"
    
    "github.com/go-openapi/errors"
    "github.com/go-openapi/runtime"
    "github.com/go-openapi/runtime/middleware"
    
    "github.com/your-org/chain-bridge/internal/api/operations"
    "github.com/your-org/chain-bridge/internal/api/handlers"
)

//go:generate swagger generate server --target ../../api --name ChainBridge --spec ../../../api/swagger.yml --principal interface{}

func configureFlags(api *operations.ChainBridgeAPI) {
    // api.CommandLineOptionsGroups = []swag.CommandLineOptionsGroup{ ... }
}

func configureAPI(api *operations.ChainBridgeAPI) http.Handler {
    // 配置API中间件
    api.ServeError = errors.ServeError
    
    // 设置日志
    api.Logger = log.Printf
    
    // JSON处理
    api.UseSwaggerUI()
    api.JSONConsumer = runtime.JSONConsumer()
    api.JSONProducer = runtime.JSONProducer()
    
    // API Key认证
    api.APIKeyAuthAuth = func(token string) (interface{}, error) {
        // 验证API Key逻辑
        return validateAPIKey(token)
    }
    
    // 注册处理器
    setupHandlers(api)
    
    // 全局中间件
    api.PreServerShutdown = func() {}
    api.ServerShutdown = func() {}
    
    return setupGlobalMiddleware(api.Serve(setupMiddlewares))
}

func setupHandlers(api *operations.ChainBridgeAPI) {
    // 初始化依赖
    transferHandler := handlers.NewTransferHandler(
        transferService,
        logger,
    )
    
    // 注册转账处理器
    api.TransferPostTransferHandler = transferHandler.PostTransfer
    api.TransferPostTransferP2PHandler = transferHandler.PostTransferP2P
    api.TransferPostTransferBatchHandler = transferHandler.PostTransferBatch
    
    // 其他处理器...
}

func setupGlobalMiddleware(handler http.Handler) http.Handler {
    return middleware.Redoc(middleware.RedocOpts{
        BasePath: "/api/v1",
        SpecURL:  "/swagger.yml",
        Path:     "docs",
    }, handler)
}
```

### 5. 开发工作流

#### Makefile示例
```makefile
# Makefile

.PHONY: generate
generate: ## 生成API代码
	swagger generate server -f api/swagger.yml -A chain-bridge --exclude-main

.PHONY: validate
validate: ## 验证Swagger规范
	swagger validate api/swagger.yml

.PHONY: serve-docs
serve-docs: ## 启动文档服务器
	swagger serve api/swagger.yml --port=8081

.PHONY: generate-client
generate-client: ## 生成客户端SDK
	swagger generate client -f api/swagger.yml -A chain-bridge

.PHONY: test
test: ## 运行测试
	go test ./...

.PHONY: build
build: generate ## 构建应用
	go build -o bin/chain-bridge cmd/server/main.go

.PHONY: dev
dev: generate ## 开发模式运行
	go run cmd/server/main.go --port=8080
```

### 6. 开发流程总结

1. **设计API** - 在`api/`目录下编写Swagger YAML文件
2. **验证规范** - `make validate` 检查Swagger文件有效性
3. **生成代码** - `make generate` 生成Go结构体和处理器框架
4. **实现逻辑** - 在生成的处理器框架中实现业务逻辑
5. **测试API** - `make serve-docs` 启动Swagger UI进行测试
6. **构建部署** - `make build` 构建生产版本

这种Swagger-First的方法确保API设计与实现保持一致，并提供自动生成的文档和客户端SDK。