# CustodialBridge 通用合约调用API设计

## 设计理念

将 `batchMint` 等合约调用抽象为通用的合约调用API，提供灵活的智能合约交互能力，同时保持安全性和易用性。

## API架构设计

### 1. 通用合约调用API

#### 单个合约调用
```http
POST /api/v1/contract/call
```

**请求体结构**:
```json
{
  "executor_user_id": "user_123",
  "chain_id": 1,
  "contract_address": "0xdAC17F958D2ee523a2206206994597C13D831ec7",
  "method_name": "transfer",
  "method_signature": "transfer(address,uint256)",
  "parameters": [
    {
      "type": "address",
      "value": "0x1234567890123456789012345678901234567890"
    },
    {
      "type": "uint256", 
      "value": "1000000000000000000"
    }
  ],
  "value": "0",
  "gas_limit": 100000,
  "gas_mode": "sponsored",
  "priority": "normal",
  "memo": "Transfer USDT to user"
}
```

#### 批量合约调用
```http
POST /api/v1/contract/batch
```

**请求体结构**:
```json
{
  "executor_user_id": "admin_user",
  "chain_id": 1,
  "calls": [
    {
      "contract_address": "0x...",
      "method_name": "batchMint",
      "method_signature": "batchMint(address[],uint256[])",
      "parameters": [
        {
          "type": "address[]",
          "value": [
            "0xuser1...",
            "0xuser2...", 
            "0xuser3..."
          ]
        },
        {
          "type": "uint256[]",
          "value": [
            "1000000000000000000",
            "2000000000000000000",
            "1500000000000000000"
          ]
        }
      ],
      "value": "0",
      "gas_limit": 300000
    },
    {
      "contract_address": "0x...",
      "method_name": "updateExchangeRate", 
      "method_signature": "updateExchangeRate(uint256)",
      "parameters": [
        {
          "type": "uint256",
          "value": "5000000000000000"
        }
      ],
      "value": "0",
      "gas_limit": 50000
    }
  ],
  "gas_mode": "sponsored",
  "priority": "high",
  "memo": "Batch mint CPOP tokens and update exchange rate"
}
```

### 2. 预定义便利API

为常用操作提供简化的API接口：

#### CPOP代币操作
```http
POST /api/v1/contract/cpop/batch-mint
```

```json
{
  "executor_user_id": "admin_user",
  "chain_id": 1,
  "recipients": [
    {
      "user_id": "user_123",
      "amount": "1000.0"
    },
    {
      "user_id": "user_456", 
      "amount": "2000.0"
    }
  ],
  "reason": "Daily reward distribution"
}
```

```http
POST /api/v1/contract/cpop/batch-burn
```

```json
{
  "executor_user_id": "admin_user",
  "chain_id": 1,
  "accounts": [
    {
      "user_id": "user_123",
      "amount": "500.0"
    }
  ],
  "reason": "Gas fee deduction"
}
```

## Swagger API定义

### 通用合约调用定义

```yaml
# api/paths/contract.yml
contract_call:
  post:
    tags: ["contract"]
    summary: "Execute a smart contract method call"
    description: "Call any whitelisted smart contract method with type-safe parameters"
    operationId: "postContractCall"
    parameters:
      - name: "payload"
        in: "body"
        required: true
        schema:
          $ref: "#/definitions/ContractCallRequest"
    responses:
      200:
        description: "Contract call executed successfully"
        schema:
          $ref: "#/definitions/ContractCallResponse"
      400:
        description: "Invalid request or contract not whitelisted"
        schema:
          $ref: "#/definitions/ErrorResponse"
      403:
        description: "Insufficient permissions"
        schema:
          $ref: "#/definitions/ErrorResponse"

contract_batch:
  post:
    tags: ["contract"]
    summary: "Execute multiple smart contract calls in batch"
    description: "Execute multiple contract calls in a single transaction for gas optimization"
    operationId: "postContractBatch"
    parameters:
      - name: "payload"
        in: "body"
        required: true
        schema:
          $ref: "#/definitions/ContractBatchRequest"
    responses:
      200:
        description: "Batch contract calls executed successfully"
        schema:
          $ref: "#/definitions/ContractBatchResponse"

cpop_batch_mint:
  post:
    tags: ["contract", "cpop"]
    summary: "Batch mint CPOP tokens"
    description: "Mint CPOP tokens to multiple users in a single transaction"
    operationId: "postCPOPBatchMint"
    parameters:
      - name: "payload"
        in: "body"
        required: true
        schema:
          $ref: "#/definitions/CPOPBatchMintRequest"
    responses:
      200:
        description: "CPOP batch mint executed successfully"
        schema:
          $ref: "#/definitions/ContractCallResponse"
```

### 数据模型定义

```yaml
# api/definitions/contract.yml
ContractCallRequest:
  type: "object"
  required:
    - "executor_user_id"
    - "chain_id"
    - "contract_address"
    - "method_name"
    - "parameters"
  properties:
    executor_user_id:
      type: "string"
      format: "uuid"
      description: "User ID who executes the contract call"
      example: "admin_550e8400-e29b-41d4-a716-446655440000"
    chain_id:
      type: "integer"
      format: "int64"
      description: "Blockchain network ID"
      example: 1
    contract_address:
      type: "string"
      pattern: "^0x[a-fA-F0-9]{40}$"
      description: "Smart contract address"
      example: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    method_name:
      type: "string"
      description: "Contract method name"
      example: "transfer"
    method_signature:
      type: "string"
      description: "Full method signature for ABI encoding"
      example: "transfer(address,uint256)"
    parameters:
      type: "array"
      description: "Method parameters with types"
      items:
        $ref: "#/definitions/ContractParameter"
    value:
      type: "string"
      pattern: "^[0-9]+$"
      description: "ETH value to send with the call (in wei)"
      example: "0"
      default: "0"
    gas_limit:
      type: "integer"
      format: "int64"
      description: "Gas limit for the transaction"
      example: 100000
    gas_mode:
      type: "string"
      enum: ["sponsored", "self"]
      description: "Gas payment mode"
      default: "sponsored"
    priority:
      type: "string"
      enum: ["low", "normal", "high"]
      description: "Transaction priority"
      default: "normal"
    memo:
      type: "string"
      maxLength: 256
      description: "Optional memo for the contract call"

ContractParameter:
  type: "object"
  required:
    - "type"
    - "value"
  properties:
    type:
      type: "string"
      description: "Solidity type of the parameter"
      example: "uint256"
      enum: [
        "uint8", "uint16", "uint32", "uint64", "uint128", "uint256",
        "int8", "int16", "int32", "int64", "int128", "int256", 
        "address", "bool", "bytes", "bytes32", "string",
        "uint256[]", "address[]", "bytes32[]", "string[]"
      ]
    value:
      description: "Parameter value (type varies based on 'type')"
      example: "1000000000000000000"

ContractCallResponse:
  type: "object"
  required:
    - "transaction_id"
    - "status"
  properties:
    transaction_id:
      type: "string"
      format: "uuid"
      description: "Transaction identifier"
      example: "tx_550e8400-e29b-41d4-a716-446655440000"
    status:
      type: "string"
      enum: ["pending", "submitted", "confirmed", "failed"]
      description: "Transaction status"
      example: "pending"
    estimated_confirmation:
      type: "integer"
      description: "Estimated confirmation time in seconds"
      example: 45
    contract_address:
      type: "string"
      pattern: "^0x[a-fA-F0-9]{40}$"
      description: "Called contract address"
    method_name:
      type: "string"
      description: "Called method name"
    gas_fee_usd:
      type: "number"
      format: "float"
      description: "Estimated gas fee in USD"
      example: 5.25
    batch_info:
      $ref: "#/definitions/BatchInfo"

ContractBatchRequest:
  type: "object"
  required:
    - "executor_user_id"
    - "chain_id"
    - "calls"
  properties:
    executor_user_id:
      type: "string"
      format: "uuid"
    chain_id:
      type: "integer"
      format: "int64"
    calls:
      type: "array"
      minItems: 1
      maxItems: 50
      items:
        $ref: "#/definitions/ContractCall"
    gas_mode:
      type: "string"
      enum: ["sponsored", "self"]
      default: "sponsored"
    priority:
      type: "string"
      enum: ["low", "normal", "high"]
      default: "normal"
    memo:
      type: "string"
      maxLength: 256

ContractCall:
  type: "object"
  required:
    - "contract_address"
    - "method_name"
    - "parameters"
  properties:
    contract_address:
      type: "string"
      pattern: "^0x[a-fA-F0-9]{40}$"
    method_name:
      type: "string"
    method_signature:
      type: "string"
    parameters:
      type: "array"
      items:
        $ref: "#/definitions/ContractParameter"
    value:
      type: "string"
      pattern: "^[0-9]+$"
      default: "0"
    gas_limit:
      type: "integer"
      format: "int64"

ContractBatchResponse:
  type: "object"
  properties:
    batch_id:
      type: "string"
      format: "uuid"
    transaction_id:
      type: "string"
      format: "uuid"
    status:
      type: "string"
      enum: ["pending", "submitted", "confirmed", "failed"]
    calls_count:
      type: "integer"
      description: "Number of contract calls in the batch"
    total_gas_fee_usd:
      type: "number"
      format: "float"
    batch_info:
      $ref: "#/definitions/BatchInfo"

# CPOP便利API
CPOPBatchMintRequest:
  type: "object"
  required:
    - "executor_user_id"
    - "chain_id"
    - "recipients"
  properties:
    executor_user_id:
      type: "string"
      format: "uuid"
    chain_id:
      type: "integer"
      format: "int64"
    recipients:
      type: "array"
      minItems: 1
      maxItems: 100
      items:
        $ref: "#/definitions/CPOPRecipient"
    reason:
      type: "string"
      maxLength: 256
      description: "Reason for minting"

CPOPRecipient:
  type: "object"
  required:
    - "user_id"
    - "amount"
  properties:
    user_id:
      type: "string"
      format: "uuid"
      description: "Recipient user ID"
    amount:
      type: "string"
      pattern: "^[0-9]+\\.?[0-9]*$"
      description: "Amount to mint (in CPOP units)"
      example: "1000.0"

CPOPBatchBurnRequest:
  type: "object"
  required:
    - "executor_user_id"
    - "chain_id"
    - "accounts"
  properties:
    executor_user_id:
      type: "string"
      format: "uuid"
    chain_id:
      type: "integer"
      format: "int64"
    accounts:
      type: "array"
      minItems: 1
      maxItems: 100
      items:
        $ref: "#/definitions/CPOPAccount"
    reason:
      type: "string"
      maxLength: 256

CPOPAccount:
  type: "object"
  required:
    - "user_id"
    - "amount"
  properties:
    user_id:
      type: "string"
      format: "uuid"
    amount:
      type: "string"
      pattern: "^[0-9]+\\.?[0-9]*$"
```

## Go代码实现

### 1. 合约调用服务

```go
// internal/service/contract_service.go
package service

import (
    "context"
    "fmt"
    "math/big"
    
    "github.com/ethereum/go-ethereum/accounts/abi"
    "github.com/ethereum/go-ethereum/common"
    "github.com/your-org/custodial-bridge/internal/models"
)

type ContractService struct {
    whitelistManager *WhitelistManager
    abiManager       *ABIManager
    batchProcessor   *BatchProcessor
    masterManager    *MasterManager
    logger           *logger.Logger
}

type ContractCallRequest struct {
    ExecutorUserID   string                `json:"executor_user_id"`
    ChainID          int64                 `json:"chain_id"`
    ContractAddress  string                `json:"contract_address"`
    MethodName       string                `json:"method_name"`
    MethodSignature  string                `json:"method_signature"`
    Parameters       []ContractParameter   `json:"parameters"`
    Value            string                `json:"value"`
    GasLimit         uint64                `json:"gas_limit"`
    GasMode          string                `json:"gas_mode"`
    Priority         string                `json:"priority"`
    Memo             string                `json:"memo"`
}

type ContractParameter struct {
    Type  string      `json:"type"`
    Value interface{} `json:"value"`
}

type ContractCallResponse struct {
    TransactionID         string     `json:"transaction_id"`
    Status               string     `json:"status"`
    EstimatedConfirmation int        `json:"estimated_confirmation"`
    ContractAddress      string     `json:"contract_address"`
    MethodName           string     `json:"method_name"`
    GasFeeUSD           float64    `json:"gas_fee_usd"`
    BatchInfo           *BatchInfo `json:"batch_info"`
}

func NewContractService(whitelistManager *WhitelistManager, abiManager *ABIManager, 
    batchProcessor *BatchProcessor, masterManager *MasterManager, logger *logger.Logger) *ContractService {
    
    return &ContractService{
        whitelistManager: whitelistManager,
        abiManager:       abiManager,
        batchProcessor:   batchProcessor,
        masterManager:    masterManager,
        logger:           logger,
    }
}

func (cs *ContractService) ExecuteContractCall(ctx context.Context, req *ContractCallRequest) (*ContractCallResponse, error) {
    // 1. 验证合约调用权限
    if err := cs.validateContractCall(req); err != nil {
        return nil, fmt.Errorf("validation failed: %w", err)
    }
    
    // 2. 编码调用数据
    callData, err := cs.encodeCallData(req)
    if err != nil {
        return nil, fmt.Errorf("encoding failed: %w", err)
    }
    
    // 3. 估算Gas费用
    gasEstimate, err := cs.estimateGas(req, callData)
    if err != nil {
        return nil, fmt.Errorf("gas estimation failed: %w", err)
    }
    
    // 4. 创建交易记录
    txID := generateTransactionID()
    transaction := &models.Transaction{
        ID:              txID,
        UserID:          req.ExecutorUserID,
        ChainID:         req.ChainID,
        Type:           "contract_call",
        ContractAddress: req.ContractAddress,
        MethodName:      req.MethodName,
        CallData:        callData,
        Value:          req.Value,
        GasLimit:       req.GasLimit,
        Status:         "pending",
        Memo:           req.Memo,
    }
    
    if err := cs.createTransaction(transaction); err != nil {
        return nil, fmt.Errorf("transaction creation failed: %w", err)
    }
    
    // 5. 添加到批量处理队列
    operation := &BatchOperation{
        TxID:            txID,
        Type:           "contract_call",
        ChainID:        req.ChainID,
        ContractAddress: req.ContractAddress,
        CallData:       callData,
        Value:          req.Value,
        GasLimit:       req.GasLimit,
        Priority:       getPriority(req.Priority),
    }
    
    cs.batchProcessor.AddOperation(operation)
    
    // 6. 构造响应
    response := &ContractCallResponse{
        TransactionID:         txID,
        Status:               "pending",
        EstimatedConfirmation: cs.getEstimatedConfirmation(req.ChainID),
        ContractAddress:      req.ContractAddress,
        MethodName:           req.MethodName,
        GasFeeUSD:           gasEstimate.FeeUSD,
        BatchInfo: &BatchInfo{
            WillBeBatched:      true,
            EstimatedBatchTime: 15,
            CurrentBatchSize:   cs.batchProcessor.GetPendingCount(req.ChainID),
        },
    }
    
    cs.logger.Info("Contract call created successfully",
        "tx_id", txID,
        "contract", req.ContractAddress,
        "method", req.MethodName,
        "executor", req.ExecutorUserID)
    
    return response, nil
}

func (cs *ContractService) validateContractCall(req *ContractCallRequest) error {
    // 1. 检查合约是否在白名单中
    if !cs.whitelistManager.IsContractWhitelisted(req.ChainID, req.ContractAddress) {
        return fmt.Errorf("contract %s not whitelisted on chain %d", req.ContractAddress, req.ChainID)
    }
    
    // 2. 检查方法是否允许
    if !cs.whitelistManager.IsMethodAllowed(req.ChainID, req.ContractAddress, req.MethodName) {
        return fmt.Errorf("method %s not allowed for contract %s", req.MethodName, req.ContractAddress)
    }
    
    // 3. 检查执行者权限
    if !cs.hasExecutionPermission(req.ExecutorUserID, req.ContractAddress, req.MethodName) {
        return fmt.Errorf("user %s does not have permission to call %s", req.ExecutorUserID, req.MethodName)
    }
    
    // 4. 验证参数类型
    if err := cs.validateParameters(req.Parameters, req.MethodSignature); err != nil {
        return fmt.Errorf("parameter validation failed: %w", err)
    }
    
    return nil
}

func (cs *ContractService) encodeCallData(req *ContractCallRequest) ([]byte, error) {
    // 1. 获取合约ABI
    contractABI, err := cs.abiManager.GetContractABI(req.ChainID, req.ContractAddress)
    if err != nil {
        return nil, fmt.Errorf("ABI not found: %w", err)
    }
    
    // 2. 解析方法
    method, exists := contractABI.Methods[req.MethodName]
    if !exists {
        return nil, fmt.Errorf("method %s not found in ABI", req.MethodName)
    }
    
    // 3. 转换参数
    args, err := cs.convertParameters(req.Parameters, method.Inputs)
    if err != nil {
        return nil, fmt.Errorf("parameter conversion failed: %w", err)
    }
    
    // 4. 编码调用数据
    callData, err := contractABI.Pack(req.MethodName, args...)
    if err != nil {
        return nil, fmt.Errorf("ABI packing failed: %w", err)
    }
    
    return callData, nil
}

func (cs *ContractService) convertParameters(params []ContractParameter, inputs abi.Arguments) ([]interface{}, error) {
    if len(params) != len(inputs) {
        return nil, fmt.Errorf("parameter count mismatch: expected %d, got %d", len(inputs), len(params))
    }
    
    var args []interface{}
    for i, param := range params {
        expectedType := inputs[i].Type
        
        convertedValue, err := cs.convertParameterValue(param, expectedType)
        if err != nil {
            return nil, fmt.Errorf("parameter %d conversion failed: %w", i, err)
        }
        
        args = append(args, convertedValue)
    }
    
    return args, nil
}

func (cs *ContractService) convertParameterValue(param ContractParameter, expectedType abi.Type) (interface{}, error) {
    switch expectedType.T {
    case abi.UintTy:
        // 处理uint类型
        value, ok := param.Value.(string)
        if !ok {
            return nil, fmt.Errorf("expected string for uint type, got %T", param.Value)
        }
        
        bigInt := new(big.Int)
        bigInt.SetString(value, 10)
        return bigInt, nil
        
    case abi.AddressTy:
        // 处理address类型
        value, ok := param.Value.(string)
        if !ok {
            return nil, fmt.Errorf("expected string for address type, got %T", param.Value)
        }
        
        if !common.IsHexAddress(value) {
            return nil, fmt.Errorf("invalid address format: %s", value)
        }
        
        return common.HexToAddress(value), nil
        
    case abi.SliceTy:
        // 处理数组类型
        switch expectedType.Elem.T {
        case abi.AddressTy:
            // address[]类型
            values, ok := param.Value.([]interface{})
            if !ok {
                return nil, fmt.Errorf("expected array for address[] type, got %T", param.Value)
            }
            
            var addresses []common.Address
            for _, v := range values {
                addr, ok := v.(string)
                if !ok {
                    return nil, fmt.Errorf("expected string in address array, got %T", v)
                }
                addresses = append(addresses, common.HexToAddress(addr))
            }
            return addresses, nil
            
        case abi.UintTy:
            // uint256[]类型
            values, ok := param.Value.([]interface{})
            if !ok {
                return nil, fmt.Errorf("expected array for uint256[] type, got %T", param.Value)
            }
            
            var amounts []*big.Int
            for _, v := range values {
                amount, ok := v.(string)
                if !ok {
                    return nil, fmt.Errorf("expected string in uint256 array, got %T", v)
                }
                bigInt := new(big.Int)
                bigInt.SetString(amount, 10)
                amounts = append(amounts, bigInt)
            }
            return amounts, nil
        }
        
    case abi.BoolTy:
        value, ok := param.Value.(bool)
        if !ok {
            return nil, fmt.Errorf("expected bool for bool type, got %T", param.Value)
        }
        return value, nil
        
    case abi.StringTy:
        value, ok := param.Value.(string)
        if !ok {
            return nil, fmt.Errorf("expected string for string type, got %T", param.Value)
        }
        return value, nil
    }
    
    return nil, fmt.Errorf("unsupported parameter type: %s", expectedType.String())
}
```

### 2. 白名单管理器

```go
// internal/service/whitelist_manager.go
package service

type WhitelistManager struct {
    config *WhitelistConfig
    logger *logger.Logger
}

type WhitelistConfig struct {
    Contracts map[int64]map[string]ContractPermission `yaml:"contracts"`
}

type ContractPermission struct {
    Name         string   `yaml:"name"`
    AllowedMethods []string `yaml:"allowed_methods"`
    RequiredRoles  []string `yaml:"required_roles"`
    RiskLevel     string   `yaml:"risk_level"`
}

func NewWhitelistManager(config *WhitelistConfig, logger *logger.Logger) *WhitelistManager {
    return &WhitelistManager{
        config: config,
        logger: logger,
    }
}

func (wm *WhitelistManager) IsContractWhitelisted(chainID int64, contractAddress string) bool {
    chainContracts, exists := wm.config.Contracts[chainID]
    if !exists {
        return false
    }
    
    _, exists = chainContracts[contractAddress]
    return exists
}

func (wm *WhitelistManager) IsMethodAllowed(chainID int64, contractAddress, methodName string) bool {
    chainContracts, exists := wm.config.Contracts[chainID]
    if !exists {
        return false
    }
    
    contract, exists := chainContracts[contractAddress]
    if !exists {
        return false
    }
    
    for _, allowedMethod := range contract.AllowedMethods {
        if allowedMethod == methodName || allowedMethod == "*" {
            return true
        }
    }
    
    return false
}
```

### 3. 便利API实现

```go
// internal/service/cpop_service.go
package service

type CPOPService struct {
    contractService *ContractService
    userService     *UserService
    logger          *logger.Logger
}

type CPOPBatchMintRequest struct {
    ExecutorUserID string          `json:"executor_user_id"`
    ChainID        int64           `json:"chain_id"`
    Recipients     []CPOPRecipient `json:"recipients"`
    Reason         string          `json:"reason"`
}

type CPOPRecipient struct {
    UserID string `json:"user_id"`
    Amount string `json:"amount"`
}

func (cs *CPOPService) BatchMint(ctx context.Context, req *CPOPBatchMintRequest) (*ContractCallResponse, error) {
    // 1. 获取用户的AA钱包地址
    var addresses []string
    var amounts []string
    
    for _, recipient := range req.Recipients {
        account, err := cs.userService.GetUserAccount(recipient.UserID, req.ChainID)
        if err != nil {
            return nil, fmt.Errorf("failed to get account for user %s: %w", recipient.UserID, err)
        }
        
        addresses = append(addresses, account.AAAddress)
        
        // 转换为wei单位 (CPOP有18位小数)
        amountWei, err := convertToWei(recipient.Amount, 18)
        if err != nil {
            return nil, fmt.Errorf("invalid amount %s: %w", recipient.Amount, err)
        }
        amounts = append(amounts, amountWei.String())
    }
    
    // 2. 构建通用合约调用请求
    contractReq := &ContractCallRequest{
        ExecutorUserID:  req.ExecutorUserID,
        ChainID:        req.ChainID,
        ContractAddress: cs.getCPOPContractAddress(req.ChainID),
        MethodName:     "batchMint",
        MethodSignature: "batchMint(address[],uint256[])",
        Parameters: []ContractParameter{
            {
                Type:  "address[]",
                Value: addresses,
            },
            {
                Type:  "uint256[]", 
                Value: amounts,
            },
        },
        GasLimit: uint64(50000 + len(req.Recipients)*30000), // 基础gas + 每个recipient的gas
        GasMode:  "sponsored",
        Priority: "normal",
        Memo:     fmt.Sprintf("CPOP Batch Mint: %s", req.Reason),
    }
    
    // 3. 执行合约调用
    return cs.contractService.ExecuteContractCall(ctx, contractReq)
}
```

## 配置文件

### 合约白名单配置
```yaml
# config/contract_whitelist.yaml
contracts:
  # Ethereum Mainnet
  1:
    "0x...": # CPOP Token地址
      name: "CPOP Token"
      allowed_methods: ["batchMint", "batchBurn", "transfer", "balanceOf"]
      required_roles: ["admin", "minter"]
      risk_level: "high"
      
    "0x...": # GasPaymaster地址
      name: "Gas Paymaster"
      allowed_methods: ["deposit", "setDailyLimit", "estimateCost"]
      required_roles: ["admin"]
      risk_level: "medium"
      
    "0x...": # EntryPoint地址
      name: "EntryPoint"
      allowed_methods: ["handleOps", "handleAggregatedOps", "depositTo"]
      required_roles: ["system"]
      risk_level: "critical"
  
  # BSC
  56:
    "0x...": # BSC CPOP Token地址
      name: "BSC CPOP Token"
      allowed_methods: ["batchMint", "batchBurn", "transfer"]
      required_roles: ["admin", "minter"]
      risk_level: "high"
```

<function_calls>
<invoke name="TodoWrite">
<parameter name="todos">[{"id": "1", "content": "\u5206\u6790\u73b0\u6709CPOP\u8bbe\u8ba1\u548c\u901a\u7528\u94b1\u5305\u9700\u6c42", "status": "completed"}, {"id": "2", "content": "\u8bbe\u8ba1\u901a\u7528\u94b1\u5305relayer\u67b6\u6784\u65b9\u6848", "status": "completed"}, {"id": "3", "content": "\u5236\u5b9aRESTful API\u63a5\u53e3\u89c4\u8303", "status": "completed"}, {"id": "4", "content": "\u8bbe\u8ba1\u6570\u636e\u5e93\u6a21\u578b\u6269\u5c55", "status": "completed"}, {"id": "5", "content": "\u63d0\u51fa\u9879\u76ee\u4ee3\u53f7\u547d\u540d", "status": "completed"}, {"id": "6", "content": "\u8bbe\u8ba1\u901a\u7528\u5408\u7ea6\u8c03\u7528API", "status": "completed"}]