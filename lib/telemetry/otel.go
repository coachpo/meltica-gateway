// Package telemetry configures OpenTelemetry providers for Meltica.
package telemetry

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/coachpo/meltica/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	apimetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

// Providers groups telemetry provider handles.
type Providers struct {
	TracerProvider trace.TracerProvider
	MeterProvider  apimetric.MeterProvider
}

// Init configures OpenTelemetry exporters based on the provided configuration.
func Init(ctx context.Context, cfg config.TelemetryConfig) (Providers, func(context.Context) error, error) {
	endpoint := strings.TrimSpace(cfg.OTLPEndpoint)
	service := strings.TrimSpace(cfg.ServiceName)
	if service == "" {
		service = "meltica-gateway"
	}

	if endpoint == "" {
		noopProviders := Providers{
			TracerProvider: nooptrace.NewTracerProvider(),
			MeterProvider:  noop.NewMeterProvider(),
		}
		otel.SetTracerProvider(noopProviders.TracerProvider)
		otel.SetMeterProvider(noopProviders.MeterProvider)
		return noopProviders, func(context.Context) error { return nil }, nil
	}

	host, insecure, err := parseEndpoint(endpoint)
	if err != nil {
		return Providers{}, nil, err
	}

	traceOpts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(host)}
	metricOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(host)}
	if insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}

	traceExp, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return Providers{}, nil, fmt.Errorf("create trace exporter: %w", err)
	}

	metricExp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return Providers{}, nil, fmt.Errorf("create metric exporter: %w", err)
	}

	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(service)))
	if err != nil {
		return Providers{}, nil, fmt.Errorf("create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	reader := sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(15*time.Second))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	providers := Providers{TracerProvider: tp, MeterProvider: mp}
	shutdown := func(ctx context.Context) error {
		var first error
		if err := tp.Shutdown(ctx); err != nil {
			first = err
		}
		if err := mp.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
		return first
	}
	return providers, shutdown, nil
}

func parseEndpoint(raw string) (string, bool, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("parse otlp endpoint: %w", err)
	}
	host := parsed.Host
	if host == "" {
		host = raw
	}
	insecure := parsed.Scheme != "https"
	return host, insecure, nil
}
