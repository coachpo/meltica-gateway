// Package config manages application configuration loading and validation.
package config

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// EventbusConfig sets in-memory event bus sizing characteristics.
type EventbusConfig struct {
	BufferSize    int                 `yaml:"bufferSize"`
	FanoutWorkers FanoutWorkerSetting `yaml:"fanoutWorkers"`
}

type fanoutWorkerKind int

const (
	fanoutWorkerUnset fanoutWorkerKind = iota
	fanoutWorkerExplicit
	fanoutWorkerAuto
	fanoutWorkerDefault
)

// FanoutWorkerSetting encapsulates the fanout worker configuration allowing both numeric and symbolic values.
type FanoutWorkerSetting struct {
	kind  fanoutWorkerKind
	value int
}

// UnmarshalYAML supports integer, "auto", and "default" values for fanout workers.
func (s *FanoutWorkerSetting) UnmarshalYAML(node *yaml.Node) error {
	if node == nil {
		*s = FanoutWorkerSetting{kind: fanoutWorkerUnset, value: 0}
		return nil
	}

	text := strings.TrimSpace(node.Value)
	if text == "" {
		s.kind = fanoutWorkerUnset
		s.value = 0
		return nil
	}

	lower := strings.ToLower(text)
	switch lower {
	case "auto":
		s.kind = fanoutWorkerAuto
		s.value = 0
		return nil
	case "default":
		s.kind = fanoutWorkerDefault
		s.value = 0
		return nil
	}

	// Attempt numeric parse for both explicit integers and scalar yaml ints.
	val, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("fanoutWorkers: invalid value %q", node.Value)
	}
	if val <= 0 {
		return fmt.Errorf("fanoutWorkers: numeric value must be > 0")
	}
	s.kind = fanoutWorkerExplicit
	s.value = val
	return nil
}

// resolve returns the effective worker count derived from the setting.
func (s FanoutWorkerSetting) resolve() int {
	switch s.kind {
	case fanoutWorkerExplicit:
		return s.value
	case fanoutWorkerAuto:
		if cores := runtime.NumCPU(); cores > 0 {
			return cores
		}
		return 4
	case fanoutWorkerDefault, fanoutWorkerUnset:
		return 4
	default:
		return 4
	}
}

// FanoutWorkerCount returns the resolved worker count for use by runtime components.
func (c EventbusConfig) FanoutWorkerCount() int {
	return c.FanoutWorkers.resolve()
}

// ObjectPoolConfig describes sizing for a single named pool.
type ObjectPoolConfig struct {
	Size          int `yaml:"size"`
	WaitQueueSize int `yaml:"waitQueueSize"`
}

// PoolConfig controls pooled object capacities.
type PoolConfig struct {
	Event        ObjectPoolConfig `yaml:"event"`
	OrderRequest ObjectPoolConfig `yaml:"orderRequest"`
}

// QueueSize returns the effective pending borrower queue size, defaulting to Size.
func (c ObjectPoolConfig) QueueSize() int {
	if c.WaitQueueSize <= 0 {
		return c.Size
	}
	return c.WaitQueueSize
}

// APIServerConfig configures the gateway's HTTP control surface.
type APIServerConfig struct {
	Addr string `yaml:"addr"`
}

// RiskConfig defines risk parameters for a single strategy.

// CircuitBreakerConfig describes cascading halt behaviour for repeated risk breaches.
type CircuitBreakerConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Threshold int    `yaml:"threshold"`
	Cooldown  string `yaml:"cooldown"`
}

// RiskConfig defines risk parameters for a single strategy.
type RiskConfig struct {
	MaxPositionSize     string               `yaml:"maxPositionSize"`
	MaxNotionalValue    string               `yaml:"maxNotionalValue"`
	NotionalCurrency    string               `yaml:"notionalCurrency"`
	OrderThrottle       float64              `yaml:"orderThrottle"`
	OrderBurst          int                  `yaml:"orderBurst"`
	MaxConcurrentOrders int                  `yaml:"maxConcurrentOrders"`
	PriceBandPercent    float64              `yaml:"priceBandPercent"`
	AllowedOrderTypes   []string             `yaml:"allowedOrderTypes"`
	KillSwitchEnabled   bool                 `yaml:"killSwitchEnabled"`
	MaxRiskBreaches     int                  `yaml:"maxRiskBreaches"`
	CircuitBreaker      CircuitBreakerConfig `yaml:"circuitBreaker"`
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
	Providers      map[Provider]map[string]any `yaml:"providers"`
	Eventbus       EventbusConfig              `yaml:"eventbus"`
	Pools          PoolConfig                  `yaml:"pools"`
	Risk           RiskConfig                  `yaml:"risk"`
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

	if err := cfg.normalise(); err != nil {
		return AppConfig{}, err
	}

	if err := cfg.Validate(); err != nil {
		return AppConfig{}, err
	}

	return cfg, nil
}

func (c *AppConfig) normalise() error {
	normalised := make(map[Provider]map[string]any, len(c.Providers))
	for key, value := range c.Providers {
		normalizedKey := Provider(normalizeProviderName(string(key)))
		if _, exists := normalised[normalizedKey]; exists {
			return fmt.Errorf("duplicate provider name %q", normalizedKey)
		}
		normalised[normalizedKey] = value
	}
	c.Providers = normalised

	c.Environment = Environment(strings.ToLower(strings.TrimSpace(string(c.Environment))))
	c.APIServer.Addr = strings.TrimSpace(c.APIServer.Addr)
	c.Telemetry.OTLPEndpoint = strings.TrimSpace(c.Telemetry.OTLPEndpoint)
	c.Telemetry.ServiceName = strings.TrimSpace(c.Telemetry.ServiceName)

	if c.Risk.OrderBurst <= 0 {
		c.Risk.OrderBurst = 1
	}
	if c.Risk.MaxRiskBreaches < 0 {
		c.Risk.MaxRiskBreaches = 0
	}
	if c.Risk.CircuitBreaker.Threshold < 0 {
		c.Risk.CircuitBreaker.Threshold = 0
	}
	for i, ot := range c.Risk.AllowedOrderTypes {
		c.Risk.AllowedOrderTypes[i] = strings.TrimSpace(ot)
	}
	return nil
}

// Validate performs semantic validation on the configuration.
func (c AppConfig) Validate() error {
	switch c.Environment {
	case EnvDev, EnvStaging, EnvProd:
	default:
		return fmt.Errorf("environment must be one of dev, staging, prod")
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider must be configured")
	}

	if c.Eventbus.BufferSize <= 0 {
		return fmt.Errorf("eventbus bufferSize must be >0")
	}
	if c.Eventbus.FanoutWorkerCount() <= 0 {
		return fmt.Errorf("eventbus fanoutWorkers must be >0")
	}

	if c.Pools.Event.Size <= 0 {
		return fmt.Errorf("pools.event.size must be >0")
	}
	if c.Pools.Event.WaitQueueSize < 0 {
		return fmt.Errorf("pools.event.waitQueueSize must be >=0")
	}
	if c.Pools.OrderRequest.Size <= 0 {
		return fmt.Errorf("pools.orderRequest.size must be >0")
	}
	if c.Pools.OrderRequest.WaitQueueSize < 0 {
		return fmt.Errorf("pools.orderRequest.waitQueueSize must be >=0")
	}

	if strings.TrimSpace(c.APIServer.Addr) == "" {
		return fmt.Errorf("apiServer addr required")
	}

	if c.Risk.MaxPositionSize == "" {
		return fmt.Errorf("risk maxPositionSize required")
	}
	if c.Risk.MaxNotionalValue == "" {
		return fmt.Errorf("risk maxNotionalValue required")
	}
	if c.Risk.NotionalCurrency == "" {
		return fmt.Errorf("risk notionalCurrency required")
	}
	if c.Risk.OrderThrottle <= 0 {
		return fmt.Errorf("risk orderThrottle must be > 0")
	}
	if c.Risk.OrderBurst <= 0 {
		return fmt.Errorf("risk orderBurst must be > 0")
	}
	if c.Risk.MaxConcurrentOrders < 0 {
		return fmt.Errorf("risk maxConcurrentOrders must be >= 0")
	}
	if c.Risk.PriceBandPercent < 0 {
		return fmt.Errorf("risk priceBandPercent must be >= 0")
	}
	if c.Risk.MaxRiskBreaches < 0 {
		return fmt.Errorf("risk maxRiskBreaches must be >= 0")
	}
	if c.Risk.CircuitBreaker.Threshold < 0 {
		return fmt.Errorf("risk circuitBreaker threshold must be >= 0")
	}
	if c.Risk.CircuitBreaker.Enabled && strings.TrimSpace(c.Risk.CircuitBreaker.Cooldown) == "" {
		return fmt.Errorf("risk circuitBreaker cooldown required when enabled")
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
