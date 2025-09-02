#!/bin/bash

# Transfer API Testing Script
# This script tests the new unified batch transfer API

BASE_URL="http://127.0.0.1:8080"
AUTH_TOKEN="3a74f76d-2527-49f0-861b-98a87c78e457"

echo "=== Testing Transfer API ==="
echo "Base URL: $BASE_URL"
echo "Auth Token: $AUTH_TOKEN"
echo ""

# Test 1: Single Transfer (your example)
echo "Test 1: Single Transfer"
echo "========================"
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/transfer" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "3a74f76d-2527-49f0-861b-98a87c78e457",
  "transfers": [
    {
      "amount": "50.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "朋友转账",
      "to_user_id": "70",
      "token_symbol": "CPOP"
    }
  ]
}' | jq '.'
echo -e "\n\n"

# Test 2: Multiple Transfers in One Batch
echo "Test 2: Multiple Transfers in One Batch"
echo "======================================="
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/transfer" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "batch-test-001",
  "transfers": [
    {
      "amount": "30.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "First transfer",
      "to_user_id": "70",
      "token_symbol": "CPOP"
    },
    {
      "amount": "20.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "Second transfer",
      "to_user_id": "71",
      "token_symbol": "CPOP"
    }
  ]
}' | jq '.'
echo -e "\n\n"

# Test 3: Idempotency Test (same operation_id as Test 1)
echo "Test 3: Idempotency Test (same operation_id as Test 1)"
echo "====================================================="
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/transfer" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "3a74f76d-2527-49f0-861b-98a87c78e457",
  "transfers": [
    {
      "amount": "50.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "朋友转账",
      "to_user_id": "70",
      "token_symbol": "CPOP"
    }
  ]
}' | jq '.'
echo -e "\n\n"

# Test 4: Invalid UUID Operation ID
echo "Test 4: Invalid UUID Operation ID"
echo "================================="
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/transfer" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "invalid-uuid",
  "transfers": [
    {
      "amount": "50.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "Test transfer",
      "to_user_id": "70",
      "token_symbol": "CPOP"
    }
  ]
}' | jq '.'
echo -e "\n\n"

# Test 5: Invalid Amount Format
echo "Test 5: Invalid Amount Format"
echo "============================="
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/transfer" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "test-invalid-amount",
  "transfers": [
    {
      "amount": "invalid-amount",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "Test transfer",
      "to_user_id": "70",
      "token_symbol": "CPOP"
    }
  ]
}' | jq '.'
echo -e "\n\n"

# Test 6: Unsupported Chain
echo "Test 6: Unsupported Chain"
echo "========================="
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/transfer" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "test-invalid-chain",
  "transfers": [
    {
      "amount": "50.000000000000000000",
      "chain_id": 99999,
      "from_user_id": "user_test_456",
      "memo": "Test transfer",
      "to_user_id": "70",
      "token_symbol": "CPOP"
    }
  ]
}' | jq '.'
echo -e "\n\n"

# Test 7: Unsupported Token on Chain
echo "Test 7: Unsupported Token on Chain"
echo "=================================="
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/transfer" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "test-invalid-token",
  "transfers": [
    {
      "amount": "50.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "Test transfer",
      "to_user_id": "70",
      "token_symbol": "INVALIDTOKEN"
    }
  ]
}' | jq '.'
echo -e "\n\n"

# Test 8: Cross-Chain Transfer (Multiple chains in one batch)
echo "Test 8: Cross-Chain Transfer (Multiple chains in one batch)"
echo "==========================================================="
curl -X 'POST' \
  "$BASE_URL/api/v1/assets/transfer" \
  -H 'accept: application/json' \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "cross-chain-test-001",
  "transfers": [
    {
      "amount": "25.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "Sepolia transfer",
      "to_user_id": "70",
      "token_symbol": "CPOP"
    },
    {
      "amount": "25.000000000000000000",
      "chain_id": 56,
      "from_user_id": "user_test_456",
      "memo": "BSC transfer",
      "to_user_id": "70",
      "token_symbol": "CPOP"
    }
  ]
}' | jq '.'
echo -e "\n\n"

echo "=== Testing Complete ==="