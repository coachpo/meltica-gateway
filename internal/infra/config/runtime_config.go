package config

import (
	"fmt"
	"strings"
	"sync"
)

// RuntimeConfig captures mutable configuration managed at runtime.
type RuntimeConfig struct {
	Eventbus  EventbusConfig  `json:"eventbus" yaml:"eventbus"`
	Pools     PoolConfig      `json:"pools" yaml:"pools"`
	Risk      RiskConfig      `json:"risk" yaml:"risk"`
	APIServer APIServerConfig `json:"apiServer" yaml:"api_server"`
	Telemetry TelemetryConfig `json:"telemetry" yaml:"telemetry"`
}

// DefaultRiskConfig returns the default risk configuration applied when no overrides are supplied.
func DefaultRiskConfig() RiskConfig {
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

// DefaultRuntimeConfig returns the default runtime configuration used when no overrides are supplied.
func DefaultRuntimeConfig() RuntimeConfig {
	cfg := RuntimeConfig{
		Eventbus: EventbusConfig{
			BufferSize:    8192,
			FanoutWorkers: FanoutWorkerSetting{kind: fanoutWorkerExplicit, value: 8},
		},
		Pools: PoolConfig{
			Event: ObjectPoolConfig{
				Size:          8192,
				WaitQueueSize: 8192,
			},
			OrderRequest: ObjectPoolConfig{
				Size:          4096,
				WaitQueueSize: 4096,
			},
		},
		Risk:      cloneRiskConfig(DefaultRiskConfig()),
		APIServer: APIServerConfig{Addr: ":8880"},
		Telemetry: TelemetryConfig{
			OTLPEndpoint:  "",
			ServiceName:   "meltica-gateway",
			OTLPInsecure:  true,
			EnableMetrics: true,
		},
	}
	cfg.Normalise()
	return cfg
}

// Clone returns a deep copy of the runtime configuration.
func (c RuntimeConfig) Clone() RuntimeConfig {
	cloned := c
	cloned.Risk = cloneRiskConfig(c.Risk)
	return cloned
}

// Normalise adjusts fields with derived defaults and trims whitespace.
func (c *RuntimeConfig) Normalise() {
	if c == nil {
		return
	}
	if isRiskConfigUnset(c.Risk) {
		c.Risk = cloneRiskConfig(DefaultRiskConfig())
	}
	c.Risk.MaxPositionSize = strings.TrimSpace(c.Risk.MaxPositionSize)
	c.Risk.MaxNotionalValue = strings.TrimSpace(c.Risk.MaxNotionalValue)
	c.Risk.NotionalCurrency = strings.TrimSpace(c.Risk.NotionalCurrency)
	c.Risk.CircuitBreaker.Cooldown = strings.TrimSpace(c.Risk.CircuitBreaker.Cooldown)

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
}

func isRiskConfigUnset(cfg RiskConfig) bool {
	if strings.TrimSpace(cfg.MaxPositionSize) != "" {
		return false
	}
	if strings.TrimSpace(cfg.MaxNotionalValue) != "" {
		return false
	}
	if strings.TrimSpace(cfg.NotionalCurrency) != "" {
		return false
	}
	if cfg.OrderThrottle != 0 || cfg.OrderBurst != 0 || cfg.MaxConcurrentOrders != 0 {
		return false
	}
	if cfg.PriceBandPercent != 0 {
		return false
	}
	if len(cfg.AllowedOrderTypes) > 0 {
		return false
	}
	if cfg.KillSwitchEnabled || cfg.MaxRiskBreaches != 0 {
		return false
	}
	if cfg.CircuitBreaker.Enabled || cfg.CircuitBreaker.Threshold != 0 {
		return false
	}
	if strings.TrimSpace(cfg.CircuitBreaker.Cooldown) != "" {
		return false
	}
	return true
}

// Validate performs semantic validation on runtime configuration fields.
func (c RuntimeConfig) Validate() error {
	if c.Eventbus.BufferSize <= 0 {
		return fmt.Errorf("eventbus.bufferSize must be > 0")
	}
	if c.Eventbus.FanoutWorkerCount() <= 0 {
		return fmt.Errorf("eventbus.fanoutWorkers must be > 0")
	}

	if c.Pools.Event.Size <= 0 {
		return fmt.Errorf("pools.event.size must be > 0")
	}
	if c.Pools.Event.WaitQueueSize < 0 {
		return fmt.Errorf("pools.event.waitQueueSize must be >= 0")
	}
	if c.Pools.OrderRequest.Size <= 0 {
		return fmt.Errorf("pools.orderRequest.size must be > 0")
	}
	if c.Pools.OrderRequest.WaitQueueSize < 0 {
		return fmt.Errorf("pools.orderRequest.waitQueueSize must be >= 0")
	}

	if strings.TrimSpace(c.APIServer.Addr) == "" {
		return fmt.Errorf("apiServer.addr required")
	}

	if strings.TrimSpace(c.Risk.MaxPositionSize) == "" {
		return fmt.Errorf("risk.maxPositionSize required")
	}
	if strings.TrimSpace(c.Risk.MaxNotionalValue) == "" {
		return fmt.Errorf("risk.maxNotionalValue required")
	}
	if strings.TrimSpace(c.Risk.NotionalCurrency) == "" {
		return fmt.Errorf("risk.notionalCurrency required")
	}
	if c.Risk.OrderThrottle <= 0 {
		return fmt.Errorf("risk.orderThrottle must be > 0")
	}
	if c.Risk.OrderBurst <= 0 {
		return fmt.Errorf("risk.orderBurst must be > 0")
	}
	if c.Risk.MaxConcurrentOrders < 0 {
		return fmt.Errorf("risk.maxConcurrentOrders must be >= 0")
	}
	if c.Risk.PriceBandPercent < 0 {
		return fmt.Errorf("risk.priceBandPercent must be >= 0")
	}
	if c.Risk.MaxRiskBreaches < 0 {
		return fmt.Errorf("risk.maxRiskBreaches must be >= 0")
	}
	if c.Risk.CircuitBreaker.Threshold < 0 {
		return fmt.Errorf("risk.circuitBreaker.threshold must be >= 0")
	}
	if c.Risk.CircuitBreaker.Enabled && strings.TrimSpace(c.Risk.CircuitBreaker.Cooldown) == "" {
		return fmt.Errorf("risk.circuitBreaker.cooldown required when enabled")
	}

	if strings.TrimSpace(c.Telemetry.ServiceName) == "" {
		return fmt.Errorf("telemetry.serviceName required")
	}

	return nil
}

// RuntimeStore provides concurrency-safe access to runtime configuration.
type RuntimeStore struct {
	mu  sync.RWMutex
	cfg RuntimeConfig
}

// NewRuntimeStore constructs a runtime configuration store using the supplied initial configuration.
func NewRuntimeStore(initial RuntimeConfig) (*RuntimeStore, error) {
	cfg := initial.Clone()
	cfg.Normalise()
	if strings.TrimSpace(cfg.Telemetry.ServiceName) == "" {
		cfg.Telemetry.ServiceName = DefaultRuntimeConfig().Telemetry.ServiceName
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &RuntimeStore{mu: sync.RWMutex{}, cfg: cfg}, nil
}

// Snapshot returns a copy of the current runtime configuration.
func (s *RuntimeStore) Snapshot() RuntimeConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Clone()
}

// Replace swaps the current runtime configuration with the supplied payload after validation.
func (s *RuntimeStore) Replace(cfg RuntimeConfig) (RuntimeConfig, error) {
	updated := cfg.Clone()
	updated.Normalise()
	if strings.TrimSpace(updated.Telemetry.ServiceName) == "" {
		updated.Telemetry.ServiceName = DefaultRuntimeConfig().Telemetry.ServiceName
	}
	if err := updated.Validate(); err != nil {
		return RuntimeConfig{}, err
	}

	s.mu.Lock()
	s.cfg = updated
	s.mu.Unlock()

	return updated.Clone(), nil
}

// UpdateRisk updates only the risk section of the runtime configuration.
func (s *RuntimeStore) UpdateRisk(cfg RiskConfig) (RiskConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	merged := s.cfg.Clone()
	merged.Risk = cloneRiskConfig(cfg)
	merged.Normalise()
	if err := merged.Validate(); err != nil {
		return RiskConfig{}, err
	}
	s.cfg = merged
	return cloneRiskConfig(merged.Risk), nil
}

func cloneRiskConfig(src RiskConfig) RiskConfig {
	cloned := src
	if len(src.AllowedOrderTypes) > 0 {
		cloned.AllowedOrderTypes = append([]string(nil), src.AllowedOrderTypes...)
	} else {
		cloned.AllowedOrderTypes = nil
	}
	return cloned
}
