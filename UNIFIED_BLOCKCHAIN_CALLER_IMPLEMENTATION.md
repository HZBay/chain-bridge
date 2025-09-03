# Unified Blockchain Caller Implementation

This document describes the implementation of a unified blockchain caller that consolidates both CPOP token (ERC20) and NFT (ERC721) operations into a single interface.

## Overview

The implementation replaces the separate `CPOPBatchCaller` and `NFTBatchCaller` with a single `UnifiedBatchCaller` that handles both types of blockchain operations. This simplifies the architecture and ensures consistent behavior across all blockchain interactions.

## Key Changes

### 1. New UnifiedBatchCaller

**File**: `internal/blockchain/unified_caller.go`

#### Features:
- **Dual Contract Support**: Manages both CPOP token (ERC20) and NFT (ERC721) contracts
- **Automatic NFT Contract Detection**: Uses the `official_nft_contract_address` from the chains table
- **Unified Interface**: Single caller instance per chain handles all operations
- **NFT Capability Detection**: `IsNFTEnabled()` method checks if NFT operations are available

#### CPOP Operations (ERC20):
- `BatchMint()` - Batch mint CPOP tokens
- `BatchBurn()` - Batch burn CPOP tokens  
- `BatchTransfer()` - Batch transfer from msg.sender
- `BatchTransferFrom()` - Batch transfer from specified addresses

#### NFT Operations (ERC721):
- `NFTBatchMint()` - Batch mint NFTs with metadata
- `NFTBatchBurn()` - Batch burn NFTs
- `NFTBatchTransferFrom()` - Batch transfer NFTs

### 2. Updated Consumer Manager

**File**: `internal/queue/consumer_manager.go`

#### Changes:
- **Unified Caller Map**: Changed from `cpopCallers` to `unifiedCallers`
- **NFT Contract Integration**: Reads `official_nft_contract_address` from chains table
- **Graceful NFT Handling**: If NFT contract address is invalid or missing, NFT operations are disabled but CPOP operations continue

#### Constructor Logic:
```go
// Get NFT contract address if available
nftContractAddr := common.Address{} // Default to zero address
if chain.OfficialNFTContractAddress.Valid && chain.OfficialNFTContractAddress.String != "" {
    if common.IsHexAddress(chain.OfficialNFTContractAddress.String) {
        nftContractAddr = common.HexToAddress(chain.OfficialNFTContractAddress.String)
    }
}

caller, err := blockchain.NewUnifiedBatchCaller(
    chain.RPCURL,
    common.HexToAddress(chain.ToeknAddress.String), // CPOP token address
    nftContractAddr,                                // NFT contract address
    auth,
    chain.ChainID,
)
```

### 3. Updated Transaction Confirmation Watcher

**File**: `internal/queue/tx_confirmation_watcher.go`

#### Changes:
- **Unified Caller Integration**: Uses `unifiedCallers` instead of `cpopCallers`
- **Consistent Interface**: Same confirmation logic works for both CPOP and NFT operations

### 4. Updated RabbitMQ Consumer

**File**: `internal/queue/rabbitmq_consumer.go` & `internal/queue/rabbitmq_batch_consumer.go`

#### Changes:
- **Unified Processing**: All batch operations go through the same unified caller
- **NFT Capability Checks**: Before processing NFT operations, verifies `caller.IsNFTEnabled()`
- **Consistent Error Handling**: Same error patterns for all operation types

#### Processing Flow:
```go
switch group.JobType {
case JobTypeAssetAdjust:
    return c.processAssetAdjustBatch(ctx, caller, jobs)  // CPOP operations
case JobTypeTransfer:
    return c.processTransferBatch(ctx, caller, jobs)     // CPOP operations
case JobTypeNFTMint:
    return c.processNFTMintBatch(ctx, caller, jobs)      // NFT operations
case JobTypeNFTBurn:
    return c.processNFTBurnBatch(ctx, caller, jobs)      // NFT operations
case JobTypeNFTTransfer:
    return c.processNFTTransferBatch(ctx, caller, jobs)  // NFT operations
}
```

## Benefits

### 1. **Simplified Architecture**
- Single caller per chain instead of multiple specialized callers
- Reduced complexity in dependency injection and management
- Consistent interface across all blockchain operations

### 2. **Database-Driven Configuration**
- NFT contract addresses read from `chains.official_nft_contract_address`
- Easy to enable/disable NFT support per chain via database
- No code changes required to add NFT support to existing chains

### 3. **Graceful Degradation**
- Chains without NFT contracts continue to work normally for CPOP operations
- Clear error messages when NFT operations are attempted on unsupported chains
- No breaking changes to existing CPOP functionality

### 4. **Consistent Error Handling**
- Same error patterns and logging across all operation types
- Unified transaction confirmation and monitoring
- Consistent notification and status update patterns

### 5. **Future Extensibility**
- Easy to add new contract types (e.g., ERC1155) to the unified caller
- Consistent pattern for adding new blockchain operation types
- Single point of enhancement for all blockchain interactions

## Configuration

### Database Setup
Ensure the `official_nft_contract_address` field is populated in the `chains` table:

```sql
UPDATE chains 
SET official_nft_contract_address = '0x...' 
WHERE chain_id = 1; -- Ethereum mainnet
```

### Migration Support
The implementation is backward compatible:
- Existing chains without NFT contract addresses continue to work
- CPOP operations are unaffected
- NFT operations gracefully fail with clear error messages

## Usage Examples

### Check NFT Support
```go
if caller.IsNFTEnabled() {
    // NFT operations are available
    result, err := caller.NFTBatchMint(ctx, recipients, tokenIds, metadataURIs)
} else {
    // Only CPOP operations available
    return fmt.Errorf("NFT operations not supported on chain %d", chainID)
}
```

### Unified Transaction Processing
```go
// Both CPOP and NFT operations use the same confirmation logic
confirmed, confirmations, err := confirmationWatcher.WaitForConfirmation(
    ctx, caller, result.TxHash, 6, 10*time.Minute)
```

## Implementation Notes

- **Gas Estimation**: Separate gas estimation logic for CPOP vs NFT operations
- **Result Conversion**: NFT operations return `NFTBatchResult` which gets converted to standard `BatchResult`
- **Contract Validation**: NFT contract address validation happens during caller creation
- **Error Context**: All error messages include chain ID for better debugging

This unified approach significantly simplifies the blockchain interaction layer while maintaining full backward compatibility and providing a clean path for future enhancements.