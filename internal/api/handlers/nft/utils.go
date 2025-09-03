package nft

import (
	"github.com/hzbay/chain-bridge/internal/services/nft"
	"github.com/hzbay/chain-bridge/internal/types"
)

// Helper function to convert batch preferences
func convertBatchPreferences(prefs *types.BatchPreference) *nft.BatchPreferences {
	if prefs == nil {
		return nil
	}

	result := &nft.BatchPreferences{}
	if prefs.MaxWaitTime != "" {
		result.MaxWaitTime = &prefs.MaxWaitTime
	}
	if prefs.Priority != nil && *prefs.Priority != "" {
		result.Priority = prefs.Priority
	}

	return result
}

// Helper function to convert batch info
func convertBatchInfo(batchInfo *nft.BatchInfo) *types.BatchInfo {
	if batchInfo == nil {
		return nil
	}

	result := &types.BatchInfo{
		BatchID: batchInfo.BatchID,
	}

	return result
}