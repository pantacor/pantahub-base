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

// Package echoutil ports the go-json-rest middleware and response writers to echo
// with identical wire output, so services can migrate without client changes.
package echoutil

import (
	"encoding/json"
	"net/http"

	cjson "github.com/gibson042/canonicaljson-go"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// ContentTypeJSON is exactly the value go-json-rest's responseWriter.WriteHeader
// defaults to.
const ContentTypeJSON = "application/json; charset=utf-8"

// ErrorFieldName matches go-json-rest's rest.ErrorFieldName.
const ErrorFieldName = "Error"

// WriteJSON matches go-json-rest: json.Marshal (canonicaljson-go under
// CanonicalJSON; no trailing newline) and Content-Type
// "application/json; charset=utf-8" unless already set.
func WriteJSON(c *echo.Context, code int, v interface{}) error {
	marshal := json.Marshal
	if canonical, _ := c.Get(KeyCanonicalJSON).(bool); canonical {
		marshal = cjson.Marshal
	}
	b, err := marshal(v)
	if err != nil {
		return err
	}

	h := c.Response().Header()
	if h.Get(echo.HeaderContentType) == "" {
		h.Set(echo.HeaderContentType, ContentTypeJSON)
	}
	c.Response().WriteHeader(code)
	_, err = c.Response().Write(b)
	return err
}

// WriteHeader mirrors go-json-rest's WriteHeader, which defaults Content-Type
// even when no body follows (e.g. 304).
func WriteHeader(c *echo.Context, code int) error {
	h := c.Response().Header()
	if h.Get(echo.HeaderContentType) == "" {
		h.Set(echo.HeaderContentType, ContentTypeJSON)
	}
	c.Response().WriteHeader(code)
	return nil
}

// Error mirrors go-json-rest's rest.Error: {"Error": msg}.
func Error(c *echo.Context, msg string, code int) error {
	return WriteJSON(c, code, map[string]string{ErrorFieldName: msg})
}

// NotFound mirrors go-json-rest's rest.NotFound.
func NotFound(c *echo.Context) error {
	return Error(c, "Resource not found", http.StatusNotFound)
}

// RestErrorWrapper mirrors utils.RestErrorWrapper: mints an incident, logs and
// forwards it via utils.MintRestError, and writes the {"code","error"} body.
func RestErrorWrapper(c *echo.Context, errorStr string, code int) error {
	return WriteJSON(c, code, utils.MintRestError(errorStr, "", code))
}

// RestErrorWrapperUser mirrors utils.RestErrorWrapperUser.
func RestErrorWrapperUser(c *echo.Context, errorStr, userMsg string, code int) error {
	return WriteJSON(c, code, utils.MintRestError(errorStr, userMsg, code))
}

// RestError mirrors utils.RestError, including its "<nil>" rendering of a nil
// error and the space-joined message.
func RestError(c *echo.Context, err error, message string, code int) error {
	errStr := "<nil>"
	if err != nil {
		errStr = err.Error()
	}
	return RestErrorWrapper(c, message+" "+errStr, code)
}

// RestErrorUser mirrors utils.RestErrorUser.
func RestErrorUser(c *echo.Context, err error, message string, code int) error {
	errStr := "<nil>"
	if err != nil {
		errStr = err.Error()
	}
	return RestErrorWrapperUser(c, errStr, message, code)
}

// RestErrorWrite mirrors utils.RestErrorWrite.
func RestErrorWrite(c *echo.Context, rerr *utils.RError) error {
	return WriteJSON(c, rerr.Code, utils.MintRestError(rerr.Error, "", rerr.Code))
}
