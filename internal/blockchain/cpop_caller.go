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

// CPOPBatchCaller handles CPOP ERC20 token batch operations
type CPOPBatchCaller struct {
	client *ethclient.Client
	token  *cpop.CPOPToken
	auth   *bind.TransactOpts
}

// BatchResult represents the result of a batch operation
type BatchResult struct {
	TxHash      string   `json:"tx_hash"`
	BlockNumber *big.Int `json:"block_number,omitempty"`
	GasUsed     uint64   `json:"gas_used"`
	GasSaved    uint64   `json:"gas_saved"`
	Efficiency  float64  `json:"efficiency_percent"`
	Status      string   `json:"status"` // "submitted", "confirmed", "failed"
}

// NewCPOPBatchCaller creates a new CPOP batch caller
func NewCPOPBatchCaller(rpcURL string, tokenAddr common.Address, auth *bind.TransactOpts) (*CPOPBatchCaller, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ethereum client: %w", err)
	}

	token, err := cpop.NewCPOPToken(tokenAddr, client)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate CPOP token contract: %w", err)
	}

	return &CPOPBatchCaller{
		client: client,
		token:  token,
		auth:   auth,
	}, nil
}

// BatchMint performs batch mint operation
func (c *CPOPBatchCaller) BatchMint(ctx context.Context, recipients []common.Address, amounts []*big.Int) (*BatchResult, error) {
	if len(recipients) != len(amounts) {
		return nil, fmt.Errorf("recipients and amounts length mismatch")
	}

	log.Info().
		Int("recipient_count", len(recipients)).
		Msg("Executing batch mint operation")

	// Execute batch mint
	tx, err := c.token.BatchMint(c.auth, recipients, amounts)
	if err != nil {
		return nil, fmt.Errorf("batch mint transaction failed: %w", err)
	}

	// Wait for confirmation
	receipt, err := c.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		log.Warn().Err(err).Str("tx_hash", tx.Hash().Hex()).Msg("Failed to get transaction receipt")
		return &BatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	// Calculate gas efficiency
	efficiency := c.calculateEfficiency(len(recipients), receipt.GasUsed)
	gasSaved := c.calculateGasSaved(len(recipients), receipt.GasUsed)

	return &BatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     receipt.GasUsed,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
	}, nil
}

// BatchBurn performs batch burn operation
func (c *CPOPBatchCaller) BatchBurn(ctx context.Context, accounts []common.Address, amounts []*big.Int) (*BatchResult, error) {
	if len(accounts) != len(amounts) {
		return nil, fmt.Errorf("accounts and amounts length mismatch")
	}

	log.Info().
		Int("account_count", len(accounts)).
		Msg("Executing batch burn operation")

	tx, err := c.token.BatchBurn(c.auth, accounts, amounts)
	if err != nil {
		return nil, fmt.Errorf("batch burn transaction failed: %w", err)
	}

	receipt, err := c.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		return &BatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	efficiency := c.calculateEfficiency(len(accounts), receipt.GasUsed)
	gasSaved := c.calculateGasSaved(len(accounts), receipt.GasUsed)

	return &BatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     receipt.GasUsed,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
	}, nil
}

// BatchTransfer performs batch transfer operation using batchTransfer (from msg.sender)
func (c *CPOPBatchCaller) BatchTransfer(ctx context.Context, recipients []common.Address, amounts []*big.Int) (*BatchResult, error) {
	if len(recipients) != len(amounts) {
		return nil, fmt.Errorf("recipients and amounts length mismatch")
	}

	log.Info().
		Int("recipient_count", len(recipients)).
		Msg("Executing batch transfer operation")

	tx, err := c.token.BatchTransfer(c.auth, recipients, amounts)
	if err != nil {
		return nil, fmt.Errorf("batch transfer transaction failed: %w", err)
	}

	receipt, err := c.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		return &BatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	efficiency := c.calculateEfficiency(len(recipients), receipt.GasUsed)
	gasSaved := c.calculateGasSaved(len(recipients), receipt.GasUsed)

	return &BatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     receipt.GasUsed,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
	}, nil
}

// BatchTransferFrom performs batch transfer operation using batchTransferFrom (from specified addresses)
func (c *CPOPBatchCaller) BatchTransferFrom(ctx context.Context, fromAddresses []common.Address, toAddresses []common.Address, amounts []*big.Int) (*BatchResult, error) {
	if len(fromAddresses) != len(toAddresses) || len(fromAddresses) != len(amounts) {
		return nil, fmt.Errorf("fromAddresses, toAddresses and amounts length mismatch")
	}

	log.Info().
		Int("transfer_count", len(fromAddresses)).
		Msg("Executing batch transfer from operation")

	tx, err := c.token.BatchTransferFrom(c.auth, fromAddresses, toAddresses, amounts)
	if err != nil {
		return nil, fmt.Errorf("batch transfer from transaction failed: %w", err)
	}

	receipt, err := c.waitForConfirmation(ctx, tx.Hash())
	if err != nil {
		return &BatchResult{
			TxHash: tx.Hash().Hex(),
			Status: "submitted",
		}, nil
	}

	efficiency := c.calculateEfficiency(len(fromAddresses), receipt.GasUsed)
	gasSaved := c.calculateGasSaved(len(fromAddresses), receipt.GasUsed)

	return &BatchResult{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		GasUsed:     receipt.GasUsed,
		GasSaved:    gasSaved,
		Efficiency:  efficiency,
		Status:      "confirmed",
	}, nil
}

// waitForConfirmation waits for transaction confirmation
func (c *CPOPBatchCaller) waitForConfirmation(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	timeout := time.NewTimer(60 * time.Second) // 1 minute timeout
	defer timeout.Stop()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout.C:
			return nil, fmt.Errorf("transaction confirmation timeout")
		case <-ticker.C:
			receipt, err := c.client.TransactionReceipt(ctx, txHash)
			if err == nil {
				if receipt.Status == 1 {
					return receipt, nil
				}
				return nil, fmt.Errorf("transaction failed with status: %d", receipt.Status)
			}
			// Continue polling if error is "not found"
		}
	}
}

// calculateEfficiency calculates gas efficiency percentage
func (c *CPOPBatchCaller) calculateEfficiency(operationCount int, actualGasUsed uint64) float64 {
	// Estimate gas cost for individual operations
	estimatedSingleOpGas := uint64(operationCount) * 21000 // Basic transfer gas * count

	if estimatedSingleOpGas <= actualGasUsed {
		return 0.0
	}

	gasSaved := estimatedSingleOpGas - actualGasUsed
	efficiency := (float64(gasSaved) / float64(estimatedSingleOpGas)) * 100

	// Cap efficiency at reasonable maximum
	if efficiency > 85.0 {
		efficiency = 85.0
	}

	return efficiency
}

// calculateGasSaved calculates the amount of gas saved
func (c *CPOPBatchCaller) calculateGasSaved(operationCount int, actualGasUsed uint64) uint64 {
	estimatedSingleOpGas := uint64(operationCount) * 21000
	if estimatedSingleOpGas <= actualGasUsed {
		return 0
	}
	return estimatedSingleOpGas - actualGasUsed
}

// GetEthClient returns the underlying ethereum client
func (c *CPOPBatchCaller) GetEthClient() *ethclient.Client {
	return c.client
}

// Close closes the ethereum client connection
func (c *CPOPBatchCaller) Close() {
	if c.client != nil {
		c.client.Close()
	}
}
