chains 相关的API未按CLAUDE.md标准修改

internal/services里面的参数验证有重复，已经统一在handler里面验证过了

帮忙实现批处理消费与上链核心逻辑：
阅读文档
docs/chain-bridge/Design.md，
与docs/chain-bridge/Design-Clean.md，
assets-adjust-implementation.md
docs/batch-processing-complete-summary.md

阅读已完成的代码重点关注代码中Todo的部分，包括internal/api/handlers/assets/adjust_assets.go
internal/api/handlers/transfer/transfer_assets.go
internal/services
internal/queue

在现有系统基础上，开发最后一步：从消息队列（MQ）中消费批量任务（BatchJob），聚合后调用区块链智能合约的批量方法进行上链操作。 已完成的功能（如API、入库、入队）无需改动。
入口internal/queue/hybrid_processor.go的函数StartBatchConsumer
internal/queue/memory_processor.go的函数StopBatchConsumer
StopBatchConsumer 函数需要能安全地通知 StartBatchConsumer 停止工作，并等待其处理完剩余任务。

上链调用

使用已提供的 cpop_token.go ABI封装方法进行上链调用：

Mint 操作 -> 调用 BatchMint 方法。

Burn 操作 -> 调用 BatchBurn 方法。

Transfer 操作 -> 调用 BatchTransfer 方法。

关键非功能性需求

幂等性：处理必须幂等，依靠 BatchJob 或其中包含的单个请求的唯一ID（如 RequestID）来避免重复上链。

可靠性：

上链成功需更新相关业务状态（如数据库状态标记为“成功”）。

上链失败需有重试机制（如指数退避）并记录日志或告警。

支持优雅停机：当收到停止信号或通道关闭时，必须将内存中尚未处理的所有批次处理完毕后再退出。

可观测性：关键步骤（开始消费、触发批量、调用合约、成功、失败）需记录日志。

已完成的功能不要去动，只开发批处理的逻辑，先整理方案别写代码。


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
