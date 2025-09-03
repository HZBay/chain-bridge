# CustodialBridge 多Master配置设计

## 概述

CustodialBridge支持多Master私钥管理，提供灵活的签名策略和安全的密钥管理方案。支持不同环境、不同链的独立Master配置。

## 配置架构

### 1. 配置文件结构

```yaml
# config/masters.yaml
masters:
  # 全局配置
  global:
    key_management:
      provider: "local"              # local, hsm, kms, vault
      rotation_enabled: true
      rotation_interval: "720h"     # 30天
      backup_count: 3
    
    security:
      require_confirmation: true
      min_signers: 1
      max_concurrent_ops: 100
      rate_limit_per_minute: 60
  
  # 按环境分组
  environments:
    development:
      chains:
        ethereum:
          chain_id: 1
          masters:
            - id: "dev-eth-master-1"
              name: "Development Ethereum Master 1"
              address: "0x1234567890123456789012345678901234567890"
              key_source:
                type: "local"
                path: "keys/dev-eth-master-1.key"
              capabilities: ["deploy", "transfer", "admin"]
              is_active: true
              created_at: "2024-01-15T10:00:00Z"
            
            - id: "dev-eth-master-2" 
              name: "Development Ethereum Master 2"
              address: "0x2345678901234567890123456789012345678901"
              key_source:
                type: "env"
                key: "DEV_ETH_MASTER_2_PRIVATE_KEY"
              capabilities: ["transfer", "deploy"]
              is_active: true
              created_at: "2024-01-15T10:00:00Z"
        
        bsc:
          chain_id: 56
          masters:
            - id: "dev-bsc-master-1"
              name: "Development BSC Master 1"
              address: "0x3456789012345678901234567890123456789012"
              key_source:
                type: "local"
                path: "keys/dev-bsc-master-1.key"
              capabilities: ["deploy", "transfer", "admin"]
              is_active: true
    
    staging:
      chains:
        ethereum:
          chain_id: 1
          masters:
            - id: "staging-eth-master-1"
              name: "Staging Ethereum Master 1" 
              address: "0x4567890123456789012345678901234567890123"
              key_source:
                type: "kms"
                provider: "aws"
                key_id: "arn:aws:kms:us-east-1:123456789:key/12345678-1234-1234-1234-123456789012"
              capabilities: ["deploy", "transfer"]
              is_active: true
              
            - id: "staging-eth-master-2"
              name: "Staging Ethereum Master 2"
              address: "0x5678901234567890123456789012345678901234"
              key_source:
                type: "vault"
                url: "https://vault.example.com"
                path: "secret/custodial-bridge/staging/eth-master-2"
              capabilities: ["transfer", "emergency"]
              is_active: true
    
    production:
      chains:
        ethereum:
          chain_id: 1
          masters:
            - id: "prod-eth-master-1"
              name: "Production Ethereum Master 1"
              address: "0x6789012345678901234567890123456789012345"
              key_source:
                type: "hsm"
                provider: "cloudhsm"
                cluster_id: "cluster-1234567890abcdef0"
                key_handle: "0x0001"
              capabilities: ["deploy", "transfer", "admin"]
              is_active: true
              
            - id: "prod-eth-master-2"
              name: "Production Ethereum Master 2"
              address: "0x7890123456789012345678901234567890123456"
              key_source:
                type: "hsm"
                provider: "cloudhsm"
                cluster_id: "cluster-1234567890abcdef0"
                key_handle: "0x0002"
              capabilities: ["transfer", "emergency"]
              is_active: true
              
            - id: "prod-eth-master-3"
              name: "Production Ethereum Backup Master"
              address: "0x8901234567890123456789012345678901234567"
              key_source:
                type: "kms"
                provider: "aws"
                key_id: "arn:aws:kms:us-east-1:123456789:key/backup-key"
              capabilities: ["emergency"]
              is_active: false  # 仅紧急情况激活
              
        bsc:
          chain_id: 56
          masters:
            - id: "prod-bsc-master-1"
              name: "Production BSC Master 1"
              address: "0x9012345678901234567890123456789012345678"
              key_source:
                type: "hsm"
                provider: "cloudhsm"
                cluster_id: "cluster-1234567890abcdef0"
                key_handle: "0x0003"
              capabilities: ["deploy", "transfer", "admin"]
              is_active: true

# 签名策略配置
signing_strategies:
  development:
    default_strategy: "single_sign"
    strategies:
      single_sign:
        min_signatures: 1
        max_signatures: 1
        timeout: "30s"
        
  staging:
    default_strategy: "dual_sign"
    strategies:
      dual_sign:
        min_signatures: 1
        max_signatures: 2
        timeout: "60s"
        fallback_strategy: "single_sign"
        
  production:
    default_strategy: "multi_sign"
    strategies:
      multi_sign:
        min_signatures: 2
        max_signatures: 3
        timeout: "120s"
        emergency_strategy: "single_emergency"
      
      single_emergency:
        min_signatures: 1
        max_signatures: 1
        timeout: "30s"
        allowed_capabilities: ["emergency"]
        requires_approval: true

# 操作权限配置
capabilities:
  deploy:
    description: "Deploy new AA accounts"
    risk_level: "high"
    requires_confirmation: true
    
  transfer:
    description: "Process transfer operations"
    risk_level: "medium"
    requires_confirmation: false
    
  admin:
    description: "Administrative operations"
    risk_level: "high" 
    requires_confirmation: true
    
  emergency:
    description: "Emergency recovery operations"
    risk_level: "critical"
    requires_confirmation: true
    requires_approval: true
```

### 2. 环境变量配置

```bash
# .env.development
CUSTODIAL_BRIDGE_ENV=development
CUSTODIAL_BRIDGE_CONFIG_PATH=./config/masters.yaml

# 开发环境私钥 (仅用于开发，生产环境禁止使用)
DEV_ETH_MASTER_1_PRIVATE_KEY=0x1234567890abcdef...
DEV_ETH_MASTER_2_PRIVATE_KEY=0xabcdef1234567890...
DEV_BSC_MASTER_1_PRIVATE_KEY=0x567890abcdef1234...

# HSM配置
HSM_CLUSTER_ID=cluster-1234567890abcdef0
HSM_USER_NAME=custodial-bridge
HSM_PASSWORD_SECRET_ARN=arn:aws:secretsmanager:us-east-1:123456789:secret:hsm-password

# KMS配置
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY

# Vault配置
VAULT_ADDR=https://vault.example.com
VAULT_TOKEN=s.1234567890abcdef
VAULT_NAMESPACE=custodial-bridge
```

```bash
# .env.production
CUSTODIAL_BRIDGE_ENV=production
CUSTODIAL_BRIDGE_CONFIG_PATH=./config/masters.yaml

# 生产环境不应包含私钥，仅包含访问凭证
HSM_CLUSTER_ID=cluster-prod1234567890abcdef0
HSM_USER_NAME=custodial-bridge-prod
HSM_PASSWORD_SECRET_ARN=arn:aws:secretsmanager:us-east-1:123456789:secret:hsm-prod-password

AWS_REGION=us-east-1
# 生产环境使用IAM Role，不使用Access Key
```

## Go代码实现

### 1. 配置结构定义

```go
// internal/config/masters.go
package config

import (
    "time"
)

type MasterConfig struct {
    Global       GlobalConfig                 `yaml:"global"`
    Environments map[string]EnvironmentConfig `yaml:"environments"`
    Strategies   map[string]StrategyConfig    `yaml:"signing_strategies"`
    Capabilities map[string]CapabilityConfig  `yaml:"capabilities"`
}

type GlobalConfig struct {
    KeyManagement KeyManagementConfig `yaml:"key_management"`
    Security      SecurityConfig      `yaml:"security"`
}

type KeyManagementConfig struct {
    Provider         string        `yaml:"provider"`
    RotationEnabled  bool          `yaml:"rotation_enabled"`
    RotationInterval time.Duration `yaml:"rotation_interval"`
    BackupCount      int           `yaml:"backup_count"`
}

type SecurityConfig struct {
    RequireConfirmation  bool `yaml:"require_confirmation"`
    MinSigners          int  `yaml:"min_signers"`
    MaxConcurrentOps    int  `yaml:"max_concurrent_ops"`
    RateLimitPerMinute  int  `yaml:"rate_limit_per_minute"`
}

type EnvironmentConfig struct {
    Chains map[string]ChainConfig `yaml:"chains"`
}

type ChainConfig struct {
    ChainID int64          `yaml:"chain_id"`
    Masters []MasterConfig `yaml:"masters"`
}

type MasterInfo struct {
    ID           string          `yaml:"id"`
    Name         string          `yaml:"name"`
    Address      string          `yaml:"address"`
    KeySource    KeySourceConfig `yaml:"key_source"`
    Capabilities []string        `yaml:"capabilities"`
    IsActive     bool            `yaml:"is_active"`
    CreatedAt    time.Time       `yaml:"created_at"`
}

type KeySourceConfig struct {
    Type     string `yaml:"type"`     // local, env, kms, hsm, vault
    Path     string `yaml:"path,omitempty"`
    Key      string `yaml:"key,omitempty"`
    Provider string `yaml:"provider,omitempty"`
    KeyID    string `yaml:"key_id,omitempty"`
    URL      string `yaml:"url,omitempty"`
    Cluster  string `yaml:"cluster_id,omitempty"`
    Handle   string `yaml:"key_handle,omitempty"`
}

type StrategyConfig struct {
    DefaultStrategy string                    `yaml:"default_strategy"`
    Strategies      map[string]SigningStrategy `yaml:"strategies"`
}

type SigningStrategy struct {
    MinSignatures      int           `yaml:"min_signatures"`
    MaxSignatures      int           `yaml:"max_signatures"`
    Timeout           time.Duration `yaml:"timeout"`
    FallbackStrategy  string        `yaml:"fallback_strategy,omitempty"`
    AllowedCapabilities []string     `yaml:"allowed_capabilities,omitempty"`
    RequiresApproval  bool          `yaml:"requires_approval,omitempty"`
}

type CapabilityConfig struct {
    Description         string `yaml:"description"`
    RiskLevel          string `yaml:"risk_level"`
    RequiresConfirmation bool   `yaml:"requires_confirmation"`
    RequiresApproval    bool   `yaml:"requires_approval,omitempty"`
}
```

### 2. Master管理器实现

```go
// internal/service/master_manager.go
package service

import (
    "context"
    "crypto/ecdsa"
    "fmt"
    "sync"
    
    "github.com/ethereum/go-ethereum/crypto"
    "github.com/your-org/custodial-bridge/internal/config"
    "github.com/your-org/custodial-bridge/internal/crypto/providers"
)

type MasterManager struct {
    config      *config.MasterConfig
    environment string
    
    // 密钥提供者
    providers map[string]providers.KeyProvider
    
    // 缓存的Master信息
    masterCache map[string]*MasterInfo
    cacheMutex  sync.RWMutex
    
    logger *logger.Logger
}

type MasterInfo struct {
    ID           string
    Name         string
    Address      string
    Capabilities []string
    IsActive     bool
    KeyProvider  providers.KeyProvider
}

func NewMasterManager(config *config.MasterConfig, environment string, logger *logger.Logger) *MasterManager {
    mm := &MasterManager{
        config:      config,
        environment: environment,
        providers:   make(map[string]providers.KeyProvider),
        masterCache: make(map[string]*MasterInfo),
        logger:      logger,
    }
    
    // 初始化密钥提供者
    mm.initProviders()
    
    // 加载Master配置
    mm.loadMasters()
    
    return mm
}

func (mm *MasterManager) initProviders() {
    // 本地密钥提供者
    mm.providers["local"] = providers.NewLocalKeyProvider()
    
    // 环境变量提供者
    mm.providers["env"] = providers.NewEnvKeyProvider()
    
    // AWS KMS提供者
    if kmsProvider, err := providers.NewKMSProvider(); err == nil {
        mm.providers["kms"] = kmsProvider
    }
    
    // HSM提供者
    if hsmProvider, err := providers.NewHSMProvider(); err == nil {
        mm.providers["hsm"] = hsmProvider
    }
    
    // Vault提供者
    if vaultProvider, err := providers.NewVaultProvider(); err == nil {
        mm.providers["vault"] = vaultProvider
    }
}

func (mm *MasterManager) loadMasters() error {
    envConfig, exists := mm.config.Environments[mm.environment]
    if !exists {
        return fmt.Errorf("environment %s not found in config", mm.environment)
    }
    
    for chainName, chainConfig := range envConfig.Chains {
        for _, masterConfig := range chainConfig.Masters {
            if !masterConfig.IsActive {
                continue
            }
            
            // 获取密钥提供者
            provider, exists := mm.providers[masterConfig.KeySource.Type]
            if !exists {
                mm.logger.Error("Key provider not found", 
                    "type", masterConfig.KeySource.Type,
                    "master_id", masterConfig.ID)
                continue
            }
            
            // 验证Master地址
            if err := mm.validateMaster(masterConfig, provider); err != nil {
                mm.logger.Error("Master validation failed",
                    "master_id", masterConfig.ID,
                    "error", err)
                continue
            }
            
            masterInfo := &MasterInfo{
                ID:           masterConfig.ID,
                Name:         masterConfig.Name,
                Address:      masterConfig.Address,
                Capabilities: masterConfig.Capabilities,
                IsActive:     masterConfig.IsActive,
                KeyProvider:  provider,
            }
            
            mm.cacheMutex.Lock()
            mm.masterCache[masterConfig.ID] = masterInfo
            mm.cacheMutex.Unlock()
            
            mm.logger.Info("Master loaded successfully",
                "master_id", masterConfig.ID,
                "chain", chainName,
                "address", masterConfig.Address)
        }
    }
    
    return nil
}

func (mm *MasterManager) validateMaster(config config.MasterInfo, provider providers.KeyProvider) error {
    // 获取公钥
    publicKey, err := provider.GetPublicKey(config.KeySource)
    if err != nil {
        return fmt.Errorf("failed to get public key: %w", err)
    }
    
    // 验证地址匹配
    expectedAddress := crypto.PubkeyToAddress(*publicKey).Hex()
    if expectedAddress != config.Address {
        return fmt.Errorf("address mismatch: expected %s, got %s", 
            config.Address, expectedAddress)
    }
    
    return nil
}

// 获取指定能力的Master
func (mm *MasterManager) GetMastersByCapability(chainID int64, capability string) ([]*MasterInfo, error) {
    mm.cacheMutex.RLock()
    defer mm.cacheMutex.RUnlock()
    
    var masters []*MasterInfo
    for _, master := range mm.masterCache {
        if !master.IsActive {
            continue
        }
        
        // 检查是否有指定能力
        if mm.hasCapability(master, capability) {
            masters = append(masters, master)
        }
    }
    
    if len(masters) == 0 {
        return nil, fmt.Errorf("no active masters found with capability %s for chain %d", 
            capability, chainID)
    }
    
    return masters, nil
}

func (mm *MasterManager) hasCapability(master *MasterInfo, capability string) bool {
    for _, cap := range master.Capabilities {
        if cap == capability {
            return true
        }
    }
    return false
}

// 使用Master签名
func (mm *MasterManager) SignWithMaster(ctx context.Context, masterID string, hash []byte) ([]byte, error) {
    mm.cacheMutex.RLock()
    master, exists := mm.masterCache[masterID]
    mm.cacheMutex.RUnlock()
    
    if !exists {
        return nil, fmt.Errorf("master %s not found", masterID)
    }
    
    if !master.IsActive {
        return nil, fmt.Errorf("master %s is not active", masterID)
    }
    
    // 使用密钥提供者签名
    signature, err := master.KeyProvider.Sign(hash)
    if err != nil {
        mm.logger.Error("Master signing failed",
            "master_id", masterID,
            "error", err)
        return nil, fmt.Errorf("signing failed: %w", err)
    }
    
    mm.logger.Info("Master signing successful",
        "master_id", masterID,
        "signature_length", len(signature))
    
    return signature, nil
}

// 轮换Master密钥
func (mm *MasterManager) RotateMasterKey(ctx context.Context, masterID string) error {
    mm.cacheMutex.Lock()
    defer mm.cacheMutex.Unlock()
    
    master, exists := mm.masterCache[masterID]
    if !exists {
        return fmt.Errorf("master %s not found", masterID)
    }
    
    // 检查是否支持密钥轮换
    if rotatable, ok := master.KeyProvider.(providers.RotatableKeyProvider); ok {
        if err := rotatable.RotateKey(master.Address); err != nil {
            return fmt.Errorf("key rotation failed: %w", err)
        }
        
        mm.logger.Info("Master key rotated successfully", "master_id", masterID)
        return nil
    }
    
    return fmt.Errorf("key provider does not support rotation")
}
```

### 3. 密钥提供者接口

```go
// internal/crypto/providers/interface.go
package providers

import (
    "crypto/ecdsa"
    "github.com/your-org/custodial-bridge/internal/config"
)

type KeyProvider interface {
    GetPublicKey(source config.KeySourceConfig) (*ecdsa.PublicKey, error)
    Sign(hash []byte) ([]byte, error)
    IsAvailable() bool
}

type RotatableKeyProvider interface {
    KeyProvider
    RotateKey(address string) error
    BackupKey(address string) error
}

// 本地文件密钥提供者
type LocalKeyProvider struct{}

func NewLocalKeyProvider() *LocalKeyProvider {
    return &LocalKeyProvider{}
}

func (p *LocalKeyProvider) GetPublicKey(source config.KeySourceConfig) (*ecdsa.PublicKey, error) {
    // 从本地文件读取私钥
    privateKey, err := crypto.LoadECDSA(source.Path)
    if err != nil {
        return nil, err
    }
    
    return &privateKey.PublicKey, nil
}

func (p *LocalKeyProvider) Sign(hash []byte) ([]byte, error) {
    // 实现本地签名逻辑
    return nil, nil
}

func (p *LocalKeyProvider) IsAvailable() bool {
    return true
}

// 环境变量密钥提供者
type EnvKeyProvider struct{}

func NewEnvKeyProvider() *EnvKeyProvider {
    return &EnvKeyProvider{}
}

func (p *EnvKeyProvider) GetPublicKey(source config.KeySourceConfig) (*ecdsa.PublicKey, error) {
    // 从环境变量读取私钥
    privateKeyHex := os.Getenv(source.Key)
    if privateKeyHex == "" {
        return nil, fmt.Errorf("environment variable %s not found", source.Key)
    }
    
    privateKey, err := crypto.HexToECDSA(privateKeyHex)
    if err != nil {
        return nil, err
    }
    
    return &privateKey.PublicKey, nil
}

// AWS KMS密钥提供者
type KMSProvider struct {
    client *kms.KMS
}

func NewKMSProvider() (*KMSProvider, error) {
    sess, err := session.NewSession()
    if err != nil {
        return nil, err
    }
    
    return &KMSProvider{
        client: kms.New(sess),
    }, nil
}

func (p *KMSProvider) GetPublicKey(source config.KeySourceConfig) (*ecdsa.PublicKey, error) {
    // 从AWS KMS获取公钥
    input := &kms.GetPublicKeyInput{
        KeyId: &source.KeyID,
    }
    
    result, err := p.client.GetPublicKey(input)
    if err != nil {
        return nil, err
    }
    
    // 解析公钥
    return parsePublicKey(result.PublicKey)
}

func (p *KMSProvider) Sign(hash []byte) ([]byte, error) {
    // 实现KMS签名逻辑
    return nil, nil
}

// HSM密钥提供者
type HSMProvider struct {
    // HSM客户端实现
}

// Vault密钥提供者  
type VaultProvider struct {
    // Vault客户端实现
}
```

### 4. 签名策略管理器

```go
// internal/service/signing_strategy.go
package service

type SigningStrategyManager struct {
    config      *config.MasterConfig
    environment string
    
    masterManager *MasterManager
    logger        *logger.Logger
}

type SigningRequest struct {
    ChainID     int64
    Capability  string
    Hash        []byte
    Operation   string
    UserID      string
    Amount      string
    Priority    string
}

type SigningResult struct {
    Signatures    [][]byte
    SignerIDs     []string
    Strategy      string
    ExecutionTime time.Duration
}

func NewSigningStrategyManager(config *config.MasterConfig, environment string, 
    masterManager *MasterManager, logger *logger.Logger) *SigningStrategyManager {
    
    return &SigningStrategyManager{
        config:        config,
        environment:   environment,
        masterManager: masterManager,
        logger:        logger,
    }
}

func (ssm *SigningStrategyManager) ExecuteSigningStrategy(ctx context.Context, req *SigningRequest) (*SigningResult, error) {
    startTime := time.Now()
    
    // 获取签名策略
    strategy, err := ssm.getSigningStrategy(req.Capability)
    if err != nil {
        return nil, err
    }
    
    // 获取可用的Master
    masters, err := ssm.masterManager.GetMastersByCapability(req.ChainID, req.Capability)
    if err != nil {
        return nil, err
    }
    
    // 检查是否需要确认
    if ssm.requiresConfirmation(req.Capability) {
        if err := ssm.requestConfirmation(ctx, req); err != nil {
            return nil, fmt.Errorf("confirmation failed: %w", err)
        }
    }
    
    // 执行签名
    signatures, signerIDs, err := ssm.executeSignatures(ctx, masters, strategy, req.Hash)
    if err != nil {
        return nil, err
    }
    
    result := &SigningResult{
        Signatures:    signatures,
        SignerIDs:     signerIDs,
        Strategy:      strategy.Name,
        ExecutionTime: time.Since(startTime),
    }
    
    ssm.logger.Info("Signing strategy executed successfully",
        "strategy", strategy.Name,
        "signers_count", len(signerIDs),
        "execution_time", result.ExecutionTime)
    
    return result, nil
}

func (ssm *SigningStrategyManager) getSigningStrategy(capability string) (*config.SigningStrategy, error) {
    envStrategy, exists := ssm.config.Strategies[ssm.environment]
    if !exists {
        return nil, fmt.Errorf("signing strategy not found for environment %s", ssm.environment)
    }
    
    strategyName := envStrategy.DefaultStrategy
    
    // 检查能力是否需要特殊策略
    for name, strategy := range envStrategy.Strategies {
        for _, allowedCap := range strategy.AllowedCapabilities {
            if allowedCap == capability {
                strategyName = name
                break
            }
        }
    }
    
    strategy, exists := envStrategy.Strategies[strategyName]
    if !exists {
        return nil, fmt.Errorf("strategy %s not found", strategyName)
    }
    
    return &strategy, nil
}

func (ssm *SigningStrategyManager) executeSignatures(ctx context.Context, masters []*MasterInfo, 
    strategy *config.SigningStrategy, hash []byte) ([][]byte, []string, error) {
    
    var signatures [][]byte
    var signerIDs []string
    
    // 创建带超时的上下文
    ctx, cancel := context.WithTimeout(ctx, strategy.Timeout)
    defer cancel()
    
    // 并发签名
    type signResult struct {
        signature []byte
        signerID  string
        err       error
    }
    
    signChan := make(chan signResult, len(masters))
    
    // 启动并发签名
    for i, master := range masters {
        if i >= strategy.MaxSignatures {
            break
        }
        
        go func(master *MasterInfo) {
            signature, err := ssm.masterManager.SignWithMaster(ctx, master.ID, hash)
            signChan <- signResult{
                signature: signature,
                signerID:  master.ID,
                err:       err,
            }
        }(master)
    }
    
    // 收集签名结果
    successCount := 0
    for i := 0; i < len(masters) && i < strategy.MaxSignatures; i++ {
        select {
        case result := <-signChan:
            if result.err != nil {
                ssm.logger.Error("Signing failed", 
                    "signer_id", result.signerID, 
                    "error", result.err)
                continue
            }
            
            signatures = append(signatures, result.signature)
            signerIDs = append(signerIDs, result.signerID)
            successCount++
            
            // 检查是否达到最小签名数
            if successCount >= strategy.MinSignatures {
                break
            }
            
        case <-ctx.Done():
            return nil, nil, fmt.Errorf("signing timeout after %v", strategy.Timeout)
        }
    }
    
    if successCount < strategy.MinSignatures {
        return nil, nil, fmt.Errorf("insufficient signatures: got %d, required %d", 
            successCount, strategy.MinSignatures)
    }
    
    return signatures, signerIDs, nil
}
```

### 5. 使用示例

```go
// internal/api/handlers/transfer_handler.go (使用Master签名的示例)

func (h *TransferHandler) PostTransfer(params transfer.PostTransferParams) middleware.Responder {
    // ... 前置验证 ...
    
    // 构建UserOperation
    userOp, err := h.buildUserOperation(transferReq)
    if err != nil {
        return transfer.NewPostTransferBadRequest().WithPayload(&models.ErrorResponse{
            Error: "Failed to build user operation",
        })
    }
    
    // 计算操作哈希
    opHash := h.calculateUserOpHash(userOp)
    
    // 创建签名请求
    signingReq := &service.SigningRequest{
        ChainID:    transferReq.ChainID,
        Capability: "transfer",
        Hash:       opHash,
        Operation:  "transfer",
        UserID:     transferReq.FromUserID,
        Amount:     transferReq.Amount,
        Priority:   transferReq.Priority,
    }
    
    // 执行签名策略
    signingResult, err := h.signingManager.ExecuteSigningStrategy(ctx, signingReq)
    if err != nil {
        h.logger.Error("Signing failed", "error", err)
        return transfer.NewPostTransferInternalServerError().WithPayload(&models.ErrorResponse{
            Error: "Signing failed",
        })
    }
    
    // 使用签名结果
    userOp.Signature = signingResult.Signatures[0] // 或者聚合多个签名
    
    // 添加到批量处理队列
    h.batchProcessor.AddOperation(operation)
    
    // ... 返回响应 ...
}
```

## 安全注意事项

### 1. 密钥安全
- **生产环境禁止使用本地文件存储私钥**
- **使用HSM或KMS等硬件安全模块**
- **定期轮换私钥**
- **实施最小权限原则**

### 2. 访问控制
- **多重身份验证**
- **操作审计日志**
- **紧急暂停机制**
- **分离职责**

### 3. 监控告警
- **异常签名活动监控**
- **密钥使用频率监控**
- **失败签名告警**
- **未授权访问告警**

这套多Master配置方案提供了从开发到生产的完整密钥管理解决方案，支持多种密钥存储方式和灵活的签名策略。