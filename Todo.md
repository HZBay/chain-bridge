### 现状分析与核心问题

当前系统存在多个状态需要联动更新：
1.  **批量状态 (`batches`)**：描述整个批处理任务的生命周期。
2.  **交易状态 (`transactions`)**：描述每一笔原始用户请求的生命周期。
3.  **用户余额 (`user_balances`)**：用户的最终资产视图，分为已确认和待确认部分。

**核心问题**：目前的逻辑可能没有在一个数据库事务内原子性地更新这三个部分，导致数据不一致。例如，链上操作成功了，但数据库状态未更新，或者反之。

---

### 状态流转设计修正方案

首先，我们明确并修正各表的状态机设计，确保其逻辑一致。

#### 1. `batches` 表状态流转 (修正后)

这个表跟踪**批量任务**的整个生命周期。

| 状态 | 含义 | 触发条件与后续动作 |
| :--- | :--- | :--- |
| `preparing` | **准备中** | 批量聚合器开始聚合一个新的批次时即创建并设置为此状态。 |
| `submitted` | **已提交** | **关键节点**。批量交易已成功发送到区块链网络，获得TxHash。此时应将关联的 `transactions` 状态更新为 `submitted`。 |
| `confirmed` | **已确认** | **最终成功**。监听到链上交易确认（达到足够区块确认数）。此时应更新关联的 `transactions` 状态为 `confirmed`，并**最终更新 `user_balances`**。 |
| `failed` | **已失败** | **最终失败**。上链提交失败或链上确认失败。此时应更新关联的 `transactions` 状态为 `failed`，并**回滚 `user_balances.pending_balance`**。 |

#### 2. `transactions` 表状态流转 (修正后)

这个表跟踪每一笔**原始用户请求**的生命周期。

| 状态 | 含义 | 触发条件与后续动作 |
| :--- | :--- | :--- |
| `pending` | ** pending** | 用户请求通过API接入，校验通过后存入数据库的最初状态。 |
| `batching` | **批处理中** | **关键节点**。该笔交易被批量聚合器选取，加入到一个 `batch` 中。此时应**更新 `user_balances.pending_balance`**（例如，冻结要转出的资产）。 |
| `submitted` | **已提交** | 与其关联的 `batch` 状态变为 `submitted`。此状态仅表示已发出，最终结果未知。 |
| `confirmed` | **已确认** | 与其关联的 `batch` 状态变为 `confirmed`。 |
| `failed` | **已失败** | 与其关联的 `batch` 状态变为 `failed`。 |

#### 3. `user_balances` 表余额更新规则

这是最需要保证一致性的核心数据。

*   **`pending_balance` (待确认余额)**：
    *   **增加**：当一笔 `transfer` 或 `burn` 操作的交易状态由 `pending` 变为 `batching` 时，应**冻结**相应资产，即从 `confirmed_balance` 中扣除，并加到 `pending_balance` 中。`mint` 操作不需要冻结。
    *   **减少/清零**：当交易最终状态变为 `confirmed` 或 `failed` 时，进行反向操作。
        *   `confirmed`：对于 `transfer`/`burn`，清空 `pending_balance` 中的该笔冻结资产；对于 `mint`，将资产从 `pending_balance` 转入 `confirmed_balance`； 上链完成后同步一下链上余额到`confirmed_balance`。
        *   `failed`：对于 `transfer`/`burn`，将冻结的资产从 `pending_balance` **解冻**回 `confirmed_balance`；对于 `mint`，清空 `pending_balance` 中对应的资产。

*   **`confirmed_balance` (已确认余额)**：
    *   只在交易**最终确认 (`confirmed`)** 或**初始冻结 (`batching`)** 时更新。
    *   用户查询余额时，应显示 `confirmed_balance + pending_balance`（或其他业务定义的可用的余额）。

**在多线程（或多协程）环境下，对 `user_balances` 表的更新必须考虑并发问题**，否则会导致数据不一致。

---

### 批处理逻辑更新方案 (关键修改点)

现在，我们基于上述状态设计，在 `StartBatchConsumer` 逻辑中嵌入状态更新。

#### 阶段一：创建与聚合批次 (`preparing` -> `batching`)

1.  **从 `jobChan` 消费 `BatchJob`**。
2.  **开启数据库事务**。
3.  **插入 `batches` 表**：创建一条新记录，状态设为 `preparing`，并记录包含了哪些 `transaction_id`。
4.  **更新 `transactions` 表**：将本批次中包含的所有交易记录的状态从 `pending` 更新为 `batching`。
5.  **更新 `user_balances` 表**：对于所有 `burn` 和 `transfer` 类型的交易，执行 `UPDATE user_balances SET confirmed_balance = confirmed_balance - amount, pending_balance = pending_balance + amount WHERE user_id = ? AND asset_type = ?`。**（这是实现冻结的关键步骤）**
6.  **提交事务**。如果此步骤失败，整个批次聚合失败，记录日志并告警。

#### 阶段二：提交上链 (`submitted`)

1.  聚合完成，调用 `BatchMint/BatchBurn/BatchTransfer` 成功，获得 `txHash`。
2.  **开启数据库事务**。
3.  **更新 `batches` 表**：将状态从 `preparing` 更新为 `submitted`，并填入 `tx_hash` 和 `submitted_at` 时间。
4.  **更新 `transactions` 表**：将本批次下所有交易的状态从 `batching` 更新为 `submitted`。
5.  **提交事务**。

#### 阶段三：链上确认与最终更新 (`confirmed`/`failed`)

*此阶段通常由一个独立的 **TxConfirmWatcher** 服务监听链上事件来完成，但它需要与批处理器共享状态更新逻辑。*

**情况A：交易成功确认 (`confirmed`)**

1.  **开启数据库事务**。
2.  **更新 `batches` 表**：将状态从 `submitted` 更新为 `confirmed`，填入 `confirmed_at` 时间。
3.  **更新 `transactions` 表**：将状态从 `submitted` 更新为 `confirmed`。
4.  **更新 `user_balances` 表**：
    *   **对于 `mint`**：`UPDATE ... SET pending_balance = pending_balance - amount, confirmed_balance = confirmed_balance + amount ...`
    *   **对于 `burn`/`transfer`**：`UPDATE ... SET pending_balance = pending_balance - amount ...` (直接解冻即可，资产在 `batching` 阶段已从确认余额中扣除)
5.  **提交事务**。

**情况B：交易失败 (`failed`)**

1.  **开启数据库事务**。
2.  **更新 `batches` 表**：将状态从 `submitted` 更新为 `failed`，记录错误信息。
3.  **更新 `transactions` 表**：将状态从 `submitted` 更新为 `failed`。
4.  **更新 `user_balances` 表**（**关键：回滚冻结**）：
    *   **对于 `mint`**：`UPDATE ... SET pending_balance = pending_balance - amount ...` (直接清除待确认的增发)
    *   **对于 `burn`/`transfer`**：`UPDATE ... SET pending_balance = pending_balance - amount, confirmed_balance = confirmed_balance + amount ...` (解冻并归还资产)
5.  **提交事务**。

### 总结与下一步行动

**方案核心**：**使用数据库事务将（业务操作 + 状态更新 + 余额更新）绑定为一个原子操作**，确保在任何一步失败时都能回滚，保持数据一致性。

**接下来修改代码的步骤**：
1.  在 `internal/queue/hybrid_processor.go` 的 `StartBatchConsumer` 中：
    *   在聚合逻辑处（`case job := <-jobChan`）添加 **阶段一** 的数据库事务操作。
    *   在成功调用智能合约后，添加 **阶段二** 的数据库事务操作。
2.  需要实现或修改一个 `TxConfirmWatcher` 服务（可能在另一个文件），它监听链上回执，并执行 **阶段三** 的数据库事务操作。
3.  确保所有数据库操作都通过相同的数据库连接池和ORM上下文管理事务。

请根据这个详细方案，优先修改 `hybrid_processor.go` 中的状态更新逻辑。



### 批处理需求核心目标

internal/queue/memory_processor.go的设计细节暂时不要考虑，框架保留，现在专注于RabbitMQProcessor的开发。

1.  **代码结构优化**：将 `RabbitMQProcessor` 的核心实现逻辑剥离到独立的 Go 文件中，以提高代码的模块化和可维护性。
2.  **统一队列命名与消费模型**：修正 `RabbitMQProcessor` (生产者) 和 `RabbitMQBatchConsumer` (消费者) 之间队列命名不一致的问题，并确立以“链(Chain)”为维度的队列分配与消费方案。

---

### 详细需求说明与实施方案

#### 1. 代码结构修改

-   **当前问题**：`RabbitMQProcessor` 的实现逻辑可能与其他代码混杂在一个文件中。
-   **修改要求**：将其核心逻辑（例如连接管理、消息发布等）移动到一个独立的 `.go` 文件（例如 `rabbitmq_processor.go`）中。
-   **好处**：使项目结构更清晰，职责更分明，便于后续阅读和维护。

#### 2. 统一队列命名策略 (关键修改)

-   **当前问题**：生产者和消费者使用的队列名称规则不一致，导致消息无法被正确投递和消费。
-   **修改要求**：**统一采用 `RabbitMQProcessor` 所使用的队列命名规则**。
-   **建议的命名规则**：
    -   队列名称应包含链的唯一标识符（例如 `ChainID`）。
    -   **示例**：`queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())`
    -   chains的信息统一从 internal/services/chains/service.go 中获取
    -   *(`{chainId}` 需要替换为从数据库表 `chains` 中获取的实际链ID)*

#### 3. 重构消费者模型 (核心变更)

-   **当前问题/方案**：原有的 `RabbitMQBatchConsumer` 可能试图从一个队列消费所有链的消息，然后再在内部进行分组处理，这与新的按链分队列的方案相悖。
-   **新方案要求**：
    1.  **为每条链创建专属队列**：`RabbitMQProcessor` 在发送消息时，**必须根据消息所属的 `chain_id` 将其发布到对应的专属队列中**（例如 `queueName := r.client.GetQueueName(job.GetJobType(), job.GetChainID(), job.GetTokenID())`）。
    2.  **为每条链创建专属消费者**：需要实现一个**管理器或工厂**，为数据库 `chains` 表中每一条**已启用**的链，动态地创建并启动一个独立的 `RabbitMQBatchConsumer` 实例。
    3.  **消费者 Workers**：每个 `RabbitMQBatchConsumer` 实例（即每个链的消费者）应该可以配置**多个 Worker Goroutine** 来并发地从其专属队列中拉取和处理消息，从而提高消费吞吐量。

---

### 总结：修改后的架构工作流程

1.  **生产者 (`RabbitMQProcessor`)**:
    -   位于独立的文件 `rabbitmq_processor.go`。
    -   根据待处理消息的 `chain_id`，将其发布到对应的队列 `batch_tx_queue_{chainId}` 中。

2.  **队列 (Queues)**:
    -   每个链都有一个与之对应的、名称唯一的队列。例如：
        -   `batch_tx_queue_1` (对于 Ethereum Mainnet)
        -   `batch_tx_queue_137` (对于 Polygon Mainnet)

3.  **消费者 (``RabbitMQBatchConsumer` 实例)**:
    -   一个 **Consumer 实例** 对应 **一个链**，监听 **一个队列**。
    -   系统启动时，根据 `chains` 表的配置，**动态地为每条链创建并启动一个 Consumer 实例**。
    -   每个 Consumer 实例可以启动 **N 个 Worker** 来并发消费其队列中的消息。

### 建议的代码修改步骤

1.  **创建新文件**：`rabbitmq_processor.go`，并将现有处理器逻辑迁移进去。
2.  **修改生产者**：重构 `RabbitMQProcessor` 的 `Publish` 方法，使其根据 `chainId` 动态生成并指定目标队列名。
3.  **修改消费者逻辑**：
    -   创建一个 **Consumer Manager** 组件，负责管理所有链的消费者生命周期。
    -   该管理器在初始化时，从数据库 `chains` 表查询所有需要监听的链。
    -   为每条链调用一个 `NewRabbitMQBatchConsumer(chainId, numWorkers)` 函数，创建消费者实例。
    -   每个消费者实例在初始化时，使用与生产者约定好的规则（`batch_tx_queue_{chainId}`）去连接指定的队列。
4.  **更新文档**：根据最终实现，更新 `batch-processing-architecture.md` 中的架构图和相关说明，确保与代码实现保持一致。

这样修改后，架构将变得非常清晰和高效，实现了天然的链间隔离，避免了不必要的消息筛选逻辑，并且具备了良好的水平扩展能力。 


> 检查user_balances表中的pending_balance等字典根据mint、burn、transfer是否有同步更新？

需要为supported-tokens数据库表添加类似chains表管理用的API

GET /api/v1/users/{user_id}/assets
接口需不需要NFT

  "nfts": [
    {
      "chain_id": 1,
      "collection_name": "CryptoPunks",
      "contract_address": "0xb47e3cd837ddf8e4c57f05d70ab865de6e193bbb",
      "token_id": "123",
      "name": "CryptoPunk #123",
      "description": "A unique CryptoPunk",
      "image": "https://www.larvalabs.com/cryptopunks/cryptopunk123.png",
      "estimated_value_usd": 50000,
      "metadata": {
        "attributes": [
          {"trait_type": "Type", "value": "Human"},
          {"trait_type": "Hat", "value": "Bandana"}
        ]
      }
    }
  ]

接口 /-/chains
目前返回信息：
{
  "data": [
    {
      "batch_size": 30,
      "batch_timeout": 300,
      "chain_id": 11155111,
      "is_enabled": true,
      "name": "Sepolia Testnet",
      "rpc_url": "https://eth-sepolia.g.alchemy.com/v2/_x4NAgu50ejHAhTH2-gJoRNS4PQv7Tjp"
    }
  ]
}
接口的200直接返回Array 不要再用data 包裹，并且返回里面内容不全，要展示完整的链信息，修改一下返回定义的yml，并make swagger。

下面两个接口：
/-/monitoring/queue/metrics
/-/monitoring/queue/stats
的返回也不需要用data字段包裹一下

/api/v1/assets/{user_id}
返回的assets 也不需要用data字段再包裹一下

/api/v1/assets/{user_id}/transactions
返回里面的字段data不需要，把data内容直接展示出来

/api/v1/assets/transfer
返回的字段data里面的内容直接放出来，不要用data字段包裹


6:24:28 ERR PANIC RECOVER error="runtime error: invalid memory address or nil pointer dereference" req={"bytes_in":"0","host":"127.0.0.1:8080","id":"oSOWDcfimhvqPYMoYORYawquFIheyiln","method":"GET","url":"/-/chains/11155111?mgmt-secret=123456"} stack="goroutine 62 [running]:\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RecoverWithConfig.func10.1.1()\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/recover.go:99 +0x130\npanic({0x1077680?, 0x1e51730?})\n\t/usr/local/go/src/runtime/panic.go:792 +0x124\ngithub.com/hzbay/chain-bridge/internal/types/cpop.(*GetChainConfigParams).BindRequest(0x40002b8dc8, 0x400047f5f0?, 0x0)\n\t/app/internal/types/cpop/get_chain_config_parameters.go:49 +0x48\ngithub.com/hzbay/chain-bridge/internal/api/handlers/chains.(*Handler).GetChainConfig(0x40003f0790, {0x151b920, 0x400024a320})\n\t/app/internal/api/handlers/chains/get_chain_config.go:26 +0x8c\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.NoCache.NoCacheWithConfig.func19.1({0x151b920, 0x400024a320})\n\t/app/internal/api/middleware/no_cache.go:88 +0x190\ngithub.com/labstack/echo/v4/middleware.KeyAuthWithConfig.func1.1({0x151b920, 0x400024a320})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/key_auth.go:136 +0x554\ngithub.com/labstack/echo/v4.(*Echo).add.func1({0x151b920, 0x400024a320})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:581 +0x48\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.CacheControl.CacheControlWithConfig.func18.1({0x151b920, 0x400024a320})\n\t/app/internal/api/middleware/cache_control.go:52 +0x38c\ngithub.com/labstack/echo/v4/middleware.CORSWithConfig.func1.1({0x151b920, 0x400024a320})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/cors.go:280 +0x714\ngithub.com/hzbay/chain-bridge/internal/api/middleware.LoggerWithConfig.func1.1({0x151b920, 0x400024a320})\n\t/app/internal/api/middleware/logger.go:259 +0xb40\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RequestID.RequestIDWithConfig.func17.1({0x151b920, 0x400024a320})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/request_id.go:68 +0x10c\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.SecureWithConfig.func11.1({0x151b920, 0x400024a320})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/secure.go:141 +0x2fc\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RecoverWithConfig.func10.1({0x151b920, 0x400024a320})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/recover.go:130 +0xe4\ngithub.com/labstack/echo/v4.(*Echo).ServeHTTP.func1({0x151b920, 0x400024a320})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:662 +0x118\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RemoveTrailingSlash.RemoveTrailingSlashWithConfig.func16.1({0x151b920, 0x400024a320})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/slash.go:117 +0x1c8\ngithub.com/labstack/echo/v4.(*Echo).ServeHTTP(0x40001ec908, {0x1504458, 0x40001707e0}, 0x40003bb680)\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:668 +0x300\nnet/http.serverHandler.ServeHTTP({0x400047f500?}, {0x1504458?, 0x40001707e0?}, 0x6?)\n\t/usr/local/go/src/net/http/server.go:3301 +0xbc\nnet/http.(*conn).serve(0x40001f7440, {0x1506880, 0x400047e870})\n\t/usr/local/go/src/net/http/server.go:2102 +0x52c\ncreated by net/http.(*Server).Serve in goroutine 55\n\t/usr/local/go/src/net/http/server.go:3454 +0x3d8\n\ngoroutine 1 [chan receive]:\ngithub.com/hzbay/chain-bridge/cmd/server.runServer.func1({0x1506880, 0x400051f8c0}, 0x4000451688)\n\t/app/cmd/server/server.go:99 +0x4c4\ngithub.com/hzbay/chain-bridge/internal/util/command.WithServer({_, _}, {{{0x40000100d7, 0x8}, 0x1538, {0x4000010067, 0x6}, {0x4000042023, 0x6}, {0x40000420b3, ...}, ...}, ...}, ...)\n\t/app/internal/util/command/command.go:33 +0x61c\ngithub.com/hzbay/chain-bridge/cmd/server.runServer({0x0?, 0x0?, 0x0?})\n\t/app/cmd/server/server.go:51 +0xb4\ngithub.com/hzbay/chain-bridge/cmd/server.New.func1(0x4000236500?, {0x12173f6?, 0x4?, 0x12173fa?})\n\t/app/cmd/server/server.go:39 +0x2c\ngithub.com/spf13/cobra.(*Command).execute(0x4000153808, {0x1f25f40, 0x0, 0x0})\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1019 +0x810\ngithub.com/spf13/cobra.(*Command).ExecuteC(0x1e75c60)\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1148 +0x350\ngithub.com/spf13/cobra.(*Command).Execute(...)\n\t/go/pkg/mod/github.com/spf13/cobra@v1"



06:06:43 DBG Request received req={"bytes_in":"0","host":"127.0.0.1:8080","id":"UCSzeHKVbNyIGMLovngvLhbauwbiFozT","method":"PUT","url":"/-/chains/11155111/batch-config?batch_size=1&batch_timeout=200&mgmt-secret=123456"}
06:06:43 ERR PANIC RECOVER error="runtime error: invalid memory address or nil pointer dereference" req={"bytes_in":"0","host":"127.0.0.1:8080","id":"UCSzeHKVbNyIGMLovngvLhbauwbiFozT","method":"PUT","url":"/-/chains/11155111/batch-config?batch_size=1&batch_timeout=200&mgmt-secret=123456"} stack="goroutine 70 [running]:\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RecoverWithConfig.func10.1.1()\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/recover.go:99 +0x130\npanic({0x1077bc0?, 0x1e51730?})\n\t/usr/local/go/src/runtime/panic.go:792 +0x124\ngithub.com/hzbay/chain-bridge/internal/types/cpop.(*UpdateChainBatchConfigParams).BindRequest(0x4000054da8, 0x4000589a70?, 0x0)\n\t/app/internal/types/cpop/update_chain_batch_config_parameters.go:64 +0x78\ngithub.com/hzbay/chain-bridge/internal/api/handlers/chains.(*Handler).UpdateBatchConfig(0x400013b840, {0x151bfc0, 0x400027b860})\n\t/app/internal/api/handlers/chains/update_batch_config.go:27 +0x90\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.NoCache.NoCacheWithConfig.func19.1({0x151bfc0, 0x400027b860})\n\t/app/internal/api/middleware/no_cache.go:88 +0x190\ngithub.com/labstack/echo/v4/middleware.KeyAuthWithConfig.func1.1({0x151bfc0, 0x400027b860})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/key_auth.go:136 +0x554\ngithub.com/labstack/echo/v4.(*Echo).add.func1({0x151bfc0, 0x400027b860})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:581 +0x48\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.CacheControl.CacheControlWithConfig.func18.1({0x151bfc0, 0x400027b860})\n\t/app/internal/api/middleware/cache_control.go:52 +0x38c\ngithub.com/labstack/echo/v4/middleware.CORSWithConfig.func1.1({0x151bfc0, 0x400027b860})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/cors.go:280 +0x714\ngithub.com/hzbay/chain-bridge/internal/api/middleware.LoggerWithConfig.func1.1({0x151bfc0, 0x400027b860})\n\t/app/internal/api/middleware/logger.go:259 +0xb40\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RequestID.RequestIDWithConfig.func17.1({0x151bfc0, 0x400027b860})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/request_id.go:68 +0x10c\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.SecureWithConfig.func11.1({0x151bfc0, 0x400027b860})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/secure.go:141 +0x2fc\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RecoverWithConfig.func10.1({0x151bfc0, 0x400027b860})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/recover.go:130 +0xe4\ngithub.com/labstack/echo/v4.(*Echo).ServeHTTP.func1({0x151bfc0, 0x400027b860})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:662 +0x118\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RemoveTrailingSlash.RemoveTrailingSlashWithConfig.func16.1({0x151bfc0, 0x400027b860})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/slash.go:117 +0x1c8\ngithub.com/labstack/echo/v4.(*Echo).ServeHTTP(0x4000294008, {0x1504af8, 0x400019a2a0}, 0x400046e000)\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:668 +0x300\nnet/http.serverHandler.ServeHTTP({0x4000589920?}, {0x1504af8?, 0x400019a2a0?}, 0x6?)\n\t/usr/local/go/src/net/http/server.go:3301 +0xbc\nnet/http.(*conn).serve(0x400017e870, {0x1506f20, 0x40004242a0})\n\t/usr/local/go/src/net/http/server.go:2102 +0x52c\ncreated by net/http.(*Server).Serve in goroutine 67\n\t/usr/local/go/src/net/http/server.go:3454 +0x3d8\n\ngoroutine 1 [chan receive, 5 minutes]:\ngithub.com/hzbay/chain-bridge/cmd/server.runServer.func1({0x1506f20, 0x40002b0120}, 0x400047d688)\n\t/app/cmd/server/server.go:99 +0x4c4\ngithub.com/hzbay/chain-bridge/internal/util/command.WithServer({_, _}, {{{0x40000100d7, 0x8}, 0x1538, {0x4000010067, 0x6}, {0x4000042023, 0x6}, {0x40000420b3, ...}, ...}, ...}, ...)\n\t/app/internal/util/command/command.go:33 +0x61c\ngithub.com/hzbay/chain-bridge/cmd/server.runServer({0x0?, 0x0?, 0x0?})\n\t/app/cmd/server/server.go:51 +0xb4\ngithub.com/hzbay/chain-bridge/cmd/server.New.func1(0x40003ba500?, {0x1217a76?, 0x4?, 0x1217a7a?})\n\t/app/cmd/server/server.go:39 +0x2c\ngithub.com/spf13/cobra.(*Command).execute(0x40000b7b08, {0x1f25ea0, 0x0, 0x0})\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1019 +0x810\ngithub.com/spf13/cobra.(*Command).ExecuteC(0x1e75c60)\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1148 +0x350\ngithub.com/spf13/cobra.(*Command).Execute(...)\n\t/go"

http://127.0.0.1:8080/-/monitoring/queue/metrics?mgmt-secret=123456
{
  "status": 400,
  "title": "Bad Request",
  "type": "generic",
  "validationErrors": [
    {
      "error": "data.connection_status in body should be one of [connected disconnected reconnecting]",
      "in": "body",
      "key": "data.connection_status"
    },
    {
      "error": "data.processor_type in body should be one of [rabbitmq memory hybrid]",
      "in": "body",
      "key": "data.processor_type"
    }
  ]
}


curl -X 'GET' \
  'http://127.0.0.1:8080/-/monitoring/queue/stats?mgmt-secret=123456' \
  -H 'accept: application/json'

{
  "status": 400,
  "title": "Bad Request",
  "type": "generic",
  "validationErrors": [
    {
      "error": "data.0.chain_id in body is required",
      "in": "body",
      "key": "data.0.chain_id"
    },
    {
      "error": "data.0.token_id in body is required",
      "in": "body",
      "key": "data.0.token_id"
    }
  ]
}


08:47:09 DBG Request received req={"bytes_in":"0","host":"127.0.0.1:8080","id":"USQQzvBSxlqhwDOTbXnwLElYwVKttyKp","method":"GET","url":"/api/v1/assets/1"}
08:47:09 ERR PANIC RECOVER error="runtime error: invalid memory address or nil pointer dereference" req={"bytes_in":"0","host":"127.0.0.1:8080","id":"USQQzvBSxlqhwDOTbXnwLElYwVKttyKp","method":"GET","url":"/api/v1/assets/1"} stack="goroutine 33 [running]:\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RecoverWithConfig.func10.1.1()\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/recover.go:99 +0x130\npanic({0x1077660?, 0x1e51730?})\n\t/usr/local/go/src/runtime/panic.go:792 +0x124\ngithub.com/hzbay/chain-bridge/internal/types/cpop.(*GetUserAssetsParams).BindRequest(0x4000054d38, 0x40005b4450?, 0x4000054d08?)\n\t/app/internal/types/cpop/get_user_assets_parameters.go:48 +0x40\ngithub.com/hzbay/chain-bridge/internal/api/handlers/assets.(*GetUserAssetsHandler).Handle(0x4000426080, {0x151b840, 0x4000264000})\n\t/app/internal/api/handlers/assets/get_user_assets.go:41 +0x90\ngithub.com/hzbay/chain-bridge/internal/api/middleware.Auth.AuthWithConfig.func1.1({0x151b840, 0x4000264000})\n\t/app/internal/api/middleware/auth.go:388 +0xa98\ngithub.com/labstack/echo/v4.(*Echo).add.func1({0x151b840, 0x4000264000})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:581 +0x48\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.CacheControl.CacheControlWithConfig.func18.1({0x151b840, 0x4000264000})\n\t/app/internal/api/middleware/cache_control.go:52 +0x38c\ngithub.com/labstack/echo/v4/middleware.CORSWithConfig.func1.1({0x151b840, 0x4000264000})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/cors.go:280 +0x714\ngithub.com/hzbay/chain-bridge/internal/api/middleware.LoggerWithConfig.func1.1({0x151b840, 0x4000264000})\n\t/app/internal/api/middleware/logger.go:259 +0xb40\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RequestID.RequestIDWithConfig.func17.1({0x151b840, 0x4000264000})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/request_id.go:68 +0x10c\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.SecureWithConfig.func11.1({0x151b840, 0x4000264000})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/secure.go:141 +0x2fc\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RecoverWithConfig.func10.1({0x151b840, 0x4000264000})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/recover.go:130 +0xe4\ngithub.com/labstack/echo/v4.(*Echo).ServeHTTP.func1({0x151b840, 0x4000264000})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:662 +0x118\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RemoveTrailingSlash.RemoveTrailingSlashWithConfig.func16.1({0x151b840, 0x4000264000})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/slash.go:117 +0x1c8\ngithub.com/labstack/echo/v4.(*Echo).ServeHTTP(0x4000294488, {0x1504378, 0x400027e0e0}, 0x400028cf00)\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:668 +0x300\nnet/http.serverHandler.ServeHTTP({0x40004524e0?}, {0x1504378?, 0x400027e0e0?}, 0x6?)\n\t/usr/local/go/src/net/http/server.go:3301 +0xbc\nnet/http.(*conn).serve(0x40005cc360, {0x15067a0, 0x40004523f0})\n\t/usr/local/go/src/net/http/server.go:2102 +0x52c\ncreated by net/http.(*Server).Serve in goroutine 11\n\t/usr/local/go/src/net/http/server.go:3454 +0x3d8\n\ngoroutine 1 [chan receive]:\ngithub.com/hzbay/chain-bridge/cmd/server.runServer.func1({0x15067a0, 0x4000297cb0}, 0x40003ef208)\n\t/app/cmd/server/server.go:99 +0x4c4\ngithub.com/hzbay/chain-bridge/internal/util/command.WithServer({_, _}, {{{0x40000100d7, 0x8}, 0x1538, {0x4000010067, 0x6}, {0x4000042023, 0x6}, {0x40000420fb, ...}, ...}, ...}, ...)\n\t/app/internal/util/command/command.go:33 +0x61c\ngithub.com/hzbay/chain-bridge/cmd/server.runServer({0x0?, 0x0?, 0x0?})\n\t/app/cmd/server/server.go:51 +0xb4\ngithub.com/hzbay/chain-bridge/cmd/server.New.func1(0x400031e500?, {0x1217316?, 0x4?, 0x121731a?})\n\t/app/cmd/server/server.go:39 +0x2c\ngithub.com/spf13/cobra.(*Command).execute(0x4000313b08, {0x1f25f40, 0x0, 0x0})\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1019 +0x810\ngithub.com/spf13/cobra.(*Command).ExecuteC(0x1e75c60)\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1148 +0x350\ngithub.com/spf13/cobra.(*Command).Execute(...)\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1071\ngithub.com/hzbay/chain-bridge/cmd.Execute()\n\t/app/cmd/root.go:29 +0x24\nmain.main()\n\t/app/main.go:6 +0x1c\n\ngoroutine 21 [select, 1 minutes]:\ndatabase/sql." userID=840d2474-5321-4b8b-80a6-05367447d336


08:50:13 DBG Request received req={"bytes_in":"0","host":"127.0.0.1:8080","id":"dQaAKkGWBUlNCIhupVNOjLLAOPMIcnIR","method":"GET","url":"/api/v1/assets/111/transactions?chain_id=11155111&token_symbol=CPOP&tx_type=mint&page=1&limit=20"}
08:50:13 ERR PANIC RECOVER error="runtime error: invalid memory address or nil pointer dereference" req={"bytes_in":"0","host":"127.0.0.1:8080","id":"dQaAKkGWBUlNCIhupVNOjLLAOPMIcnIR","method":"GET","url":"/api/v1/assets/111/transactions?chain_id=11155111&token_symbol=CPOP&tx_type=mint&page=1&limit=20"} stack="goroutine 33 [running]:\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RecoverWithConfig.func10.1.1()\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/recover.go:99 +0x130\npanic({0x1077660?, 0x1e51730?})\n\t/usr/local/go/src/runtime/panic.go:792 +0x124\ngithub.com/hzbay/chain-bridge/internal/types/cpop.(*GetUserTransactionsParams).BindRequest(0x4000054ce0, 0x40004152c0?, 0x0)\n\t/app/internal/types/cpop/get_user_transactions_parameters.go:98 +0x7c\ngithub.com/hzbay/chain-bridge/internal/api/handlers/transfer.(*GetUserTransactionsHandler).Handle(0x4000426230, {0x151b840, 0x40001fa140})\n\t/app/internal/api/handlers/transfer/get_user_transactions.go:41 +0xdc\ngithub.com/hzbay/chain-bridge/internal/api/middleware.Auth.AuthWithConfig.func1.1({0x151b840, 0x40001fa140})\n\t/app/internal/api/middleware/auth.go:388 +0xa98\ngithub.com/labstack/echo/v4.(*Echo).add.func1({0x151b840, 0x40001fa140})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:581 +0x48\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.CacheControl.CacheControlWithConfig.func18.1({0x151b840, 0x40001fa140})\n\t/app/internal/api/middleware/cache_control.go:52 +0x38c\ngithub.com/labstack/echo/v4/middleware.CORSWithConfig.func1.1({0x151b840, 0x40001fa140})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/cors.go:280 +0x714\ngithub.com/hzbay/chain-bridge/internal/api/middleware.LoggerWithConfig.func1.1({0x151b840, 0x40001fa140})\n\t/app/internal/api/middleware/logger.go:259 +0xb40\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RequestID.RequestIDWithConfig.func17.1({0x151b840, 0x40001fa140})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/request_id.go:68 +0x10c\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.SecureWithConfig.func11.1({0x151b840, 0x40001fa140})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/secure.go:141 +0x2fc\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RecoverWithConfig.func10.1({0x151b840, 0x40001fa140})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/recover.go:130 +0xe4\ngithub.com/labstack/echo/v4.(*Echo).ServeHTTP.func1({0x151b840, 0x40001fa140})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:662 +0x118\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RemoveTrailingSlash.RemoveTrailingSlashWithConfig.func16.1({0x151b840, 0x40001fa140})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/slash.go:117 +0x1c8\ngithub.com/labstack/echo/v4.(*Echo).ServeHTTP(0x4000294488, {0x1504378, 0x40001980e0}, 0x400028c000)\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:668 +0x300\nnet/http.serverHandler.ServeHTTP({0x40004524e0?}, {0x1504378?, 0x40001980e0?}, 0x6?)\n\t/usr/local/go/src/net/http/server.go:3301 +0xbc\nnet/http.(*conn).serve(0x40005cc360, {0x15067a0, 0x40004523f0})\n\t/usr/local/go/src/net/http/server.go:2102 +0x52c\ncreated by net/http.(*Server).Serve in goroutine 11\n\t/usr/local/go/src/net/http/server.go:3454 +0x3d8\n\ngoroutine 1 [chan receive, 4 minutes]:\ngithub.com/hzbay/chain-bridge/cmd/server.runServer.func1({0x15067a0, 0x4000297cb0}, 0x40003ef208)\n\t/app/cmd/server/server.go:99 +0x4c4\ngithub.com/hzbay/chain-bridge/internal/util/command.WithServer({_, _}, {{{0x40000100d7, 0x8}, 0x1538, {0x4000010067, 0x6}, {0x4000042023, 0x6}, {0x40000420fb, ...}, ...}, ...}, ...)\n\t/app/internal/util/command/command.go:33 +0x61c\ngithub.com/hzbay/chain-bridge/cmd/server.runServer({0x0?, 0x0?, 0x0?})\n\t/app/cmd/server/server.go:51 +0xb4\ngithub.com/hzbay/chain-bridge/cmd/server.New.func1(0x400031e500?, {0x1217316?, 0x4?, 0x121731a?})\n\t/app/cmd/server/server.go:39 +0x2c\ngithub.com/spf13/cobra.(*Command).execute(0x4000313b08, {0x1f25f40, 0x0, 0x0})\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1019 +0x810\ngithub.com/spf13/cobra.(*Command).ExecuteC(0x1e75c60)\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1148 +0x350\ngithub.com/spf13/cobra.(*Command).Execute(...)\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1071\ngithub.com/hzbay/chain-bridge/cmd.Execute()\n\t/app/cmd/root.go:29 +0x24\nmain.main()\n\t/app/main.go:6 +0x1c\n\ngoroutine 21 [sele" userID=840d2474-5321-4b8b-80a6-05367447d336

08:51:26 INF Processing asset adjustment request adjustment_count=1 operation_id=op_daily_rewards_001 req={"bytes_in":"387","host":"127.0.0.1:8080","id":"HYLufAnSdDsQPdLiGtPoCDgaHuLErmfN","method":"POST","url":"/api/v1/assets/adjust"} userID=840d2474-5321-4b8b-80a6-05367447d336
08:51:26 INF Processing asset adjustment request adjustment_count=1 operation_id=op_daily_rewards_001
08:51:26 ERR PANIC RECOVER error="uuid: Parse(op_daily_rewards_001): invalid UUID length: 20" req={"bytes_in":"387","host":"127.0.0.1:8080","id":"HYLufAnSdDsQPdLiGtPoCDgaHuLErmfN","method":"POST","url":"/api/v1/assets/adjust"} stack="goroutine 33 [running]:\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RecoverWithConfig.func10.1.1()\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/recover.go:99 +0x130\npanic({0xff9a20?, 0x4000380c70?})\n\t/usr/local/go/src/runtime/panic.go:792 +0x124\ngithub.com/google/uuid.MustParse({0x40004381b0, 0x14})\n\t/go/pkg/mod/github.com/google/uuid@v1.6.0/uuid.go:169 +0xa0\ngithub.com/hzbay/chain-bridge/internal/services/assets.(*service).AdjustAssets(0x4000155700, {0x15067a0, 0x40000e8a20}, 0x40000e8a50)\n\t/app/internal/services/assets/service.go:59 +0xd4\ngithub.com/hzbay/chain-bridge/internal/api/handlers/assets.(*AdjustAssetsHandler).Handle(0x40004260b0, {0x151b840, 0x4000264280})\n\t/app/internal/api/handlers/assets/adjust_assets.go:52 +0x158\ngithub.com/hzbay/chain-bridge/internal/api/middleware.Auth.AuthWithConfig.func1.1({0x151b840, 0x4000264280})\n\t/app/internal/api/middleware/auth.go:388 +0xa98\ngithub.com/labstack/echo/v4.(*Echo).add.func1({0x151b840, 0x4000264280})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:581 +0x48\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.CacheControl.CacheControlWithConfig.func18.1({0x151b840, 0x4000264280})\n\t/app/internal/api/middleware/cache_control.go:52 +0x38c\ngithub.com/labstack/echo/v4/middleware.CORSWithConfig.func1.1({0x151b840, 0x4000264280})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/cors.go:280 +0x714\ngithub.com/hzbay/chain-bridge/internal/api/middleware.LoggerWithConfig.func1.1({0x151b840, 0x4000264280})\n\t/app/internal/api/middleware/logger.go:259 +0xb40\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RequestID.RequestIDWithConfig.func17.1({0x151b840, 0x4000264280})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/request_id.go:68 +0x10c\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.SecureWithConfig.func11.1({0x151b840, 0x4000264280})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/secure.go:141 +0x2fc\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RecoverWithConfig.func10.1({0x151b840, 0x4000264280})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/recover.go:130 +0xe4\ngithub.com/labstack/echo/v4.(*Echo).ServeHTTP.func1({0x151b840, 0x4000264280})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:662 +0x118\ngithub.com/hzbay/chain-bridge/internal/api/router.Init.RemoveTrailingSlash.RemoveTrailingSlashWithConfig.func16.1({0x151b840, 0x4000264280})\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/slash.go:117 +0x1c8\ngithub.com/labstack/echo/v4.(*Echo).ServeHTTP(0x4000294488, {0x1504378, 0x40001981c0}, 0x400028c3c0)\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/echo.go:668 +0x300\nnet/http.serverHandler.ServeHTTP({0x1500118?}, {0x1504378?, 0x40001981c0?}, 0x6?)\n\t/usr/local/go/src/net/http/server.go:3301 +0xbc\nnet/http.(*conn).serve(0x40005cc360, {0x15067a0, 0x40004523f0})\n\t/usr/local/go/src/net/http/server.go:2102 +0x52c\ncreated by net/http.(*Server).Serve in goroutine 11\n\t/usr/local/go/src/net/http/server.go:3454 +0x3d8\n\ngoroutine 1 [chan receive, 5 minutes]:\ngithub.com/hzbay/chain-bridge/cmd/server.runServer.func1({0x15067a0, 0x4000297cb0}, 0x40003ef208)\n\t/app/cmd/server/server.go:99 +0x4c4\ngithub.com/hzbay/chain-bridge/internal/util/command.WithServer({_, _}, {{{0x40000100d7, 0x8}, 0x1538, {0x4000010067, 0x6}, {0x4000042023, 0x6}, {0x40000420fb, ...}, ...}, ...}, ...)\n\t/app/internal/util/command/command.go:33 +0x61c\ngithub.com/hzbay/chain-bridge/cmd/server.runServer({0x0?, 0x0?, 0x0?})\n\t/app/cmd/server/server.go:51 +0xb4\ngithub.com/hzbay/chain-bridge/cmd/server.New.func1(0x400031e500?, {0x1217316?, 0x4?, 0x121731a?})\n\t/app/cmd/server/server.go:39 +0x2c\ngithub.com/spf13/cobra.(*Command).execute(0x4000313b08, {0x1f25f40, 0x0, 0x0})\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1019 +0x810\ngithub.com/spf13/cobra.(*Command).ExecuteC(0x1e75c60)\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1148 +0x350\ngithub.com/spf13/cobra.(*Command).Execute(...)\n\t/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go:1071\ngithub.com/hzbay/chain-bridge/cmd.Exec" userID=840d2474-5321-4b8b-80a6-05367447d336


08:52:34 DBG Request received req={"bytes_in":"403","host":"127.0.0.1:8080","id":"frCZAptdoHMQoIINaRKRBMcIdzYCHxmZ","method":"POST","url":"/api/v1/assets/adjust"}
08:52:34 INF Processing asset adjustment request adjustment_count=1 operation_id=3a74f76d-2527-49f0-861b-98a87c78e457 req={"bytes_in":"403","host":"127.0.0.1:8080","id":"frCZAptdoHMQoIINaRKRBMcIdzYCHxmZ","method":"POST","url":"/api/v1/assets/adjust"} userID=840d2474-5321-4b8b-80a6-05367447d336
08:52:34 INF Processing asset adjustment request adjustment_count=1 operation_id=3a74f76d-2527-49f0-861b-98a87c78e457
08:52:34 DBG New operation, proceeding with processing operation_id=3a74f76d-2527-49f0-861b-98a87c78e457
08:52:34 WRN Failed to find token, using default error="sql: no rows in result set" chain_id=56 symbol=CPOP
08:52:34 ERR Failed to process asset adjustments error="failed to insert transaction for user user_123: models: unable to insert into transactions: pq: encode: unknown type for types.Decimal" adjustments=[{"amount":"+100.0","business_type":"reward","chain_id":56,"reason_detail":"Daily check-in reward","reason_type":"daily_checkin","token_symbol":"CPOP","user_id":"user_123"}] operation_id=3a74f76d-2527-49f0-861b-98a87c78e457 req={"bytes_in":"403","host":"127.0.0.1:8080","id":"frCZAptdoHMQoIINaRKRBMcIdzYCHxmZ","method":"POST","url":"/api/v1/assets/adjust"} userID=840d2474-5321-4b8b-80a6-05367447d336
08:52:34 DBG Response sent req={"bytes_in":"403","host":"127.0.0.1:8080","id":"frCZAptdoHMQoIINaRKRBMcIdzYCHxmZ","method":"POST","url":"/api/v1/assets/adjust"} res={"bytes_out":64,"duration_ms":23.851375,"error":"failed to insert transaction for user user_123: models: unable to insert into transactions: pq: encode: unknown type for types.Decimal","status":500} userID=840d2474-5321-4b8b-80a6-05367447d336
