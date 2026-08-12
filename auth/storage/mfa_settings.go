// Copyright 2026 Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//

package storage

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	// MFASettingsCollection collection holding one MFA settings doc per account
	MFASettingsCollection = "pantahub_mfa_settings"

	// MFAUsedJTICollection collection of consumed MFA-pending token ids (TTL'd)
	MFAUsedJTICollection = "pantahub_mfa_used_jtis"
)

// TOTPFactor is the state of an account's TOTP authenticator enrollment.
// SecretEnc is the AES-256-GCM encrypted RFC 6238 secret; it is never
// serialized to JSON.
type TOTPFactor struct {
	SecretEnc    []byte     `json:"-" bson:"secret_enc"`
	Confirmed    bool       `json:"confirmed" bson:"confirmed"`
	LastUsedStep int64      `json:"-" bson:"last_used_step"`
	CreatedAt    time.Time  `json:"created_at" bson:"created_at"`
	ConfirmedAt  *time.Time `json:"confirmed_at,omitempty" bson:"confirmed_at,omitempty"`
}

// RecoveryCode is a single-use backup code, stored hashed (bcrypt).
type RecoveryCode struct {
	Hash   string     `json:"-" bson:"hash"`
	UsedAt *time.Time `json:"used_at,omitempty" bson:"used_at,omitempty"`
}

// MFASettings is the per-account multi-factor authentication state. Accounts
// without a doc here (or with Enabled == false) log in with password only.
type MFASettings struct {
	ID  primitive.ObjectID `json:"-" bson:"_id"`
	Prn string             `json:"prn" bson:"prn"`

	// Owner is the account PRN this settings doc belongs to (unique index)
	Owner string `json:"owner" bson:"owner"`

	// Enabled is true once at least one confirmed second factor exists
	Enabled bool `json:"enabled" bson:"enabled"`

	// UserHandle is the stable random WebAuthn user.id for this account
	UserHandle []byte `json:"-" bson:"user_handle"`

	TOTP          *TOTPFactor    `json:"totp,omitempty" bson:"totp,omitempty"`
	RecoveryCodes []RecoveryCode `json:"-" bson:"recovery_codes,omitempty"`

	// FailedAttempts counts consecutive failed second-factor proofs;
	// LockedUntil is set when the throttling threshold is crossed
	FailedAttempts int        `json:"-" bson:"failed_attempts"`
	LockedUntil    *time.Time `json:"-" bson:"locked_until,omitempty"`

	TimeCreated  time.Time `json:"time-created" bson:"time-created"`
	TimeModified time.Time `json:"time-modified" bson:"time-modified"`
}

// RecoveryCodesRemaining counts the not-yet-used recovery codes
func (s *MFASettings) RecoveryCodesRemaining() int {
	n := 0
	for _, c := range s.RecoveryCodes {
		if c.UsedAt == nil {
			n++
		}
	}
	return n
}

// HasConfirmedTOTP tells if the account has a confirmed TOTP factor
func (s *MFASettings) HasConfirmedTOTP() bool {
	return s != nil && s.TOTP != nil && s.TOTP.Confirmed
}

// IsLocked tells if the account's second factor step is throttled right now
func (s *MFASettings) IsLocked(now time.Time) bool {
	return s != nil && s.LockedUntil != nil && s.LockedUntil.After(now)
}
