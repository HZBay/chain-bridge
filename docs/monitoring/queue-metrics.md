# ChainBridge 队列监控指标

## 📋 概述

ChainBridge 批处理系统采用按链分队列架构，需要全面的监控指标来确保系统稳定运行和性能优化。

## 📊 核心监控指标

### 1. 队列级别指标

#### 队列状态指标
```go
type QueueMetrics struct {
    QueueName       string    `json:"queue_name"`        // cpop.transfer.56.1
    ChainID         int64     `json:"chain_id"`          // 56
    JobType         string    `json:"job_type"`          // transfer
    TokenID         int       `json:"token_id"`          // 1
    
    // 消息统计
    PendingCount    int       `json:"pending_count"`     // 待处理消息数
    ProcessingCount int       `json:"processing_count"`  // 处理中消息数
    CompletedCount  int64     `json:"completed_count"`   // 已完成消息数
    FailedCount     int64     `json:"failed_count"`      // 失败消息数
    
    // 性能指标
    AverageLatency  int64     `json:"average_latency_ms"` // 平均处理延迟(ms)
    Throughput      float64   `json:"throughput_per_sec"` // 每秒处理量
    ErrorRate       float64   `json:"error_rate"`         // 错误率
    
    // 时间信息
    LastProcessed   time.Time `json:"last_processed"`     // 最后处理时间
    LastError       time.Time `json:"last_error"`         // 最后错误时间
}
```

#### 批处理效率指标
```go
type BatchEfficiencyMetrics struct {
    ChainID                int64   `json:"chain_id"`
    TokenID                int     `json:"token_id"`
    JobType                string  `json:"job_type"`
    
    // 批量统计
    TotalBatches           int64   `json:"total_batches"`
    AverageBatchSize       float64 `json:"average_batch_size"`
    OptimalBatchSize       int     `json:"optimal_batch_size"`
    
    // Gas 优化
    TotalGasUsed           int64   `json:"total_gas_used"`
    EstimatedGasWithoutBatch int64 `json:"estimated_gas_without_batch"`
    GasSavingsPercentage   float64 `json:"gas_savings_percentage"`
    GasSavingsUSD          float64 `json:"gas_savings_usd"`
    
    // 时间效率
    AverageProcessingTime  int64   `json:"average_processing_time_ms"`
    AverageWaitTime        int64   `json:"average_wait_time_ms"`
}
```

### 2. 消费者级别指标

#### 消费者状态指标
```go
type ConsumerMetrics struct {
    ChainID          int64     `json:"chain_id"`
    ChainName        string    `json:"chain_name"`
    WorkerCount      int       `json:"worker_count"`
    ActiveWorkers    int       `json:"active_workers"`
    
    // 处理统计
    ProcessedJobs    int64     `json:"processed_jobs"`
    FailedJobs       int64     `json:"failed_jobs"`
    RetriedJobs      int64     `json:"retried_jobs"`
    
    // 性能指标
    AverageLatency   int64     `json:"average_latency_ms"`
    PeakLatency      int64     `json:"peak_latency_ms"`
    Throughput       float64   `json:"throughput_per_sec"`
    
    // 健康状态
    IsHealthy        bool      `json:"is_healthy"`
    LastHeartbeat    time.Time `json:"last_heartbeat"`
    ErrorCount       int64     `json:"error_count"`
    ConsecutiveErrors int      `json:"consecutive_errors"`
}
```

### 3. 系统级别指标

#### 系统健康指标
```go
type SystemHealthMetrics struct {
    // 服务状态
    DatabaseStatus   string    `json:"database_status"`   // healthy, degraded, down
    RedisStatus      string    `json:"redis_status"`
    RabbitMQStatus   string    `json:"rabbitmq_status"`
    BlockchainStatus string    `json:"blockchain_status"`
    
    // 资源使用
    CPUUsage         float64   `json:"cpu_usage_percent"`
    MemoryUsage      float64   `json:"memory_usage_percent"`
    DiskUsage        float64   `json:"disk_usage_percent"`
    
    // 连接状态
    DatabaseConnections int    `json:"database_connections"`
    RedisConnections    int    `json:"redis_connections"`
    RabbitMQConnections int    `json:"rabbitmq_connections"`
    
    // 错误统计
    TotalErrors      int64     `json:"total_errors"`
    ErrorRate        float64   `json:"error_rate"`
    LastError        time.Time `json:"last_error"`
}
```

## 📈 监控 API 端点

### 1. 队列指标查询

#### 获取所有队列指标
```http
GET /api/v1/monitoring/queue/metrics
```

响应示例：
```json
{
  "data": {
    "queues": [
      {
        "queue_name": "cpop.transfer.56.1",
        "chain_id": 56,
        "job_type": "transfer",
        "token_id": 1,
        "pending_count": 15,
        "processing_count": 3,
        "completed_count": 1250,
        "failed_count": 5,
        "average_latency_ms": 2500,
        "throughput_per_sec": 12.5,
        "error_rate": 0.004,
        "last_processed": "2024-01-15T10:30:00Z"
      }
    ],
    "summary": {
      "total_queues": 12,
      "total_pending": 45,
      "total_processing": 8,
      "total_completed": 15600,
      "total_failed": 25,
      "overall_error_rate": 0.0016
    }
  },
  "meta": {
    "timestamp": "2024-01-15T10:30:00Z",
    "request_id": "req_123456789"
  }
}
```

#### 获取特定链的队列指标
```http
GET /api/v1/monitoring/queue/metrics?chain_id=56
```

#### 获取特定队列的详细指标
```http
GET /api/v1/monitoring/queue/metrics/{queue_name}
```

### 2. 批处理效率查询

#### 获取批处理效率统计
```http
GET /api/v1/monitoring/queue/efficiency
```

响应示例：
```json
{
  "data": {
    "efficiency_stats": [
      {
        "chain_id": 56,
        "chain_name": "BSC",
        "token_id": 1,
        "token_symbol": "CPOP",
        "job_type": "transfer",
        "total_batches": 125,
        "average_batch_size": 23.5,
        "optimal_batch_size": 25,
        "gas_savings_percentage": 76.8,
        "gas_savings_usd": 1250.50,
        "average_processing_time_ms": 15000
      }
    ],
    "summary": {
      "total_gas_savings_usd": 15680.25,
      "average_efficiency": 75.2,
      "total_batches_processed": 1250
    }
  }
}
```

### 3. 消费者状态查询

#### 获取所有消费者状态
```http
GET /api/v1/monitoring/consumers/status
```

响应示例：
```json
{
  "data": {
    "consumers": [
      {
        "chain_id": 56,
        "chain_name": "BSC",
        "worker_count": 3,
        "active_workers": 3,
        "processed_jobs": 1250,
        "failed_jobs": 5,
        "average_latency_ms": 2500,
        "throughput_per_sec": 12.5,
        "is_healthy": true,
        "last_heartbeat": "2024-01-15T10:29:45Z"
      }
    ],
    "summary": {
      "total_consumers": 4,
      "healthy_consumers": 4,
      "total_workers": 12,
      "active_workers": 12
    }
  }
}
```

### 4. 系统健康检查

#### 获取系统健康状态
```http
GET /api/v1/monitoring/health
```

响应示例：
```json
{
  "data": {
    "status": "healthy",
    "services": {
      "database": "healthy",
      "redis": "healthy",
      "rabbitmq": "healthy",
      "blockchain": "healthy"
    },
    "resources": {
      "cpu_usage_percent": 45.2,
      "memory_usage_percent": 62.8,
      "disk_usage_percent": 35.1
    },
    "connections": {
      "database_connections": 15,
      "redis_connections": 8,
      "rabbitmq_connections": 12
    },
    "errors": {
      "total_errors": 25,
      "error_rate": 0.0016,
      "last_error": "2024-01-15T09:45:30Z"
    }
  }
}
```

## 🚨 告警规则

### 1. 队列告警

#### 队列积压告警
```yaml
# Prometheus 告警规则
- alert: QueueBacklogHigh
  expr: chainbridge_queue_pending_count > 100
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "队列积压过高"
    description: "队列 {{ $labels.queue_name }} 积压消息数: {{ $value }}"

- alert: QueueBacklogCritical
  expr: chainbridge_queue_pending_count > 500
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "队列积压严重"
    description: "队列 {{ $labels.queue_name }} 积压消息数: {{ $value }}"
```

#### 队列错误率告警
```yaml
- alert: QueueErrorRateHigh
  expr: chainbridge_queue_error_rate > 0.05
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "队列错误率过高"
    description: "队列 {{ $labels.queue_name }} 错误率: {{ $value | humanizePercentage }}"

- alert: QueueErrorRateCritical
  expr: chainbridge_queue_error_rate > 0.1
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "队列错误率严重"
    description: "队列 {{ $labels.queue_name }} 错误率: {{ $value | humanizePercentage }}"
```

### 2. 消费者告警

#### 消费者健康告警
```yaml
- alert: ConsumerUnhealthy
  expr: chainbridge_consumer_healthy == 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "消费者不健康"
    description: "链 {{ $labels.chain_id }} 的消费者不健康"

- alert: ConsumerHighLatency
  expr: chainbridge_consumer_average_latency_ms > 30000
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "消费者延迟过高"
    description: "链 {{ $labels.chain_id }} 消费者平均延迟: {{ $value }}ms"
```

### 3. 系统告警

#### 系统资源告警
```yaml
- alert: HighCPUUsage
  expr: chainbridge_cpu_usage_percent > 80
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "CPU 使用率过高"
    description: "CPU 使用率: {{ $value }}%"

- alert: HighMemoryUsage
  expr: chainbridge_memory_usage_percent > 85
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "内存使用率过高"
    description: "内存使用率: {{ $value }}%"

- alert: HighDiskUsage
  expr: chainbridge_disk_usage_percent > 90
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "磁盘使用率过高"
    description: "磁盘使用率: {{ $value }}%"
```

## 📊 监控面板

### 1. Grafana 仪表板配置

#### 队列监控面板
```json
{
  "dashboard": {
    "title": "ChainBridge Queue Monitoring",
    "panels": [
      {
        "title": "Queue Pending Count",
        "type": "graph",
        "targets": [
          {
            "expr": "chainbridge_queue_pending_count",
            "legendFormat": "{{ queue_name }}"
          }
        ]
      },
      {
        "title": "Queue Throughput",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(chainbridge_queue_completed_count[5m])",
            "legendFormat": "{{ queue_name }}"
          }
        ]
      },
      {
        "title": "Queue Error Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "chainbridge_queue_error_rate",
            "legendFormat": "{{ queue_name }}"
          }
        ]
      }
    ]
  }
}
```

#### 批处理效率面板
```json
{
  "dashboard": {
    "title": "ChainBridge Batch Efficiency",
    "panels": [
      {
        "title": "Gas Savings",
        "type": "stat",
        "targets": [
          {
            "expr": "sum(chainbridge_batch_gas_savings_usd)",
            "legendFormat": "Total Gas Savings (USD)"
          }
        ]
      },
      {
        "title": "Batch Efficiency",
        "type": "graph",
        "targets": [
          {
            "expr": "chainbridge_batch_efficiency_percentage",
            "legendFormat": "{{ chain_name }} - {{ token_symbol }}"
          }
        ]
      }
    ]
  }
}
```

## 🔧 监控工具集成

### 1. Prometheus 配置

#### 指标收集配置
```yaml
# prometheus.yml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'chainbridge'
    static_configs:
      - targets: ['chainbridge:8080']
    metrics_path: '/api/v1/monitoring/metrics'
    scrape_interval: 10s
```

### 2. 自定义指标暴露

#### Go 代码中的指标定义
```go
package monitoring

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // 队列指标
    QueuePendingCount = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "chainbridge_queue_pending_count",
            Help: "Number of pending messages in queue",
        },
        []string{"queue_name", "chain_id", "job_type", "token_id"},
    )
    
    QueueCompletedCount = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "chainbridge_queue_completed_count",
            Help: "Number of completed messages in queue",
        },
        []string{"queue_name", "chain_id", "job_type", "token_id"},
    )
    
    QueueErrorRate = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "chainbridge_queue_error_rate",
            Help: "Error rate of queue processing",
        },
        []string{"queue_name", "chain_id", "job_type", "token_id"},
    )
    
    // 批处理效率指标
    BatchGasSavings = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "chainbridge_batch_gas_savings_usd",
            Help: "Gas savings in USD from batch processing",
        },
        []string{"chain_id", "token_id", "job_type"},
    )
    
    BatchEfficiency = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "chainbridge_batch_efficiency_percentage",
            Help: "Batch processing efficiency percentage",
        },
        []string{"chain_id", "token_id", "job_type"},
    )
)
```

## 📋 总结

ChainBridge 队列监控系统提供了全面的监控指标，包括队列状态、批处理效率、消费者健康和系统资源使用情况。通过 Prometheus + Grafana 的监控栈，可以实现实时监控、告警和可视化分析，确保系统的稳定运行和性能优化。

**核心特点**:
1. **全面监控**: 覆盖队列、消费者、批处理和系统各个层面
2. **实时告警**: 基于阈值的智能告警机制
3. **可视化分析**: 丰富的 Grafana 仪表板
4. **性能优化**: 基于监控数据的性能调优建议
5. **故障诊断**: 详细的错误信息和故障定位