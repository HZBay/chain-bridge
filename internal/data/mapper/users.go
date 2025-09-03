package mapper

import (
	"github.com/hzbay/chain-bridge/internal/data/dto"
	"github.com/hzbay/chain-bridge/internal/models"
)

func LocalAppUserProfileToDTO(appUserProfile *models.AppUserProfile) dto.AppUserProfile {
	return dto.AppUserProfile{
		UserID:          appUserProfile.UserID,
		LegalAcceptedAt: appUserProfile.LegalAcceptedAt,
		UpdatedAt:       appUserProfile.UpdatedAt,
	}
}

func LocalUserToDTO(user *models.User) dto.User {
	result := dto.User{
		ID:                  user.ID,
		Username:            user.Username,
		IsActive:            user.IsActive,
		Scopes:              user.Scopes,
		LastAuthenticatedAt: user.LastAuthenticatedAt,
		UpdatedAt:           user.UpdatedAt,
		PasswordHash:        user.Password,
	}

	if user.R != nil && user.R.AppUserProfile != nil {
		result.Profile = LocalAppUserProfileToDTO(user.R.AppUserProfile).Ptr()
	}

	return result
}
