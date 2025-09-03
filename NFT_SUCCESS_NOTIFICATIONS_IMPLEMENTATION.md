# NFT Operation Success Notifications Implementation

This document describes the implementation of NFT operation success notifications that are sent after blockchain confirmation.

## Overview

The implementation extends the existing `TxConfirmationWatcher` to detect NFT operations and send appropriate success notifications after blockchain confirmation. This ensures users are notified only when their NFT operations have been successfully confirmed on the blockchain.

## Key Changes

### 1. Enhanced TxConfirmationWatcher

**File**: `internal/queue/tx_confirmation_watcher.go`

#### New Features:
- Added `NotificationProcessor` interface and dependency
- Extended `BatchOperation` struct with NFT-specific fields (`CollectionID`, `NFTTokenID`)
- Added NFT asset finalization methods
- Added success notification sending logic

#### New Methods:
- `finalizeNFTAssetsForBatch()` - Finalizes NFT assets after successful blockchain confirmation
- `finalizeNFTForMint()` - Updates NFT asset to minted status
- `finalizeNFTForBurn()` - Updates NFT asset to burned status  
- `finalizeNFTForTransfer()` - Updates NFT ownership and unlocks asset
- `sendSuccessNotifications()` - Sends success notifications for all operations in a batch
- `sendNFTSuccessNotification()` - Sends NFT operation success notification
- `sendNFTTransferReceivedNotification()` - Sends notification to NFT transfer recipient

### 2. Updated Consumer Manager

**File**: `internal/queue/consumer_manager.go`

#### Changes:
- Modified `NewTxConfirmationWatcher` call to include notification processor
- The batch processor itself serves as the notification processor

### 3. NFT Asset State Management

The implementation properly manages NFT asset states during the confirmation process:

#### For NFT Mint:
- Sets `is_minted = true`
- Sets `is_locked = false`
- Updates `updated_at` timestamp

#### For NFT Burn:
- Sets `is_burned = true` 
- Sets `is_locked = false`
- Updates `updated_at` timestamp

#### For NFT Transfer:
- Updates `owner_user_id` to new owner
- Sets `is_locked = false` 
- Updates `updated_at` timestamp

## Notification Types

The implementation adds the following new notification types:

### 1. NFT Mint Success
- **Type**: `nft_mint_success`
- **Recipients**: NFT recipient (minter)
- **Data**: `collection_id`, `nft_token_id`, `chain_id`, `tx_hash`, `timestamp`, `status`

### 2. NFT Burn Success  
- **Type**: `nft_burn_success`
- **Recipients**: NFT owner (burner)
- **Data**: `collection_id`, `nft_token_id`, `chain_id`, `tx_hash`, `timestamp`, `status`

### 3. NFT Transfer Success
- **Type**: `nft_transfer_success` 
- **Recipients**: NFT sender
- **Data**: `collection_id`, `nft_token_id`, `to_user_id`, `chain_id`, `tx_hash`, `timestamp`, `status`

### 4. NFT Transfer Received
- **Type**: `nft_transfer_received`
- **Recipients**: NFT recipient  
- **Data**: `collection_id`, `nft_token_id`, `from_user_id`, `chain_id`, `tx_hash`, `timestamp`, `status`

## Integration Flow

1. **NFT Operation Initiated**: User initiates NFT mint/burn/transfer through API
2. **Transaction Recorded**: Operation recorded in database with `status = 'recorded'`
3. **Batch Processing**: Operation queued and processed in blockchain batch
4. **Blockchain Submission**: Batch submitted to blockchain with `status = 'submitted'`
5. **Confirmation Monitoring**: `TxConfirmationWatcher` monitors transaction for confirmations
6. **Confirmation Achieved**: After required confirmations (default: 6 blocks)
   - Database updated with `status = 'confirmed'`
   - NFT assets finalized (minted/burned/transferred)
   - Success notifications sent to relevant users
7. **User Notification**: Users receive notifications confirming successful NFT operations

## Benefits

1. **Reliability**: Notifications sent only after blockchain confirmation
2. **Comprehensive Coverage**: All NFT operations (mint, burn, transfer) are covered
3. **Multi-user Support**: Transfer notifications sent to both sender and recipient
4. **Detailed Information**: Notifications include all relevant NFT and transaction details
5. **Non-blocking**: Notification failures don't affect transaction processing
6. **Consistent Architecture**: Reuses existing notification infrastructure

## Error Handling

- Notification sending failures are logged as warnings but don't affect transaction confirmation
- Missing NFT-specific data is handled gracefully with appropriate error messages
- Database transaction failures during NFT finalization are properly rolled back
- Graceful handling when notification processor is not available

## Configuration

No additional configuration is required. The implementation:
- Uses existing notification queue infrastructure
- Follows existing notification priority patterns (Normal priority for success notifications)
- Integrates seamlessly with existing batch processing and confirmation watching