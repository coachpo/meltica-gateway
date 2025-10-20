// Package telemetry provides OpenTelemetry initialization and instrumentation.
package telemetry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	instrumentationsdk "go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.32.0"
)

const (
	serviceName    = "meltica"
	serviceVersion = "1.0.0"
)

var (
	// globalEnvironment stores the environment name for use in metric labels
	globalEnvironment string
)

// Config defines OpenTelemetry configuration parameters.
type Config struct {
	Enabled          bool
	OTLPEndpoint     string
	OTLPInsecure     bool
	EnableMetrics    bool
	MetricInterval   time.Duration
	ShutdownTimeout  time.Duration
	ConsoleExporter  bool
	ServiceName      string
	ServiceVersion   string
	ServiceNamespace string
	Environment      string
}

// DefaultConfig returns the default telemetry configuration based on environment variables.
func DefaultConfig() Config {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4318"
	}
	svcName := os.Getenv("OTEL_SERVICE_NAME")
	if svcName == "" {
		svcName = serviceName
	}
	env := strings.TrimSpace(os.Getenv("OTEL_RESOURCE_ENVIRONMENT"))
	if env == "" {
		env = strings.TrimSpace(os.Getenv("MELTICA_ENV"))
	}
	if env == "" {
		env = "development"
	}
	return Config{
		Enabled:          os.Getenv("OTEL_ENABLED") != "false",
		OTLPEndpoint:     endpoint,
		OTLPInsecure:     os.Getenv("OTEL_EXPORTER_OTLP_INSECURE") == "true",
		EnableMetrics:    os.Getenv("OTEL_METRICS_ENABLED") != "false", // Default: true
		MetricInterval:   30 * time.Second,
		ShutdownTimeout:  5 * time.Second,
		ConsoleExporter:  os.Getenv("OTEL_CONSOLE_EXPORTER") == "true",
		ServiceName:      svcName,
		ServiceVersion:   serviceVersion,
		ServiceNamespace: os.Getenv("OTEL_SERVICE_NAMESPACE"),
		Environment:      env,
	}
}

// Provider manages OpenTelemetry meter provider (metrics only).
type Provider struct {
	meterProvider *sdkmetric.MeterProvider
	config        Config
}

// NewProvider initializes a new telemetry provider with the given configuration.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	// Set global environment for metric labels
	globalEnvironment = strings.ToLower(cfg.Environment)
	
	if !cfg.Enabled {
		return &Provider{
			meterProvider: nil,
			config:        cfg,
		}, nil
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	var mp *sdkmetric.MeterProvider

	if cfg.EnableMetrics {
		mp, err = newMeterProvider(ctx, res, cfg)
		if err != nil {
			return nil, fmt.Errorf("create meter provider: %w", err)
		}
		otel.SetMeterProvider(mp)
	}
	return &Provider{
		meterProvider: mp,
		config:        cfg,
	}, nil
}

// Shutdown gracefully shuts down the telemetry provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.meterProvider == nil {
		return nil
	}
	if mErr := p.meterProvider.Shutdown(ctx); mErr != nil {
		return fmt.Errorf("shutdown meter: %w", mErr)
	}
	return nil
}

// Meter returns a meter with the given name.
func (p *Provider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	if p.meterProvider == nil {
		return otel.Meter(name, opts...)
	}
	return p.meterProvider.Meter(name, opts...)
}

func newResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []resource.Option{
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
		),
	}
	if cfg.ServiceNamespace != "" {
		attrs = append(attrs, resource.WithAttributes(
			semconv.ServiceNamespaceKey.String(cfg.ServiceNamespace),
		))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, resource.WithAttributes(
			attribute.String("environment", strings.ToLower(cfg.Environment)),
		))
	}
	attrs = append(attrs, resource.WithProcessRuntimeName())
	attrs = append(attrs, resource.WithProcessRuntimeVersion())
	attrs = append(attrs, resource.WithHost())
	res, err := resource.New(ctx, attrs...)
	if err != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", err)
	}
	return res, nil
}

func newMeterProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdkmetric.MeterProvider, error) {
	endpoint := stripScheme(cfg.OTLPEndpoint)
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(endpoint),
	}
	if cfg.OTLPInsecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}
	
	exporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}

	// Configure Views for histogram bucket customization
	views := createHistogramViews()

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter,
			sdkmetric.WithInterval(cfg.MetricInterval),
		)),
		sdkmetric.WithView(views...),
	)
	return mp, nil
}

// createHistogramViews configures explicit histogram buckets optimized for observed latency patterns.
func createHistogramViews() []sdkmetric.View {
	return []sdkmetric.View{
		// Dispatcher processing duration: 0.1ms - 500ms (event processing latency)
		sdkmetric.NewView(
			sdkmetric.Instrument{
				Name:        "dispatcher.processing.duration",
				Description: "Dispatcher processing duration",
				Kind:        sdkmetric.InstrumentKindHistogram,
				Unit:        "ms",
				Scope: instrumentationsdk.Scope{
					Name:       "",
					Version:    "",
					SchemaURL:  "",
					Attributes: attribute.Set{},
				},
			},
			sdkmetric.Stream{
				Name:        "",
				Description: "",
				Unit:        "",
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: []float64{0.1, 0.5, 1, 2, 5, 10, 25, 50, 100, 250, 500},
					NoMinMax:   false,
				},
				AttributeFilter:                   nil,
				ExemplarReservoirProviderSelector: nil,
			},
		),
		// Pool borrow duration: 0.01ms - 50ms (memory pool operations)
		sdkmetric.NewView(
			sdkmetric.Instrument{
				Name:        "pool.borrow.duration",
				Description: "Pool borrow operation duration",
				Kind:        sdkmetric.InstrumentKindHistogram,
				Unit:        "ms",
				Scope: instrumentationsdk.Scope{
					Name:       "",
					Version:    "",
					SchemaURL:  "",
					Attributes: attribute.Set{},
				},
			},
			sdkmetric.Stream{
				Name:        "",
				Description: "",
				Unit:        "",
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 25, 50},
					NoMinMax:   false,
				},
				AttributeFilter:                   nil,
				ExemplarReservoirProviderSelector: nil,
			},
		),
		// Orderbook cold start duration: 100ms - 30s (initial snapshot loading)
		sdkmetric.NewView(
			sdkmetric.Instrument{
				Name:        "orderbook.coldstart.duration",
				Description: "Orderbook cold start duration",
				Kind:        sdkmetric.InstrumentKindHistogram,
				Unit:        "ms",
				Scope: instrumentationsdk.Scope{
					Name:       "",
					Version:    "",
					SchemaURL:  "",
					Attributes: attribute.Set{},
				},
			},
			sdkmetric.Stream{
				Name:        "",
				Description: "",
				Unit:        "",
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: []float64{100, 250, 500, 1000, 2000, 5000, 10000, 30000},
					NoMinMax:   false,
				},
				AttributeFilter:                   nil,
				ExemplarReservoirProviderSelector: nil,
			},
		),
		// Orderbook recovery duration: 50ms - 10s (gap recovery with retry)
		sdkmetric.NewView(
			sdkmetric.Instrument{
				Name:        "orderbook.recovery.duration",
				Description: "Orderbook recovery duration",
				Kind:        sdkmetric.InstrumentKindHistogram,
				Unit:        "ms",
				Scope: instrumentationsdk.Scope{
					Name:       "",
					Version:    "",
					SchemaURL:  "",
					Attributes: attribute.Set{},
				},
			},
			sdkmetric.Stream{
				Name:        "",
				Description: "",
				Unit:        "",
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: []float64{50, 100, 200, 500, 1000, 2000, 5000, 10000},
					NoMinMax:   false,
				},
				AttributeFilter:                   nil,
				ExemplarReservoirProviderSelector: nil,
			},
		),
		// Orderbook recovery retry count: 0 - 5 retries
		sdkmetric.NewView(
			sdkmetric.Instrument{
				Name:        "orderbook.recovery.retry_count",
				Description: "Orderbook recovery retry count",
				Kind:        sdkmetric.InstrumentKindHistogram,
				Unit:        "{retry}",
				Scope: instrumentationsdk.Scope{
					Name:       "",
					Version:    "",
					SchemaURL:  "",
					Attributes: attribute.Set{},
				},
			},
			sdkmetric.Stream{
				Name:        "",
				Description: "",
				Unit:        "",
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: []float64{0, 1, 2, 3, 4, 5},
					NoMinMax:   false,
				},
				AttributeFilter:                   nil,
				ExemplarReservoirProviderSelector: nil,
			},
		),
		// Event bus fanout size: 1 - 100 subscribers
		sdkmetric.NewView(
			sdkmetric.Instrument{
				Name:        "eventbus.fanout.size",
				Description: "Event bus fanout subscriber count",
				Kind:        sdkmetric.InstrumentKindHistogram,
				Unit:        "1",
				Scope: instrumentationsdk.Scope{
					Name:       "",
					Version:    "",
					SchemaURL:  "",
					Attributes: attribute.Set{},
				},
			},
			sdkmetric.Stream{
				Name:        "",
				Description: "",
				Unit:        "",
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: []float64{1, 2, 5, 10, 20, 50, 100},
					NoMinMax:   false,
				},
				AttributeFilter:                   nil,
				ExemplarReservoirProviderSelector: nil,
			},
		),
	}
}

// stripScheme removes http:// or https:// prefix from endpoint URL.
// OTLP HTTP exporters expect just host:port, not a full URL with scheme.
func stripScheme(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	return endpoint
}

// Environment returns the configured environment name for use in metric labels.
func Environment() string {
	if globalEnvironment == "" {
		return "development"
	}
	return globalEnvironment
}
