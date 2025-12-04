package pkceservice

import (
	"context"
	"time"

	"gitlab.com/pantacor/pantahub-base/auth/storage"
)

const (
	AuthCodeExpiresIn = 300 // 5 minutes
)

// CreatePKCEState creates and stores a new PKCE state
func CreatePKCEState(ctx context.Context, codeChallenge, codeChallengeMethod, redirectURI, state string) (*storage.PKCEState, error) {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return nil, err
	}

	pks := storage.NewPKCEState()
	pks.AuthCode = pks.ID // Use the generated ID as the AuthCode
	pks.CodeChallenge = codeChallenge
	pks.CodeChallengeMethod = codeChallengeMethod
	pks.RedirectURI = redirectURI
	pks.State = state
	pks.ExpiresAt = time.Now().Add(time.Second * AuthCodeExpiresIn)
	pks.IsUsed = false

	err = pkceRepo.Create(ctx, pks)
	if err != nil {
		return nil, err
	}

	go func() {
		newCtx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
		defer cancel()

		pkceRepo.DeleteExpired(newCtx)
	}()

	return pks, nil
}

// GetPKCEState retrieves a PKCE state by its authorization code
func GetPKCEState(ctx context.Context, authCode string) (*storage.PKCEState, bool) {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return nil, false
	}

	pks, err := pkceRepo.FindByAuthCode(ctx, authCode)
	if err != nil {
		return nil, false
	}
	return pks, true
}

// MarkPKCEStateAsUsed marks a PKCE state as used
func MarkPKCEStateAsUsed(ctx context.Context, authCode string) bool {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return false
	}

	pks, err := pkceRepo.FindByAuthCode(ctx, authCode)
	if err != nil {
		return false
	}

	pks.IsUsed = true
	err = pkceRepo.Update(ctx, pks)
	if err != nil {
		return false
	}
	return true
}

// UpdatePKCEStateUserID updates the UserID of a PKCE state
func UpdatePKCEStateUserID(ctx context.Context, authCode, userID string) bool {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return false
	}

	pks, err := pkceRepo.FindByAuthCode(ctx, authCode)
	if err != nil {
		return false
	}

	pks.UserID = userID
	err = pkceRepo.Update(ctx, pks)
	if err != nil {
		return false
	}

	return true
}

// UpdatePKCEStateWorkspaceID updates the WorkspaceID of a PKCE state
func UpdatePKCEStateWorkspaceID(ctx context.Context, authCode, workspaceID string) bool {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return false
	}

	pks, err := pkceRepo.FindByAuthCode(ctx, authCode)
	if err != nil {
		return false
	}

	pks.WorkspaceID = workspaceID
	err = pkceRepo.Update(ctx, pks)
	if err != nil {
		return false
	}
	return true
}

// DeletePKCEState deletes a PKCE state from the store
func DeletePKCEState(ctx context.Context, authCode string) {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return
	}
	pkceRepo.Delete(ctx, authCode)
}
