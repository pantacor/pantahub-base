package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/auth/oauth"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	oauthConnectPRNCookie = "oauth_connect_prn"
	oauthConnectCookieTTL = 10 * time.Minute
)

type oauthConnectCookiePayload struct {
	PRN      string `json:"prn"`
	IssuedAt int64  `json:"iat"`
}

type connectedProviderConnectRequest struct {
	Service    string `json:"service"`
	RedirectTo string `json:"redirect_uri"`
}

type connectedProviderDisconnectRequest struct {
	Service    string `json:"service"`
	ProviderID string `json:"provider_id"`
}

type connectedProviderResponse struct {
	Service     string    `json:"service"`
	Email       string    `json:"email,omitempty"`
	ConnectedAt time.Time `json:"connected_at,omitempty"`
}

func oauthConnectCookieKey() []byte {
	key := sha256.Sum256([]byte("pantahub-oauth-connect-cookie:" + utils.GetEnv(utils.EnvPantahubJWTAuthSecret)))
	return key[:]
}

func encodeOAuthConnectCookie(prn string, issuedAt time.Time) (string, error) {
	if prn == "" {
		return "", errors.New("account PRN is required")
	}

	payload, err := json.Marshal(oauthConnectCookiePayload{PRN: prn, IssuedAt: issuedAt.Unix()})
	if err != nil {
		return "", err
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, oauthConnectCookieKey())
	_, _ = mac.Write([]byte(payloadEncoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payloadEncoded + "." + signature, nil
}

func decodeOAuthConnectCookie(value string, now time.Time) (string, error) {
	payloadEncoded, signatureEncoded, found := strings.Cut(value, ".")
	if !found || payloadEncoded == "" || signatureEncoded == "" {
		return "", errors.New("malformed OAuth connect cookie")
	}

	provided, err := base64.RawURLEncoding.DecodeString(signatureEncoded)
	if err != nil {
		return "", errors.New("malformed OAuth connect cookie signature")
	}
	mac := hmac.New(sha256.New, oauthConnectCookieKey())
	_, _ = mac.Write([]byte(payloadEncoded))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return "", errors.New("invalid OAuth connect cookie signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadEncoded)
	if err != nil {
		return "", errors.New("malformed OAuth connect cookie payload")
	}
	payload := oauthConnectCookiePayload{}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil || payload.PRN == "" {
		return "", errors.New("malformed OAuth connect cookie payload")
	}
	issuedAt := time.Unix(payload.IssuedAt, 0)
	if issuedAt.After(now.Add(time.Minute)) || now.Sub(issuedAt) > oauthConnectCookieTTL {
		return "", errors.New("expired OAuth connect cookie")
	}

	return payload.PRN, nil
}

func setOAuthConnectCookie(w http.ResponseWriter, prn string) error {
	value, err := encodeOAuthConnectCookie(prn, time.Now())
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthConnectPRNCookie,
		Value:    value,
		Path:     "/",
		Expires:  time.Now().Add(oauthConnectCookieTTL),
		MaxAge:   int(oauthConnectCookieTTL / time.Second),
		HttpOnly: true,
		Secure:   utils.GetEnv(utils.EnvPantahubScheme) == "https",
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func getOAuthConnectPRN(r *rest.Request) (string, bool, error) {
	value := utils.GetCookie(r, oauthConnectPRNCookie)
	if value == "" {
		return "", false, nil
	}
	prn, err := decodeOAuthConnectCookie(value, time.Now())
	return prn, true, err
}

func socialConnectAccountPRN(r *rest.Request) (string, error) {
	authInfo := utils.GetAuthInfo(r)
	if authInfo == nil || authInfo.Caller == "" {
		return "", errors.New("you need to be logged in")
	}
	return string(authInfo.Caller), nil
}

// handleGetConnectedProviders lists the external identities connected to the
// authenticated account.
func (a *App) handleGetConnectedProviders(w rest.ResponseWriter, r *rest.Request) {
	accountPRN, err := socialConnectAccountPRN(r)
	if err != nil {
		utils.RestError(w, err, err.Error(), http.StatusUnauthorized)
		return
	}

	providers, err := listConnectedProviders(r.Context(), accountPRN, a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts"))
	if err != nil {
		status := http.StatusInternalServerError
		if err == mongo.ErrNoDocuments {
			status = http.StatusNotFound
		}
		utils.RestError(w, err, "Unable to list connected providers", status)
		return
	}
	response := make([]connectedProviderResponse, 0, len(providers))
	for _, provider := range providers {
		response = append(response, connectedProviderResponse{
			Service:     provider.Service,
			Email:       provider.Email,
			ConnectedAt: provider.ConnectedAt,
		})
	}
	w.WriteJson(response)
}

// handlePostConnectedProvider starts an authenticated OAuth connect flow. The
// signed cookie binds the callback to the account that initiated this request;
// the callback never trusts an account PRN supplied by the browser.
func (a *App) handlePostConnectedProvider(w rest.ResponseWriter, r *rest.Request) {
	accountPRN, err := socialConnectAccountPRN(r)
	if err != nil {
		utils.RestError(w, err, err.Error(), http.StatusUnauthorized)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	account, err := getUserByPRN(r.Context(), accountPRN, collection)
	if err != nil {
		utils.RestError(w, err, "Account not found", http.StatusUnauthorized)
		return
	}
	if account.Type != accounts.AccountTypeUser && account.Type != accounts.AccountTypeAdmin {
		utils.RestError(w, nil, "This account type cannot connect OAuth providers", http.StatusForbidden)
		return
	}

	payload := connectedProviderConnectRequest{}
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		if err := r.DecodeJsonPayload(&payload); err != nil {
			utils.RestError(w, err, "Invalid connect payload", http.StatusBadRequest)
			return
		}
	} else {
		_ = r.ParseForm()
		payload.Service = r.FormValue("service")
		payload.RedirectTo = r.FormValue("redirect_uri")
	}

	service := oauth.ServiceType(strings.ToLower(strings.TrimSpace(payload.Service)))
	if _, ok := oauth.ServicesConfigs[service]; !ok {
		utils.RestError(w, nil, "We can't connect to that service", http.StatusBadRequest)
		return
	}

	redirectTo := payload.RedirectTo
	if queryRedirect := r.URL.Query().Get("redirect_uri"); queryRedirect != "" {
		redirectTo = queryRedirect
	}
	if redirectTo != "" {
		audit := auditContext(r, "social_connect")
		audit.Service = string(service)
		if err := validateSocialRedirectURI(redirectTo, audit); err != nil {
			utils.RestError(w, err, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if err := setOAuthConnectCookie(w, accountPRN); err != nil {
		utils.RestError(w, err, "Unable to start OAuth connect flow", http.StatusInternalServerError)
		return
	}

	authorizeURL, err := oauth.AuthorizationURLByService(service, redirectTo, w)
	if err != nil {
		utils.RestError(w, err, "Unable to start OAuth connect flow", http.StatusInternalServerError)
		return
	}
	w.WriteJson(map[string]string{"authorize_url": authorizeURL})
}

// handleDeleteConnectedProvider disconnects one provider identity. A missing
// provider_id is accepted for compatibility and removes all identities for
// the requested service.
func (a *App) handleDeleteConnectedProvider(w rest.ResponseWriter, r *rest.Request) {
	accountPRN, err := socialConnectAccountPRN(r)
	if err != nil {
		utils.RestError(w, err, err.Error(), http.StatusUnauthorized)
		return
	}

	payload := connectedProviderDisconnectRequest{}
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		if err := r.DecodeJsonPayload(&payload); err != nil {
			utils.RestError(w, err, "Invalid disconnect payload", http.StatusBadRequest)
			return
		}
	} else {
		_ = r.ParseForm()
		payload.Service = r.FormValue("service")
		payload.ProviderID = r.FormValue("provider_id")
	}
	if payload.Service == "" {
		payload.Service = r.URL.Query().Get("service")
	}
	if payload.ProviderID == "" {
		payload.ProviderID = r.URL.Query().Get("provider_id")
	}
	service := strings.ToLower(strings.TrimSpace(payload.Service))
	if service == "" {
		utils.RestError(w, nil, "Provider service is required", http.StatusBadRequest)
		return
	}

	if err := disconnectProvider(r.Context(), accountPRN, service, payload.ProviderID,
		a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")); err != nil {
		status := http.StatusInternalServerError
		if err == mongo.ErrNoDocuments {
			status = http.StatusNotFound
		}
		utils.RestError(w, err, "Connected provider not found", status)
		return
	}
	w.WriteJson(true)
}

func redirectAfterProviderConnect(w rest.ResponseWriter, r *rest.Request, redirectTo string, provider accounts.ConnectedProvider) {
	if redirectTo == "" {
		w.WriteJson(provider)
		return
	}
	u, err := url.Parse(redirectTo)
	if err != nil {
		utils.RestError(w, err, "Invalid redirect URI", http.StatusBadRequest)
		return
	}
	query := u.Query()
	query.Set("connected_provider", provider.Service)
	u.RawQuery = query.Encode()
	http.Redirect(w, r.Request, u.String(), http.StatusTemporaryRedirect)
}
