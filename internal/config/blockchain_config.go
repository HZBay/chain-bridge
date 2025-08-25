package config

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/hzbay/chain-bridge/internal/blockchain"
	"github.com/hzbay/chain-bridge/internal/util"
)

// BlockchainConfig contains configuration for all supported blockchain networks
type BlockchainConfig struct {
	Chains map[int64]ChainConfig
}

// ChainConfig contains configuration for a specific blockchain network
type ChainConfig struct {
	Name                 string  `envconfig:"NAME"`
	RPCEndpoint          string  `envconfig:"RPC_ENDPOINT"`
	WalletManagerAddress string  `envconfig:"WALLET_MANAGER_ADDRESS"`
	EntryPointAddress    string  `envconfig:"ENTRY_POINT_ADDRESS"`
	GasPriceFactor       float64 `envconfig:"GAS_PRICE_FACTOR"`
	DefaultGasLimit      uint64  `envconfig:"DEFAULT_GAS_LIMIT"`
	DeploymentPrivateKey string  `envconfig:"DEPLOYMENT_PRIVATE_KEY" json:"-"` // sensitive field, excluded from JSON
}

// ToCPOPConfig converts ChainConfig to blockchain.CPOPConfig
func (c ChainConfig) ToCPOPConfig() blockchain.CPOPConfig {
	return blockchain.CPOPConfig{
		RPCEndpoint:          c.RPCEndpoint,
		WalletManagerAddress: common.HexToAddress(c.WalletManagerAddress),
		EntryPointAddress:    common.HexToAddress(c.EntryPointAddress),
		GasPriceFactor:       c.GasPriceFactor,
		DefaultGasLimit:      c.DefaultGasLimit,
	}
}

// GetDeploymentPrivateKey retrieves the deployment private key for a specific chain
func (bc BlockchainConfig) GetDeploymentPrivateKey(chainID int64) (string, error) {
	chainConfig, exists := bc.Chains[chainID]
	if !exists {
		return "", fmt.Errorf("chain %d not configured", chainID)
	}

	if chainConfig.DeploymentPrivateKey == "" {
		return "", fmt.Errorf("deployment private key not configured for chain %d", chainID)
	}

	return chainConfig.DeploymentPrivateKey, nil
}

// LoadBlockchainConfig loads blockchain configuration from environment variables
func LoadBlockchainConfig() BlockchainConfig {
	return BlockchainConfig{
		Chains: map[int64]ChainConfig{
			56: { // BSC Mainnet
				Name:                 util.GetEnv("BLOCKCHAIN_CHAINS_56_NAME", "BSC"),
				RPCEndpoint:          util.GetEnv("BLOCKCHAIN_CHAINS_56_RPC_ENDPOINT", "https://bsc-dataseed.binance.org/"),
				WalletManagerAddress: util.GetEnv("BLOCKCHAIN_CHAINS_56_WALLET_MANAGER_ADDRESS", ""),
				EntryPointAddress:    util.GetEnv("BLOCKCHAIN_CHAINS_56_ENTRY_POINT_ADDRESS", ""),
				GasPriceFactor:       util.GetEnvAsFloat("BLOCKCHAIN_CHAINS_56_GAS_PRICE_FACTOR", 1.2),
				DefaultGasLimit:      util.GetEnvAsUint64("BLOCKCHAIN_CHAINS_56_DEFAULT_GAS_LIMIT", 500000),
				DeploymentPrivateKey: util.GetEnv("DEPLOYMENT_PRIVATE_KEY_CHAIN_56", ""),
			},
			1: { // Ethereum Mainnet
				Name:                 util.GetEnv("BLOCKCHAIN_CHAINS_1_NAME", "Ethereum"),
				RPCEndpoint:          util.GetEnv("BLOCKCHAIN_CHAINS_1_RPC_ENDPOINT", ""),
				WalletManagerAddress: util.GetEnv("BLOCKCHAIN_CHAINS_1_WALLET_MANAGER_ADDRESS", ""),
				EntryPointAddress:    util.GetEnv("BLOCKCHAIN_CHAINS_1_ENTRY_POINT_ADDRESS", ""),
				GasPriceFactor:       util.GetEnvAsFloat("BLOCKCHAIN_CHAINS_1_GAS_PRICE_FACTOR", 1.2),
				DefaultGasLimit:      util.GetEnvAsUint64("BLOCKCHAIN_CHAINS_1_DEFAULT_GAS_LIMIT", 500000),
				DeploymentPrivateKey: util.GetEnv("DEPLOYMENT_PRIVATE_KEY_CHAIN_1", ""),
			},
			97: { // BSC Testnet
				Name:                 util.GetEnv("BLOCKCHAIN_CHAINS_97_NAME", "BSC Testnet"),
				RPCEndpoint:          util.GetEnv("BLOCKCHAIN_CHAINS_97_RPC_ENDPOINT", "https://data-seed-prebsc-1-s1.binance.org:8545/"),
				WalletManagerAddress: util.GetEnv("BLOCKCHAIN_CHAINS_97_WALLET_MANAGER_ADDRESS", ""),
				EntryPointAddress:    util.GetEnv("BLOCKCHAIN_CHAINS_97_ENTRY_POINT_ADDRESS", ""),
				GasPriceFactor:       util.GetEnvAsFloat("BLOCKCHAIN_CHAINS_97_GAS_PRICE_FACTOR", 1.1),
				DefaultGasLimit:      util.GetEnvAsUint64("BLOCKCHAIN_CHAINS_97_DEFAULT_GAS_LIMIT", 500000),
				DeploymentPrivateKey: util.GetEnv("DEPLOYMENT_PRIVATE_KEY_CHAIN_97", ""),
			},
		},
	}
}

// GetValidatedChains returns only chains that have all required configuration
func (bc BlockchainConfig) GetValidatedChains() map[int64]ChainConfig {
	validChains := make(map[int64]ChainConfig)

	for chainID, chainConfig := range bc.Chains {
		// Only include chains that have required configuration
		if chainConfig.RPCEndpoint != "" &&
			chainConfig.WalletManagerAddress != "" &&
			chainConfig.EntryPointAddress != "" {
			validChains[chainID] = chainConfig
		}
	}

	return validChains
}
