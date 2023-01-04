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
	"log"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"google.golang.org/grpc/credentials"
)

func Init(serviceName string) *sdktrace.TracerProvider {
	var (
		signozToken  = os.Getenv("SIGNOZ_ACCESS_TOKEN")
		collectorURL = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		insecure     = os.Getenv("OTEL_INSECURE_MODE")
	)
	headers := map[string]string{
		"signoz-access-token": signozToken,
	}

	secureOption := otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")) // config can be passed to configure TLS
	if len(insecure) > 0 {
		secureOption = otlptracegrpc.WithInsecure()
	}

	exporter, err := otlptrace.New(
		context.Background(),
		otlptracegrpc.NewClient(
			secureOption,
			otlptracegrpc.WithEndpoint(collectorURL),
			otlptracegrpc.WithHeaders(headers),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	hostname, _ := os.Hostname()
	k8snode := os.Getenv("K8S_NODE_NAME")
	k8snamespace := os.Getenv("K8S_NAMESPACE")
	sampler := sdktrace.AlwaysSample()
	if os.Getenv("OTEL_TRACE_RATIO") != "" {
		ratio, err := strconv.ParseFloat(os.Getenv("OTEL_TRACE_RATIO"), 64)
		if err == nil {
			sampler = sdktrace.TraceIDRatioBased(ratio)
		}
	}

	// For the demonstration, use sdktrace.AlwaysSample sampler to sample all traces.
	// In a production application, use sdktrace.ProbabilitySampler with a desired probability.
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exporter)),
		sdktrace.WithResource(
			resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String(serviceName),
				semconv.HostNameKey.String(hostname),
				semconv.K8SNodeNameKey.String(k8snode),
				semconv.K8SNamespaceNameKey.String(k8snamespace),
				semconv.DeploymentEnvironmentKey.String(k8snamespace),
			),
		),
	)

	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return traceProvider
}
