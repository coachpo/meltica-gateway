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

	json "github.com/goccy/go-json"
	"gopkg.in/yaml.v3"
)

// EventbusConfig sets in-memory event bus sizing characteristics.
type EventbusConfig struct {
	BufferSize    int                 `yaml:"buffer_size" json:"buffer_size"`
	FanoutWorkers FanoutWorkerSetting `yaml:"fanout_workers" json:"fanout_workers"`
}

// MetaConfig captures descriptive metadata for the configuration bundle.
type MetaConfig struct {
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description" json:"description"`
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
		return fmt.Errorf("fanout_workers: invalid value %q", node.Value)
	}
	if val <= 0 {
		return fmt.Errorf("fanout_workers: numeric value must be > 0")
	}
	s.kind = fanoutWorkerExplicit
	s.value = val
	return nil
}

// MarshalJSON renders the fanout worker setting into JSON preserving symbolic values.
func (s FanoutWorkerSetting) MarshalJSON() ([]byte, error) {
	var value any
	switch s.kind {
	case fanoutWorkerExplicit:
		value = s.value
	case fanoutWorkerAuto:
		value = "auto"
	case fanoutWorkerDefault, fanoutWorkerUnset:
		value = "default"
	default:
		value = "default"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("fanout_workers: marshal: %w", err)
	}
	return data, nil
}

// UnmarshalJSON accepts integer, "auto", and "default" values for fanout workers.
func (s *FanoutWorkerSetting) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			*s = FanoutWorkerSetting{kind: fanoutWorkerUnset, value: 0}
			return nil
		}
		switch strings.ToLower(trimmed) {
		case "auto":
			*s = FanoutWorkerSetting{kind: fanoutWorkerAuto, value: 0}
			return nil
		case "default":
			*s = FanoutWorkerSetting{kind: fanoutWorkerDefault, value: 0}
			return nil
		}

		val, err := strconv.Atoi(trimmed)
		if err != nil {
			return fmt.Errorf("fanout_workers: invalid value %q", text)
		}
		if val <= 0 {
			return fmt.Errorf("fanout_workers: numeric value must be > 0")
		}
		*s = FanoutWorkerSetting{kind: fanoutWorkerExplicit, value: val}
		return nil
	}

	var numeric int
	if err := json.Unmarshal(data, &numeric); err == nil {
		if numeric <= 0 {
			return fmt.Errorf("fanout_workers: numeric value must be > 0")
		}
		*s = FanoutWorkerSetting{kind: fanoutWorkerExplicit, value: numeric}
		return nil
	}

	return fmt.Errorf("fanout_workers: invalid json value")
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
	Size          int `yaml:"size" json:"size"`
	WaitQueueSize int `yaml:"wait_queue_size" json:"wait_queue_size"`
}

// PoolConfig controls pooled object capacities.
type PoolConfig struct {
	Event        ObjectPoolConfig `yaml:"event" json:"event"`
	OrderRequest ObjectPoolConfig `yaml:"order_request" json:"order_request"`
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
	Addr string `yaml:"addr" json:"addr"`
}

// RiskConfig defines risk parameters for a single strategy.

// CircuitBreakerConfig describes cascading halt behaviour for repeated risk breaches.
type CircuitBreakerConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	Threshold int    `yaml:"threshold" json:"threshold"`
	Cooldown  string `yaml:"cooldown" json:"cooldown"`
}

// RiskConfig defines risk parameters for a single strategy.
type RiskConfig struct {
	MaxPositionSize     string               `yaml:"max_position_size" json:"max_position_size"`
	MaxNotionalValue    string               `yaml:"max_notional_value" json:"max_notional_value"`
	NotionalCurrency    string               `yaml:"notional_currency" json:"notional_currency"`
	OrderThrottle       float64              `yaml:"order_throttle" json:"order_throttle"`
	OrderBurst          int                  `yaml:"order_burst" json:"order_burst"`
	MaxConcurrentOrders int                  `yaml:"max_concurrent_orders" json:"max_concurrent_orders"`
	PriceBandPercent    float64              `yaml:"price_band_percent" json:"price_band_percent"`
	AllowedOrderTypes   []string             `yaml:"allowed_order_types" json:"allowed_order_types"`
	KillSwitchEnabled   bool                 `yaml:"kill_switch_enabled" json:"kill_switch_enabled"`
	MaxRiskBreaches     int                  `yaml:"max_risk_breaches" json:"max_risk_breaches"`
	CircuitBreaker      CircuitBreakerConfig `yaml:"circuit_breaker" json:"circuit_breaker"`
}

// TelemetryConfig configures OTLP exporters (metrics only).
type TelemetryConfig struct {
	OTLPEndpoint  string `yaml:"otlp_endpoint" json:"otlp_endpoint"`
	ServiceName   string `yaml:"service_name" json:"service_name"`
	OTLPInsecure  bool   `yaml:"otlp_insecure" json:"otlp_insecure"`
	EnableMetrics bool   `yaml:"enable_metrics" json:"enable_metrics"`
}

// AppConfig is the unified Meltica application configuration sourced from YAML.
type AppConfig struct {
	Environment    Environment                 `yaml:"environment" json:"environment"`
	Meta           MetaConfig                  `yaml:"meta" json:"meta"`
	Runtime        RuntimeConfig               `yaml:"runtime" json:"runtime"`
	Providers      map[Provider]map[string]any `yaml:"providers" json:"providers"`
	LambdaManifest LambdaManifest              `yaml:"lambda_manifest" json:"lambda_manifest"`
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
	c.Meta.Name = strings.TrimSpace(c.Meta.Name)
	c.Meta.Version = strings.TrimSpace(c.Meta.Version)
	c.Meta.Description = strings.TrimSpace(c.Meta.Description)

	c.Runtime.Normalise()
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

	if err := c.Runtime.Validate(); err != nil {
		return fmt.Errorf("runtime: %w", err)
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
