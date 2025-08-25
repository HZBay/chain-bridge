# ChainBridge 开发流程规范

## 项目概述

基于 [go-starter](https://github.com/allaboutapps/go-starter) 模板的生产级 RESTful JSON 后端服务，使用 Go + PostgreSQL + Docker 技术栈。

## 核心特性

- **Docker 开发环境**: VSCode DevContainers + Docker Compose
- **数据库**: PostgreSQL + sql-migrate + SQLBoiler
- **API**: go-swagger 代码生成 + Echo 框架  
- **测试**: IntegreSQL 集成测试
- **监控**: 健康检查、性能分析

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

### 3. 实现 Handler
在生成的类型基础上编写 handler 逻辑：

```go
// 使用生成的类型
response := &types.QueueMetricsResponse{
    Data: metrics,
}
// 使用 util.ValidateAndReturn 而不是 c.JSON
return util.ValidateAndReturn(c, http.StatusOK, response)
```

### 4. Handler 文件组织
每个接口单独一个文件，参考 wallet 模式：
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

### ✅ 正确做法
- API 优先：先定义接口规范，再生成代码，最后实现逻辑
- 使用 `util.ValidateAndReturn()` 返回响应
- 使用生成的类型而不是通用类型
- 每个接口单独文件

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
```

## 项目结构

```
├── api/                     # API 定义
│   ├── config/main.yml      # 主配置
│   ├── definitions/         # 类型定义
│   ├── paths/              # 路径定义
│   └── swagger.yml         # 生成的完整 API
├── internal/
│   ├── api/handlers/       # API 处理器
│   │   ├── chains/
│   │   ├── monitoring/
│   │   └── wallet/
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
└── wallet/
    ├── handler.go
    ├── create_user_wallet.go
    └── get_user_wallet.go
```