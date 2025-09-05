#!/bin/bash

# NFT Mint API 测试用例 - 使用有效的collection
# 使用已知存在的 cpop_official_nft_collection

BASE_URL="http://127.0.0.1:8080"
AUTH_TOKEN="b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc"

echo "=== NFT Mint API 测试用例 (有效Collection) ==="
echo "基础URL: $BASE_URL"
echo "认证Token: $AUTH_TOKEN"
echo ""

# 测试用例1: 单个NFT铸造
echo "测试用例1: cpop_official_nft_collection - 单个NFT铸造"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/nft/mint" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655470031",
  "collection_id": "cpop_official_nft_collection",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "90",
      "business_type": "reward",
      "reason_type": "test_mint",
      "reason_detail": "Test mint for cpop_official_nft_collection",
      "meta": {
        "name": "Test Rare NFT",
        "description": "A test NFT for cpop_official_nft_collection",
        "image": "https://example.com/test-rare.png",
        "external_url": "https://example.com/test-rare",
        "attributes": [
          {
            "trait_type": "Test Type",
            "value": "Rare Collection Test"
          }
        ]
      }
    }
  ],
  "batch_preferences": {
    "priority": "normal",
    "max_wait_time": "10m"
  }
}'
echo -e "\n\n"

# 测试用例2: 批量NFT铸造
echo "测试用例2: cpop_official_nft_collection - 批量NFT铸造"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/nft/mint" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655470032",
  "collection_id": "cpop_official_nft_collection",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "90",
      "business_type": "reward",
      "reason_type": "welcome_bonus",
      "reason_detail": "Starter pack item 1",
      "meta": {
        "name": "Starter Weapon",
        "description": "Basic weapon for new players",
        "image": "https://example.com/starter-weapon.png",
        "attributes": [
          {
            "trait_type": "Item Type",
            "value": "Weapon"
          },
          {
            "trait_type": "Rarity",
            "value": "Common"
          }
        ]
      }
    },
    {
      "to_user_id": "90",
      "business_type": "reward",
      "reason_type": "welcome_bonus",
      "reason_detail": "Starter pack item 2",
      "meta": {
        "name": "Starter Shield",
        "description": "Basic shield for new players",
        "image": "https://example.com/starter-shield.png",
        "attributes": [
          {
            "trait_type": "Item Type",
            "value": "Shield"
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
    "priority": "high",
    "max_wait_time": "5m"
  }
}'
echo -e "\n\n"

# 测试用例3: 最小参数测试
echo "测试用例3: cpop_official_nft_collection - 最小参数测试"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/nft/mint" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655470033",
  "collection_id": "cpop_official_nft_collection",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "90",
      "business_type": "reward",
      "reason_type": "daily_reward",
      "reason_detail": "Daily login reward",
      "meta": {
        "name": "Daily Badge",
        "description": "Badge for daily login",
        "image": "https://example.com/daily-badge.png",
        "attributes": []
      }
    }
  ]
}'
echo -e "\n\n"

# 测试用例4: 测试不同的business_type
echo "测试用例4: cpop_official_nft_collection - 消费类型测试"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/nft/mint" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655470034",
  "collection_id": "cpop_official_nft_collection",
  "chain_id": 11155111,
  "mint_operations": [
    {
      "to_user_id": "90",
      "business_type": "consumption",
      "reason_type": "purchase",
      "reason_detail": "Purchased from shop",
      "meta": {
        "name": "Purchased Item",
        "description": "Item purchased from in-game shop",
        "image": "https://example.com/purchased-item.png",
        "attributes": [
          {
            "trait_type": "Source",
            "value": "Shop Purchase"
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
echo -e "\n\n"

echo "=== 有效Collection测试完成 ==="