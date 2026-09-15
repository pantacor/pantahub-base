package pkceservice

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"sync/atomic"
	"time"

	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
)

const (
	AuthCodeExpiresIn = 300 // 5 minutes
)

// CreatePKCEState creates and stores a new PKCE state
func CreatePKCEState(ctx context.Context, codeChallenge, codeChallengeMethod, redirectURI, state, clientID, scope string) (*storage.PKCEState, error) {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return nil, err
	}

	// Generate auth_code (secure)
	b := make([]byte, 32)
	_, err = rand.Read(b)
	if err != nil {
		return nil, err
	}
	authCode := base64.RawURLEncoding.EncodeToString(b)

	// Generate session_id (public)
	bSession := make([]byte, 16)
	_, err = rand.Read(bSession)
	if err != nil {
		return nil, err
	}
	sessionID := base64.RawURLEncoding.EncodeToString(bSession)

	// Generate user_code (readable) - Kept for backward compat or other flows if needed, but not primary for Unified
	userCode := utils.RandStringUpper(4) + "-" + utils.RandStringUpper(4)

	pks := storage.NewPKCEState()
	pks.AuthCode = authCode
	pks.SessionID = sessionID
	pks.UserCode = userCode
	pks.ClientID = clientID
	pks.Scope = scope
	pks.CodeChallenge = codeChallenge
	pks.CodeChallengeMethod = codeChallengeMethod
	pks.RedirectURI = redirectURI
	pks.State = state
	pks.ExpiresAt = time.Now().Add(time.Second * AuthCodeExpiresIn)
	pks.IsUsed = false
	pks.Interval = 5

	err = pkceRepo.Create(ctx, pks)
	if err != nil {
		return nil, err
	}

	sweepExpiredStates(pkceRepo)

	return pks, nil
}

// expiredSweepRunning guards the cleanup goroutine below.
var expiredSweepRunning atomic.Bool

// sweepExpiredStates removes expired PKCE states in the background, with at
// most one sweep in flight.
//
// Every call to CreatePKCEState used to spawn its own goroutine for this, so a
// burst of device authorisations produced a burst of goroutines all issuing
// the same delete, each holding a context alive for an hour. The work is
// global rather than per-request, so one sweep at a time is enough and a
// caller that finds one already running can simply move on.
func sweepExpiredStates(pkceRepo *storage.PKCERepo) {
	if !expiredSweepRunning.CompareAndSwap(false, true) {
		return
	}

	go func() {
		defer expiredSweepRunning.Store(false)

		newCtx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
		defer cancel()

		if err := pkceRepo.DeleteExpired(newCtx); err != nil {
			log.Printf("WARNING: sweeping expired PKCE states failed: %v", err)
		}
	}()
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

// GetPKCEStateByUserCode retrieves a PKCE state by its user code
func GetPKCEStateByUserCode(ctx context.Context, userCode string) (*storage.PKCEState, bool) {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return nil, false
	}

	pks, err := pkceRepo.FindByUserCode(ctx, userCode)
	if err != nil {
		return nil, false
	}
	return pks, true
}

// GetPKCEStateBySessionID retrieves a PKCE state by its session ID
func GetPKCEStateBySessionID(ctx context.Context, sessionID string) (*storage.PKCEState, bool) {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return nil, false
	}

	pks, err := pkceRepo.FindBySessionID(ctx, sessionID)
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

// UpdatePKCEStateToken updates the Token of a PKCE state
func UpdatePKCEStateToken(ctx context.Context, authCode, token string) bool {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return false
	}

	pks, err := pkceRepo.FindByAuthCode(ctx, authCode)
	if err != nil {
		return false
	}

	pks.Token = token
	err = pkceRepo.Update(ctx, pks)
	if err != nil {
		return false
	}

	return true
}

// UpdateLastPollTime updates the LastPollAt time of a PKCE state
func UpdateLastPollTime(ctx context.Context, authCode string) bool {
	pkceRepo, err := storage.GetPKCERepo()
	if err != nil {
		return false
	}

	pks, err := pkceRepo.FindByAuthCode(ctx, authCode)
	if err != nil {
		return false
	}

	pks.LastPollAt = time.Now()
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
	if err := pkceRepo.Delete(ctx, authCode); err != nil {
		log.Printf("WARNING: deleting PKCE state failed: %v", err)
	}
}
