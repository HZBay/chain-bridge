# Transfer API Testing Commands
# Run these commands individually to test different scenarios

## Basic Setup
BASE_URL="http://127.0.0.1:8080"
AUTH_TOKEN="3a74f76d-2527-49f0-861b-98a87c78e457"

## Test 1: Your Original Single Transfer
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/transfer' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer 3a74f76d-2527-49f0-861b-98a87c78e457' \
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

## Test 2: Multiple Transfers in One Batch
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/transfer' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer 3a74f76d-2527-49f0-861b-98a87c78e457' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440001",
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

## Test 3: Idempotency Test (same operation_id as Test 1)
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/transfer' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer 3a74f76d-2527-49f0-861b-98a87c78e457' \
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

## Test 4: Invalid UUID Format for operation_id
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/transfer' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer 3a74f76d-2527-49f0-861b-98a87c78e457' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "invalid-uuid-format",
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

## Test 5: Invalid Amount Format
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/transfer' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer 3a74f76d-2527-49f0-861b-98a87c78e457' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440002",
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

## Test 6: Empty transfers array
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/transfer' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer 3a74f76d-2527-49f0-861b-98a87c78e457' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440003",
  "transfers": []
}' | jq '.'

## Test 7: Missing required fields
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/transfer' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer 3a74f76d-2527-49f0-861b-98a87c78e457' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440004",
  "transfers": [
    {
      "amount": "50.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456"
    }
  ]
}' | jq '.'

## Test 8: Cross-Chain Transfers
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/transfer' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer 3a74f76d-2527-49f0-861b-98a87c78e457' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440005",
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

## Test 9: Different User ID Formats
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/transfer' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer 3a74f76d-2527-49f0-861b-98a87c78e457' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440006",
  "transfers": [
    {
      "amount": "10.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "550e8400-e29b-41d4-a716-446655440000",
      "memo": "UUID user ID",
      "to_user_id": "simple_user_123",
      "token_symbol": "CPOP"
    },
    {
      "amount": "15.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "simple_user_123",
      "memo": "Simple user ID",
      "to_user_id": "70",
      "token_symbol": "CPOP"
    }
  ]
}' | jq '.'

## Test 10: Large Batch (5 transfers)
curl -X 'POST' \
  'http://127.0.0.1:8080/api/v1/assets/transfer' \
  -H 'accept: application/json' \
  -H 'Authorization: Bearer 3a74f76d-2527-49f0-861b-98a87c78e457' \
  -H 'Content-Type: application/json' \
  -d '{
  "operation_id": "550e8400-e29b-41d4-a716-446655440007",
  "transfers": [
    {
      "amount": "10.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "Transfer 1",
      "to_user_id": "70",
      "token_symbol": "CPOP"
    },
    {
      "amount": "20.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "Transfer 2",
      "to_user_id": "71",
      "token_symbol": "CPOP"
    },
    {
      "amount": "30.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "Transfer 3",
      "to_user_id": "72",
      "token_symbol": "CPOP"
    },
    {
      "amount": "40.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "Transfer 4",
      "to_user_id": "73",
      "token_symbol": "CPOP"
    },
    {
      "amount": "50.000000000000000000",
      "chain_id": 11155111,
      "from_user_id": "user_test_456",
      "memo": "Transfer 5",
      "to_user_id": "74",
      "token_symbol": "CPOP"
    }
  ]
}' | jq '.'