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
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// Context keys set by Instrument, named as go-json-rest's TimerMiddleware and
// RecorderMiddleware named their r.Env entries.
const (
	KeyStartTime    = "START_TIME"
	KeyElapsedTime  = "ELAPSED_TIME"
	KeyStatusCode   = "STATUS_CODE"
	KeyBytesWritten = "BYTES_WRITTEN"
)

// Instrument records START_TIME, ELAPSED_TIME, STATUS_CODE and BYTES_WRITTEN
// (go-json-rest Timer+Recorder). It writes returned errors via c.Error first, so
// 404/405 are not logged as status 0.
func Instrument() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			start := time.Now()
			c.Set(KeyStartTime, &start)

			if err := next(c); err != nil {
				c.Echo().HTTPErrorHandler(c, err)
			}

			elapsed := time.Since(start)
			c.Set(KeyElapsedTime, &elapsed)

			// go-json-rest records 0 until a header is written; echo defaults to 200.
			status := 0
			res := Response(c)
			if res != nil && res.Committed {
				status = res.Status
			}
			c.Set(KeyStatusCode, status)
			if res != nil {
				c.Set(KeyBytesWritten, res.Size)
			}
			return nil
		}
	}
}

// accessEnv exposes context values under go-json-rest's r.Env keys.
func accessEnv(c *echo.Context) map[string]interface{} {
	env := map[string]interface{}{}
	for _, k := range []string{KeyStartTime, KeyElapsedTime, KeyStatusCode, KeyRemoteUser, KeyJWTPayload} {
		if v := c.Get(k); v != nil {
			env[k] = v
		}
	}
	return env
}

// serviceRequest strips the mount prefix like http.StripPrefix, so logged
// RequestURI stays relative to the service as before.
func serviceRequest(r *http.Request, prefix string) *http.Request {
	if prefix == "" {
		return r
	}
	p := strings.TrimPrefix(r.URL.Path, prefix)
	rp := strings.TrimPrefix(r.URL.RawPath, prefix)
	if len(p) >= len(r.URL.Path) || (r.URL.RawPath != "" && len(rp) >= len(r.URL.RawPath)) {
		return r
	}
	r2 := new(http.Request)
	*r2 = *r
	r2.URL = new(url.URL)
	*r2.URL = *r.URL
	r2.URL.Path = p
	r2.URL.RawPath = rp
	return r2
}

// lastWriteRecorder keeps the last Write, as utils.ResponseWriterWrapper does.
type lastWriteRecorder struct {
	http.ResponseWriter
	last []byte
}

func (w *lastWriteRecorder) Write(b []byte) (int, error) {
	w.last = b
	return w.ResponseWriter.Write(b)
}

// AccessLogFluent mirrors utils.AccessLogFluentMiddleware. Must wrap Instrument.
func AccessLogFluent(mw *utils.AccessLogFluentMiddleware, stripPrefix string) echo.MiddlewareFunc {
	if !mw.Init() {
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			var requestBody, responseBody []byte

			if mw.LogsBody(c.Request()) {
				requestBody = utils.ReadAndRestoreBody(c.Request())
				res := Response(c)
				rec := &lastWriteRecorder{ResponseWriter: res.ResponseWriter}
				res.ResponseWriter = rec
				err := next(c)
				responseBody = rec.last
				res.ResponseWriter = rec.ResponseWriter
				if err != nil {
					return err
				}
			} else if err := next(c); err != nil {
				return err
			}

			if mw.Logger == nil {
				return nil
			}

			// Range-checked int64 -> uint64 (gosec G115).
			var responseSize uint64
			if size := Response(c).Size; size > 0 {
				responseSize = uint64(size)
			}

			mw.Post(utils.BuildAccessLogFluentRecord(mw, serviceRequest(c.Request(), stripPrefix),
				accessEnv(c), responseSize, requestBody, responseBody))
			return nil
		}
	}
}

// accessLogRecord is one JSON access log line; field names and order are what
// log consumers expect.
type accessLogRecord struct {
	Timestamp    *time.Time
	StatusCode   int
	ResponseTime *time.Duration
	HttpMethod   string //nolint:revive // log field name
	RequestURI   string
	RemoteUser   string
	UserAgent    string
}

// AccessLogJSON writes one JSON record per request. Must wrap Instrument.
func AccessLogJSON(logger *log.Logger, stripPrefix string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if err := next(c); err != nil {
				return err
			}

			env := accessEnv(c)
			r := serviceRequest(c.Request(), stripPrefix)

			rec := accessLogRecord{
				HttpMethod: r.Method,
				RequestURI: r.URL.RequestURI(),
				UserAgent:  r.UserAgent(),
			}
			if v, ok := env[KeyStartTime].(*time.Time); ok {
				rec.Timestamp = v
			}
			if v, ok := env[KeyStatusCode].(int); ok {
				rec.StatusCode = v
			}
			if v, ok := env[KeyElapsedTime].(*time.Duration); ok {
				rec.ResponseTime = v
			}
			if v, ok := env[KeyRemoteUser].(string); ok {
				rec.RemoteUser = v
			}

			b, err := json.Marshal(&rec)
			if err != nil {
				panic(err)
			}
			logger.Printf("%s", b)
			return nil
		}
	}
}
