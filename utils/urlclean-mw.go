package utils

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/ant0ine/go-json-rest/rest"
)

// RecorderMiddleware keeps a record of the HTTP status code of the response,
// and the number of bytes written.
// The result is available to the wrapping handlers as request.Env["STATUS_CODE"].(int),
// and as request.Env["BYTES_WRITTEN"].(int64)
type URLCleanMiddleware struct{}

// MiddlewareFunc makes RecorderMiddleware implement the Middleware interface.
func (mw *URLCleanMiddleware) MiddlewareFunc(h rest.HandlerFunc) rest.HandlerFunc {
	return func(w rest.ResponseWriter, r *rest.Request) {
		var err error
		r.URL.Path = strings.TrimSuffix(r.URL.Path, "/")
		r.URL, err = url.Parse(r.URL.String())

		if err != nil {
			rest.Error(w, "Error cleaning trailing / from path", http.StatusInternalServerError)
		}

		// call the handler
		h(w, r)
	}
}
