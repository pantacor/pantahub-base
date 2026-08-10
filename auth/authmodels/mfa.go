package authmodels

import "encoding/json"

// MFA method names as exposed in login responses and pending tokens
const (
	MFAMethodTOTP     = "totp"
	MFAMethodWebauthn = "webauthn"
	MFAMethodRecovery = "recovery"
)

// MFARequiredResponse replaces TokenResponse on POST /auth/login when the
// account has two-factor authentication enabled. The client must complete
// the login on one of the /auth/login/mfa/* endpoints using MFAToken.
type MFARequiredResponse struct {
	MFARequired bool     `json:"mfa_required"`
	MFAToken    string   `json:"mfa_token"`
	Methods     []string `json:"methods"`
}

// MFALoginRequest completes a pending login with a TOTP or recovery code
type MFALoginRequest struct {
	MFAToken string `json:"mfa_token"`
	Code     string `json:"code"`
}

// MFAPasswordRequest carries the fresh-auth proof for sensitive MFA
// management operations (enroll, disable, regenerate recovery codes). The
// caller supplies either the account password or a sudo token obtained by
// re-proving an existing factor (for accounts with no usable password).
type MFAPasswordRequest struct {
	Password  string `json:"password"`
	SudoToken string `json:"sudo_token"`
}

// SudoResponse carries a short-lived grant proving a fresh factor re-auth
type SudoResponse struct {
	SudoToken string `json:"sudo_token"`
}

// MFASudoRecoveryRequest re-proves a factor with a TOTP or recovery code
type MFASudoCodeRequest struct {
	Code string `json:"code"`
}

// WebauthnSudoFinishRequest completes a WebAuthn re-auth ceremony
type WebauthnSudoFinishRequest struct {
	SessionID  string          `json:"session_id"`
	Credential json.RawMessage `json:"credential" swaggertype:"object"`
}

// TOTPEnrollResponse is returned when a TOTP enrollment starts. Secret and
// OtpauthURL are shown exactly once; the enrollment stays pending until the
// first valid code is posted to /auth/mfa/totp/confirm.
type TOTPEnrollResponse struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauth_url"`
}

// TOTPConfirmRequest confirms a pending TOTP enrollment with a first code
type TOTPConfirmRequest struct {
	Code string `json:"code"`
}

// RecoveryCodesResponse carries a freshly generated recovery code set; shown
// exactly once
type RecoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// MFAStatusTOTP TOTP section of MFAStatusResponse
type MFAStatusTOTP struct {
	Enabled bool `json:"enabled"`
	Pending bool `json:"pending"`
}

// WebauthnCredentialInfo public metadata of a registered security key or
// passkey (never includes key material)
type WebauthnCredentialInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	IsPasskey   bool   `json:"is_passkey"`
	TimeCreated string `json:"time-created"`
	LastUsedAt  string `json:"last_used_at,omitempty"`
}

// MFAStatusResponse describes the caller's MFA state for the settings UI.
// PasswordSet tells the UI whether to ask for the password or to re-prove a
// factor (sudo) when authorizing sensitive changes.
type MFAStatusResponse struct {
	MFAEnabled             bool                     `json:"mfa_enabled"`
	PasswordSet            bool                     `json:"password_set"`
	TOTP                   MFAStatusTOTP            `json:"totp"`
	Webauthn               []WebauthnCredentialInfo `json:"webauthn"`
	RecoveryCodesRemaining int                      `json:"recovery_codes_remaining"`
}

// WebauthnRegisterRequest starts a security key / passkey registration
type WebauthnRegisterRequest struct {
	Password  string `json:"password"`
	SudoToken string `json:"sudo_token"`
	Passkey   bool   `json:"passkey"`
}

// WebauthnOptionsResponse carries the ceremony options for the browser and
// the server-side session id that must come back with the client response
type WebauthnOptionsResponse struct {
	SessionID string      `json:"session_id"`
	Options   interface{} `json:"options"`
}

// WebauthnRegisterFinishRequest completes a registration ceremony.
// Credential is the JSON PublicKeyCredential produced by the browser.
type WebauthnRegisterFinishRequest struct {
	SessionID  string          `json:"session_id"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential" swaggertype:"object"`
}

// WebauthnRegisterFinishResponse returns the stored credential metadata and,
// when this registration enabled MFA, the fresh recovery codes (shown once)
type WebauthnRegisterFinishResponse struct {
	Credential    WebauthnCredentialInfo `json:"credential"`
	RecoveryCodes []string               `json:"recovery_codes,omitempty"`
}

// WebauthnRenameRequest renames a registered credential
type WebauthnRenameRequest struct {
	Name string `json:"name"`
}

// WebauthnLoginRequest starts the WebAuthn second factor of a pending login
type WebauthnLoginRequest struct {
	MFAToken string `json:"mfa_token"`
}

// WebauthnLoginFinishRequest completes the WebAuthn second factor
type WebauthnLoginFinishRequest struct {
	MFAToken   string          `json:"mfa_token"`
	SessionID  string          `json:"session_id"`
	Credential json.RawMessage `json:"credential" swaggertype:"object"`
}

// PasskeyLoginFinishRequest completes a usernameless passkey sign-in
type PasskeyLoginFinishRequest struct {
	SessionID  string          `json:"session_id"`
	Credential json.RawMessage `json:"credential" swaggertype:"object"`
}
