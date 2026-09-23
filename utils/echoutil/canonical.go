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

package echoutil

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// KeyCanonicalJSON marks a request whose JSON responses are canonical.
const KeyCanonicalJSON = "echoutil.canonical-json"

// CanonicalJSON mirrors utils.CanonicalJSONMiddleware for everything after it
// in the chain: WriteJSON encodes with canonicaljson-go and every header write
// carries "PhJsonFormat: gibson042-canonicaljson".
func CanonicalJSON() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Set(KeyCanonicalJSON, true)
			res := Response(c)
			orig := res.ResponseWriter
			res.ResponseWriter = &phJSONFormatWriter{ResponseWriter: orig}
			defer func() { res.ResponseWriter = orig }()
			return next(c)
		}
	}
}

type phJSONFormatWriter struct {
	http.ResponseWriter
}

func (w *phJSONFormatWriter) WriteHeader(code int) {
	w.ResponseWriter.Header().Add("PhJsonFormat", "gibson042-canonicaljson")
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *phJSONFormatWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
