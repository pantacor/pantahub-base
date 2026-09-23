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
	"slices"

	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// scopeName identifies this instrumentation in the emitted spans.
const scopeName = "gitlab.com/pantacor/pantahub-base/utils/echoutil"

// Otel traces with otelecho's span names and attributes (owner decision).
// Register with e.Use: span names come from the matched route. Hand-rolled
// because otel-go-contrib ships otelecho for echo v4 only.
func Otel(serviceName string) echo.MiddlewareFunc {
	tracer := otel.GetTracerProvider().Tracer(scopeName)
	propagator := otel.GetTextMapPropagator()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			savedCtx := req.Context()
			defer func() { c.SetRequest(req.WithContext(savedCtx)) }()

			ctx := propagator.Extract(savedCtx, propagation.HeaderCarrier(req.Header))
			route := c.RouteInfo().Path
			attrs := []attribute.KeyValue{
				semconv.ServerAddress(serviceName),
				semconv.HTTPRequestMethodKey.String(req.Method),
				semconv.URLPath(req.URL.Path),
				semconv.URLScheme(scheme(req)),
				semconv.UserAgentOriginal(req.UserAgent()),
			}
			if route != "" {
				attrs = append(attrs, semconv.HTTPRoute(route))
			}

			ctx, span := tracer.Start(ctx, spanName(req.Method, route),
				oteltrace.WithAttributes(attrs...),
				oteltrace.WithSpanKind(oteltrace.SpanKindServer),
			)
			defer span.End()

			c.SetRequest(req.WithContext(ctx))

			err := next(c)
			if err != nil {
				span.SetAttributes(attribute.String("echo.error", err.Error()))
			}

			if res, uerr := echo.UnwrapResponse(c.Response()); uerr == nil {
				span.SetAttributes(semconv.HTTPResponseStatusCode(res.Status))
				// Server spans are errors only from 5xx up; 4xx is the client's fault.
				if res.Status >= http.StatusInternalServerError {
					span.SetStatus(codes.Error, "")
				}
			}

			return err
		}
	}
}

// spanName mirrors otelecho: "METHOD /route/template", with unknown methods
// collapsed to HTTP so they cannot explode span-name cardinality.
func spanName(method, route string) string {
	if !slices.Contains([]string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace,
	}, method) {
		method = "HTTP"
	}
	if route == "" {
		return method
	}
	return method + " " + route
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
