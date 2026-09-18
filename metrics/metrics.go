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
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var responseTime *prometheus.HistogramVec = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name: "api_response",
		Help: "Register api responses",
	},
	[]string{"endpoint", "method", "code"},
)

// observe records one response; endpoint is the prefix-stripped path, as
// existing dashboards expect. Shared with EchoMiddleware.
func observe(endpoint, method string, status int, elapsed *time.Duration) {
	responseTime.WithLabelValues(endpoint, method, strconv.Itoa(status)).Observe(elapsed.Seconds())
}

func init() {
	prometheus.MustRegister(responseTime)
}
