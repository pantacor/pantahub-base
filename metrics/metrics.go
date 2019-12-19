package metrics

import (
	"strconv"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/prometheus/client_golang/prometheus"
)

var responseTime *prometheus.HistogramVec = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name: "api_response",
		Help: "Register api responses",
	},
	[]string{"endpoint", "method", "code"},
)

type MetricsMiddleware struct{}

func (m *MetricsMiddleware) MiddlewareFunc(handler rest.HandlerFunc) rest.HandlerFunc {
	return func(w rest.ResponseWriter, r *rest.Request) {
		handler(w, r)
		elapse := r.Env["ELAPSED_TIME"].(*time.Duration)

		endpoint := r.Request.RequestURI
		method := r.Request.Method
		code := strconv.Itoa(r.Env["STATUS_CODE"].(int))

		responseTime.WithLabelValues(endpoint, method, code).Observe(elapse.Seconds())
	}
}

func init() {
	prometheus.MustRegister(responseTime)
}
