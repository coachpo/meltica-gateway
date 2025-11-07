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
	"time"

	"gopkg.in/yaml.v3"

	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
)

// EventbusConfig sets in-memory event bus sizing characteristics.
type EventbusConfig struct {
	BufferSize               int                 `yaml:"bufferSize"`
	FanoutWorkers            FanoutWorkerSetting `yaml:"fanoutWorkers"`
	ExtensionPayloadCapBytes int                 `yaml:"extensionPayloadCapBytes"`
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

// StrategiesConfig defines where JavaScript strategy sources are discovered.
type StrategiesConfig struct {
	Directory       string `yaml:"directory"`
	RequireRegistry bool   `yaml:"requireRegistry"`
}

// DatabaseConfig controls PostgreSQL connectivity and migration behaviour.
type DatabaseConfig struct {
	DSN               string        `yaml:"dsn"`
	MaxConns          int32         `yaml:"maxConns"`
	MinConns          int32         `yaml:"minConns"`
	MaxConnLifetime   time.Duration `yaml:"maxConnLifetime"`
	MaxConnIdleTime   time.Duration `yaml:"maxConnIdleTime"`
	HealthCheckPeriod time.Duration `yaml:"healthCheckPeriod"`
	RunMigrations     bool          `yaml:"runMigrations"`
}

func (c *DatabaseConfig) applyDefaults() {
	c.DSN = strings.TrimSpace(c.DSN)
	if c.DSN == "" {
		c.DSN = "postgresql://localhost:5432/meltica"
	}
	if c.MaxConns <= 0 {
		c.MaxConns = 16
	}
	if c.MinConns <= 0 {
		c.MinConns = 1
	}
	if c.MinConns > c.MaxConns {
		c.MinConns = c.MaxConns
	}
	if c.MaxConnLifetime <= 0 {
		c.MaxConnLifetime = 30 * time.Minute
	}
	if c.MaxConnIdleTime <= 0 {
		c.MaxConnIdleTime = 5 * time.Minute
	}
	if c.HealthCheckPeriod <= 0 {
		c.HealthCheckPeriod = 30 * time.Second
	}
}

func (c DatabaseConfig) validate() error {
	if strings.TrimSpace(c.DSN) == "" {
		return fmt.Errorf("dsn required")
	}
	if c.MaxConns <= 0 {
		return fmt.Errorf("maxConns must be >0")
	}
	if c.MinConns < 0 {
		return fmt.Errorf("minConns must be >=0")
	}
	if c.MinConns > c.MaxConns {
		return fmt.Errorf("minConns must be <= maxConns")
	}
	if c.MaxConnLifetime <= 0 {
		return fmt.Errorf("maxConnLifetime must be >0")
	}
	if c.MaxConnIdleTime <= 0 {
		return fmt.Errorf("maxConnIdleTime must be >0")
	}
	if c.HealthCheckPeriod <= 0 {
		return fmt.Errorf("healthCheckPeriod must be >0")
	}
	return nil
}

// AppConfig is the unified Meltica application configuration sourced from YAML.
type AppConfig struct {
	Environment Environment                 `yaml:"environment"`
	Providers   map[Provider]map[string]any `yaml:"providers"`
	Eventbus    EventbusConfig              `yaml:"eventbus"`
	Pools       PoolConfig                  `yaml:"pools"`
	Risk        RiskConfig                  `yaml:"risk"`
	APIServer   APIServerConfig             `yaml:"apiServer"`
	Telemetry   TelemetryConfig             `yaml:"telemetry"`
	Strategies  StrategiesConfig            `yaml:"strategies"`
	Database    DatabaseConfig              `yaml:"database"`
}

func defaultRiskConfig() RiskConfig {
	return RiskConfig{
		MaxPositionSize:     "250",
		MaxNotionalValue:    "50000",
		NotionalCurrency:    "USDT",
		OrderThrottle:       5,
		OrderBurst:          3,
		MaxConcurrentOrders: 6,
		PriceBandPercent:    1.0,
		AllowedOrderTypes:   []string{"Limit", "Market"},
		KillSwitchEnabled:   true,
		MaxRiskBreaches:     3,
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:   true,
			Threshold: 4,
			Cooldown:  "90s",
		},
	}
}

func (c *AppConfig) applyRiskDefaults(riskProvided bool) {
	if riskProvided {
		return
	}
	c.Risk = defaultRiskConfig()
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

	var envelope map[string]any
	if err := yaml.Unmarshal(bytes, &envelope); err != nil {
		return AppConfig{}, fmt.Errorf("unmarshal config: %w", err)
	}
	riskProvided := false
	if rawRisk, ok := envelope["risk"]; ok && rawRisk != nil {
		switch typed := rawRisk.(type) {
		case map[string]any:
			if len(typed) > 0 {
				riskProvided = true
			}
		default:
			riskProvided = true
		}
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.applyRiskDefaults(riskProvided)

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

	if c.Eventbus.ExtensionPayloadCapBytes == 0 {
		c.Eventbus.ExtensionPayloadCapBytes = eventbus.DefaultExtensionPayloadCapBytes
	}

	strategyDir := strings.TrimSpace(c.Strategies.Directory)
	if strategyDir == "" {
		strategyDir = "strategies"
	}
	c.Strategies.Directory = filepath.Clean(strategyDir)

	if c.Risk.OrderBurst <= 0 {
		c.Risk.OrderBurst = 1
	}
	if c.Risk.MaxRiskBreaches < 0 {
		c.Risk.MaxRiskBreaches = 0
	}
	if c.Risk.CircuitBreaker.Threshold < 0 {
		c.Risk.CircuitBreaker.Threshold = 0
	}
	if len(c.Risk.AllowedOrderTypes) > 0 {
		normalized := make([]string, 0, len(c.Risk.AllowedOrderTypes))
		seen := make(map[string]struct{}, len(c.Risk.AllowedOrderTypes))
		for _, ot := range c.Risk.AllowedOrderTypes {
			trimmed := strings.TrimSpace(ot)
			if trimmed == "" {
				continue
			}
			key := strings.ToLower(trimmed)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			normalized = append(normalized, trimmed)
		}
		c.Risk.AllowedOrderTypes = normalized
	}

	c.Database.applyDefaults()

	return nil
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
	if c.Eventbus.FanoutWorkerCount() <= 0 {
		return fmt.Errorf("eventbus fanoutWorkers must be >0")
	}
	if c.Eventbus.ExtensionPayloadCapBytes <= 0 {
		return fmt.Errorf("eventbus extensionPayloadCapBytes must be >0")
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
	if strings.TrimSpace(c.Strategies.Directory) == "" {
		return fmt.Errorf("strategies directory required")
	}

	if err := c.Database.validate(); err != nil {
		return fmt.Errorf("database: %w", err)
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
