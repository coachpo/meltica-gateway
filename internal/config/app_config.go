package config

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DispatcherConfig manages route configuration.
type DispatcherConfig struct {
	Routes map[string]RouteConfig `yaml:"routes"`
}

// RouteConfig describes canonical routing behaviour.
type RouteConfig struct {
	WSTopics []string           `yaml:"wsTopics"`
	RestFns  []RestFnConfig     `yaml:"restFns"`
	Filters  []FilterRuleConfig `yaml:"filters"`
}

// RestFnConfig defines a REST fetch routine triggered by dispatcher routes.
type RestFnConfig struct {
	Name     string        `yaml:"name"`
	Endpoint string        `yaml:"endpoint"`
	Interval time.Duration `yaml:"interval"`
	Parser   string        `yaml:"parser"`
}

// FilterRuleConfig declares a single filter predicate.
type FilterRuleConfig struct {
	Field string `yaml:"field"`
	Op    string `yaml:"op"`
	Value any    `yaml:"value"`
}

// EventbusConfig sets event bus buffer sizing and worker fanout.
type EventbusConfig struct {
	BufferSize    int `yaml:"bufferSize"`
	FanoutWorkers int `yaml:"fanoutWorkers"`
}

// PoolConfig controls pooled object capacities.
type PoolConfig struct {
	EventSize        int `yaml:"eventSize"`
	OrderRequestSize int `yaml:"orderRequestSize"`
}

// APIServerConfig configures the gateway's HTTP control surface.
type APIServerConfig struct {
	Addr string `yaml:"addr"`
}

// TelemetryConfig configures OTLP exporters (metrics only).
type TelemetryConfig struct {
	OTLPEndpoint  string `yaml:"otlpEndpoint"`
	ServiceName   string `yaml:"serviceName"`
	OTLPInsecure  bool   `yaml:"otlpInsecure"`
	EnableMetrics bool   `yaml:"enableMetrics"` // Default: true
}

// BackpressureConfig configures dispatcher token-bucket behaviour.
type BackpressureConfig struct {
	TokenRatePerStream int `yaml:"token_rate_per_stream"`
	TokenBurst         int `yaml:"token_burst"`
}

// DispatcherRuntimeConfig aggregates dispatcher tunables for runtime configuration.
type DispatcherRuntimeConfig struct {
	Backpressure     BackpressureConfig `yaml:"backpressure"`
	CoalescableTypes []string           `yaml:"coalescable_types"`
}

// AppConfig is the unified Meltica application configuration combining all concerns.
type AppConfig struct {
	Environment  Environment                 `yaml:"environment"`
	Exchanges    map[Exchange]map[string]any `yaml:"exchanges"`
	Dispatcher   DispatcherConfig            `yaml:"dispatcher"`
	Eventbus     EventbusConfig              `yaml:"eventbus"`
	Pools        PoolConfig                  `yaml:"pools"`
	APIServer    APIServerConfig             `yaml:"apiServer"`
	Telemetry    TelemetryConfig             `yaml:"telemetry"`
	ManifestPath string                      `yaml:"manifest"`
}

// Load loads the unified Meltica configuration strictly from the provided YAML source.
func Load(ctx context.Context, configPath string) (AppConfig, error) {
	_ = ctx

	reader, closer, err := openConfigFile(configPath)
	if err != nil {
		return AppConfig{}, err
	}
	defer closer()

	bytes, err := io.ReadAll(reader)
	if err != nil {
		return AppConfig{}, fmt.Errorf("read config: %w", err)
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.normalise()

	if err := cfg.Validate(ctx); err != nil {
		return AppConfig{}, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func (c *AppConfig) normalise() {
	if c.Exchanges == nil {
		c.Exchanges = make(map[Exchange]map[string]any)
	} else {
		normalised := make(map[Exchange]map[string]any, len(c.Exchanges))
		for key, value := range c.Exchanges {
			normalizedKey := Exchange(normalizeExchangeName(string(key)))
			normalised[normalizedKey] = value
		}
		c.Exchanges = normalised
	}

	c.Environment = Environment(strings.ToLower(strings.TrimSpace(string(c.Environment))))
	c.APIServer.Addr = strings.TrimSpace(c.APIServer.Addr)
	c.ManifestPath = strings.TrimSpace(c.ManifestPath)
	c.Telemetry.OTLPEndpoint = strings.TrimSpace(c.Telemetry.OTLPEndpoint)
	c.Telemetry.ServiceName = strings.TrimSpace(c.Telemetry.ServiceName)
}

// Validate performs comprehensive validation on the unified configuration.
func (c *AppConfig) Validate(ctx context.Context) error {
	_ = ctx

	// Validate environment
	if c.Environment != EnvDev && c.Environment != EnvStaging && c.Environment != EnvProd {
		return fmt.Errorf("invalid environment: %s", c.Environment)
	}

	// Validate dispatcher routes
	if c.Dispatcher.Routes == nil {
		// Routes are optional; empty map is valid
		c.Dispatcher.Routes = make(map[string]RouteConfig)
	}
	for name, route := range c.Dispatcher.Routes {
		for i := range route.RestFns {
			if route.RestFns[i].Interval <= 0 {
				return fmt.Errorf("dispatcher route %s restFns[%d]: interval must be >0", name, i)
			}
		}
	}

	// Validate eventbus
	if c.Eventbus.BufferSize <= 0 {
		return fmt.Errorf("eventbus bufferSize must be >0")
	}
	if c.Eventbus.FanoutWorkers <= 0 {
		return fmt.Errorf("eventbus fanoutWorkers must be >0")
	}

	if c.Pools.EventSize <= 0 {
		return fmt.Errorf("pool eventSize must be >0")
	}
	if c.Pools.OrderRequestSize <= 0 {
		return fmt.Errorf("pool orderRequestSize must be >0")
	}

	if strings.TrimSpace(c.APIServer.Addr) == "" {
		return fmt.Errorf("api server addr required")
	}

	// Validate telemetry
	if c.Telemetry.ServiceName == "" {
		return fmt.Errorf("telemetry service name required")
	}

	if strings.TrimSpace(c.ManifestPath) == "" {
		return fmt.Errorf("manifest path required")
	}

	return nil
}

func openConfigFile(path string) (io.Reader, func(), error) {
	var (
		closeFn    func()
		candidates []string
		seen       = make(map[string]struct{})
	)
	addCandidate := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	addCandidate(path)
	for _, fallback := range []string{
		"config/app.yaml",
		"internal/config/app.yaml",
		"config/app.example.yaml",
		"internal/config/app.example.yaml",
	} {
		addCandidate(fallback)
	}

	var lastErr error
	for _, candidate := range candidates {
		file, err := os.Open(candidate) // #nosec G304 -- configuration paths are controlled by operators.
		if err == nil {
			closeFn = func() { _ = file.Close() }
			return file, closeFn, nil
		}
		if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("open app config: %w", err)
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return nil, nil, fmt.Errorf("open app config: %w", lastErr)
}

func cloneStringAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return make(map[string]any)
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
