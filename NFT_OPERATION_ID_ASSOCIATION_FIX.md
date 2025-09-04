# NFT Batch Minting OperationID Association Fix

## Problem Identified

**Issue**: In NFT batch minting, all operations within a single batch were sharing the same `OperationID`, making it impossible to associate specific `token_id`s returned from the blockchain with individual mint operations.

**Root Cause**: 
- One `BatchMintNFTs` request has one `mainOperationID` 
- Multiple NFTs are minted in the batch for different users
- When blockchain returns multiple `token_id`s, we couldn't map which `token_id` belongs to which user

## Solution Implemented

### 1. **Individual Operation IDs**
Each mint operation within a batch now gets its own unique `operation_id`:

```go
// Before: All operations shared mainOperationID
for _, mintOp := range request.MintOperations {
    // ❌ Same operation_id for all operations
    _, err := tx.ExecContext(ctx, `INSERT INTO transactions (...) VALUES (..., $2, ...)`, 
        transactionID, mainOperationID.String(), ...)
}

// After: Each operation gets individual operation_id  
for _, mintOp := range request.MintOperations {
    individualOperationID := uuid.New() // ✅ Unique for each operation
    _, err := tx.ExecContext(ctx, `INSERT INTO transactions (...) VALUES (..., $2, ...)`, 
        transactionID, individualOperationID.String(), ...)
}
```

### 2. **Enhanced NFTMintJob Structure**
Added dual operation ID tracking:

```go
type NFTMintJob struct {
    // ... existing fields ...
    
    // Batch-level operation ID (for idempotency of the entire batch request)
    BatchOperationID      string `json:"batch_operation_id,omitempty"`
    // Individual operation ID (for tracking each specific mint operation)  
    IndividualOperationID string `json:"individual_operation_id,omitempty"`
}
```

### 3. **Proper Token ID Mapping**
Updated the token ID extraction logic to use order-based mapping:

```go
// Get operations ordered by creation time
query := `
    SELECT operation_id, user_id, collection_id 
    FROM transactions 
    WHERE batch_id = $1 AND tx_type = 'nft_mint' AND nft_token_id = '-1' 
    ORDER BY created_at ASC`  // ✅ Preserves minting order

// Map by index: operations[i] -> tokenIDs[i]
for i := 0; i < minCount; i++ {
    op := operations[i]           // ✅ Individual operation
    actualTokenID := nftResult.TokenIDs[i] // ✅ Corresponding token_id
    
    // Update with individual operation_id
    err = w.updateNFTTokenIDDirect(ctx, op.OperationID, actualTokenID, op.CollectionID)
}
```

## Benefits

1. **Accurate Association**: Each `token_id` can now be mapped to the correct user/operation
2. **Proper Notifications**: Success notifications include the correct `operation_id` and `token_id` for each user
3. **Idempotency Maintained**: Batch-level idempotency still works with `mainOperationID`
4. **Individual Tracking**: Each mint operation can be tracked independently
5. **Order Preservation**: Database ordering matches blockchain minting order

## Notification Flow

```
1. User A requests batch mint [UserA, UserB, UserC]
2. System creates:
   - Batch OperationID: "batch-123" 
   - Individual OperationIDs: ["op-a", "op-b", "op-c"]
3. Blockchain mints and returns: ["101", "102", "103"]  
4. System maps:
   - UserA (op-a) -> token_id: "101"
   - UserB (op-b) -> token_id: "102" 
   - UserC (op-c) -> token_id: "103"
5. Notifications sent:
   - UserA: {operation_id: "op-a", nft_token_id: "101"}
   - UserB: {operation_id: "op-b", nft_token_id: "102"}
   - UserC: {operation_id: "op-c", nft_token_id: "103"}
```

## Files Modified

- `internal/services/nft/service.go` - Individual operation ID generation
- `internal/queue/types.go` - Enhanced NFTMintJob structure  
- `internal/queue/tx_confirmation_watcher.go` - Improved token ID mapping logic

This fix ensures that NFT batch minting properly associates each minted token with its corresponding user and operation, enabling accurate notifications and tracking.