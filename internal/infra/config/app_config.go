// Package config manages application configuration loading and validation.
package config

import (
	"context"
	"errors"
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
	BufferSize    int                 `yaml:"buffer_size" json:"bufferSize"`
	FanoutWorkers FanoutWorkerSetting `yaml:"fanout_workers" json:"fanoutWorkers"`
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
		return fmt.Errorf("fanoutWorkers: invalid value %q", node.Value)
	}
	if val <= 0 {
		return fmt.Errorf("fanoutWorkers: numeric value must be > 0")
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
		return nil, fmt.Errorf("fanoutWorkers: marshal: %w", err)
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
			return fmt.Errorf("fanoutWorkers: invalid value %q", text)
		}
		if val <= 0 {
			return fmt.Errorf("fanoutWorkers: numeric value must be > 0")
		}
		*s = FanoutWorkerSetting{kind: fanoutWorkerExplicit, value: val}
		return nil
	}

	var numeric int
	if err := json.Unmarshal(data, &numeric); err == nil {
		if numeric <= 0 {
			return fmt.Errorf("fanoutWorkers: numeric value must be > 0")
		}
		*s = FanoutWorkerSetting{kind: fanoutWorkerExplicit, value: numeric}
		return nil
	}

	return fmt.Errorf("fanoutWorkers: invalid json value")
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
	WaitQueueSize int `yaml:"wait_queue_size" json:"waitQueueSize"`
}

// PoolConfig controls pooled object capacities.
type PoolConfig struct {
	Event        ObjectPoolConfig `yaml:"event" json:"event"`
	OrderRequest ObjectPoolConfig `yaml:"order_request" json:"orderRequest"`
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
	MaxPositionSize     string               `yaml:"max_position_size" json:"maxPositionSize"`
	MaxNotionalValue    string               `yaml:"max_notional_value" json:"maxNotionalValue"`
	NotionalCurrency    string               `yaml:"notional_currency" json:"notionalCurrency"`
	OrderThrottle       float64              `yaml:"order_throttle" json:"orderThrottle"`
	OrderBurst          int                  `yaml:"order_burst" json:"orderBurst"`
	MaxConcurrentOrders int                  `yaml:"max_concurrent_orders" json:"maxConcurrentOrders"`
	PriceBandPercent    float64              `yaml:"price_band_percent" json:"priceBandPercent"`
	AllowedOrderTypes   []string             `yaml:"allowed_order_types" json:"allowedOrderTypes"`
	KillSwitchEnabled   bool                 `yaml:"kill_switch_enabled" json:"killSwitchEnabled"`
	MaxRiskBreaches     int                  `yaml:"max_risk_breaches" json:"maxRiskBreaches"`
	CircuitBreaker      CircuitBreakerConfig `yaml:"circuit_breaker" json:"circuitBreaker"`
}

// TelemetryConfig configures OTLP exporters (metrics only).
type TelemetryConfig struct {
	OTLPEndpoint  string `yaml:"otlp_endpoint" json:"otlpEndpoint"`
	ServiceName   string `yaml:"service_name" json:"serviceName"`
	OTLPInsecure  bool   `yaml:"otlp_insecure" json:"otlpInsecure"`
	EnableMetrics bool   `yaml:"enable_metrics" json:"enableMetrics"`
}

// AppConfig is the unified Meltica application configuration sourced from YAML.
type AppConfig struct {
	Environment    Environment                 `yaml:"environment" json:"environment"`
	Meta           MetaConfig                  `yaml:"meta" json:"meta"`
	Runtime        RuntimeConfig               `yaml:"runtime" json:"runtime"`
	Providers      map[Provider]map[string]any `yaml:"providers" json:"providers"`
	LambdaManifest LambdaManifest              `yaml:"lambda_manifest" json:"lambdaManifest"`
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

// DefaultAppConfig returns the baseline application configuration used when no file is supplied.
func DefaultAppConfig() AppConfig {
	return AppConfig{
		Environment: EnvDev,
		Meta: MetaConfig{
			Name:        "",
			Version:     "",
			Description: "",
		},
		Runtime:   DefaultRuntimeConfig(),
		Providers: nil,
		LambdaManifest: LambdaManifest{
			Lambdas: nil,
		},
	}
}

// Clone returns a deep copy of the application configuration.
func (c AppConfig) Clone() AppConfig {
	clone := AppConfig{
		Environment: c.Environment,
		Meta: MetaConfig{
			Name:        c.Meta.Name,
			Version:     c.Meta.Version,
			Description: c.Meta.Description,
		},
		Runtime:        c.Runtime.Clone(),
		Providers:      nil,
		LambdaManifest: LambdaManifest{Lambdas: nil},
	}

	if len(c.Providers) > 0 {
		providers := make(map[Provider]map[string]any, len(c.Providers))
		for name, cfg := range c.Providers {
			if len(cfg) == 0 {
				providers[name] = nil
				continue
			}
			providerCfg := make(map[string]any, len(cfg))
			for key, value := range cfg {
				providerCfg[key] = cloneAny(value)
			}
			providers[name] = providerCfg
		}
		clone.Providers = providers
	}

	clone.LambdaManifest = c.LambdaManifest.Clone()
	return clone
}

// LoadOrDefault attempts to load the configuration file, returning defaults when the file is absent.
func LoadOrDefault(ctx context.Context, configPath string) (AppConfig, bool, error) {
	cfg, err := Load(ctx, configPath)
	if err == nil {
		return cfg, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		def := DefaultAppConfig()
		if err := def.normalise(); err != nil {
			return AppConfig{}, false, err
		}
		if err := def.Validate(); err != nil {
			return AppConfig{}, false, err
		}
		return def, false, nil
	}
	return AppConfig{}, false, err
}

// SaveAppConfig persists the supplied configuration to disk using an atomic write strategy.
func SaveAppConfig(path string, cfg AppConfig) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("config path required")
	}

	clone := cfg.Clone()
	if err := clone.normalise(); err != nil {
		return err
	}
	if err := clone.Validate(); err != nil {
		return err
	}

	encoded, err := yaml.Marshal(clone)
	if err != nil {
		return fmt.Errorf("encode app config: %w", err)
	}

	dir := filepath.Dir(trimmed)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "app-config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp app config: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write app config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sync app config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close app config: %w", err)
	}
	if err := os.Rename(tmpPath, trimmed); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace app config: %w", err)
	}
	return nil
}

func cloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		if v == nil {
			return map[string]any(nil)
		}
		clone := make(map[string]any, len(v))
		for key, val := range v {
			clone[key] = cloneAny(val)
		}
		return clone
	case []any:
		if v == nil {
			return []any(nil)
		}
		clone := make([]any, len(v))
		for i, item := range v {
			clone[i] = cloneAny(item)
		}
		return clone
	case []string:
		return append([]string(nil), v...)
	default:
		return v
	}
}
