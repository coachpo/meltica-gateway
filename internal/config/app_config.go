package config

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// EventbusConfig sets in-memory event bus sizing characteristics.
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
	EnableMetrics bool   `yaml:"enableMetrics"`
}

// AppConfig is the unified Meltica application configuration sourced from YAML.
type AppConfig struct {
	Environment    Environment                 `yaml:"environment"`
	Exchanges      map[Exchange]map[string]any `yaml:"exchanges"`
	Eventbus       EventbusConfig              `yaml:"eventbus"`
	Pools          PoolConfig                  `yaml:"pools"`
	APIServer      APIServerConfig             `yaml:"apiServer"`
	Telemetry      TelemetryConfig             `yaml:"telemetry"`
	LambdaManifest LambdaManifest              `yaml:"lambdaManifest"`
}

// Load reads and validates an AppConfig from the provided YAML file.
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

	if err := cfg.Validate(); err != nil {
		return AppConfig{}, err
	}

	return cfg, nil
}

func (c *AppConfig) normalise() {
	normalised := make(map[Exchange]map[string]any, len(c.Exchanges))
	for key, value := range c.Exchanges {
		normalizedKey := Exchange(normalizeExchangeName(string(key)))
		normalised[normalizedKey] = value
	}
	c.Exchanges = normalised

	c.Environment = Environment(normalizeExchangeName(string(c.Environment)))
	c.APIServer.Addr = strings.TrimSpace(c.APIServer.Addr)
	c.Telemetry.OTLPEndpoint = strings.TrimSpace(c.Telemetry.OTLPEndpoint)
	c.Telemetry.ServiceName = strings.TrimSpace(c.Telemetry.ServiceName)
}

// Validate performs semantic validation on the configuration.
func (c AppConfig) Validate() error {
	switch c.Environment {
	case EnvDev, EnvStaging, EnvProd:
	default:
		return fmt.Errorf("environment must be one of dev, staging, prod")
	}

	if c.Eventbus.BufferSize <= 0 {
		return fmt.Errorf("eventbus bufferSize must be >0")
	}
	if c.Eventbus.FanoutWorkers <= 0 {
		return fmt.Errorf("eventbus fanoutWorkers must be >0")
	}

	if c.Pools.EventSize <= 0 {
		return fmt.Errorf("pools eventSize must be >0")
	}
	if c.Pools.OrderRequestSize <= 0 {
		return fmt.Errorf("pools orderRequestSize must be >0")
	}

	if strings.TrimSpace(c.APIServer.Addr) == "" {
		return fmt.Errorf("apiServer addr required")
	}

	if strings.TrimSpace(c.Telemetry.ServiceName) == "" {
		return fmt.Errorf("telemetry serviceName required")
	}

	if err := c.LambdaManifest.Validate(); err != nil {
		return fmt.Errorf("lambda manifest: %w", err)
	}

	return nil
}

func openConfigFile(path string) (io.Reader, func(), error) {
	candidate := strings.TrimSpace(path)
	candidate = filepath.Clean(candidate)

	file, err := os.Open(candidate) // #nosec G304 -- path is operator controlled.
	if err != nil {
		return nil, nil, fmt.Errorf("open app config: %w", err)
	}
	return file, func() { _ = file.Close() }, nil
}
