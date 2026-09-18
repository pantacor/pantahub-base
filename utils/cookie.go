// Copyright (c) 2017-2026 Pantacor Ltd.
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

package utils

import (
	"net/http"
	"strings"
	"time"
)

// IsSecureRequest reports whether the response to r reaches the client over
// TLS, and so whether cookies set on it must carry the Secure attribute.
//
// This used to be decided with r.URL.Scheme == "https", which is never true
// for a server-side request: net/http documents that for requests received by
// a server, "fields other than Path and RawQuery will be empty". Every cookie
// set through here was therefore missing Secure, including over HTTPS.
//
// The deployment's own declared scheme is the authoritative answer and is not
// client-controllable, so it is checked first; TLS terminates at the ingress,
// which means r.TLS is nil in production and cannot be relied on alone.
// X-Forwarded-Proto is only ever consulted to turn Secure on, never off.
func IsSecureRequest(r *http.Request) bool {
	if strings.EqualFold(GetEnv(EnvPantahubScheme), "https") {
		return true
	}

	if r == nil {
		return false
	}

	if r.TLS != nil {
		return true
	}

	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// GetCookie retrieves a cookie by its name.
// It returns the cookie's value or an error if the cookie is not found.
func GetCookie(r *http.Request, name string) string {
	var cookie string
	if c, err := r.Cookie(name); err == nil {
		cookie = c.Value
	}
	return cookie
}

// CookieOption defines a functional option for SetCookie, allowing customization of cookie properties.
type CookieOption func(*http.Cookie)

// WithMaxAge sets the MaxAge for the cookie in seconds.
// A value of 0 means a session cookie. A value of -1 means to delete the cookie immediately.
// If WithExpires is also used, the Expires attribute takes precedence for most browsers.
func WithMaxAge(maxAge int) CookieOption {
	//#nosec G124 -- Secure is set from IsSecureRequest, which gosec cannot follow
	return func(c *http.Cookie) {
		c.MaxAge = maxAge
		if maxAge < 0 {
			// Explicitly set Expires to a past date for immediate deletion when MaxAge is negative.
			// This ensures immediate deletion even if the browser doesn't fully respect MaxAge -1
			// or if another Expires value was set before this option.
			c.Expires = time.Unix(0, 0)
		}
	}
}

// WithExpires sets the Expires attribute for the cookie to a specific time.
// This attribute specifies a date and time at which the cookie will expire.
// If both Expires and MaxAge are set, Expires takes precedence for most browsers.
func WithExpires(expires time.Time) CookieOption {
	//#nosec G124 -- Secure is set from IsSecureRequest, which gosec cannot follow
	return func(c *http.Cookie) {
		c.Expires = expires
	}
}

// WithHttpOnly sets the HttpOnly flag for the cookie.
func WithHttpOnly(httpOnly bool) CookieOption {
	//#nosec G124 -- Secure is set from IsSecureRequest, which gosec cannot follow
	return func(c *http.Cookie) {
		c.HttpOnly = httpOnly
	}
}

// WithSameSite sets the SameSite policy for the cookie.
func WithSameSite(sameSite http.SameSite) CookieOption {
	//#nosec G124 -- Secure is set from IsSecureRequest, which gosec cannot follow
	return func(c *http.Cookie) {
		c.SameSite = sameSite
	}
}

// SetCookie sets a new HTTP cookie with sensible defaults.
// It automatically handles the Secure flag based on how the client reaches us.
// Optional arguments (MaxAge, Expires, HttpOnly, SameSite) can be provided using CookieOption functions.
//
// Default values for options if not explicitly set via CookieOption:
//   - Path: "/"
//   - HttpOnly: true
//   - Secure: determined by IsSecureRequest (the deployment scheme, TLS, or
//     X-Forwarded-Proto)
//   - SameSite: http.SameSiteLaxMode
//   - MaxAge: 0 (results in a session cookie if no Expires date is explicitly set)
//   - Expires: not set (also contributes to a session cookie if MaxAge is 0)
func SetCookie(w http.ResponseWriter, r *http.Request, name, value string, opts ...CookieOption) {
	//#nosec G124 -- Secure is set from IsSecureRequest, which gosec cannot follow
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true, // Default based on prompt's delete example
		Secure:   IsSecureRequest(r),
		SameSite: http.SameSiteLaxMode, // Default based on prompt's delete example
	}

	// Apply any provided functional options to override defaults
	for _, opt := range opts {
		opt(cookie)
	}

	http.SetCookie(w, cookie)
}

// DeleteCookie removes a cookie by setting its MaxAge to -1 and Expires to a past date.
// This function strictly follows the example provided in the prompt for deleting a cookie.
func DeleteCookie(w http.ResponseWriter, r *http.Request, name string) {
	//#nosec G124 -- Secure is set from IsSecureRequest, which gosec cannot follow
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "", // Value is typically empty for deletion
		Path:     "/",
		Expires:  time.Unix(0, 0), // A time in the past
		MaxAge:   -1,              // Immediate expiration
		HttpOnly: true,
		Secure:   IsSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}
