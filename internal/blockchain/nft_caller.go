package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog/log"

	cpop "github.com/HzBay/account-abstraction/cpop-abis"
)

// NFTBatchCaller handles CP_NFT ERC721 token batch operations
type NFTBatchCaller struct {
	client      *ethclient.Client
	nftContract *cpop.CPNFT
	auth        *bind.TransactOpts
}

// NFTBatchResult represents the result of an NFT batch operation
type NFTBatchResult struct {
	TxHash      string   `json:"tx_hash"`
	BlockNumber *big.Int `json:"block_number,omitempty"`
	GasUsed     uint64   `json:"gas_used"`
	GasSaved    uint64   `json:"gas_saved"`
	Efficiency  float64  `json:"efficiency_percent"`
	Status      string   `json:"status"` // "submitted", "confirmed", "failed"
	TokenIDs    []string `json:"token_ids,omitempty"`
}

// NewNFTBatchCaller creates a new NFT batch caller
func NewNFTBatchCaller(rpcURL string, nftAddr common.Address, auth *bind.TransactOpts) (*NFTBatchCaller, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ethereum client: %w", err)
	}

	nftContract, err := cpop.NewCPNFT(nftAddr, client)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate CP_NFT contract: %w", err)
	}

	return &NFTBatchCaller{
		client:      client,
		nftContract: nftContract,
		auth:        auth,
	}, nil
}

// BatchMint executes a batch mint operation for NFTs
func (n *NFTBatchCaller) BatchMint(ctx context.Context, recipients []common.Address, tokenIds []*big.Int, metadataURIs []string) (*NFTBatchResult, error) {
	if len(recipients) != len(tokenIds) || len(recipients) != len(metadataURIs) {
		return nil, fmt.Errorf("recipients, tokenIds, and metadataURIs arrays must have the same length")
	}

	log.Info().
		Int("batch_size", len(recipients)).
		Str("contract_address", n.nftContract.Address().Hex()).
		Msg("Executing NFT batch mint")

	// Estimate gas cost for individual operations vs batch
	individualGas := uint64(len(recipients)) * EstimatedNFTMintGas

	// Execute batch mint transaction
	tx, err := n.nftContract.BatchMint(n.auth, recipients, tokenIds, metadataURIs)
	if err != nil {
		return nil, fmt.Errorf("batch NFT mint failed: %w", err)
	}

	log.Info().
		Str("tx_hash", tx.Hash().Hex()).
		Uint64("gas_limit", tx.Gas()).
		Msg("NFT batch mint transaction submitted")

	// Wait for confirmation in a separate goroutine or return immediately based on configuration
	receipt, err := n.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		// Return partial result if confirmation fails
		return &NFTBatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	// Calculate efficiency metrics
	actualGas := receipt.GasUsed
	gasSaved := uint64(0)
	efficiency := float64(0)

	if individualGas > actualGas {
		gasSaved = individualGas - actualGas
		efficiency = (float64(gasSaved) / float64(individualGas)) * 100
	}

	// Convert token IDs to strings
	tokenIDStrings := make([]string, len(tokenIds))
	for i, tokenID := range tokenIds {
		tokenIDStrings[i] = tokenID.String()
	}

	return &NFTBatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     actualGas,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
		TokenIDs:    tokenIDStrings,
	}, nil
}

// BatchBurn executes a batch burn operation for NFTs
func (n *NFTBatchCaller) BatchBurn(ctx context.Context, tokenIds []*big.Int) (*NFTBatchResult, error) {
	log.Info().
		Int("batch_size", len(tokenIds)).
		Str("contract_address", n.nftContract.Address().Hex()).
		Msg("Executing NFT batch burn")

	// Estimate gas cost for individual operations vs batch
	individualGas := uint64(len(tokenIds)) * EstimatedNFTBurnGas

	// Execute batch burn transaction
	tx, err := n.nftContract.BatchBurn(n.auth, tokenIds)
	if err != nil {
		return nil, fmt.Errorf("batch NFT burn failed: %w", err)
	}

	log.Info().
		Str("tx_hash", tx.Hash().Hex()).
		Uint64("gas_limit", tx.Gas()).
		Msg("NFT batch burn transaction submitted")

	// Wait for confirmation
	receipt, err := n.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		return &NFTBatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	// Calculate efficiency metrics
	actualGas := receipt.GasUsed
	gasSaved := uint64(0)
	efficiency := float64(0)

	if individualGas > actualGas {
		gasSaved = individualGas - actualGas
		efficiency = (float64(gasSaved) / float64(individualGas)) * 100
	}

	// Convert token IDs to strings
	tokenIDStrings := make([]string, len(tokenIds))
	for i, tokenID := range tokenIds {
		tokenIDStrings[i] = tokenID.String()
	}

	return &NFTBatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     actualGas,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
		TokenIDs:    tokenIDStrings,
	}, nil
}

// BatchTransferFrom executes a batch transfer operation for NFTs
func (n *NFTBatchCaller) BatchTransferFrom(ctx context.Context, fromAddresses, toAddresses []common.Address, tokenIds []*big.Int) (*NFTBatchResult, error) {
	if len(fromAddresses) != len(toAddresses) || len(fromAddresses) != len(tokenIds) {
		return nil, fmt.Errorf("fromAddresses, toAddresses, and tokenIds arrays must have the same length")
	}

	log.Info().
		Int("batch_size", len(tokenIds)).
		Str("contract_address", n.nftContract.Address().Hex()).
		Msg("Executing NFT batch transfer")

	// Estimate gas cost for individual operations vs batch
	individualGas := uint64(len(tokenIds)) * EstimatedNFTTransferGas

	// Execute batch transfer transaction
	tx, err := n.nftContract.BatchTransferFrom(n.auth, fromAddresses, toAddresses, tokenIds)
	if err != nil {
		return nil, fmt.Errorf("batch NFT transfer failed: %w", err)
	}

	log.Info().
		Str("tx_hash", tx.Hash().Hex()).
		Uint64("gas_limit", tx.Gas()).
		Msg("NFT batch transfer transaction submitted")

	// Wait for confirmation
	receipt, err := n.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		return &NFTBatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	// Calculate efficiency metrics
	actualGas := receipt.GasUsed
	gasSaved := uint64(0)
	efficiency := float64(0)

	if individualGas > actualGas {
		gasSaved = individualGas - actualGas
		efficiency = (float64(gasSaved) / float64(individualGas)) * 100
	}

	// Convert token IDs to strings
	tokenIDStrings := make([]string, len(tokenIds))
	for i, tokenID := range tokenIds {
		tokenIDStrings[i] = tokenID.String()
	}

	return &NFTBatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     actualGas,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
		TokenIDs:    tokenIDStrings,
	}, nil
}

// GetNFTInfo retrieves information about a specific NFT
func (n *NFTBatchCaller) GetNFTInfo(ctx context.Context, tokenId *big.Int) (*NFTInfo, error) {
	// Get owner
	owner, err := n.nftContract.OwnerOf(&bind.CallOpts{Context: ctx}, tokenId)
	if err != nil {
		return nil, fmt.Errorf("failed to get NFT owner: %w", err)
	}

	// Get token URI
	tokenURI, err := n.nftContract.TokenURI(&bind.CallOpts{Context: ctx}, tokenId)
	if err != nil {
		return nil, fmt.Errorf("failed to get token URI: %w", err)
	}

	// Check if token exists (by checking if owner is zero address)
	zeroAddress := common.HexToAddress("0x0000000000000000000000000000000000000000")
	exists := owner != zeroAddress

	return &NFTInfo{
		TokenID:  tokenId.String(),
		Owner:    owner.Hex(),
		TokenURI: tokenURI,
		Exists:   exists,
	}, nil
}

// EstimateNFTBatchCost estimates the gas cost for an NFT batch operation
func (n *NFTBatchCaller) EstimateNFTBatchCost(ctx context.Context, operationType string, batchSize int) (*NFTCostEstimate, error) {
	// Get current gas price
	gasPrice, err := n.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price: %w", err)
	}

	var estimatedGas uint64
	switch operationType {
	case "mint":
		estimatedGas = uint64(batchSize) * EstimatedNFTMintGas
	case "burn":
		estimatedGas = uint64(batchSize) * EstimatedNFTBurnGas
	case "transfer":
		estimatedGas = uint64(batchSize) * EstimatedNFTTransferGas
	default:
		return nil, fmt.Errorf("unknown operation type: %s", operationType)
	}

	// Add batch overhead (usually batch operations are more efficient)
	batchOverhead := estimatedGas / 10 // Assume 10% overhead for batch operations
	totalGas := estimatedGas + batchOverhead

	totalCost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(totalGas))

	return &NFTCostEstimate{
		OperationType: operationType,
		BatchSize:     batchSize,
		EstimatedGas:  totalGas,
		GasPrice:      gasPrice,
		TotalCostWei:  totalCost,
		TotalCostETH:  weiToEther(totalCost),
		IndividualGas: estimatedGas,
		BatchOverhead: batchOverhead,
	}, nil
}

// waitForConfirmation waits for transaction confirmation
func (n *NFTBatchCaller) waitForConfirmation(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	timeout := time.After(5 * time.Minute)    // 5 minute timeout
	ticker := time.NewTicker(3 * time.Second) // Check every 3 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("transaction confirmation timeout after 5 minutes")
		case <-ticker.C:
			receipt, err := n.client.TransactionReceipt(ctx, txHash)
			if err == nil {
				if receipt.Status == 1 {
					return receipt, nil
				} else {
					return nil, fmt.Errorf("transaction failed with status: %d", receipt.Status)
				}
			}
			// Continue waiting if transaction is not mined yet
		}
	}
}

// NFTInfo represents information about an NFT
type NFTInfo struct {
	TokenID  string `json:"token_id"`
	Owner    string `json:"owner"`
	TokenURI string `json:"token_uri"`
	Exists   bool   `json:"exists"`
}

// NFTCostEstimate represents cost estimation for NFT operations
type NFTCostEstimate struct {
	OperationType string   `json:"operation_type"`
	BatchSize     int      `json:"batch_size"`
	EstimatedGas  uint64   `json:"estimated_gas"`
	GasPrice      *big.Int `json:"gas_price"`
	TotalCostWei  *big.Int `json:"total_cost_wei"`
	TotalCostETH  string   `json:"total_cost_eth"`
	IndividualGas uint64   `json:"individual_gas"`
	BatchOverhead uint64   `json:"batch_overhead"`
}

// Gas estimation constants for NFT operations
const (
	EstimatedNFTMintGas     = uint64(120000) // Estimated gas for single NFT mint
	EstimatedNFTBurnGas     = uint64(50000)  // Estimated gas for single NFT burn
	EstimatedNFTTransferGas = uint64(70000)  // Estimated gas for single NFT transfer
)
