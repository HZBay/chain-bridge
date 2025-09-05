#!/bin/bash

# NFT Mint API 测试用例
# 测试接口: POST /api/v1/assets/nft/mint

BASE_URL="http://127.0.0.1:8080"
AUTH_TOKEN="b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc"

echo "=== NFT Mint API 测试用例 ==="
echo "基础URL: $BASE_URL"
echo "认证Token: $AUTH_TOKEN"
echo ""

# 测试用例1: 基础奖励NFT铸造 (单个NFT)
echo "测试用例1: 基础奖励NFT铸造"
echo "描述: 铸造单个奖励类型的NFT给用户"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/nft/mint" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
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
        "description": "Congratulations on reaching Level 10! This badge represents your dedication and skill.",
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
          },
          {
            "trait_type": "Date Earned",
            "value": "2024-12-21"
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
echo -e "\n\n"

# 测试用例2: 高优先级稀有NFT铸造
echo "测试用例2: 高优先级稀有NFT铸造"
echo "描述: 铸造稀有NFT，使用高优先级处理"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/nft/mint" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
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
        "description": "A legendary Christmas Dragon NFT with sparkling snow effects and festive decorations. Only 100 will ever exist!",
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
            "trait_type": "Event",
            "value": "Christmas 2024"
          },
          {
            "trait_type": "Power Level",
            "value": "9500"
          },
          {
            "trait_type": "Special Effect",
            "value": "Snow Sparkle"
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

# 测试用例3: 批量NFT铸造 (多个用户)
echo "测试用例3: 批量NFT铸造"
echo "描述: 为多个用户批量铸造不同的NFT"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/nft/mint" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
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
      "reason_detail": "New user welcome package",
      "meta": {
        "name": "Starter Sword",
        "description": "A basic sword for new adventurers. Perfect for starting your journey!",
        "image": "https://example.com/items/starter-sword.png",
        "external_url": "https://example.com/items/starter-sword",
        "attributes": [
          {
            "trait_type": "Item Type",
            "value": "Weapon"
          },
          {
            "trait_type": "Weapon Class",
            "value": "Sword"
          },
          {
            "trait_type": "Rarity",
            "value": "Common"
          },
          {
            "trait_type": "Attack Power",
            "value": "50"
          },
          {
            "trait_type": "Durability",
            "value": "100"
          }
        ]
      }
    },
    {
      "to_user_id": "user789",
      "business_type": "reward",
      "reason_type": "welcome_bonus",
      "reason_detail": "New user welcome package",
      "meta": {
        "name": "Leather Armor",
        "description": "Basic leather armor providing minimal protection for beginners.",
        "image": "https://example.com/items/leather-armor.png",
        "external_url": "https://example.com/items/leather-armor",
        "attributes": [
          {
            "trait_type": "Item Type",
            "value": "Armor"
          },
          {
            "trait_type": "Armor Class",
            "value": "Light"
          },
          {
            "trait_type": "Rarity",
            "value": "Common"
          },
          {
            "trait_type": "Defense Power",
            "value": "25"
          },
          {
            "trait_type": "Weight",
            "value": "5"
          }
        ]
      }
    },
    {
      "to_user_id": "user101112",
      "business_type": "reward",
      "reason_type": "referral_bonus",
      "reason_detail": "Successful referral reward",
      "meta": {
        "name": "Golden Coin",
        "description": "A shiny golden coin awarded for successful referrals.",
        "image": "https://example.com/items/golden-coin.png",
        "external_url": "https://example.com/items/golden-coin",
        "attributes": [
          {
            "trait_type": "Item Type",
            "value": "Currency"
          },
          {
            "trait_type": "Material",
            "value": "Gold"
          },
          {
            "trait_type": "Rarity",
            "value": "Uncommon"
          },
          {
            "trait_type": "Value",
            "value": "1000"
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
echo -e "\n\n"

# 测试用例4: 消费类型NFT (游戏内购买)
echo "测试用例4: 消费类型NFT铸造"
echo "描述: 用户购买游戏道具的NFT铸造"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/nft/mint" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
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
        "description": "A magical potion that instantly restores health when consumed.",
        "image": "https://example.com/potions/health-potion.png",
        "external_url": "https://example.com/shop/potions/health",
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
          },
          {
            "trait_type": "Rarity",
            "value": "Common"
          },
          {
            "trait_type": "Shop Price",
            "value": "50 CPOP"
          },
          {
            "trait_type": "Usage",
            "value": "Single Use"
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

# 测试用例5: BSC主网NFT铸造
echo "测试用例5: BSC主网NFT铸造"
echo "描述: 在BSC主网上铸造NFT"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/nft/mint" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
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
        "description": "Exclusive trophy NFT for the December 2024 PvP Tournament Champion. A symbol of skill and victory!",
        "image": "https://example.com/trophies/champion-2024-12.png",
        "external_url": "https://example.com/tournaments/2024-12/champion",
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
            "trait_type": "Participants",
            "value": "1000+"
          },
          {
            "trait_type": "Rarity",
            "value": "Unique"
          },
          {
            "trait_type": "Network",
            "value": "BSC Mainnet"
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
echo -e "\n\n"

# 测试用例6: 最小化参数测试
echo "测试用例6: 最小化参数测试"
echo "描述: 只包含必需参数的最简NFT铸造请求"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/nft/mint" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
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
echo -e "\n\n"

echo "=== 所有测试用例执行完成 ==="
