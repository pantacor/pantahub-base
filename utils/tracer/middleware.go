// Copyright (c) 2022  Pantacor Ltd.
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

package tracer

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/ant0ine/go-json-rest/rest"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	tracerKey  = "pantacor-go-json-rest-rest"
	tracerName = "gitlab.com/pantacor/pantahub-base/base/utils/tracer"
)

type GetSpanNameFunc func(string) string

// OtelMiddleware sent trace to open telemetry collector
type OtelMiddleware struct {
	ServiceName string
	Opts        []Option
	Router      rest.RouterApp
}

type tracerResponseWriter struct {
	writer     rest.ResponseWriter
	ctx        context.Context
	span       oteltrace.Span
	tracer     oteltrace.Tracer
	StatusCode int
}

func CreateTracerWriter(w rest.ResponseWriter, ctx context.Context, span oteltrace.Span, tracer oteltrace.Tracer) *tracerResponseWriter {
	return &tracerResponseWriter{
		writer: w,
		ctx:    ctx,
		span:   span,
		tracer: tracer,
	}
}

// Identical to the http.ResponseWriter interface
func (w *tracerResponseWriter) Header() http.Header {
	return w.writer.Header()
}

// Use EncodeJson to generate the payload, write the headers with http.StatusOK if
// they are not already written, then write the payload.
// The Content-Type header is set to "application/json", unless already specified.
func (w *tracerResponseWriter) WriteJson(v interface{}) error {
	_, childSpan := w.tracer.Start(w.ctx, "WriteJson")
	defer childSpan.End()

	resp, err := w.EncodeJson(v)
	if err != nil {
		return err
	}

	_, err = w.Write(resp)
	return err
}

// Encode the data structure to JSON, mainly used to wrap ResponseWriter in
// middlewares.
func (w *tracerResponseWriter) EncodeJson(v interface{}) ([]byte, error) {
	_, childSpan := w.tracer.Start(w.ctx, "EncodeJson")
	defer childSpan.End()
	return w.writer.EncodeJson(v)
}

// Identical to the http.ResponseWriter interface
func (w *tracerResponseWriter) Write(body []byte) (int, error) {
	_, childSpan := w.tracer.Start(w.ctx, "Write")
	defer childSpan.End()
	return w.writer.Write(body)
}

// Similar to the http.ResponseWriter interface, with additional JSON related
// headers set.
func (w *tracerResponseWriter) WriteHeader(code int) {
	_, childSpan := w.tracer.Start(w.ctx, "WriteHeader")
	defer childSpan.End()
	w.StatusCode = code
	w.writer.WriteHeader(code)
}

// Count of bytes written as response
func (w *tracerResponseWriter) Count() uint64 {
	return w.writer.Count()
}

func GetTraceHeaderFromJaeger(r *http.Request) {
	traceID := r.Header.Get("Uber-Trace-ID")
	if traceID == "" {
		return
	}

	traceSlice := strings.Split(traceID, ":")
	if len(traceSlice) < 4 {
		return
	}

	traceparent := fmt.Sprintf(
		"00-%s-%s-0%s",
		traceSlice[0],
		traceSlice[1],
		traceSlice[3],
	)

	r.Header.Set("traceparent", traceparent)
}

// MiddlewareFunc makes OtelMiddleware implement the Middleware interface.
func (mw *OtelMiddleware) MiddlewareFunc(h rest.HandlerFunc) rest.HandlerFunc {
	cfg := config{}
	var tracer oteltrace.Tracer

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		for _, opt := range mw.Opts {
			opt.apply(&cfg)
		}
		if cfg.TracerProvider == nil {
			cfg.TracerProvider = otel.GetTracerProvider()
		}
		tracer = cfg.TracerProvider.Tracer(
			tracerName,
			oteltrace.WithInstrumentationVersion(SemVersion()),
		)
		if cfg.Propagators == nil {
			cfg.Propagators = otel.GetTextMapPropagator()
		}
	}

	return func(w rest.ResponseWriter, r *rest.Request) {
		if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
			h(w, r)
		}
		request := r.Request
		savedCtx := request.Context()
		defer func() {
			request = request.WithContext(savedCtx)
			r.Request = request
		}()

		fmt.Printf("%+v", r.Header)

		ctx := cfg.Propagators.Extract(savedCtx, propagation.HeaderCarrier(request.Header))
		opts := []oteltrace.SpanStartOption{
			oteltrace.WithAttributes(semconv.NetAttributesFromHTTPRequest("tcp", request)...),
			oteltrace.WithAttributes(semconv.EndUserAttributesFromHTTPRequest(request)...),
			oteltrace.WithAttributes(semconv.HTTPServerAttributesFromHTTPRequest(mw.ServiceName, r.Request.RequestURI, request)...),
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
		}

		spanName := defaultGetSpanName(r.RequestURI)
		if mw.Router != nil {
			route, _, findit, _ := mw.Router.FindRoute(r.Method, r.URL)
			if findit && route != nil {
				paths := strings.Split(r.RequestURI, "/")
				path := paths[0]
				for _, v := range paths {
					if path == "" && v != "" {
						path = v
						break
					}
				}
				spanName = fmt.Sprintf("%s%s", path, strings.ReplaceAll(route.PathExp, "#", ":"))
			}
		}
		ctx, span := tracer.Start(ctx, spanName, opts...)
		defer span.End()

		r.Request = request.WithContext(ctx)

		// serve the request to the next middleware
		writer := CreateTracerWriter(w, ctx, span, tracer)
		cfg.Propagators.Inject(r.Request.Context(), propagation.HeaderCarrier(w.Header()))

		h(writer, r)

		code := writer.StatusCode
		if code == 0 {
			code = 200
		}
		attrs := semconv.HTTPAttributesFromHTTPStatusCode(code)
		spanStatus, spanMessage := semconv.SpanStatusFromHTTPStatusCodeAndSpanKind(code, oteltrace.SpanKindServer)
		span.SetAttributes(attrs...)
		span.SetStatus(spanStatus, spanMessage)
	}
}

func defaultGetSpanName(uri string) string {
	spanName := ""
	for _, v := range strings.SplitAfter(uri, "/") {
		if len(v) == 24 {
			spanName = fmt.Sprintf("%s%s", spanName, "#id")
		} else {
			spanName = fmt.Sprintf("%s%s", spanName, v)
		}
	}

	return spanName
}
