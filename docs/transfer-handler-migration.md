# Transfer Handler API分离重构指南

## 概述
将transfer handler按API端点分离为独立文件，提高代码组织性和可维护性。

## 新的文件结构

```
internal/api/handlers/transfer/
├── common.go                  # 共享功能和常量
├── transfer_assets.go         # POST /transfer API处理
├── get_user_transactions.go   # GET /users/{user_id}/transactions API处理
└── handler.go                 # 向后兼容的遗留接口
```

## API端点映射

### 1. 资产转账 API
- **文件**: `transfer_assets.go`
- **端点**: `POST /transfer`
- **Handler**: `TransferAssetsHandler`
- **功能**: 处理用户间资产转账请求

### 2. 用户交易历史 API
- **文件**: `get_user_transactions.go`
- **端点**: `GET /users/{user_id}/transactions`
- **Handler**: `GetUserTransactionsHandler`
- **功能**: 获取用户交易历史记录

## 使用新的Handler结构

### 注册所有路由（推荐方式）
```go
import "github.com/hzbay/chain-bridge/internal/api/handlers/transfer"

// 在服务器初始化时注册所有transfer相关路由
func initRoutes(s *api.Server) {
    routes := transfer.RegisterRoutes(s)
    s.Router.Routes = append(s.Router.Routes, routes...)
}
```

### 使用特定的Handler
```go
// 直接使用特定的handler
func setupTransferRoutes(s *api.Server) {
    // 资产转账路由
    transferRoute := transfer.TransferAssetsRoute(s)
    
    // 用户交易历史路由
    transactionsRoute := transfer.GetUserTransactionsRoute(s)
}
```

### 手动创建Handler实例
```go
// 创建transfer service
transferService := transfer.NewService(db, batchProcessor, batchOptimizer)

// 创建特定的handler
transferHandler := transfer.NewTransferAssetsHandler(transferService)
transactionsHandler := transfer.NewGetUserTransactionsHandler(transferService)

// 手动注册路由
router.POST("/transfer", transferHandler.Handle)
router.GET("/users/:user_id/transactions", transactionsHandler.Handle)
```

## 向后兼容性

### 遗留代码支持
现有使用`Handler`结构的代码仍然可以正常工作：

```go
// 这种方式仍然支持，但已标记为deprecated
handler := transfer.NewHandler(transferService)
router.POST("/transfer", handler.TransferAssets)
router.GET("/users/:user_id/transactions", handler.GetUserTransactions)
```

### 迁移步骤
1. **立即可用**: 新代码使用新的handler结构
2. **渐进迁移**: 现有代码继续工作，逐步迁移到新结构
3. **完全迁移**: 最终移除遗留的`Handler`结构

## 新特性

### 1. 增强的参数验证
```go
// transfer_assets.go中的增强验证
if params.Request == nil {
    return echo.NewHTTPError(http.StatusBadRequest, "Request body is required")
}
```

### 2. 改进的日志记录
```go
// 详细的审计日志
log.Info().
    Interface("request", params.Request).
    Msg("Processing transfer assets request")

log.Info().
    Str("operation_id", *transferResponse.OperationID).
    Str("status", *transferResponse.Status).
    Bool("will_be_batched", batchInfo.WillBeBatched).
    Msg("Transfer assets request completed successfully")
```

### 3. 分页参数优化
```go
// 自动验证和修正分页参数
if serviceParams.Page < 1 {
    serviceParams.Page = 1
}
if serviceParams.Limit < 1 || serviceParams.Limit > 100 {
    serviceParams.Limit = 20 // 重置为默认值
}
```

### 4. 常量定义
```go
// common.go中定义的常量
const (
    StatusRecorded   = "recorded"
    StatusBatching   = "batching"
    StatusSubmitted  = "submitted"
    StatusConfirmed  = "confirmed"
    StatusFailed     = "failed"
    
    DefaultPage  = 1
    DefaultLimit = 20
    MaxLimit     = 100
)
```

## 测试

### 单元测试示例
```go
func TestTransferAssetsHandler(t *testing.T) {
    // 创建mock service
    mockService := &MockTransferService{}
    
    // 创建handler
    handler := transfer.NewTransferAssetsHandler(mockService)
    
    // 创建测试请求
    req := &types.TransferRequest{
        FromUserID:  &fromUser,
        ToUserID:    &toUser,
        Amount:      &amount,
        ChainID:     &chainID,
        TokenSymbol: &symbol,
    }
    
    // 测试handler
    // ... 测试逻辑
}
```

### 集成测试示例
```go
func TestTransferEndpoints(t *testing.T) {
    // 设置测试服务器
    server := setupTestServer()
    
    // 注册transfer路由
    transfer.RegisterRoutes(server)
    
    // 测试POST /transfer
    response := performTransfer(t, server, transferRequest)
    assert.Equal(t, http.StatusOK, response.StatusCode)
    
    // 测试GET /users/{user_id}/transactions
    transactions := getUserTransactions(t, server, userID)
    assert.NotNil(t, transactions)
}
```

## 最佳实践

### 1. 错误处理
- 使用结构化日志记录错误上下文
- 返回适当的HTTP状态码
- 提供用户友好的错误消息

### 2. 参数验证
- 在handler层进行基本验证
- 在service层进行业务逻辑验证
- 提供详细的验证错误信息

### 3. 响应格式
- 遵循API规范的响应格式
- 包含必要的元数据（如分页信息）
- 提供一致的错误响应结构

### 4. 性能优化
- 使用适当的分页大小限制
- 添加必要的性能监控
- 考虑缓存常用查询结果

## 总结

新的handler结构提供了：
- ✅ 更好的代码组织和可维护性
- ✅ 按API端点分离的清晰职责
- ✅ 向后兼容性支持
- ✅ 增强的参数验证和错误处理
- ✅ 改进的日志记录和监控
- ✅ 更容易的单元测试和集成测试

建议新项目直接使用新的handler结构，现有项目可以渐进式迁移。