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

// EstimatedNFTMintGas estimated gas for individual NFT mint operation
const EstimatedNFTMintGas = 200000

// EstimatedNFTBurnGas estimated gas for individual NFT burn operation
const EstimatedNFTBurnGas = 150000

// EstimatedNFTTransferGas estimated gas for individual NFT transfer operation
const EstimatedNFTTransferGas = 100000

// BatchResult represents the result of a batch operation
type BatchResult struct {
	TxHash      string   `json:"tx_hash"`
	BlockNumber *big.Int `json:"block_number,omitempty"`
	GasUsed     uint64   `json:"gas_used"`
	GasSaved    uint64   `json:"gas_saved"`
	Efficiency  float64  `json:"efficiency_percent"`
	Status      string   `json:"status"` // "submitted", "confirmed", "failed"
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

// BatchCaller handles both CPOP ERC20 and NFT ERC721 batch operations
type BatchCaller struct {
	client      *ethclient.Client
	cpopToken   *cpop.CPOPToken
	nftContract *cpop.CPNFT
	auth        *bind.TransactOpts
	chainID     int64
}

// NewBatchCaller creates a new unified batch caller for both CPOP and NFT operations
func NewBatchCaller(rpcURL string, cpopTokenAddr, nftContractAddr common.Address, auth *bind.TransactOpts, chainID int64) (*BatchCaller, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ethereum client: %w", err)
	}

	// Initialize CPOP token contract
	cpopToken, err := cpop.NewCPOPToken(cpopTokenAddr, client)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate CPOP token contract: %w", err)
	}

	// Initialize NFT contract (optional - may be nil if not configured)
	var nftContract *cpop.CPNFT
	if nftContractAddr != (common.Address{}) {
		nftContract, err = cpop.NewCPNFT(nftContractAddr, client)
		if err != nil {
			log.Warn().
				Err(err).
				Str("nft_contract_addr", nftContractAddr.Hex()).
				Int64("chain_id", chainID).
				Msg("Failed to instantiate NFT contract, NFT operations will be disabled")
		}
	}

	return &BatchCaller{
		client:      client,
		cpopToken:   cpopToken,
		nftContract: nftContract,
		auth:        auth,
		chainID:     chainID,
	}, nil
}

// GetEthClient returns the underlying Ethereum client
func (u *BatchCaller) GetEthClient() *ethclient.Client {
	return u.client
}

// GetChainID returns the chain ID
func (u *BatchCaller) GetChainID() int64 {
	return u.chainID
}

// IsNFTEnabled returns true if NFT operations are supported
func (u *BatchCaller) IsNFTEnabled() bool {
	return u.nftContract != nil
}

// =============================================================================
// CPOP TOKEN OPERATIONS (ERC20)
// =============================================================================

// BatchMint performs batch mint operation for CPOP tokens
func (u *BatchCaller) BatchMint(ctx context.Context, recipients []common.Address, amounts []*big.Int) (*BatchResult, error) {
	if len(recipients) != len(amounts) {
		return nil, fmt.Errorf("recipients and amounts length mismatch")
	}

	log.Info().
		Int("recipient_count", len(recipients)).
		Int64("chain_id", u.chainID).
		Msg("Executing CPOP batch mint operation")

	// Execute batch mint
	tx, err := u.cpopToken.BatchMint(u.auth, recipients, amounts)
	if err != nil {
		return nil, fmt.Errorf("CPOP batch mint transaction failed: %w", err)
	}

	// Wait for confirmation
	receipt, err := u.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		log.Warn().Err(err).Str("tx_hash", tx.Hash().Hex()).Msg("Failed to get CPOP transaction receipt")
		return &BatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	// Calculate gas efficiency
	efficiency := u.calculateCPOPEfficiency(len(recipients), receipt.GasUsed)
	gasSaved := u.calculateCPOPGasSaved(len(recipients), receipt.GasUsed)

	return &BatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     receipt.GasUsed,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
	}, nil
}

// BatchBurn performs batch burn operation for CPOP tokens
func (u *BatchCaller) BatchBurn(ctx context.Context, accounts []common.Address, amounts []*big.Int) (*BatchResult, error) {
	if len(accounts) != len(amounts) {
		return nil, fmt.Errorf("accounts and amounts length mismatch")
	}

	log.Info().
		Int("account_count", len(accounts)).
		Int64("chain_id", u.chainID).
		Msg("Executing CPOP batch burn operation")

	tx, err := u.cpopToken.BatchBurn(u.auth, accounts, amounts)
	if err != nil {
		return nil, fmt.Errorf("CPOP batch burn transaction failed: %w", err)
	}

	receipt, err := u.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		return &BatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	efficiency := u.calculateCPOPEfficiency(len(accounts), receipt.GasUsed)
	gasSaved := u.calculateCPOPGasSaved(len(accounts), receipt.GasUsed)

	return &BatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     receipt.GasUsed,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
	}, nil
}

// BatchTransfer performs batch transfer operation for CPOP tokens (from msg.sender)
func (u *BatchCaller) BatchTransfer(ctx context.Context, recipients []common.Address, amounts []*big.Int) (*BatchResult, error) {
	if len(recipients) != len(amounts) {
		return nil, fmt.Errorf("recipients and amounts length mismatch")
	}

	log.Info().
		Int("recipient_count", len(recipients)).
		Int64("chain_id", u.chainID).
		Msg("Executing CPOP batch transfer operation")

	tx, err := u.cpopToken.BatchTransfer(u.auth, recipients, amounts)
	if err != nil {
		return nil, fmt.Errorf("CPOP batch transfer transaction failed: %w", err)
	}

	receipt, err := u.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		return &BatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	efficiency := u.calculateCPOPEfficiency(len(recipients), receipt.GasUsed)
	gasSaved := u.calculateCPOPGasSaved(len(recipients), receipt.GasUsed)

	return &BatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     receipt.GasUsed,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
	}, nil
}

// BatchTransferFrom performs batch transfer operation for CPOP tokens (from specified addresses)
func (u *BatchCaller) BatchTransferFrom(ctx context.Context, fromAddresses []common.Address, toAddresses []common.Address, amounts []*big.Int) (*BatchResult, error) {
	if len(fromAddresses) != len(toAddresses) || len(fromAddresses) != len(amounts) {
		return nil, fmt.Errorf("fromAddresses, toAddresses and amounts length mismatch")
	}

	log.Info().
		Int("transfer_count", len(fromAddresses)).
		Int64("chain_id", u.chainID).
		Msg("Executing CPOP batch transfer from operation")

	tx, err := u.cpopToken.BatchTransferFrom(u.auth, fromAddresses, toAddresses, amounts)
	if err != nil {
		return nil, fmt.Errorf("CPOP batch transfer from transaction failed: %w", err)
	}

	receipt, err := u.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		return &BatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	efficiency := u.calculateCPOPEfficiency(len(fromAddresses), receipt.GasUsed)
	gasSaved := u.calculateCPOPGasSaved(len(fromAddresses), receipt.GasUsed)

	return &BatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     receipt.GasUsed,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
	}, nil
}

// =============================================================================
// NFT OPERATIONS (ERC721)
// =============================================================================

// NFTBatchMint executes a batch mint operation for NFTs
func (u *BatchCaller) NFTBatchMint(ctx context.Context, recipients []common.Address, tokenIDs []*big.Int, _ []string) (*NFTBatchResult, error) {
	if u.nftContract == nil {
		return nil, fmt.Errorf("NFT contract not available for chain %d", u.chainID)
	}

	if len(recipients) != len(tokenIds) {
		return nil, fmt.Errorf("recipients and tokenIds arrays must have the same length")
	}

	log.Info().
		Int("batch_size", len(recipients)).
		Int64("chain_id", u.chainID).
		Msg("Executing NFT batch mint")

	// Estimate gas cost for individual operations vs batch
	individualGas := uint64(len(recipients)) * EstimatedNFTMintGas

	// Execute batch mint transaction - only recipients and tokenIds are supported
	tx, err := u.nftContract.BatchMint(u.auth, recipients)
	if err != nil {
		return nil, fmt.Errorf("batch NFT mint failed: %w", err)
	}

	log.Info().
		Str("tx_hash", tx.Hash().Hex()).
		Uint64("gas_limit", tx.Gas()).
		Msg("NFT batch mint transaction submitted")

	// Wait for confirmation
	receipt, err := u.waitForConfirmation(ctx, tx.Hash())
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

// NFTBatchBurn executes a batch burn operation for NFTs
func (u *BatchCaller) NFTBatchBurn(ctx context.Context, tokenIDs []*big.Int) (*NFTBatchResult, error) {
	if u.nftContract == nil {
		return nil, fmt.Errorf("NFT contract not available for chain %d", u.chainID)
	}

	log.Info().
		Int("batch_size", len(tokenIds)).
		Int64("chain_id", u.chainID).
		Msg("Executing NFT batch burn")

	// Estimate gas cost for individual operations vs batch
	individualGas := uint64(len(tokenIds)) * EstimatedNFTBurnGas

	// Execute batch burn transaction
	tx, err := u.nftContract.BatchBurn(u.auth, tokenIds)
	if err != nil {
		return nil, fmt.Errorf("batch NFT burn failed: %w", err)
	}

	log.Info().
		Str("tx_hash", tx.Hash().Hex()).
		Uint64("gas_limit", tx.Gas()).
		Msg("NFT batch burn transaction submitted")

	// Wait for confirmation
	receipt, err := u.waitForConfirmation(ctx, tx.Hash())
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

// NFTBatchTransferFrom executes a batch transfer operation for NFTs
func (u *BatchCaller) NFTBatchTransferFrom(ctx context.Context, fromAddresses, toAddresses []common.Address, tokenIDs []*big.Int) (*NFTBatchResult, error) {
	if u.nftContract == nil {
		return nil, fmt.Errorf("NFT contract not available for chain %d", u.chainID)
	}

	if len(fromAddresses) != len(toAddresses) || len(fromAddresses) != len(tokenIds) {
		return nil, fmt.Errorf("fromAddresses, toAddresses, and tokenIds arrays must have the same length")
	}

	log.Info().
		Int("batch_size", len(tokenIds)).
		Int64("chain_id", u.chainID).
		Msg("Executing NFT batch transfer")

	// Estimate gas cost for individual operations vs batch
	individualGas := uint64(len(tokenIds)) * EstimatedNFTTransferGas

	// Execute batch transfer transaction
	tx, err := u.nftContract.BatchTransferFrom(u.auth, fromAddresses, toAddresses, tokenIds)
	if err != nil {
		return nil, fmt.Errorf("batch NFT transfer failed: %w", err)
	}

	log.Info().
		Str("tx_hash", tx.Hash().Hex()).
		Uint64("gas_limit", tx.Gas()).
		Msg("NFT batch transfer transaction submitted")

	// Wait for confirmation
	receipt, err := u.waitForConfirmation(ctx, tx.Hash())
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

// =============================================================================
// HELPER METHODS
// =============================================================================

// waitForConfirmation waits for transaction confirmation
func (u *BatchCaller) waitForConfirmation(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
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
			receipt, err := u.client.TransactionReceipt(ctx, txHash)
			if err == nil {
				if receipt.Status == 1 {
					return receipt, nil
				}
				return nil, fmt.Errorf("transaction failed with status: %d", receipt.Status)
			}
			// Continue waiting if transaction is not mined yet
		}
	}
}

// calculateCPOPEfficiency calculates gas efficiency for CPOP operations
func (u *BatchCaller) calculateCPOPEfficiency(operationCount int, actualGas uint64) float64 {
	// Estimated gas for individual operations
	if operationCount < 0 {
		return 0
	}
	estimatedIndividualGas := uint64(operationCount) * 21000 // Base transfer gas
	if actualGas < estimatedIndividualGas {
		return ((float64(estimatedIndividualGas - actualGas)) / float64(estimatedIndividualGas)) * 100
	}
	return 0
}

// calculateCPOPGasSaved calculates gas saved for CPOP operations
func (u *BatchCaller) calculateCPOPGasSaved(operationCount int, actualGas uint64) uint64 {
	if operationCount < 0 {
		return 0
	}
	estimatedIndividualGas := uint64(operationCount) * 21000
	if actualGas < estimatedIndividualGas {
		return estimatedIndividualGas - actualGas
	}
	return 0
}
