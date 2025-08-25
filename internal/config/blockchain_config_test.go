package config

import (
	"os"
	"testing"
)

func TestLoadBlockchainConfig(t *testing.T) {
	// Set test environment variables
	os.Setenv("BLOCKCHAIN_CHAINS_97_NAME", "BSC Testnet")
	os.Setenv("BLOCKCHAIN_CHAINS_97_RPC_ENDPOINT", "https://data-seed-prebsc-1-s1.binance.org:8545/")
	os.Setenv("BLOCKCHAIN_CHAINS_97_WALLET_MANAGER_ADDRESS", "0x1234567890123456789012345678901234567890")
	os.Setenv("BLOCKCHAIN_CHAINS_97_ENTRY_POINT_ADDRESS", "0x0987654321098765432109876543210987654321")
	os.Setenv("DEPLOYMENT_PRIVATE_KEY_CHAIN_97", "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	defer func() {
		// Clean up environment variables
		os.Unsetenv("BLOCKCHAIN_CHAINS_97_NAME")
		os.Unsetenv("BLOCKCHAIN_CHAINS_97_RPC_ENDPOINT")
		os.Unsetenv("BLOCKCHAIN_CHAINS_97_WALLET_MANAGER_ADDRESS")
		os.Unsetenv("BLOCKCHAIN_CHAINS_97_ENTRY_POINT_ADDRESS")
		os.Unsetenv("DEPLOYMENT_PRIVATE_KEY_CHAIN_97")
	}()

	config := LoadBlockchainConfig()

	// Verify chain 97 is loaded
	chain97, exists := config.Chains[97]
	if !exists {
		t.Error("Chain 97 not found in loaded config")
		return
	}

	// Verify configuration values
	if chain97.Name != "BSC Testnet" {
		t.Errorf("Expected name 'BSC Testnet', got %s", chain97.Name)
	}

	if chain97.RPCEndpoint != "https://data-seed-prebsc-1-s1.binance.org:8545/" {
		t.Errorf("Expected RPC endpoint 'https://data-seed-prebsc-1-s1.binance.org:8545/', got %s", chain97.RPCEndpoint)
	}

	// Test private key retrieval
	privateKey, err := config.GetDeploymentPrivateKey(97)
	if err != nil {
		t.Errorf("Failed to get deployment private key: %v", err)
	}

	expectedKey := "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	if privateKey != expectedKey {
		t.Errorf("Expected private key %s, got %s", expectedKey, privateKey)
	}
}

func TestGetValidatedChains(t *testing.T) {
	// Set up test config with complete and incomplete chains
	config := BlockchainConfig{
		Chains: map[int64]ChainConfig{
			97: { // Complete configuration
				Name:                 "BSC Testnet",
				RPCEndpoint:          "https://data-seed-prebsc-1-s1.binance.org:8545/",
				WalletManagerAddress: "0x1234567890123456789012345678901234567890",
				EntryPointAddress:    "0x0987654321098765432109876543210987654321",
				DeploymentPrivateKey: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
			1: { // Incomplete configuration (missing addresses)
				Name:        "Ethereum",
				RPCEndpoint: "https://mainnet.infura.io/v3/PROJECT_ID",
			},
		},
	}

	validChains := config.GetValidatedChains()

	// Should only include chain 97
	if len(validChains) != 1 {
		t.Errorf("Expected 1 valid chain, got %d", len(validChains))
	}

	if _, exists := validChains[97]; !exists {
		t.Error("Chain 97 should be valid but was not included")
	}

	if _, exists := validChains[1]; exists {
		t.Error("Chain 1 should be invalid but was included")
	}
}

func TestGetDeploymentPrivateKeyError(t *testing.T) {
	config := BlockchainConfig{
		Chains: map[int64]ChainConfig{
			97: { // No private key configured
				Name:        "BSC Testnet",
				RPCEndpoint: "https://data-seed-prebsc-1-s1.binance.org:8545/",
			},
		},
	}

	// Test missing chain
	_, err := config.GetDeploymentPrivateKey(999)
	if err == nil {
		t.Error("Expected error for non-existent chain, got nil")
	}

	// Test missing private key
	_, err = config.GetDeploymentPrivateKey(97)
	if err == nil {
		t.Error("Expected error for missing private key, got nil")
	}
}
