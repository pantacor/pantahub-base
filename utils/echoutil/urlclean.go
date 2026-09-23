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
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"
)

// URLClean mirrors utils.URLCleanMiddleware: drops one trailing "/" before
// routing, so a service root is routed as the bare prefix (g.GET("")).
// Re-parsing discards a RawPath that no longer matches, as go-json-rest does.
func URLClean() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			r := c.Request()
			r.URL.Path = strings.TrimSuffix(r.URL.Path, "/")
			u, err := url.Parse(r.URL.String())
			if err != nil {
				return RestErrorWrapper(c, "Error cleaning trailing / from path", http.StatusInternalServerError)
			}
			r.URL = u
			return next(c)
		}
	}
}
