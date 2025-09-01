# RabbitMQBatchConsumer Database Configuration Setup

## Overview
The `RabbitMQBatchConsumer` now loads configuration directly from the database without depending on `chains.Service`.

## Setup Process

### 1. Apply Database Migration
```bash
# Apply the migration to add new fields
app db migrate
```

### 2. Regenerate Database Models
```bash
# Regenerate SQLBoiler models to include new fields
make sql-boiler
```

### 3. Uncomment Database Field Access
After regenerating models, uncomment these lines in `internal/queue/rabbitmq_batch_consumer.go`:

```go
// In loadChainBatchConfig() method:
if chain.MaxWaitTimeMs.Valid {
    config.MaxWaitTimeMs = chain.MaxWaitTimeMs.Int
}
if chain.ConsumerCount.Valid {
    config.ConsumerCount = chain.ConsumerCount.Int
}
```

## Configuration Fields

| Field | Database Column | Default | Description |
|-------|----------------|---------|-------------|
| `maxBatchSize` | `optimal_batch_size` | 25 | Working batch size for processing |
| `minBatchSize` | `min_batch_size` | 10 | Minimum batch size allowed |
| `maxWaitTime` | `max_wait_time_ms` | 15000ms | Maximum wait time before processing batch |
| `consumerCount` | `consumer_count` | 1 | Number of consumer workers for this chain |

## Usage Examples

### Loading Configuration
```go
// Configuration is automatically loaded when creating consumer
consumer := NewRabbitMQBatchConsumerForChain(...)

// Or reload configuration at runtime
err := consumer.ReloadConfig()
```

### Getting Current Configuration
```go
config := consumer.GetBatchConfig()
fmt.Printf("Chain %d: batch_size=%d, wait_time=%dms, consumers=%d\n",
    chainID, config.OptimalBatchSize, config.MaxWaitTimeMs, config.ConsumerCount)
```

## Database Management

### Update Configuration
```sql
-- Example: Optimize Ethereum for larger batches
UPDATE chains 
SET optimal_batch_size = 30,
    max_wait_time_ms = 20000,
    consumer_count = 1
WHERE chain_id = 1;

-- Example: Optimize Polygon for faster processing
UPDATE chains 
SET optimal_batch_size = 15,
    max_wait_time_ms = 8000,
    consumer_count = 2
WHERE chain_id = 137;
```

### Check Current Configuration
```sql
SELECT chain_id, name, 
       optimal_batch_size, min_batch_size, max_batch_size,
       max_wait_time_ms, consumer_count
FROM chains 
WHERE is_enabled = true
ORDER BY chain_id;
```

## Benefits

✅ **Per-chain optimization** without code deployment  
✅ **Runtime configuration** updates via `ReloadConfig()`  
✅ **Database-driven** configuration management  
✅ **No service dependencies** - direct database queries  
✅ **Fallback defaults** when database values are missing