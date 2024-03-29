//
// Copyright 2017  Pantacor Ltd.
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

package utils

import (
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"gopkg.in/resty.v1"
)

var (
	debugEnabled bool
)

func init() {
	dbg := GetEnv(EnvRestyDebug)

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		resty.GetClient().Transport = otelhttp.NewTransport(http.DefaultTransport)
	}

	if dbg != "" {
		debugEnabled = true
	} else {
		debugEnabled = false
	}
}

// R create a *resty.Request honouring global client settings configurable
// through environments.
func R() *resty.Request {
	return RT(60)
}

func RT(timeout int) *resty.Request {

	return resty.
		SetTimeout(time.Duration(timeout) * time.Second).
		SetDebug(debugEnabled).
		SetAllowGetMethodPayload(true).R()
}
