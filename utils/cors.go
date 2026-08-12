package utils

import (
	"net/url"
	"strings"

	"github.com/ant0ine/go-json-rest/rest"
)

// AllowlistedOrigin checks if the given origin is allowed.
func AllowlistedOrigin(origin string, request *rest.Request) bool {
	if origin == "" {
		return false
	}

	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}

	// 1. Check configured origins in PANTAHUB_WEBAUTHN_RP_ORIGINS or PANTAHUB_CORS_ALLOWED_ORIGINS
	configured := GetEnv(EnvPantahubWebauthnRPOrigins)
	if configured != "" {
		for _, o := range strings.Split(configured, ",") {
			o = strings.TrimSpace(o)
			if o != "" && strings.EqualFold(o, origin) {
				return true
			}
		}
	}

	corsOrigins := GetEnv("PANTAHUB_CORS_ALLOWED_ORIGINS")
	if corsOrigins != "" {
		for _, o := range strings.Split(corsOrigins, ",") {
			o = strings.TrimSpace(o)
			if o != "" && strings.EqualFold(o, origin) {
				return true
			}
		}
	}

	// 2. Check against host and www host configuration
	host := GetEnv(EnvPantahubHost)
	wwwHost := GetEnv(EnvPantahubWWWHost)

	hostname := u.Hostname()

	if host != "" && (strings.EqualFold(hostname, host) || strings.EqualFold(u.Host, host)) {
		return true
	}
	if wwwHost != "" && (strings.EqualFold(hostname, wwwHost) || strings.EqualFold(u.Host, wwwHost)) {
		return true
	}

	// 3. Default local development origins
	if strings.EqualFold(hostname, "localhost") || strings.EqualFold(hostname, "127.0.0.1") {
		return true
	}

	return false
}
