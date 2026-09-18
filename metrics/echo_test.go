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

package metrics

import (
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// Differential test: both stacks must observe under identical label values.

func samples(t *testing.T) map[string]uint64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]uint64{}
	for _, mf := range mfs {
		if mf.GetName() != "api_response" {
			continue
		}
		for _, m := range mf.GetMetric() {
			out[labelKey(m.GetLabel())] = m.GetHistogram().GetSampleCount()
		}
	}
	return out
}

func labelKey(ls []*dto.LabelPair) string {
	k := ""
	for _, l := range ls {
		k += l.GetName() + "=" + l.GetValue() + ";"
	}
	return k
}

func delta(before, after map[string]uint64) map[string]uint64 {
	d := map[string]uint64{}
	for k, v := range after {
		if v != before[k] {
			d[k] = v - before[k]
		}
	}
	return d
}

func TestEchoMiddlewareMatchesLabels(t *testing.T) {
	for _, tc := range []struct{ name, method, path string }{
		{"root", "GET", "/svc/"},
		{"param path", "GET", "/svc/items/abc"},
		{"unknown route", "GET", "/svc/nope"},
		{"wrong method", "DELETE", "/svc/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echoutil.New()
			e.Pre(EchoMiddleware("/svc"), echoutil.Instrument())
			e.GET("/svc/", func(c *echo.Context) error { return echoutil.WriteJSON(c, 200, "ok") })
			e.GET("/svc/items/:id", func(c *echo.Context) error { return echoutil.WriteJSON(c, 200, "ok") })

			gjr := recorded[map[string]uint64](t, "gjr", nil)

			b := samples(t)
			e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.path, nil))
			x := delta(b, samples(t))

			if len(gjr) != 1 || len(x) != 1 {
				t.Fatalf("expected one observation each: go-json-rest=%v echo=%v", gjr, x)
			}
			for k := range gjr {
				if _, ok := x[k]; !ok {
					t.Errorf("labels differ: go-json-rest=%v echo=%v", gjr, x)
				}
			}
		})
	}
}
