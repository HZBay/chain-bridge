#!/bin/bash

# 混合API测试脚本 - 随机调用多个接口
# 测试接口：/api/v1/assets/adjust, /api/v1/assets/nft/mint, /api/v1/assets/nft/burn, /api/v1/assets/nft/transfer, /api/v1/assets/transfer

BASE_URL="http://127.0.0.1:8080"
AUTH_TOKEN="b9c09785-aeb5-4fe5-b9c6-3dd26b25d7dc"
USER_ID="90"
CHAIN_ID="11155111"
COLLECTION_ID="cpop_official_nft_collection"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 计数器
SUCCESS_COUNT=0
ERROR_COUNT=0
TOTAL_TESTS=0

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
    ((SUCCESS_COUNT++))
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
    ((ERROR_COUNT++))
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# 测试函数
test_api() {
    local api_name="$1"
    local method="$2"
    local url="$3"
    local data="$4"
    local expected_status="$5"
    
    ((TOTAL_TESTS++))
    log_info "Testing $api_name..."
    
    if [ -n "$data" ]; then
        response=$(curl -s -w "\n%{http_code}" -X "$method" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer $AUTH_TOKEN" \
            -d "$data" \
            "$url")
    else
        response=$(curl -s -w "\n%{http_code}" -X "$method" \
            -H "Authorization: Bearer $AUTH_TOKEN" \
            "$url")
    fi
    
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')
    
    if [ "$http_code" = "$expected_status" ]; then
        log_success "$api_name - HTTP $http_code"
    else
        log_error "$api_name - Expected HTTP $expected_status, got $http_code"
        echo "Response: $body"
    fi
}

# 用户90实际拥有的NFT token ID列表
AVAILABLE_TOKEN_IDS=("316" "341" "342" "343" "344" "345" "346" "347" "348" "349" "350")

# 生成随机数据
generate_random_data() {
    # 生成标准UUID格式的operation_id
    local operation_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
    
    # 从实际拥有的NFT token ID中随机选择一个
    local token_id=${AVAILABLE_TOKEN_IDS[$((RANDOM % ${#AVAILABLE_TOKEN_IDS[@]}))]}
    local amount=$((RANDOM % 100 + 1))
    # 使用有账户的用户进行测试，主要是用户90
    local user_id="90"
    
    echo "$operation_id,$token_id,$amount,$user_id"
}

# 测试 /api/v1/assets/adjust
test_assets_adjust() {
    local data=$(generate_random_data)
    IFS=',' read -r operation_id token_id amount user_id <<< "$data"
    
    local json_data=$(cat <<EOF
{
    "operation_id": "$operation_id",
    "adjustments": [
        {
            "user_id": "$user_id",
            "chain_id": $CHAIN_ID,
            "token_symbol": "CPOP",
            "amount": "+$amount.000000000000000000",
            "business_type": "reward",
            "reason_type": "test_adjust",
            "reason_detail": "Mixed API test - adjust"
        }
    ]
}
EOF
)
    
    test_api "Assets Adjust" "POST" "$BASE_URL/api/v1/assets/adjust" "$json_data" "200"
}

# 测试 /api/v1/assets/nft/mint
test_nft_mint() {
    local data=$(generate_random_data)
    IFS=',' read -r operation_id token_id amount user_id <<< "$data"
    
    local json_data=$(cat <<EOF
{
    "operation_id": "$operation_id",
    "collection_id": "$COLLECTION_ID",
    "chain_id": $CHAIN_ID,
    "mint_operations": [
        {
            "to_user_id": "$user_id",
            "business_type": "reward",
            "reason_type": "test_mint",
            "reason_detail": "Mixed API test - mint"
        }
    ]
}
EOF
)
    
    test_api "NFT Mint" "POST" "$BASE_URL/api/v1/assets/nft/mint" "$json_data" "200"
}

# 测试 /api/v1/assets/nft/burn
test_nft_burn() {
    local data=$(generate_random_data)
    IFS=',' read -r operation_id token_id amount user_id <<< "$data"
    
    local json_data=$(cat <<EOF
{
    "operation_id": "$operation_id",
    "collection_id": "$COLLECTION_ID",
    "chain_id": $CHAIN_ID,
    "burn_operations": [
        {
            "owner_user_id": "$user_id",
            "token_id": "$token_id",
            "business_type": "consumption",
            "reason_type": "test_burn",
            "reason_detail": "Mixed API test - burn"
        }
    ]
}
EOF
)
    
    test_api "NFT Burn" "POST" "$BASE_URL/api/v1/assets/nft/burn" "$json_data" "200"
}

# 测试 /api/v1/assets/nft/transfer
test_nft_transfer() {
    local data=$(generate_random_data)
    IFS=',' read -r operation_id token_id amount user_id <<< "$data"
    
    # 使用相同的用户进行转账测试（自己转给自己）
    local to_user_id="$user_id"
    
    local json_data=$(cat <<EOF
{
    "operation_id": "$operation_id",
    "collection_id": "$COLLECTION_ID",
    "chain_id": $CHAIN_ID,
    "transfer_operations": [
        {
            "from_user_id": "$user_id",
            "to_user_id": "$to_user_id",
            "token_id": "$token_id",
            "business_type": "transfer",
            "reason_type": "test_transfer",
            "reason_detail": "Mixed API test - transfer"
        }
    ]
}
EOF
)
    
    test_api "NFT Transfer" "POST" "$BASE_URL/api/v1/assets/nft/transfer" "$json_data" "200"
}

# 测试 /api/v1/assets/transfer
test_assets_transfer() {
    local data=$(generate_random_data)
    IFS=',' read -r operation_id token_id amount user_id <<< "$data"
    
    # 使用相同的用户进行转账测试（自己转给自己）
    local to_user_id="$user_id"
    
    local json_data=$(cat <<EOF
{
    "operation_id": "$operation_id",
    "transfers": [
        {
            "from_user_id": "$user_id",
            "to_user_id": "$to_user_id",
            "chain_id": $CHAIN_ID,
            "token_symbol": "CPOP",
            "amount": "$amount.000000000000000000",
            "memo": "Mixed API test - assets transfer"
        }
    ]
}
EOF
)
    
    test_api "Assets Transfer" "POST" "$BASE_URL/api/v1/assets/transfer" "$json_data" "200"
}

# 测试 GET 接口
test_get_assets() {
    local user_id=$((RANDOM % 10 + 90))
    test_api "Get Assets" "GET" "$BASE_URL/api/v1/assets/$user_id" "" "200"
}

test_get_transactions() {
    local user_id=$((RANDOM % 10 + 90))
    local params=("" "?limit=5" "?tx_type=nft_mint" "?status=confirmed" "?page=1&limit=3")
    local param=${params[$RANDOM % ${#params[@]}]}
    test_api "Get Transactions" "GET" "$BASE_URL/api/v1/assets/$user_id/transactions$param" "" "200"
}

# 主测试循环
main() {
    log_info "开始混合API测试..."
    log_info "测试接口: /api/v1/assets/adjust, /api/v1/assets/nft/mint, /api/v1/assets/nft/burn, /api/v1/assets/nft/transfer, /api/v1/assets/transfer"
    log_info "随机测试 50 次..."
    echo
    
    # 定义测试函数数组
    tests=(
        "test_assets_adjust"
        "test_nft_mint" 
        "test_nft_burn"
        "test_nft_transfer"
        "test_assets_transfer"
        "test_get_assets"
        "test_get_transactions"
    )
    
    # 随机执行测试
    for i in {1..50}; do
        log_info "=== 第 $i 次测试 ==="
        
        # 随机选择一个测试
        test_func=${tests[$RANDOM % ${#tests[@]}]}
        
        # 执行测试
        $test_func
        
        # 随机延迟 0.1-0.5 秒
        sleep $(echo "scale=1; $RANDOM/32767*0.4+0.1" | bc)
        
        echo
    done
    
    # 输出测试结果
    echo "=========================================="
    log_info "测试完成!"
    log_success "成功: $SUCCESS_COUNT"
    log_error "失败: $ERROR_COUNT"
    log_info "总计: $TOTAL_TESTS"
    
    if [ $ERROR_COUNT -eq 0 ]; then
        log_success "所有测试通过! 🎉"
    else
        log_warning "有 $ERROR_COUNT 个测试失败"
    fi
}

# 检查依赖
check_dependencies() {
    if ! command -v curl &> /dev/null; then
        log_error "curl 未安装"
        exit 1
    fi
    
    if ! command -v uuidgen &> /dev/null; then
        log_error "uuidgen 未安装"
        exit 1
    fi
    
    if ! command -v bc &> /dev/null; then
        log_error "bc 未安装"
        exit 1
    fi
}

# 运行测试
check_dependencies
main
