// Copyright 2026  Pantacor Ltd.
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

// Package mfaservice implements the primitives for two-factor authentication:
// TOTP (RFC 6238) enrollment and verification, single-use recovery codes and
// the short-lived single-use MFA-pending token issued between the password
// step and the second factor of a login.
package mfaservice

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
	"golang.org/x/crypto/bcrypt"
)

const (
	// TOTPPeriod RFC 6238 time step in seconds. 30s/SHA1/6 digits is the only
	// parameter set every authenticator app implements.
	TOTPPeriod = 30

	// TOTPSkew steps of clock drift accepted either side of now
	TOTPSkew = 1

	// TOTPSecretSize secret size in bytes (160 bit, >= NIST 112 bit minimum)
	TOTPSecretSize = 20

	// RecoveryCodeCount codes generated per set
	RecoveryCodeCount = 10

	// MFAPendingTokenUse value of the token_use claim of MFA-pending tokens
	MFAPendingTokenUse = "mfa_pending"

	// MFASudoTokenUse value of the token_use claim of MFA "sudo" tokens: a
	// short-lived grant proving the user just re-authenticated with an
	// existing factor, required to authorize sensitive MFA-management
	// operations on accounts that have no usable password (social/passkey).
	MFASudoTokenUse = "mfa_sudo"

	// MaxMFAFailures consecutive failed proofs before the MFA step locks
	MaxMFAFailures = 10

	// MFALockDuration how long the MFA step locks after MaxMFAFailures
	MFALockDuration = 15 * time.Minute

	// recoveryCodeAlphabet unambiguous base32 (no 0/1/8 lookalikes issues for
	// humans typing codes back in)
	recoveryCodeAlphabet = "abcdefghjkmnpqrstvwxyz23456789"

	// recoveryBcryptCost cost for hashing recovery codes; lower than the
	// account password cost (14) on purpose: a verification attempt has to
	// compare against up to RecoveryCodeCount hashes and the step is
	// throttled independently
	recoveryBcryptCost = 10
)

var (
	// ErrMFANotConfigured the PANTAHUB_MFA_ENC_KEY is missing/invalid
	ErrMFANotConfigured = errors.New("mfa is not configured on this server")

	// ErrInvalidPendingToken the MFA-pending token failed validation
	ErrInvalidPendingToken = errors.New("invalid mfa pending token")
)

// MFAPendingClaims are the claims of the single-purpose token issued after a
// successful password check for an MFA-enabled account. It deliberately does
// NOT carry the "prn"/"type"/"id" claims of a session token, so the API auth
// middleware rejects it everywhere except the dedicated step-up endpoints.
type MFAPendingClaims struct {
	TokenUse string   `json:"token_use"`
	Username string   `json:"mfa_username"`
	Prn      string   `json:"mfa_prn"`
	Scope    string   `json:"mfa_scope,omitempty"`
	Amr      []string `json:"amr"`
	Methods  []string `json:"methods"`
	jwtgo.StandardClaims
}

// getEncKey loads and validates the AES-256 key for TOTP secrets at rest
func getEncKey() ([]byte, error) {
	b64 := utils.GetEnv(utils.EnvPantahubMfaEncKey)
	if b64 == "" {
		return nil, ErrMFANotConfigured
	}
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(key) != 32 {
		return nil, ErrMFANotConfigured
	}
	return key, nil
}

// EncryptSecret encrypts a TOTP secret with AES-256-GCM. Output layout is
// nonce || ciphertext.
func EncryptSecret(plaintext string) ([]byte, error) {
	key, err := getEncKey()
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// DecryptSecret reverses EncryptSecret
func DecryptSecret(data []byte) (string, error) {
	key, err := getEncKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(data) < gcm.NonceSize() {
		return "", errors.New("mfa secret ciphertext too short")
	}

	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// GenerateTOTPKey creates a fresh RFC 6238 key with app-compatible defaults.
// The returned key exposes .Secret() (base32) and .URL() (otpauth:// URI).
func GenerateTOTPKey(issuer, accountName string) (*otp.Key, error) {
	if issuer == "" {
		issuer = "Pantahub"
	}
	if accountName == "" {
		return nil, errors.New("account name required for totp enrollment")
	}
	return totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
		Period:      TOTPPeriod,
		SecretSize:  TOTPSecretSize,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
}

// VerifyTOTPCode checks code against secret accepting TOTPSkew steps of
// drift. It returns the matched time-step so the caller can enforce that
// each step is only accepted once (NIST 800-63B replay resistance).
func VerifyTOTPCode(secret, code string, at time.Time) (matchedStep int64, ok bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}

	baseStep := at.Unix() / TOTPPeriod
	for offset := int64(-TOTPSkew); offset <= TOTPSkew; offset++ {
		stepTime := time.Unix((baseStep+offset)*TOTPPeriod, 0)
		expected, err := totp.GenerateCodeCustom(secret, stepTime, totp.ValidateOpts{
			Period:    TOTPPeriod,
			Skew:      0,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return baseStep + offset, true
		}
	}

	return 0, false
}

// GenerateRecoveryCodes creates a fresh set of single-use recovery codes.
// It returns the plaintext codes (shown to the user exactly once) and the
// bcrypt-hashed records for storage.
func GenerateRecoveryCodes() ([]string, []storage.RecoveryCode, error) {
	plain := make([]string, 0, RecoveryCodeCount)
	hashed := make([]storage.RecoveryCode, 0, RecoveryCodeCount)

	for i := 0; i < RecoveryCodeCount; i++ {
		code, err := randomRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(code), recoveryBcryptCost)
		if err != nil {
			return nil, nil, err
		}
		plain = append(plain, code)
		hashed = append(hashed, storage.RecoveryCode{Hash: string(hash)})
	}

	return plain, hashed, nil
}

// VerifyRecoveryCode finds the unused recovery code matching the given
// plaintext. Returns its index or ok == false.
func VerifyRecoveryCode(codes []storage.RecoveryCode, code string) (index int, ok bool) {
	code = normalizeRecoveryCode(code)
	for i, c := range codes {
		if c.UsedAt != nil {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(c.Hash), []byte(code)) == nil {
			return i, true
		}
	}
	return 0, false
}

func normalizeRecoveryCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	return strings.ReplaceAll(code, " ", "")
}

func randomRecoveryCode() (string, error) {
	const codeLen = 10
	buf := make([]byte, codeLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	// rejection sampling for an unbiased draw over the alphabet
	n := len(recoveryCodeAlphabet)
	limit := 256 - (256 % n)
	chars := make([]byte, codeLen)
	for i := 0; i < codeLen; i++ {
		b := buf[i]
		for int(b) >= limit {
			one := make([]byte, 1)
			if _, err := rand.Read(one); err != nil {
				return "", err
			}
			b = one[0]
		}
		chars[i] = recoveryCodeAlphabet[int(b)%n]
	}

	return string(chars[:5]) + "-" + string(chars[5:]), nil
}

// PendingTokenTimeout TTL of MFA-pending tokens
func PendingTokenTimeout() time.Duration {
	minutes, err := strconv.Atoi(utils.GetEnv(utils.EnvPantahubMfaPendingTimeoutMinutes))
	if err != nil || minutes <= 0 {
		minutes = 5
	}
	return time.Duration(minutes) * time.Minute
}

// CreateMFAPendingToken mints the single-use token that carries a login
// between the successful password step and the second factor. Signed with
// the same key as session tokens but structurally unusable as one (see
// MFAPendingClaims).
func CreateMFAPendingToken(jwtMiddleware *jwt.JWTMiddleware, username, prn, scope string, amr, methods []string) (string, error) {
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}

	now := time.Now()
	claims := &MFAPendingClaims{
		TokenUse: MFAPendingTokenUse,
		Username: username,
		Prn:      prn,
		Scope:    scope,
		Amr:      amr,
		Methods:  methods,
		StandardClaims: jwtgo.StandardClaims{
			Id:        hex.EncodeToString(jti),
			IssuedAt:  now.Unix(),
			ExpiresAt: now.Add(PendingTokenTimeout()).Unix(),
		},
	}

	token := jwtgo.NewWithClaims(jwtgo.GetSigningMethod(jwtMiddleware.SigningAlgorithm), claims)
	return token.SignedString(jwtMiddleware.Key)
}

// ParseMFAPendingToken validates signature, expiry and purpose of an
// MFA-pending token. jti single-use enforcement is the caller's job (via
// MFARepo.ConsumeJTI on success).
func ParseMFAPendingToken(jwtMiddleware *jwt.JWTMiddleware, tokenString string) (*MFAPendingClaims, error) {
	claims := &MFAPendingClaims{}

	token, err := jwtgo.ParseWithClaims(tokenString, claims, func(t *jwtgo.Token) (interface{}, error) {
		if t.Method.Alg() != jwtMiddleware.SigningAlgorithm {
			return nil, fmt.Errorf("unexpected signing method: %s", t.Method.Alg())
		}
		if strings.HasPrefix(jwtMiddleware.SigningAlgorithm, "HS") {
			return jwtMiddleware.Key, nil
		}
		return jwtMiddleware.Pub, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidPendingToken
	}

	if claims.TokenUse != MFAPendingTokenUse || claims.Prn == "" || claims.Id == "" {
		return nil, ErrInvalidPendingToken
	}

	return claims, nil
}

// CreateSudoToken mints a short-lived grant proving the user re-authenticated
// with an existing factor. Unlike the pending token it is NOT single-use: it
// is valid (for the same TTL) for the handful of management calls a user
// makes in one sitting, mirroring GitHub's "sudo mode". Structurally unusable
// as a session for the same reason as the pending token (no prn/type claims).
func CreateSudoToken(jwtMiddleware *jwt.JWTMiddleware, prn, factor string) (string, error) {
	now := time.Now()
	claims := &MFAPendingClaims{
		TokenUse: MFASudoTokenUse,
		Prn:      prn,
		Amr:      []string{factor},
		StandardClaims: jwtgo.StandardClaims{
			IssuedAt:  now.Unix(),
			ExpiresAt: now.Add(PendingTokenTimeout()).Unix(),
		},
	}
	token := jwtgo.NewWithClaims(jwtgo.GetSigningMethod(jwtMiddleware.SigningAlgorithm), claims)
	return token.SignedString(jwtMiddleware.Key)
}

// ParseSudoToken validates signature, expiry and purpose of a sudo token and
// returns the account PRN it authorizes.
func ParseSudoToken(jwtMiddleware *jwt.JWTMiddleware, tokenString string) (*MFAPendingClaims, error) {
	claims := &MFAPendingClaims{}

	token, err := jwtgo.ParseWithClaims(tokenString, claims, func(t *jwtgo.Token) (interface{}, error) {
		if t.Method.Alg() != jwtMiddleware.SigningAlgorithm {
			return nil, fmt.Errorf("unexpected signing method: %s", t.Method.Alg())
		}
		if strings.HasPrefix(jwtMiddleware.SigningAlgorithm, "HS") {
			return jwtMiddleware.Key, nil
		}
		return jwtMiddleware.Pub, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidPendingToken
	}

	if claims.TokenUse != MFASudoTokenUse || claims.Prn == "" {
		return nil, ErrInvalidPendingToken
	}

	return claims, nil
}

// HasMethod tells if the pending token allows the given second factor
func (c *MFAPendingClaims) HasMethod(method string) bool {
	for _, m := range c.Methods {
		if m == method {
			return true
		}
	}
	return false
}
