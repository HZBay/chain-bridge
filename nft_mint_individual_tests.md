# NFT Mint API 测试用例

## 基础信息
- **接口**: `POST /api/v1/assets/nft/mint`
- **基础URL**: `http://127.0.0.1:8080`
- **认证Token**: `Bearer b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc`

---

## 测试用例1: 基础奖励NFT铸造
**描述**: 铸造单个奖励类型的NFT给用户

```bash
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/nft/mint' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440001",
  "collection_id": "cpop_nft_collection_1",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "user123",
      "business_type": "reward",
      "reason_type": "achievement_unlock",
      "reason_detail": "Level 10 achievement",
      "meta": {
        "name": "Achievement Badge - Level 10",
        "description": "Congratulations on reaching Level 10!",
        "image": "https://example.com/badges/level-10.png",
        "external_url": "https://example.com/achievement/level-10",
        "attributes": [
          {
            "trait_type": "Achievement Type",
            "value": "Level Milestone"
          },
          {
            "trait_type": "Level",
            "value": "10"
          },
          {
            "trait_type": "Rarity",
            "value": "Common"
          }
        ]
      }
    }
  ],
  "batch_preferences": {
    "priority": "normal",
    "max_wait_time": "15m"
  }
}'
```

---

## 测试用例2: 高优先级稀有NFT铸造
**描述**: 铸造稀有NFT，使用高优先级处理

```bash
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/nft/mint' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440002",
  "collection_id": "cpop_rare_collection",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "user456",
      "business_type": "reward",
      "reason_type": "special_event",
      "reason_detail": "Christmas 2024 Special Event Winner",
      "meta": {
        "name": "Christmas Dragon 2024",
        "description": "A legendary Christmas Dragon NFT. Only 100 will ever exist!",
        "image": "https://example.com/nfts/christmas-dragon-2024.png",
        "external_url": "https://example.com/collection/christmas-2024/dragon",
        "attributes": [
          {
            "trait_type": "Background",
            "value": "Snowy Mountains",
            "rarity_percentage": 5.0
          },
          {
            "trait_type": "Dragon Type",
            "value": "Ice Dragon"
          },
          {
            "trait_type": "Rarity",
            "value": "Legendary"
          },
          {
            "trait_type": "Power Level",
            "value": "9500"
          }
        ]
      }
    }
  ],
  "batch_preferences": {
    "priority": "high",
    "max_wait_time": "5m"
  }
}'
```

---

## 测试用例3: 批量NFT铸造
**描述**: 为同一用户批量铸造多个不同的NFT

```bash
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/nft/mint' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440003",
  "collection_id": "cpop_starter_pack",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "user789",
      "business_type": "reward",
      "reason_type": "welcome_bonus",
      "reason_detail": "New user welcome package - Sword",
      "meta": {
        "name": "Starter Sword",
        "description": "A basic sword for new adventurers.",
        "image": "https://example.com/items/starter-sword.png",
        "attributes": [
          {
            "trait_type": "Item Type",
            "value": "Weapon"
          },
          {
            "trait_type": "Attack Power",
            "value": "50"
          }
        ]
      }
    },
    {
      "to_user_id": "user789",
      "business_type": "reward",
      "reason_type": "welcome_bonus",
      "reason_detail": "New user welcome package - Armor",
      "meta": {
        "name": "Leather Armor",
        "description": "Basic leather armor for beginners.",
        "image": "https://example.com/items/leather-armor.png",
        "attributes": [
          {
            "trait_type": "Item Type",
            "value": "Armor"
          },
          {
            "trait_type": "Defense Power",
            "value": "25"
          }
        ]
      }
    }
  ],
  "batch_preferences": {
    "priority": "normal",
    "max_wait_time": "20m"
  }
}'
```

---

## 测试用例4: 消费类型NFT铸造
**描述**: 用户购买游戏道具的NFT铸造

```bash
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/nft/mint' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440004",
  "collection_id": "cpop_shop_items",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "user131415",
      "business_type": "consumption",
      "reason_type": "shop_purchase",
      "reason_detail": "Purchased Magic Potion from in-game shop",
      "meta": {
        "name": "Health Potion",
        "description": "A magical potion that instantly restores health.",
        "image": "https://example.com/potions/health-potion.png",
        "attributes": [
          {
            "trait_type": "Item Type",
            "value": "Consumable"
          },
          {
            "trait_type": "Effect",
            "value": "Heal"
          },
          {
            "trait_type": "Healing Amount",
            "value": "100"
          }
        ]
      }
    }
  ],
  "batch_preferences": {
    "priority": "low",
    "max_wait_time": "30m"
  }
}'
```

---

## 测试用例5: BSC主网NFT铸造
**描述**: 在BSC主网上铸造NFT

```bash
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/nft/mint' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440005",
  "collection_id": "cpop_mainnet_collection",
  "chain_id": 56,
  "mint_operations": [
    {
      "to_user_id": "user161718",
      "business_type": "reward",
      "reason_type": "tournament_winner",
      "reason_detail": "Winner of December 2024 PvP Tournament",
      "meta": {
        "name": "Champion Trophy 2024",
        "description": "Exclusive trophy NFT for tournament champion.",
        "image": "https://example.com/trophies/champion-2024-12.png",
        "attributes": [
          {
            "trait_type": "Achievement",
            "value": "Tournament Champion"
          },
          {
            "trait_type": "Tournament",
            "value": "December 2024 PvP"
          },
          {
            "trait_type": "Rank",
            "value": "1st Place"
          },
          {
            "trait_type": "Rarity",
            "value": "Unique"
          }
        ]
      }
    }
  ],
  "batch_preferences": {
    "priority": "high",
    "max_wait_time": "10m"
  }
}'
```

---

## 测试用例6: 最小化参数测试
**描述**: 只包含必需参数的最简NFT铸造请求

```bash
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/nft/mint' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440006",
  "collection_id": "cpop_basic_collection",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "user192021",
      "business_type": "reward",
      "reason_type": "daily_login",
      "reason_detail": "Daily login bonus",
      "meta": {
        "name": "Daily Login Badge",
        "description": "Badge for daily login",
        "image": "https://example.com/badges/daily-login.png",
        "attributes": []
      }
    }
  ]
}'
```

---

## 测试用例7: 错误测试 - 缺少必需参数
**描述**: 测试缺少必需参数时的错误响应

```bash
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/nft/mint' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440007",
  "collection_id": "cpop_basic_collection",
  "mint_operations": [
    {
      "to_user_id": "user_error_test",
      "business_type": "reward",
      "reason_type": "test",
      "reason_detail": "Error test - missing chain_id"
    }
  ]
}'
```

---

## 测试用例8: 错误测试 - 无效认证
**描述**: 测试无效认证token的错误响应

```bash
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/nft/mint' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer invalid-token-12345' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440008",
  "collection_id": "cpop_basic_collection",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "user_auth_test",
      "business_type": "reward",
      "reason_type": "test",
      "reason_detail": "Auth error test",
      "meta": {
        "name": "Test NFT",
        "description": "Test NFT for auth error",
        "image": "https://example.com/test.png",
        "attributes": []
      }
    }
  ]
}'
```

---

## 预期响应格式

### 成功响应 (200)
```json
{
  "data": {
    "operation_id": "550e8400-e29b-41d4-a716-446655440001",
    "processed_count": 1,
    "status": "recorded"
  },
  "batch_info": {
    "pending_operations": 3,
    "next_batch_estimate": "5-10 minutes",
    "will_be_batched": true,
    "batch_id": "batch_nft_mint_20241221",
    "current_batch_size": 24,
    "optimal_batch_size": 25,
    "expected_efficiency": "75-77%",
    "estimated_gas_savings": "156.80 USD",
    "batch_type": "batchMintNFT"
  }
}
```

### 错误响应 (400)
```json
{
  "error": "INVALID_REQUEST",
  "message": "Missing required field: chain_id",
  "details": {
    "field": "chain_id",
    "code": "FIELD_REQUIRED"
  }
}
```

### 认证错误响应 (401)
```json
{
  "error": "UNAUTHORIZED",
  "message": "Invalid or expired authentication token",
  "details": {
    "code": "TOKEN_INVALID"
  }
}
```
