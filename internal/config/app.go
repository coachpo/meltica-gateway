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

// AdapterSet encapsulates adapter-specific configuration.
type AdapterSet struct {
	Binance BinanceAdapterConfig `yaml:"binance"`
}

// BinanceAdapterConfig declares Binance transport configuration.
type BinanceAdapterConfig struct {
	WS                  BinanceWSConfig   `yaml:"ws"`
	REST                BinanceRESTConfig `yaml:"rest"`
	BookRefreshInterval time.Duration     `yaml:"book_refresh_interval"`
}

// BinanceWSConfig controls websocket connectivity.
type BinanceWSConfig struct {
	PublicURL        string        `yaml:"publicUrl"`
	HandshakeTimeout time.Duration `yaml:"handshakeTimeout"`
}

// BinanceRESTConfig governs REST polling settings by name.
type BinanceRESTConfig struct {
	BaseURL  string             `yaml:"baseUrl"`
	Snapshot RESTSnapshotConfig `yaml:"snapshot"`
}

// RESTSnapshotConfig defines a REST polling schedule.
type RESTSnapshotConfig struct {
	Endpoint string        `yaml:"endpoint"`
	Interval time.Duration `yaml:"interval"`
	Limit    int           `yaml:"limit"`
}

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

// EventbusConfig sets event bus buffer sizing.
type EventbusConfig struct {
	BufferSize int `yaml:"bufferSize"`
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
	Environment  Environment
	Exchanges    map[Exchange]ExchangeSettings
	Adapters     AdapterSet
	Dispatcher   DispatcherConfig
	Eventbus     EventbusConfig
	Telemetry    TelemetryConfig
	ManifestPath string
}

// appConfigYAML is the YAML representation that maps to AppConfig.
type appConfigYAML struct {
	Environment string                          `yaml:"environment"`
	Exchanges   map[string]exchangeSettingsYAML `yaml:"exchanges"`
	Adapters    AdapterSet                      `yaml:"adapter"`
	Dispatcher  DispatcherConfig                `yaml:"dispatcher"`
	Eventbus    EventbusConfig                  `yaml:"eventbus"`
	Telemetry   TelemetryConfig                 `yaml:"telemetry"`
	Manifest    string                          `yaml:"manifest"`
}

type exchangeSettingsYAML struct {
	REST                  map[string]string `yaml:"rest"`
	Websocket             WebsocketSettings `yaml:"websocket"`
	Credentials           Credentials       `yaml:"credentials"`
	HTTPTimeout           string            `yaml:"http_timeout"`
	HandshakeTimeout      string            `yaml:"handshake_timeout"`
	SymbolRefreshInterval string            `yaml:"symbol_refresh_interval"`
}

// Load loads the unified Meltica configuration with precedence: defaults → YAML → env vars.
func Load(ctx context.Context, configPath string) (AppConfig, error) {
	// Step 1: Start with code defaults
	cfg := defaultAppConfig()

	// Step 2: Override with YAML if present
	yamlErr := cfg.loadYAML(ctx, configPath)
	if yamlErr != nil && !isConfigNotFoundError(yamlErr) {
		return AppConfig{}, fmt.Errorf("load yaml config: %w", yamlErr)
	}

	// Step 3: Override with environment variables
	cfg.loadEnv()

	// Step 4: Validate the final configuration
	if err := cfg.Validate(ctx); err != nil {
		return AppConfig{}, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

// isConfigNotFoundError checks if the error is due to config file not found.
func isConfigNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return os.IsNotExist(err) || strings.Contains(err.Error(), "open app config")
}

// defaultAppConfig returns the default configuration with sensible defaults.
func defaultAppConfig() AppConfig {
	return AppConfig{
		Environment: EnvProd,
		Exchanges: map[Exchange]ExchangeSettings{
			ExchangeBinance: {
				REST: map[string]string{
					BinanceRESTSurfaceSpot:    "https://api.binance.com",
					BinanceRESTSurfaceLinear:  "https://fapi.binance.com",
					BinanceRESTSurfaceInverse: "https://dapi.binance.com",
				},
				Websocket: WebsocketSettings{
					PublicURL:  "wss://stream.binance.com:9443/stream",
					PrivateURL: "wss://stream.binance.com:9443/ws",
				},
				Credentials:           Credentials{APIKey: "", APISecret: ""},
				HTTPTimeout:           10 * time.Second,
				HandshakeTimeout:      10 * time.Second,
				SymbolRefreshInterval: 0,
			},
		},
		Adapters: AdapterSet{
			Binance: BinanceAdapterConfig{
				WS: BinanceWSConfig{
					PublicURL:        "wss://stream.binance.com:9443/stream",
					HandshakeTimeout: 10 * time.Second,
				},
				REST: BinanceRESTConfig{
					BaseURL: "https://api.binance.com",
					Snapshot: RESTSnapshotConfig{
						Endpoint: "/api/v3/depth",
						Interval: 5 * time.Second,
						Limit:    100,
					},
				},
				BookRefreshInterval: 1 * time.Minute,
			},
		},
		Dispatcher: DispatcherConfig{
			Routes: make(map[string]RouteConfig),
		},
		Eventbus: EventbusConfig{
			BufferSize: 1024,
		},
		Telemetry: TelemetryConfig{
			OTLPEndpoint:  "http://localhost:4318",
			ServiceName:   "meltica-gateway",
			OTLPInsecure:  false,
			EnableMetrics: true,
		},
		ManifestPath: "config/runtime.yaml",
	}
}

// loadYAML loads and merges YAML configuration into the AppConfig.
func (c *AppConfig) loadYAML(ctx context.Context, path string) error {
	_ = ctx
	path = strings.TrimSpace(path)
	if path == "" {
		path = os.Getenv("MELTICA_CONFIG")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		path = "config/app.yaml"
	}

	reader, closer, err := openConfigFile(path)
	if err != nil {
		return err
	}
	defer closer()

	bytes, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var yamlCfg appConfigYAML
	if err := yaml.Unmarshal(bytes, &yamlCfg); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}

	// Merge YAML into AppConfig
	if yamlCfg.Environment != "" {
		c.Environment = Environment(strings.ToLower(strings.TrimSpace(yamlCfg.Environment)))
	}

	// Merge exchanges
	for name, exYAML := range yamlCfg.Exchanges {
		exchange := Exchange(normalizeExchangeName(name))
		existing, ok := c.Exchanges[exchange]
		if !ok {
			existing = ExchangeSettings{
				REST: make(map[string]string),
				Websocket: WebsocketSettings{
					PublicURL:  "",
					PrivateURL: "",
				},
				Credentials: Credentials{
					APIKey:    "",
					APISecret: "",
				},
				HTTPTimeout:           0,
				HandshakeTimeout:      0,
				SymbolRefreshInterval: 0,
			}
		}

		// Merge REST endpoints
		for surface, url := range exYAML.REST {
			if url != "" {
				existing.REST[surface] = url
			}
		}

		// Merge websocket settings
		if exYAML.Websocket.PublicURL != "" {
			existing.Websocket.PublicURL = exYAML.Websocket.PublicURL
		}
		if exYAML.Websocket.PrivateURL != "" {
			existing.Websocket.PrivateURL = exYAML.Websocket.PrivateURL
		}

		// Merge credentials
		if exYAML.Credentials.APIKey != "" {
			existing.Credentials.APIKey = exYAML.Credentials.APIKey
		}
		if exYAML.Credentials.APISecret != "" {
			existing.Credentials.APISecret = exYAML.Credentials.APISecret
		}

		// Parse durations
		if exYAML.HTTPTimeout != "" {
			if dur, err := time.ParseDuration(exYAML.HTTPTimeout); err == nil {
				existing.HTTPTimeout = dur
			}
		}
		if exYAML.HandshakeTimeout != "" {
			if dur, err := time.ParseDuration(exYAML.HandshakeTimeout); err == nil {
				existing.HandshakeTimeout = dur
			}
		}
		if exYAML.SymbolRefreshInterval != "" {
			if dur, err := time.ParseDuration(exYAML.SymbolRefreshInterval); err == nil {
				existing.SymbolRefreshInterval = dur
			}
		}

		c.Exchanges[exchange] = existing
	}

	// Merge streaming-specific config
	c.Adapters = yamlCfg.Adapters
	c.Dispatcher = yamlCfg.Dispatcher
	c.Eventbus = yamlCfg.Eventbus
	c.Telemetry = yamlCfg.Telemetry
	if manifest := strings.TrimSpace(yamlCfg.Manifest); manifest != "" {
		c.ManifestPath = manifest
	}

	return nil
}

// loadEnv loads environment variable overrides into AppConfig.
func (c *AppConfig) loadEnv() {
	// Environment
	if env := strings.TrimSpace(os.Getenv("MELTICA_ENV")); env != "" {
		c.Environment = Environment(strings.ToLower(env))
	}

	// Binance exchange settings
	if bin, ok := c.Exchanges[ExchangeBinance]; ok {
		if bin.REST == nil {
			bin.REST = make(map[string]string)
		}

		if v := strings.TrimSpace(os.Getenv("BINANCE_SPOT_BASE_URL")); v != "" {
			bin.REST[BinanceRESTSurfaceSpot] = v
		}
		if v := strings.TrimSpace(os.Getenv("BINANCE_LINEAR_BASE_URL")); v != "" {
			bin.REST[BinanceRESTSurfaceLinear] = v
		}
		if v := strings.TrimSpace(os.Getenv("BINANCE_INVERSE_BASE_URL")); v != "" {
			bin.REST[BinanceRESTSurfaceInverse] = v
		}
		if v := strings.TrimSpace(os.Getenv("BINANCE_WS_PUBLIC_URL")); v != "" {
			bin.Websocket.PublicURL = v
		}
		if v := strings.TrimSpace(os.Getenv("BINANCE_WS_PRIVATE_URL")); v != "" {
			bin.Websocket.PrivateURL = v
		}
		if v := strings.TrimSpace(os.Getenv("BINANCE_HTTP_TIMEOUT")); v != "" {
			if dur, err := time.ParseDuration(v); err == nil {
				bin.HTTPTimeout = dur
			}
		}
		if v := strings.TrimSpace(os.Getenv("BINANCE_WS_HANDSHAKE_TIMEOUT")); v != "" {
			if dur, err := time.ParseDuration(v); err == nil {
				bin.HandshakeTimeout = dur
			}
		}
		if v := strings.TrimSpace(os.Getenv("BINANCE_API_KEY")); v != "" {
			bin.Credentials.APIKey = v
		}
		if v := strings.TrimSpace(os.Getenv("BINANCE_API_SECRET")); v != "" {
			bin.Credentials.APISecret = v
		}

		c.Exchanges[ExchangeBinance] = bin
	}

	// Telemetry overrides
	if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")); v != "" {
		c.Telemetry.OTLPEndpoint = v
	}
	if v := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); v != "" {
		c.Telemetry.ServiceName = v
	}
	if v := strings.TrimSpace(os.Getenv("MELTICA_MANIFEST")); v != "" {
		c.ManifestPath = v
	}
}

// Validate performs comprehensive validation on the unified configuration.
func (c *AppConfig) Validate(ctx context.Context) error {
	_ = ctx

	// Validate environment
	if c.Environment != EnvDev && c.Environment != EnvStaging && c.Environment != EnvProd {
		return fmt.Errorf("invalid environment: %s", c.Environment)
	}

	// Validate exchanges
	for name, ex := range c.Exchanges {
		if len(ex.REST) == 0 {
			return fmt.Errorf("exchange %s: REST endpoints required", name)
		}
		if ex.Websocket.PublicURL == "" {
			return fmt.Errorf("exchange %s: websocket public URL required", name)
		}
	}

	// Validate adapters
	if c.Adapters.Binance.BookRefreshInterval == 0 {
		c.Adapters.Binance.BookRefreshInterval = 1 * time.Minute
	}

	// Validate dispatcher routes
	if c.Dispatcher.Routes == nil {
		// Routes are optional; empty map is valid
		c.Dispatcher.Routes = make(map[string]RouteConfig)
	}

	// Apply default interval to RestFns that don't specify one
	for name, route := range c.Dispatcher.Routes {
		modified := false
		for i := range route.RestFns {
			if route.RestFns[i].Interval <= 0 {
				route.RestFns[i].Interval = c.Adapters.Binance.BookRefreshInterval
				modified = true
			}
		}
		if modified {
			c.Dispatcher.Routes[name] = route
		}
	}

	// Validate eventbus
	if c.Eventbus.BufferSize <= 0 {
		return fmt.Errorf("eventbus bufferSize must be >0")
	}

	// Validate telemetry
	if c.Telemetry.ServiceName == "" {
		c.Telemetry.ServiceName = "meltica"
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
