package utils

import (
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
)

// GetCookie retrieves a cookie by its name.
// It returns the cookie's value or an error if the cookie is not found.
func GetCookie(r *rest.Request, name string) string {
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
	return func(c *http.Cookie) {
		c.Expires = expires
	}
}

// WithHttpOnly sets the HttpOnly flag for the cookie.
func WithHttpOnly(httpOnly bool) CookieOption {
	return func(c *http.Cookie) {
		c.HttpOnly = httpOnly
	}
}

// WithSameSite sets the SameSite policy for the cookie.
func WithSameSite(sameSite http.SameSite) CookieOption {
	return func(c *http.Cookie) {
		c.SameSite = sameSite
	}
}

// SetCookie sets a new HTTP cookie with sensible defaults.
// It automatically handles the Secure flag based on the request's URL scheme.
// Optional arguments (MaxAge, Expires, HttpOnly, SameSite) can be provided using CookieOption functions.
//
// Default values for options if not explicitly set via CookieOption:
//   - Path: "/"
//   - HttpOnly: true
//   - Secure: determined by r.URL.Scheme == "https"
//   - SameSite: http.SameSiteLaxMode
//   - MaxAge: 0 (results in a session cookie if no Expires date is explicitly set)
//   - Expires: not set (also contributes to a session cookie if MaxAge is 0)
func SetCookie(w rest.ResponseWriter, r *rest.Request, name, value string, opts ...CookieOption) {
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true, // Default based on prompt's delete example
		Secure:   r.URL.Scheme == "https",
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
func DeleteCookie(w rest.ResponseWriter, r *rest.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "", // Value is typically empty for deletion
		Path:     "/",
		Expires:  time.Unix(0, 0), // A time in the past
		MaxAge:   -1,              // Immediate expiration
		HttpOnly: true,
		Secure:   r.URL.Scheme == "https",
		SameSite: http.SameSiteLaxMode,
	})
}
