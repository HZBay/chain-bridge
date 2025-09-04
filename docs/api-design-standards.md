# ChainBridge API 设计规范

## 📋 目录

- [设计原则](#设计原则)
- [API 规范](#api-规范)
- [数据模型设计](#数据模型设计)
- [错误处理](#错误处理)
- [认证和授权](#认证和授权)
- [版本管理](#版本管理)
- [文档规范](#文档规范)
- [最佳实践](#最佳实践)

---

## 🎯 设计原则

### 1. RESTful 设计原则

- **资源导向**: API 设计围绕资源而非操作
- **HTTP 方法语义**: 正确使用 GET、POST、PUT、DELETE 等方法
- **无状态**: 每个请求包含所有必要信息
- **统一接口**: 一致的 API 设计模式

### 2. 开发者友好

- **直观的 URL 结构**: 清晰、可预测的 API 路径
- **一致的响应格式**: 统一的成功和错误响应结构
- **详细的文档**: 完整的 API 文档和示例
- **版本兼容性**: 向后兼容的版本管理策略

### 3. 性能优化

- **分页支持**: 大数据集的分页查询
- **字段选择**: 支持字段过滤减少数据传输
- **缓存友好**: 设计支持 HTTP 缓存
- **批量操作**: 支持批量处理提高效率

---

## 📡 API 规范

### URL 设计规范

#### 基础 URL 结构
```
https://api.chainbridge.com/v1/{resource}
```

#### 资源命名规范
- 使用复数名词表示资源集合
- 使用小写字母和连字符分隔
- 保持简洁和直观

```bash
# ✅ 正确的资源命名
GET    /api/v1/assets                    # 获取资产列表
GET    /api/v1/assets/{asset_id}         # 获取特定资产
POST   /api/v1/assets/adjust             # 调整资产
GET    /api/v1/transfers                 # 获取转账列表
POST   /api/v1/transfers                 # 创建转账
GET    /api/v1/accounts/{user_id}        # 获取用户账户
POST   /api/v1/accounts/{user_id}/create # 创建用户账户

# ❌ 错误的资源命名
GET    /api/v1/getAssets                 # 动词不应出现在 URL 中
GET    /api/v1/asset                     # 应使用复数形式
POST   /api/v1/assetAdjustment           # 应使用连字符分隔
```

#### 路径参数规范
```bash
# 使用有意义的参数名
GET /api/v1/assets/{user_id}             # 用户 ID
GET /api/v1/chains/{chain_id}            # 链 ID
GET /api/v1/transfers/{transfer_id}      # 转账 ID
GET /api/v1/accounts/{user_id}/chains/{chain_id}  # 嵌套资源
```

#### 查询参数规范
```bash
# 分页参数
GET /api/v1/transfers?page=1&limit=20

# 过滤参数
GET /api/v1/transfers?status=completed&chain_id=1

# 排序参数
GET /api/v1/transfers?sort=created_at&order=desc

# 字段选择
GET /api/v1/assets?fields=id,symbol,balance

# 时间范围
GET /api/v1/transfers?from=2024-01-01&to=2024-01-31
```

### HTTP 方法使用规范

#### GET - 查询操作
```bash
# 获取资源列表
GET /api/v1/assets
GET /api/v1/transfers
GET /api/v1/chains

# 获取特定资源
GET /api/v1/assets/{asset_id}
GET /api/v1/transfers/{transfer_id}
GET /api/v1/accounts/{user_id}

# 获取子资源
GET /api/v1/accounts/{user_id}/assets
GET /api/v1/chains/{chain_id}/tokens
```

#### POST - 创建操作
```bash
# 创建新资源
POST /api/v1/transfers
POST /api/v1/accounts/{user_id}/create

# 执行操作
POST /api/v1/assets/adjust
POST /api/v1/accounts/{user_id}/deploy
POST /api/v1/batch/process
```

#### PUT - 更新操作
```bash
# 完整更新资源
PUT /api/v1/chains/{chain_id}/config
PUT /api/v1/tokens/{token_id}

# 更新状态
PUT /api/v1/transfers/{transfer_id}/status
```

#### DELETE - 删除操作
```bash
# 删除资源
DELETE /api/v1/tokens/{token_id}
DELETE /api/v1/accounts/{user_id}
```

### 响应格式规范

#### 成功响应格式
```json
{
  "data": {
    // 实际数据内容
  },
  "meta": {
    "timestamp": "2024-01-15T10:30:00Z",
    "request_id": "req_123456789",
    "version": "v1"
  }
}
```

#### 分页响应格式
```json
{
  "data": [
    // 数据数组
  ],
  "meta": {
    "timestamp": "2024-01-15T10:30:00Z",
    "request_id": "req_123456789",
    "version": "v1",
    "pagination": {
      "page": 1,
      "limit": 20,
      "total": 150,
      "total_pages": 8,
      "has_next": true,
      "has_prev": false
    }
  }
}
```

#### 复合响应格式
```json
{
  "data": {
    "operation_id": "op_123456789",
    "status": "recorded",
    "processed_count": 1
  },
  "batch_info": {
    "pending_operations": 3,
    "next_batch_estimate": "5-10 minutes",
    "will_be_batched": true,
    "batch_id": "batch_daily_rewards_20241215",
    "current_batch_size": 24,
    "optimal_batch_size": 25,
    "expected_efficiency": "75-77%",
    "estimated_gas_savings": "156.80 USD"
  },
  "meta": {
    "timestamp": "2024-01-15T10:30:00Z",
    "request_id": "req_123456789",
    "version": "v1"
  }
}
```

---

## 📊 数据模型设计

### 基础数据类型

#### 通用字段
```yaml
# 所有资源都包含的基础字段
BaseResource:
  type: object
  properties:
    id:
      type: string
      description: "资源唯一标识符"
      example: "asset_123456789"
    created_at:
      type: string
      format: date-time
      description: "创建时间"
      example: "2024-01-15T10:30:00Z"
    updated_at:
      type: string
      format: date-time
      description: "最后更新时间"
      example: "2024-01-15T10:30:00Z"
```

#### 分页参数
```yaml
PaginationParams:
  type: object
  properties:
    page:
      type: integer
      minimum: 1
      default: 1
      description: "页码，从1开始"
    limit:
      type: integer
      minimum: 1
      maximum: 100
      default: 20
      description: "每页数量"
    sort:
      type: string
      description: "排序字段"
      example: "created_at"
    order:
      type: string
      enum: [asc, desc]
      default: desc
      description: "排序方向"
```

#### 时间范围参数
```yaml
TimeRangeParams:
  type: object
  properties:
    from:
      type: string
      format: date-time
      description: "开始时间"
      example: "2024-01-01T00:00:00Z"
    to:
      type: string
      format: date-time
      description: "结束时间"
      example: "2024-01-31T23:59:59Z"
```

### 业务数据模型

#### 资产模型
```yaml
AssetInfo:
  type: object
  required: [chain_id, symbol, confirmed_balance, pending_balance, locked_balance]
  properties:
    chain_id:
      type: integer
      format: int64
      description: "区块链 ID"
      example: 56
    chain_name:
      type: string
      description: "区块链名称"
      example: "BSC"
    symbol:
      type: string
      description: "代币符号"
      example: "CPOP"
    name:
      type: string
      description: "代币名称"
      example: "ChainBridge PoP Token"
    contract_address:
      type: string
      description: "合约地址"
      example: "0x742d35Cc6634C0532925a3b8D238b45D2F78d8F3"
    decimals:
      type: integer
      description: "代币精度"
      example: 18
    confirmed_balance:
      type: string
      description: "链上确认余额"
      example: "5000.0"
    pending_balance:
      type: string
      description: "包含待处理变更的余额"
      example: "5050.0"
    locked_balance:
      type: string
      description: "锁定余额"
      example: "0.0"
    balance_usd:
      type: number
      format: float
      description: "USD 价值"
      example: 250.0
    sync_status:
      type: string
      enum: [synced, syncing, pending]
      description: "同步状态"
      default: synced
```

#### 转账模型
```yaml
TransferInfo:
  type: object
  required: [id, from_user_id, to_user_id, chain_id, token_symbol, amount, status]
  properties:
    id:
      type: string
      description: "转账 ID"
      example: "transfer_123456789"
    from_user_id:
      type: string
      description: "发送方用户 ID"
      example: "user_123"
    to_user_id:
      type: string
      description: "接收方用户 ID"
      example: "user_456"
    chain_id:
      type: integer
      format: int64
      description: "区块链 ID"
      example: 56
    token_symbol:
      type: string
      description: "代币符号"
      example: "CPOP"
    amount:
      type: string
      description: "转账金额"
      example: "100.0"
    status:
      type: string
      enum: [pending, processing, completed, failed]
      description: "转账状态"
    tx_hash:
      type: string
      description: "区块链交易哈希"
      example: "0x1234567890abcdef..."
    created_at:
      type: string
      format: date-time
      description: "创建时间"
    completed_at:
      type: string
      format: date-time
      description: "完成时间"
```

#### 账户模型
```yaml
AccountInfo:
  type: object
  required: [user_id, chain_id, aa_address, is_deployed]
  properties:
    user_id:
      type: string
      description: "用户 ID"
      example: "user_123"
    chain_id:
      type: integer
      format: int64
      description: "区块链 ID"
      example: 56
    aa_address:
      type: string
      description: "AA 钱包地址"
      example: "0x742d35Cc6634C0532925a3b8D238b45D2F78d8F3"
    owner:
      type: string
      description: "所有者地址"
      example: "0x1234567890abcdef..."
    is_deployed:
      type: boolean
      description: "是否已部署"
      example: true
    deployment_tx_hash:
      type: string
      description: "部署交易哈希"
      example: "0x1234567890abcdef..."
    master_signer:
      type: string
      description: "主签名者地址"
      example: "0x1234567890abcdef..."
    created_at:
      type: string
      format: date-time
      description: "创建时间"
```

### 请求模型设计

#### 资产调整请求
```yaml
AssetAdjustRequest:
  type: object
  required: [operation_id, adjustments]
  properties:
    operation_id:
      type: string
      description: "操作 ID，用于幂等性控制"
      example: "op_daily_rewards_001"
    adjustments:
      type: array
      items:
        $ref: "#/definitions/AssetAdjustment"
      description: "资产调整列表"
    batch_preference:
      $ref: "#/definitions/BatchPreference"

AssetAdjustment:
  type: object
  required: [user_id, chain_id, token_symbol, amount, business_type, reason_type]
  properties:
    user_id:
      type: string
      description: "用户 ID"
      example: "user_123"
    chain_id:
      type: integer
      format: int64
      description: "区块链 ID"
      example: 56
    token_symbol:
      type: string
      description: "代币符号"
      example: "CPOP"
    amount:
      type: string
      description: "调整金额（正数为增加，负数为减少）"
      example: "+100.0"
    business_type:
      type: string
      enum: [reward, gas_fee, consumption, refund]
      description: "业务类型"
    reason_type:
      type: string
      description: "原因类型"
      example: "daily_checkin"
    reason_detail:
      type: string
      description: "详细原因"
      example: "Daily check-in reward"

BatchPreference:
  type: object
  properties:
    priority:
      type: string
      enum: [low, normal, high]
      description: "处理优先级"
      default: normal
    max_wait_time:
      type: string
      description: "最大等待时间"
      example: "15m"
```

#### 转账请求
```yaml
TransferRequest:
  type: object
  required: [operation_id, from_user_id, to_user_id, chain_id, token_symbol, amount]
  properties:
    operation_id:
      type: string
      description: "操作 ID，用于幂等性控制"
      example: "transfer_123456789"
    from_user_id:
      type: string
      description: "发送方用户 ID"
      example: "user_123"
    to_user_id:
      type: string
      description: "接收方用户 ID"
      example: "user_456"
    chain_id:
      type: integer
      format: int64
      description: "区块链 ID"
      example: 56
    token_symbol:
      type: string
      description: "代币符号"
      example: "CPOP"
    amount:
      type: string
      description: "转账金额"
      example: "100.0"
    memo:
      type: string
      description: "转账备注"
      example: "Payment for services"
    batch_preference:
      $ref: "#/definitions/BatchPreference"
```

---

## ❌ 错误处理

### 错误响应格式

#### 标准错误响应
```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request validation failed",
    "details": [
      {
        "field": "amount",
        "message": "Amount must be a positive number"
      }
    ]
  },
  "meta": {
    "timestamp": "2024-01-15T10:30:00Z",
    "request_id": "req_123456789",
    "version": "v1"
  }
}
```

#### 验证错误响应
```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request validation failed",
    "details": [
      {
        "field": "operation_id",
        "in": "body.operation_id",
        "message": "Must be a valid UUID format"
      },
      {
        "field": "token_symbol",
        "in": "body.adjustments[0].token_symbol",
        "message": "Token 'INVALID' is not supported on chain 56"
      }
    ]
  },
  "meta": {
    "timestamp": "2024-01-15T10:30:00Z",
    "request_id": "req_123456789",
    "version": "v1"
  }
}
```

### HTTP 状态码使用规范

#### 2xx 成功状态码
- **200 OK**: 请求成功，返回数据
- **201 Created**: 资源创建成功
- **202 Accepted**: 请求已接受，异步处理中

#### 4xx 客户端错误
- **400 Bad Request**: 请求参数错误
- **401 Unauthorized**: 未认证或认证失败
- **403 Forbidden**: 已认证但无权限
- **404 Not Found**: 资源不存在
- **409 Conflict**: 资源冲突（如重复创建）
- **422 Unprocessable Entity**: 请求格式正确但业务逻辑错误
- **429 Too Many Requests**: 请求频率超限

#### 5xx 服务器错误
- **500 Internal Server Error**: 服务器内部错误
- **502 Bad Gateway**: 网关错误
- **503 Service Unavailable**: 服务不可用
- **504 Gateway Timeout**: 网关超时

### 错误代码规范

#### 错误代码分类
```yaml
# 错误代码定义
ErrorCodes:
  # 认证和授权错误 (AUTH_*)
  AUTH_TOKEN_INVALID: "认证令牌无效"
  AUTH_TOKEN_EXPIRED: "认证令牌已过期"
  AUTH_INSUFFICIENT_PERMISSIONS: "权限不足"
  
  # 验证错误 (VALIDATION_*)
  VALIDATION_REQUIRED_FIELD: "必填字段缺失"
  VALIDATION_INVALID_FORMAT: "字段格式无效"
  VALIDATION_OUT_OF_RANGE: "字段值超出范围"
  
  # 业务逻辑错误 (BUSINESS_*)
  BUSINESS_INSUFFICIENT_BALANCE: "余额不足"
  BUSINESS_ACCOUNT_NOT_FOUND: "账户不存在"
  BUSINESS_TOKEN_NOT_SUPPORTED: "代币不支持"
  
  # 系统错误 (SYSTEM_*)
  SYSTEM_DATABASE_ERROR: "数据库错误"
  SYSTEM_BLOCKCHAIN_ERROR: "区块链错误"
  SYSTEM_EXTERNAL_SERVICE_ERROR: "外部服务错误"
```

---

## 🔐 认证和授权

### 认证方式

#### JWT Token 认证
```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

#### API Key 认证
```http
X-API-Key: ak_1234567890abcdef...
```

### 权限控制

#### 角色定义
```yaml
# 用户角色
UserRoles:
  USER: "普通用户"
  ADMIN: "管理员"
  DEVELOPER: "开发者"
  OPERATOR: "运营人员"

# 权限定义
Permissions:
  # 账户权限
  ACCOUNT_READ: "读取账户信息"
  ACCOUNT_CREATE: "创建账户"
  ACCOUNT_UPDATE: "更新账户"
  ACCOUNT_DELETE: "删除账户"
  
  # 资产权限
  ASSET_READ: "读取资产信息"
  ASSET_ADJUST: "调整资产余额"
  ASSET_TRANSFER: "转账资产"
  
  # 管理权限
  ADMIN_READ: "读取管理信息"
  ADMIN_WRITE: "写入管理信息"
  ADMIN_DELETE: "删除管理信息"
```

#### 权限矩阵
| 角色 | ACCOUNT_READ | ACCOUNT_CREATE | ASSET_READ | ASSET_ADJUST | ADMIN_READ |
|------|-------------|---------------|------------|-------------|------------|
| USER | ✅ | ✅ | ✅ | ❌ | ❌ |
| ADMIN | ✅ | ✅ | ✅ | ✅ | ✅ |
| DEVELOPER | ✅ | ✅ | ✅ | ✅ | ✅ |
| OPERATOR | ✅ | ✅ | ✅ | ✅ | ❌ |

---

## 📚 版本管理

### 版本策略

#### URL 版本控制
```bash
# 当前版本
https://api.chainbridge.com/v1/assets

# 未来版本
https://api.chainbridge.com/v2/assets
```

#### 版本兼容性
- **向后兼容**: 新版本必须兼容旧版本
- **废弃通知**: 提前 6 个月通知 API 废弃
- **版本支持**: 同时支持最近 2 个主要版本

### 版本变更规范

#### 兼容性变更
- 添加新的可选字段
- 添加新的 API 端点
- 添加新的枚举值
- 扩展现有字段的允许值

#### 不兼容变更
- 删除字段或端点
- 修改字段类型
- 修改必需字段
- 修改错误响应格式

---

## 📖 文档规范

### Swagger 文档结构

#### API 定义文件组织
```
api/
├── config/
│   └── main.yml              # 主配置文件
├── definitions/              # 数据模型定义
│   ├── assets.yml           # 资产相关模型
│   ├── transfer.yml         # 转账相关模型
│   ├── account.yml          # 账户相关模型
│   ├── errors.yml           # 错误模型
│   └── common.yml           # 通用模型
├── paths/                   # API 路径定义
│   ├── assets.yml           # 资产 API 路径
│   ├── transfer.yml         # 转账 API 路径
│   ├── account.yml          # 账户 API 路径
│   └── monitoring.yml       # 监控 API 路径
└── swagger.yml              # 生成的完整文档
```

#### 文档注释规范
```yaml
# API 端点文档
/api/v1/assets/{user_id}:
  get:
    tags:
      - Assets
    summary: Get user assets overview
    description: |
      获取用户跨链资产概览，包含以下信息：
      - 各链上的代币余额
      - 资产 USD 价值
      - 同步状态
      - 批量处理状态
    operationId: getUserAssets
    security:
      - Bearer: []
    parameters:
      - name: user_id
        in: path
        description: 用户唯一标识符
        required: true
        type: string
        example: "user_123"
    responses:
      "200":
        description: 成功获取用户资产信息
        schema:
          $ref: "#/definitions/AssetsResponse"
      "400":
        $ref: "#/responses/BadRequestError"
      "401":
        $ref: "#/responses/UnauthorizedError"
      "404":
        description: 用户不存在
        schema:
          $ref: "#/definitions/APIError"
```

### 示例和测试

#### 请求示例
```yaml
# 在 API 定义中包含示例
AssetAdjustRequest:
  type: object
  required: [operation_id, adjustments]
  properties:
    operation_id:
      type: string
      description: "操作 ID，用于幂等性控制"
      example: "op_daily_rewards_001"
    adjustments:
      type: array
      items:
        $ref: "#/definitions/AssetAdjustment"
      example:
        - user_id: "user_123"
          chain_id: 56
          token_symbol: "CPOP"
          amount: "+100.0"
          business_type: "reward"
          reason_type: "daily_checkin"
          reason_detail: "Daily check-in reward"
```

#### 响应示例
```yaml
# 在响应定义中包含示例
AssetAdjustResponse:
  type: object
  required: [operation_id, processed_count, status]
  properties:
    operation_id:
      type: string
      description: "操作 ID"
      example: "op_daily_rewards_001"
    processed_count:
      type: integer
      description: "已处理的调整数量"
      example: 1
    status:
      type: string
      description: "处理状态"
      example: "recorded"
  example:
    operation_id: "op_daily_rewards_001"
    processed_count: 1
    status: "recorded"
```

---

## 🎯 最佳实践

### 1. API 设计最佳实践

#### 资源设计
```bash
# ✅ 好的设计
GET /api/v1/users/{user_id}/assets          # 获取用户的资产
POST /api/v1/users/{user_id}/assets/adjust  # 调整用户资产
GET /api/v1/users/{user_id}/transfers       # 获取用户的转账记录

# ❌ 不好的设计
GET /api/v1/getUserAssets                   # 动词不应出现在 URL 中
POST /api/v1/adjustUserAssets               # 应该使用资源路径
GET /api/v1/userAssets                      # 应该使用复数形式
```

#### 参数设计
```bash
# ✅ 好的参数设计
GET /api/v1/transfers?status=completed&chain_id=1&page=1&limit=20
GET /api/v1/assets?fields=id,symbol,balance&sort=balance&order=desc

# ❌ 不好的参数设计
GET /api/v1/transfers?completed=true&chain=1&p=1&l=20  # 参数名不清晰
GET /api/v1/assets?f=id,symbol,balance                  # 参数名缩写
```

### 2. 数据验证最佳实践

#### 输入验证
```go
// 在 Handler 层进行参数验证
func (h *Handler) Handle(c echo.Context) error {
    var request types.AssetAdjustRequest
    if err := util.BindAndValidateBody(c, &request); err != nil {
        return err
    }
    
    // 业务逻辑验证在 Service 层进行
    result, err := h.service.AdjustAssets(ctx, &request)
    if err != nil {
        return err
    }
    
    return util.ValidateAndReturn(c, http.StatusOK, result)
}
```

#### 业务规则验证
```go
// 在 Service 层进行业务规则验证
func (s *service) AdjustAssets(ctx context.Context, req *types.AssetAdjustRequest) (*types.AssetAdjustResponse, error) {
    // 验证用户是否存在
    if err := s.validateUserExists(ctx, req.UserID); err != nil {
        return nil, fmt.Errorf("user_validation: %w", err)
    }
    
    // 验证代币是否支持
    if err := s.validateTokenSupported(ctx, req.ChainID, req.TokenSymbol); err != nil {
        return nil, fmt.Errorf("token_validation: %w", err)
    }
    
    // 验证余额是否充足
    if err := s.validateSufficientBalance(ctx, req); err != nil {
        return nil, fmt.Errorf("balance_validation: %w", err)
    }
    
    // 执行调整逻辑
    return s.executeAdjustment(ctx, req)
}
```

### 3. 错误处理最佳实践

#### 错误分类
```go
// 定义错误类型
type APIError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Details []ErrorDetail `json:"details,omitempty"`
}

type ErrorDetail struct {
    Field   string `json:"field,omitempty"`
    In      string `json:"in,omitempty"`
    Message string `json:"message"`
}

// 错误处理函数
func HandleError(err error) *APIError {
    switch {
    case errors.Is(err, ErrValidation):
        return &APIError{
            Code:    "VALIDATION_ERROR",
            Message: "Request validation failed",
            Details: extractValidationDetails(err),
        }
    case errors.Is(err, ErrNotFound):
        return &APIError{
            Code:    "RESOURCE_NOT_FOUND",
            Message: "Requested resource not found",
        }
    case errors.Is(err, ErrInsufficientBalance):
        return &APIError{
            Code:    "INSUFFICIENT_BALANCE",
            Message: "Insufficient balance for operation",
        }
    default:
        return &APIError{
            Code:    "INTERNAL_ERROR",
            Message: "Internal server error",
        }
    }
}
```

### 4. 性能优化最佳实践

#### 分页实现
```go
// 分页参数结构
type PaginationParams struct {
    Page  int `query:"page" validate:"min=1"`
    Limit int `query:"limit" validate:"min=1,max=100"`
}

// 分页查询实现
func (s *service) GetTransfers(ctx context.Context, userID string, params PaginationParams) (*types.TransferHistoryResponse, error) {
    offset := (params.Page - 1) * params.Limit
    
    // 查询总数
    total, err := s.countTransfers(ctx, userID)
    if err != nil {
        return nil, err
    }
    
    // 查询数据
    transfers, err := s.getTransfers(ctx, userID, offset, params.Limit)
    if err != nil {
        return nil, err
    }
    
    // 构建分页信息
    pagination := &types.PaginationInfo{
        Page:       params.Page,
        Limit:      params.Limit,
        Total:      total,
        TotalPages: int(math.Ceil(float64(total) / float64(params.Limit))),
        HasNext:    params.Page*params.Limit < total,
        HasPrev:    params.Page > 1,
    }
    
    return &types.TransferHistoryResponse{
        Data:       transfers,
        Pagination: pagination,
    }, nil
}
```

#### 缓存策略
```go
// 缓存键生成
func (s *service) getUserAssetsCacheKey(userID string) string {
    return fmt.Sprintf("user_assets:%s", userID)
}

// 缓存实现
func (s *service) GetUserAssets(ctx context.Context, userID string) (*types.AssetsResponse, error) {
    cacheKey := s.getUserAssetsCacheKey(userID)
    
    // 尝试从缓存获取
    if cached, err := s.redis.Get(ctx, cacheKey).Result(); err == nil {
        var response types.AssetsResponse
        if err := json.Unmarshal([]byte(cached), &response); err == nil {
            return &response, nil
        }
    }
    
    // 从数据库获取
    response, err := s.getUserAssetsFromDB(ctx, userID)
    if err != nil {
        return nil, err
    }
    
    // 缓存结果
    if data, err := json.Marshal(response); err == nil {
        s.redis.Set(ctx, cacheKey, data, 5*time.Minute)
    }
    
    return response, nil
}
```

### 5. 安全最佳实践

#### 输入清理
```go
// 清理用户输入
func sanitizeInput(input string) string {
    // 移除 HTML 标签
    input = html.EscapeString(input)
    
    // 限制长度
    if len(input) > 1000 {
        input = input[:1000]
    }
    
    return strings.TrimSpace(input)
}
```

#### 权限检查
```go
// 权限检查中间件
func RequirePermission(permission string) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            user := getUserFromContext(c)
            if !user.HasPermission(permission) {
                return httperrors.NewHTTPError(http.StatusForbidden, "Insufficient permissions")
            }
            return next(c)
        }
    }
}
```

---

## 📋 总结

ChainBridge API 设计规范遵循 RESTful 原则，注重开发者体验和系统性能。通过统一的响应格式、完善的错误处理、清晰的版本管理，以及详细的设计文档，为开发者提供了高质量、易用的 API 服务。

**核心特点**:
1. **一致性**: 统一的 API 设计模式和响应格式
2. **可预测性**: 清晰的 URL 结构和参数规范
3. **可扩展性**: 灵活的版本管理和向后兼容策略
4. **安全性**: 完善的认证授权和输入验证机制
5. **性能**: 优化的缓存策略和分页实现
6. **可维护性**: 清晰的代码结构和完善的文档

通过遵循这些设计规范，ChainBridge API 将为区块链应用开发提供稳定、高效、易用的服务接口。