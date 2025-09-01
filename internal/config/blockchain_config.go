package config

import (
	"fmt"

	"github.com/hzbay/chain-bridge/internal/util"
)

// BlockchainConfig contains unified blockchain configuration from environment variables
type BlockchainConfig struct {
	// 统一的部署私钥，所有链共用
	UnifiedDeploymentPrivateKey string `json:"-"` // sensitive field, excluded from JSON
	// 默认 Gas 相关配置
	DefaultGasPriceFactor float64
	DefaultGasLimit       uint64
}

// GetUnifiedDeploymentPrivateKey 获取统一的部署私钥（所有链共用）
func (bc BlockchainConfig) GetUnifiedDeploymentPrivateKey() (string, error) {
	if bc.UnifiedDeploymentPrivateKey == "" {
		return "", fmt.Errorf("unified deployment private key not configured")
	}
	return bc.UnifiedDeploymentPrivateKey, nil
}

// Deprecated: GetDeploymentPrivateKey 保留向后兼容，实际使用统一私钥
func (bc BlockchainConfig) GetDeploymentPrivateKey(_ int64) (string, error) {
	return bc.GetUnifiedDeploymentPrivateKey()
}

// LoadBlockchainConfig loads unified blockchain configuration from environment variables
func LoadBlockchainConfig() BlockchainConfig {
	return BlockchainConfig{
		UnifiedDeploymentPrivateKey: util.GetEnv("DEPLOYMENT_PRIVATE_KEY", "d8930e1e484f11002d262207542a5f994c96ca4788e2d47aaf9a6ccebffb2edd"),
		DefaultGasPriceFactor:       util.GetEnvAsFloat("BLOCKCHAIN_DEFAULT_GAS_PRICE_FACTOR", 1.2),
		DefaultGasLimit:             util.GetEnvAsUint64("BLOCKCHAIN_DEFAULT_GAS_LIMIT", 500000),
	}
}
