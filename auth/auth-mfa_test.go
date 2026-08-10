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

// Package auth package to manage extensions of the oauth protocol
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"gitlab.com/pantacor/pantahub-base/auth/mfaservice"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/testutils"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

type mfaTestEnv struct {
	server    *httptest.Server
	serverURL *url.URL
	mongo     *mongo.Client
}

func mfaSetUp(t *testing.T) *mfaTestEnv {
	t.Helper()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv(utils.EnvPantahubMfaEncKey, base64.StdEncoding.EncodeToString(key))
	t.Setenv(utils.EnvPantahubMfaEnabled, "true")
	// error responses log incidents to fluentd and getLogger() fatals when
	// the (defaulted) endpoint is unreachable; disable it for tests
	t.Setenv(utils.EnvFluentPort, "")

	mongoClient, err := utils.GetMongoClientTest()
	if err != nil {
		t.Fatalf("error getting mongoClient (%s)", err.Error())
	}

	// clean MFA state so runs are reproducible
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := mongoClient.Database(utils.MongoDb)
	_ = db.Collection(storage.MFASettingsCollection).Drop(ctx)
	_ = db.Collection(storage.MFAUsedJTICollection).Drop(ctx)

	jwtMW := &jwt.JWTMiddleware{
		Key:        []byte("secret key"),
		Realm:      "pantahub services",
		Timeout:    time.Minute * 60,
		MaxRefresh: time.Hour * 24,
	}

	app := New(jwtMW, mongoClient)
	server := httptest.NewServer(app.API.MakeHandler())
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("error parsing test server URL: %s", err.Error())
	}

	return &mfaTestEnv{server: server, serverURL: serverURL, mongo: mongoClient}
}

func (e *mfaTestEnv) url(path string) string {
	u := *e.serverURL
	u.Path = path
	return u.String()
}

func mfaCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{
		Period:    mfaservice.TOTPPeriod,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func parseJSON(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	result := map[string]interface{}{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("bad json from server: %s: %s", err.Error(), string(body))
	}
	return result
}

// enrollTOTP drives a full enrollment for a user and returns the totp
// secret and the plaintext recovery codes
func enrollTOTP(t *testing.T, e *mfaTestEnv, username, password string) (string, []string) {
	t.Helper()

	token := testutils.DoLogin(t, e.serverURL, username, password)

	res, err := utils.R().SetAuthToken(token).
		SetBody(map[string]string{"password": password}).
		Post(e.url("/mfa/totp"))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode() != http.StatusOK {
		t.Fatalf("totp enroll must yield 200, got %d: %s", res.StatusCode(), res.Body())
	}

	enroll := parseJSON(t, res.Body())
	secret, _ := enroll["secret"].(string)
	if secret == "" {
		t.Fatal("enroll response missed the secret")
	}
	if otpauth, _ := enroll["otpauth_url"].(string); otpauth == "" {
		t.Fatal("enroll response missed the otpauth_url")
	}

	res, err = utils.R().SetAuthToken(token).
		SetBody(map[string]string{"code": mfaCode(t, secret, time.Now())}).
		Post(e.url("/mfa/totp/confirm"))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode() != http.StatusOK {
		t.Fatalf("totp confirm must yield 200, got %d: %s", res.StatusCode(), res.Body())
	}

	confirm := parseJSON(t, res.Body())
	rawCodes, _ := confirm["recovery_codes"].([]interface{})
	codes := []string{}
	for _, c := range rawCodes {
		codes = append(codes, c.(string))
	}
	if len(codes) != mfaservice.RecoveryCodeCount {
		t.Fatalf("expected %d recovery codes, got %d", mfaservice.RecoveryCodeCount, len(codes))
	}

	return secret, codes
}

// startMFALogin does the password step and returns the pending mfa_token
func startMFALogin(t *testing.T, e *mfaTestEnv, username, password string) string {
	t.Helper()

	res, err := utils.R().SetBody(map[string]string{
		"username": username,
		"password": password,
	}).Post(e.url("/login"))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode() != http.StatusOK {
		t.Fatalf("mfa login step 1 must yield 200, got %d: %s", res.StatusCode(), res.Body())
	}

	body := parseJSON(t, res.Body())
	if required, _ := body["mfa_required"].(bool); !required {
		t.Fatalf("expected mfa_required response, got: %s", res.Body())
	}
	if token, ok := body["token"]; ok && token != "" {
		t.Fatalf("mfa_required response must not contain a session token: %s", res.Body())
	}

	mfaToken, _ := body["mfa_token"].(string)
	if mfaToken == "" {
		t.Fatal("mfa_required response missed mfa_token")
	}

	return mfaToken
}

func TestMFATOTPFlow(t *testing.T) {
	e := mfaSetUp(t)

	var secret string
	var recoveryCodes []string
	var sessionToken string

	t.Run("status requires auth", func(t *testing.T) {
		res, err := utils.R().Get(e.url("/mfa"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() == http.StatusOK {
			t.Errorf("unauthenticated /mfa must not yield 200, got %d", res.StatusCode())
		}
	})

	t.Run("status starts disabled", func(t *testing.T) {
		token := testutils.DoLogin(t, e.serverURL, "user1", "user1")
		res, err := utils.R().SetAuthToken(token).Get(e.url("/mfa"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Fatalf("/mfa must yield 200, got %d: %s", res.StatusCode(), res.Body())
		}
		body := parseJSON(t, res.Body())
		if enabled, _ := body["mfa_enabled"].(bool); enabled {
			t.Error("mfa must start disabled")
		}
	})

	t.Run("enroll rejects wrong password", func(t *testing.T) {
		token := testutils.DoLogin(t, e.serverURL, "user1", "user1")
		res, err := utils.R().SetAuthToken(token).
			SetBody(map[string]string{"password": "WRONG"}).
			Post(e.url("/mfa/totp"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusUnauthorized {
			t.Errorf("enroll with wrong password must yield 401, got %d", res.StatusCode())
		}
	})

	t.Run("enroll and confirm", func(t *testing.T) {
		secret, recoveryCodes = enrollTOTP(t, e, "user1", "user1")
	})

	t.Run("login now requires mfa", func(t *testing.T) {
		mfaToken := startMFALogin(t, e, "user1", "user1")

		// wrong code fails
		res, err := utils.R().SetBody(map[string]string{
			"mfa_token": mfaToken,
			"code":      "000000",
		}).Post(e.url("/login/mfa/totp"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusUnauthorized {
			t.Errorf("wrong totp code must yield 401, got %d", res.StatusCode())
		}

		// garbage pending token fails
		res, err = utils.R().SetBody(map[string]string{
			"mfa_token": "not-a-token",
			"code":      mfaCode(t, secret, time.Now().Add(mfaservice.TOTPPeriod*time.Second)),
		}).Post(e.url("/login/mfa/totp"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusUnauthorized {
			t.Errorf("garbage mfa_token must yield 401, got %d", res.StatusCode())
		}

		// next-step code succeeds (the enrollment consumed the current step)
		code := mfaCode(t, secret, time.Now().Add(mfaservice.TOTPPeriod*time.Second))
		res, err = utils.R().SetBody(map[string]string{
			"mfa_token": mfaToken,
			"code":      code,
		}).Post(e.url("/login/mfa/totp"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Fatalf("totp step-up must yield 200, got %d: %s", res.StatusCode(), res.Body())
		}
		body := parseJSON(t, res.Body())
		sessionToken, _ = body["token"].(string)
		if sessionToken == "" {
			t.Fatal("step-up response missed the session token")
		}

		// the minted session works against the API
		res, err = utils.R().SetAuthToken(sessionToken).Get(e.url("/auth_status"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Errorf("session token from step-up must work, got %d", res.StatusCode())
		}
		status := parseJSON(t, res.Body())
		if prn, _ := status["prn"].(string); prn != "prn:pantahub.com:auth:/user1" {
			t.Errorf("unexpected prn in auth_status: %v", status["prn"])
		}

		// the pending token is single use
		res, err = utils.R().SetBody(map[string]string{
			"mfa_token": mfaToken,
			"code":      mfaCode(t, secret, time.Now().Add(2*mfaservice.TOTPPeriod*time.Second)),
		}).Post(e.url("/login/mfa/totp"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusUnauthorized {
			t.Errorf("reused mfa_token must yield 401, got %d", res.StatusCode())
		}
	})

	t.Run("totp code replay is rejected", func(t *testing.T) {
		// the +1 step code was consumed by the previous login
		code := mfaCode(t, secret, time.Now().Add(mfaservice.TOTPPeriod*time.Second))
		mfaToken := startMFALogin(t, e, "user1", "user1")
		res, err := utils.R().SetBody(map[string]string{
			"mfa_token": mfaToken,
			"code":      code,
		}).Post(e.url("/login/mfa/totp"))
		if err != nil {
			t.Fatal(err)
		}
		// the same step can never be accepted twice
		if res.StatusCode() == http.StatusOK {
			t.Error("replayed totp step must be rejected")
		}
	})

	t.Run("mfa pending token is no session", func(t *testing.T) {
		mfaToken := startMFALogin(t, e, "user1", "user1")
		res, err := utils.R().SetAuthToken(mfaToken).Get(e.url("/auth_status"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() == http.StatusOK {
			t.Error("mfa pending token must not be usable as a session token")
		}

		res, err = utils.R().SetAuthToken(mfaToken).Get(e.url("/mfa"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() == http.StatusOK {
			t.Error("mfa pending token must not read mfa settings")
		}
	})

	t.Run("recovery code login", func(t *testing.T) {
		mfaToken := startMFALogin(t, e, "user1", "user1")
		res, err := utils.R().SetBody(map[string]string{
			"mfa_token": mfaToken,
			"code":      recoveryCodes[0],
		}).Post(e.url("/login/mfa/recovery"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Fatalf("recovery login must yield 200, got %d: %s", res.StatusCode(), res.Body())
		}
		body := parseJSON(t, res.Body())
		if token, _ := body["token"].(string); token == "" {
			t.Fatal("recovery login response missed the session token")
		}

		// same code again fails (single use)
		mfaToken = startMFALogin(t, e, "user1", "user1")
		res, err = utils.R().SetBody(map[string]string{
			"mfa_token": mfaToken,
			"code":      recoveryCodes[0],
		}).Post(e.url("/login/mfa/recovery"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusUnauthorized {
			t.Errorf("used recovery code must yield 401, got %d", res.StatusCode())
		}
	})

	t.Run("recovery codes count decreases", func(t *testing.T) {
		// login requires mfa now: reuse the session minted by the step-up
		res, err := utils.R().SetAuthToken(sessionToken).Get(e.url("/mfa"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Fatalf("/mfa must yield 200, got %d: %s", res.StatusCode(), res.Body())
		}
		body := parseJSON(t, res.Body())
		remaining, _ := body["recovery_codes_remaining"].(float64)
		if int(remaining) != mfaservice.RecoveryCodeCount-1 {
			t.Errorf("expected %d remaining codes, got %v", mfaservice.RecoveryCodeCount-1, remaining)
		}
	})

	t.Run("regenerate recovery codes", func(t *testing.T) {
		res, err := utils.R().SetAuthToken(sessionToken).
			SetBody(map[string]string{"password": "user1"}).
			Post(e.url("/mfa/recovery/regenerate"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Fatalf("regenerate must yield 200, got %d: %s", res.StatusCode(), res.Body())
		}
		body := parseJSON(t, res.Body())
		fresh, _ := body["recovery_codes"].([]interface{})
		if len(fresh) != mfaservice.RecoveryCodeCount {
			t.Fatalf("expected %d fresh codes, got %d", mfaservice.RecoveryCodeCount, len(fresh))
		}

		// an old (still unused) code is now invalid
		mfaToken := startMFALogin(t, e, "user1", "user1")
		res, err = utils.R().SetBody(map[string]string{
			"mfa_token": mfaToken,
			"code":      recoveryCodes[1],
		}).Post(e.url("/login/mfa/recovery"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusUnauthorized {
			t.Errorf("regeneration must invalidate old codes, got %d", res.StatusCode())
		}

		// a fresh code works
		mfaToken = startMFALogin(t, e, "user1", "user1")
		res, err = utils.R().SetBody(map[string]string{
			"mfa_token": mfaToken,
			"code":      fresh[0].(string),
		}).Post(e.url("/login/mfa/recovery"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Errorf("fresh recovery code must work, got %d: %s", res.StatusCode(), res.Body())
		}
	})

	t.Run("disable totp restores single-step login", func(t *testing.T) {
		res, err := utils.R().SetAuthToken(sessionToken).
			SetBody(map[string]string{"password": "user1"}).
			Delete(e.url("/mfa/totp"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusNoContent {
			t.Fatalf("disable totp must yield 204, got %d: %s", res.StatusCode(), res.Body())
		}

		res, err = utils.R().SetAuthToken(sessionToken).Get(e.url("/mfa"))
		if err != nil {
			t.Fatal(err)
		}
		body := parseJSON(t, res.Body())
		if enabled, _ := body["mfa_enabled"].(bool); enabled {
			t.Error("mfa must be disabled after removing the only factor")
		}

		// normal login again yields a session token directly
		token := testutils.DoLogin(t, e.serverURL, "user1", "user1")
		if token == "" {
			t.Error("single-step login must work again")
		}
	})
}

func TestMFALockout(t *testing.T) {
	e := mfaSetUp(t)

	secret, _ := enrollTOTP(t, e, "user2", "user2")
	_ = secret

	sawLock := false
	for i := 0; i < mfaservice.MaxMFAFailures+1; i++ {
		mfaToken := startMFALogin(t, e, "user2", "user2")
		res, err := utils.R().SetBody(map[string]string{
			"mfa_token": mfaToken,
			"code":      "000000",
		}).Post(e.url("/login/mfa/totp"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() == http.StatusTooManyRequests {
			sawLock = true
			break
		}
		if res.StatusCode() != http.StatusUnauthorized {
			t.Fatalf("failed proof must yield 401 or 429, got %d", res.StatusCode())
		}
	}

	if !sawLock {
		t.Fatal("lockout must engage after repeated failures")
	}

	// even a valid code is rejected while locked
	mfaToken := startMFALogin(t, e, "user2", "user2")
	res, err := utils.R().SetBody(map[string]string{
		"mfa_token": mfaToken,
		"code":      mfaCode(t, secret, time.Now()),
	}).Post(e.url("/login/mfa/totp"))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode() != http.StatusTooManyRequests {
		t.Errorf("locked mfa step must yield 429 even for valid codes, got %d", res.StatusCode())
	}
}

func TestMFAWebauthnEndpoints(t *testing.T) {
	e := mfaSetUp(t)
	t.Setenv(utils.EnvPantahubWebauthnRPID, "localhost")
	t.Setenv(utils.EnvPantahubWebauthnRPOrigins, "http://localhost")

	ctx := context.Background()
	db := e.mongo.Database(utils.MongoDb)
	_ = db.Collection(storage.WebauthnCredentialsCollection).Drop(ctx)
	_ = db.Collection(storage.WebauthnSessionsCollection).Drop(ctx)

	token := testutils.DoLogin(t, e.serverURL, "user3", "user3")

	t.Run("register begin rejects wrong password", func(t *testing.T) {
		res, err := utils.R().SetAuthToken(token).
			SetBody(map[string]interface{}{"password": "WRONG", "passkey": false}).
			Post(e.url("/mfa/webauthn/register"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusUnauthorized {
			t.Errorf("register with wrong password must yield 401, got %d", res.StatusCode())
		}
	})

	var registerSession string

	t.Run("register begin returns creation options", func(t *testing.T) {
		res, err := utils.R().SetAuthToken(token).
			SetBody(map[string]interface{}{"password": "user3", "passkey": true}).
			Post(e.url("/mfa/webauthn/register"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Fatalf("register begin must yield 200, got %d: %s", res.StatusCode(), res.Body())
		}

		body := parseJSON(t, res.Body())
		registerSession, _ = body["session_id"].(string)
		if registerSession == "" {
			t.Fatal("register begin missed session_id")
		}

		options, _ := body["options"].(map[string]interface{})
		publicKey, _ := options["publicKey"].(map[string]interface{})
		if publicKey == nil {
			t.Fatalf("register begin missed options.publicKey: %s", res.Body())
		}
		rp, _ := publicKey["rp"].(map[string]interface{})
		if rp["id"] != "localhost" {
			t.Errorf("unexpected rp.id: %v", rp["id"])
		}
		sel, _ := publicKey["authenticatorSelection"].(map[string]interface{})
		if sel["residentKey"] != "required" || sel["userVerification"] != "required" {
			t.Errorf("passkey registration must require resident key + uv: %v", sel)
		}
		if _, hasUser := publicKey["user"].(map[string]interface{}); !hasUser {
			t.Error("register begin missed user entity")
		}
	})

	t.Run("register finish rejects garbage credential", func(t *testing.T) {
		res, err := utils.R().SetAuthToken(token).
			SetBody(map[string]interface{}{
				"session_id": registerSession,
				"name":       "My key",
				"credential": map[string]interface{}{"id": "nope"},
			}).
			Post(e.url("/mfa/webauthn/register/finish"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() == http.StatusOK {
			t.Error("garbage credential must not register")
		}
	})

	t.Run("webauthn session is single use", func(t *testing.T) {
		// the previous (failed) finish consumed the session
		res, err := utils.R().SetAuthToken(token).
			SetBody(map[string]interface{}{
				"session_id": registerSession,
				"name":       "My key",
				"credential": map[string]interface{}{"id": "nope"},
			}).
			Post(e.url("/mfa/webauthn/register/finish"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusUnauthorized {
			t.Errorf("consumed session must yield 401, got %d", res.StatusCode())
		}
	})

	t.Run("simulated credential enables webauthn step-up", func(t *testing.T) {
		// a full ceremony needs a real authenticator (covered by the browser
		// E2E tests); simulate the stored outcome to exercise the login path
		mfaRepo := storage.NewMFARepo(e.mongo)
		settings, err := mfaRepo.GetByOwner(ctx, "prn:pantahub.com:auth:/user3")
		if err != nil || settings == nil {
			t.Fatalf("expected settings after register begin: %v", err)
		}

		webauthnRepo := storage.NewWebauthnRepo(e.mongo)
		err = webauthnRepo.CreateCredential(ctx, &storage.WebauthnCredential{
			Owner:     "prn:pantahub.com:auth:/user3",
			Name:      "Simulated key",
			IsPasskey: false,
			Credential: webauthn.Credential{
				ID:        []byte("simulated-credential-id"),
				PublicKey: []byte("simulated-public-key"),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := mfaRepo.SetEnabled(ctx, "prn:pantahub.com:auth:/user3", true); err != nil {
			t.Fatal(err)
		}

		// password step now returns the challenge with webauthn offered
		res, err := utils.R().SetBody(map[string]string{
			"username": "user3",
			"password": "user3",
		}).Post(e.url("/login"))
		if err != nil {
			t.Fatal(err)
		}
		body := parseJSON(t, res.Body())
		if required, _ := body["mfa_required"].(bool); !required {
			t.Fatalf("expected mfa_required, got: %s", res.Body())
		}
		methods, _ := body["methods"].([]interface{})
		hasWebauthn := false
		for _, m := range methods {
			if m == "webauthn" {
				hasWebauthn = true
			}
		}
		if !hasWebauthn {
			t.Fatalf("methods must include webauthn: %v", methods)
		}
		mfaToken, _ := body["mfa_token"].(string)

		// assertion options list the registered credential
		res, err = utils.R().SetBody(map[string]string{"mfa_token": mfaToken}).
			Post(e.url("/login/mfa/webauthn"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Fatalf("webauthn login begin must yield 200, got %d: %s", res.StatusCode(), res.Body())
		}
		beginBody := parseJSON(t, res.Body())
		sessionID, _ := beginBody["session_id"].(string)
		options, _ := beginBody["options"].(map[string]interface{})
		publicKey, _ := options["publicKey"].(map[string]interface{})
		allowed, _ := publicKey["allowCredentials"].([]interface{})
		if len(allowed) != 1 {
			t.Errorf("expected 1 allowed credential, got %v", allowed)
		}

		// garbage assertion fails and counts as an mfa failure
		res, err = utils.R().SetBody(map[string]interface{}{
			"mfa_token":  mfaToken,
			"session_id": sessionID,
			"credential": map[string]interface{}{"id": "nope"},
		}).Post(e.url("/login/mfa/webauthn/finish"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() == http.StatusOK {
			t.Error("garbage assertion must not log in")
		}
	})

	t.Run("passkey login begin is public", func(t *testing.T) {
		res, err := utils.R().SetBody(map[string]string{}).
			Post(e.url("/login/webauthn/begin"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Fatalf("passkey begin must yield 200, got %d: %s", res.StatusCode(), res.Body())
		}
		body := parseJSON(t, res.Body())
		if sid, _ := body["session_id"].(string); sid == "" {
			t.Error("passkey begin missed session_id")
		}
		options, _ := body["options"].(map[string]interface{})
		publicKey, _ := options["publicKey"].(map[string]interface{})
		if publicKey["userVerification"] != "required" {
			t.Errorf("passkey assertion must require uv: %v", publicKey["userVerification"])
		}
	})

	t.Run("passkey login finish rejects garbage", func(t *testing.T) {
		res, err := utils.R().SetBody(map[string]string{}).
			Post(e.url("/login/webauthn/begin"))
		if err != nil {
			t.Fatal(err)
		}
		body := parseJSON(t, res.Body())
		sessionID, _ := body["session_id"].(string)

		res, err = utils.R().SetBody(map[string]interface{}{
			"session_id": sessionID,
			"credential": map[string]interface{}{"id": "nope"},
		}).Post(e.url("/login/webauthn/finish"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() == http.StatusOK {
			t.Error("garbage passkey assertion must not log in")
		}
	})

	t.Run("webauthn unconfigured yields 501", func(t *testing.T) {
		t.Setenv(utils.EnvPantahubWebauthnRPID, "")
		res, err := utils.R().SetBody(map[string]string{}).
			Post(e.url("/login/webauthn/begin"))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != http.StatusNotImplemented {
			t.Errorf("unconfigured webauthn must yield 501, got %d", res.StatusCode())
		}
	})
}

func TestMFADisabledFeatureFlag(t *testing.T) {
	e := mfaSetUp(t)
	t.Setenv(utils.EnvPantahubMfaEnabled, "false")

	token := testutils.DoLogin(t, e.serverURL, "user1", "user1")
	res, err := utils.R().SetAuthToken(token).Get(e.url("/mfa"))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode() != http.StatusNotImplemented {
		t.Errorf("disabled feature must yield 501, got %d", res.StatusCode())
	}
}
