package config

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/schema"
	"gopkg.in/yaml.v3"
)

// StreamingConfig captures the gateway streaming configuration tree.
type StreamingConfig struct {
	Adapters   AdapterSet       `yaml:"adapter"`
	Dispatcher DispatcherConfig `yaml:"dispatcher"`
	Snapshot   SnapshotConfig   `yaml:"snapshot"`
	Databus    DatabusConfig    `yaml:"databus"`
	Telemetry  TelemetryConfig  `yaml:"telemetry"`
}

// AdapterSet encapsulates adapter-specific configuration.
type AdapterSet struct {
	Binance BinanceAdapterConfig `yaml:"binance"`
}

// BinanceAdapterConfig declares Binance transport configuration.
type BinanceAdapterConfig struct {
	WS   BinanceWSConfig   `yaml:"ws"`
	REST BinanceRESTConfig `yaml:"rest"`
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

// SnapshotConfig controls snapshot store behaviour.
type SnapshotConfig struct {
	TTL time.Duration `yaml:"ttl"`
}

// DatabusConfig sets data bus buffer sizing.
type DatabusConfig struct {
	BufferSize    int `yaml:"bufferSize"`
	PerInstrument int `yaml:"perInstrument"`
}

// TelemetryConfig configures OTLP exporters.
type TelemetryConfig struct {
	OTLPEndpoint string `yaml:"otlpEndpoint"`
	ServiceName  string `yaml:"serviceName"`
}

// ProviderConfig declares exchange connection settings for V2 streaming configuration.
type ProviderConfig struct {
	Name                string        `yaml:"name"`
	WSEndpoint          string        `yaml:"ws_endpoint"`
	RESTEndpoint        string        `yaml:"rest_endpoint"`
	Symbols             []string      `yaml:"symbols"`
	BookRefreshInterval time.Duration `yaml:"book_refresh_interval"`
}

// MergeRuleConfig defines orchestrator merge behaviour for multi-provider subscriptions.
type MergeRuleConfig struct {
	MergeKey       string        `yaml:"merge_key"`
	Providers      []string      `yaml:"providers"`
	WindowDuration time.Duration `yaml:"window_duration"`
	MaxEvents      int           `yaml:"max_events"`
	PartialPolicy  string        `yaml:"partial_policy"`
}

// OrchestratorConfig captures merge rules for V2 configuration.
type OrchestratorConfig struct {
	MergeRules []MergeRuleConfig `yaml:"merge_rules"`
}

// StreamOrderingConfig configures dispatcher ordering buffers.
type StreamOrderingConfig struct {
	LatenessTolerance time.Duration `yaml:"lateness_tolerance"`
	FlushInterval     time.Duration `yaml:"flush_interval"`
	MaxBufferSize     int           `yaml:"max_buffer_size"`
}

// BackpressureConfig configures dispatcher token-bucket behaviour.
type BackpressureConfig struct {
	TokenRatePerStream int `yaml:"token_rate_per_stream"`
	TokenBurst         int `yaml:"token_burst"`
}

// DispatcherRuntimeConfig aggregates dispatcher tunables for V2 configuration.
type DispatcherRuntimeConfig struct {
	StreamOrdering   StreamOrderingConfig `yaml:"stream_ordering"`
	Backpressure     BackpressureConfig   `yaml:"backpressure"`
	CoalescableTypes []string             `yaml:"coalescable_types"`
}

// SubscriptionConfig declares consumer subscription preferences.
type SubscriptionConfig struct {
	Symbol     string   `yaml:"symbol"`
	Providers  []string `yaml:"providers"`
	EventTypes []string `yaml:"event_types"`
	Merged     bool     `yaml:"merged"`
}

// ConsumerConfig captures consumer runtime settings including trading switch state.
type ConsumerConfig struct {
	Name          string               `yaml:"name"`
	ConsumerID    string               `yaml:"consumer_id"`
	TradingSwitch string               `yaml:"trading_switch"`
	Subscriptions []SubscriptionConfig `yaml:"subscriptions"`
}

// StreamingConfigV2 reflects the V2 monolithic configuration layout introduced in plan.md.
type StreamingConfigV2 struct {
	Providers    []ProviderConfig        `yaml:"providers"`
	Orchestrator OrchestratorConfig      `yaml:"orchestrator"`
	Dispatcher   DispatcherRuntimeConfig `yaml:"dispatcher"`
	Consumers    []ConsumerConfig        `yaml:"consumers"`
	Telemetry    TelemetryConfig         `yaml:"telemetry"`
}

// LoadStreamingConfig loads a streaming configuration YAML document from disk.
func LoadStreamingConfig(ctx context.Context, path string) (StreamingConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = os.Getenv("MELTICA_CONFIG")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		path = "config/streaming.yaml"
	}

	reader, closer, err := openStreamingFile(path)
	if err != nil {
		return StreamingConfig{}, err
	}
	defer closer()

	bytes, err := io.ReadAll(reader)
	if err != nil {
		return StreamingConfig{}, fmt.Errorf("read streaming config: %w", err)
	}

	var cfg StreamingConfig
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return StreamingConfig{}, fmt.Errorf("unmarshal streaming config: %w", err)
	}

	if err := cfg.Validate(ctx); err != nil {
		return StreamingConfig{}, err
	}
	return cfg, nil
}

// Validate performs semantic validation on the loaded configuration.
func (c StreamingConfig) Validate(ctx context.Context) error {
	_ = ctx
	if c.Dispatcher.Routes == nil {
		return fmt.Errorf("streaming dispatcher routes required")
	}
	for name, route := range c.Dispatcher.Routes {
		typeName := schema.CanonicalType(name)
		if err := typeName.Validate(); err != nil {
			return fmt.Errorf("dispatcher route %s: %w", name, err)
		}
		for _, filter := range route.Filters {
			if strings.TrimSpace(filter.Field) == "" {
				return fmt.Errorf("dispatcher route %s: filter field required", name)
			}
			if strings.TrimSpace(filter.Op) == "" {
				return fmt.Errorf("dispatcher route %s: filter op required", name)
			}
		}
		for _, rest := range route.RestFns {
			if rest.Interval <= 0 {
				return fmt.Errorf("dispatcher route %s: rest interval must be >0", name)
			}
		}
	}
	if c.Databus.BufferSize <= 0 {
		return fmt.Errorf("databus bufferSize must be >0")
	}
	if c.Databus.PerInstrument <= 0 {
		return fmt.Errorf("databus perInstrument must be >0")
	}
	if c.Snapshot.TTL < 0 {
		return fmt.Errorf("snapshot ttl must be >=0")
	}
	return nil
}

// Validate performs semantic validation on the V2 streaming configuration.
func (c StreamingConfigV2) Validate(ctx context.Context) error {
	_ = ctx
	if len(c.Providers) == 0 {
		return fmt.Errorf("providers required")
	}
	for i, p := range c.Providers {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("provider[%d]: name required", i)
		}
		if strings.TrimSpace(p.WSEndpoint) == "" {
			return fmt.Errorf("provider[%d]: ws_endpoint required", i)
		}
		if strings.TrimSpace(p.RESTEndpoint) == "" {
			return fmt.Errorf("provider[%d]: rest_endpoint required", i)
		}
		if len(p.Symbols) == 0 {
			return fmt.Errorf("provider[%d]: symbols required", i)
		}
		for _, symbol := range p.Symbols {
			if err := schema.ValidateInstrument(symbol); err != nil {
				return fmt.Errorf("provider[%d]: %w", i, err)
			}
		}
		if p.BookRefreshInterval <= 0 {
			return fmt.Errorf("provider[%d]: book_refresh_interval must be >0", i)
		}
	}

	for i, rule := range c.Orchestrator.MergeRules {
		if strings.TrimSpace(rule.MergeKey) == "" {
			return fmt.Errorf("orchestrator.merge_rules[%d]: merge_key required", i)
		}
		if len(rule.Providers) < 2 {
			return fmt.Errorf("orchestrator.merge_rules[%d]: at least two providers required", i)
		}
		if rule.WindowDuration <= 0 {
			return fmt.Errorf("orchestrator.merge_rules[%d]: window_duration must be >0", i)
		}
		if rule.MaxEvents <= 0 {
			return fmt.Errorf("orchestrator.merge_rules[%d]: max_events must be >0", i)
		}
		policy := strings.ToLower(strings.TrimSpace(rule.PartialPolicy))
		if policy != "suppress" && policy != "emit" && policy != "drop" {
			return fmt.Errorf("orchestrator.merge_rules[%d]: partial_policy must be suppress|emit|drop", i)
		}
	}

	if c.Dispatcher.StreamOrdering.LatenessTolerance <= 0 {
		return fmt.Errorf("dispatcher.stream_ordering.lateness_tolerance must be >0")
	}
	if c.Dispatcher.StreamOrdering.FlushInterval <= 0 {
		return fmt.Errorf("dispatcher.stream_ordering.flush_interval must be >0")
	}
	if c.Dispatcher.StreamOrdering.MaxBufferSize <= 0 {
		return fmt.Errorf("dispatcher.stream_ordering.max_buffer_size must be >0")
	}
	if c.Dispatcher.Backpressure.TokenRatePerStream <= 0 {
		return fmt.Errorf("dispatcher.backpressure.token_rate_per_stream must be >0")
	}
	if c.Dispatcher.Backpressure.TokenBurst <= 0 {
		return fmt.Errorf("dispatcher.backpressure.token_burst must be >0")
	}

	for i, consumer := range c.Consumers {
		if strings.TrimSpace(consumer.Name) == "" {
			return fmt.Errorf("consumers[%d]: name required", i)
		}
		if strings.TrimSpace(consumer.ConsumerID) == "" {
			return fmt.Errorf("consumers[%d]: consumer_id required", i)
		}
		if len(consumer.Subscriptions) == 0 {
			return fmt.Errorf("consumers[%d]: subscriptions required", i)
		}
		tswitch := strings.ToLower(strings.TrimSpace(consumer.TradingSwitch))
		if tswitch != "enabled" && tswitch != "disabled" && tswitch != "" {
			return fmt.Errorf("consumers[%d]: trading_switch must be enabled|disabled", i)
		}
		for j, sub := range consumer.Subscriptions {
			if strings.TrimSpace(sub.Symbol) == "" {
				return fmt.Errorf("consumers[%d].subscriptions[%d]: symbol required", i, j)
			}
			if err := schema.ValidateInstrument(sub.Symbol); err != nil {
				return fmt.Errorf("consumers[%d].subscriptions[%d]: %w", i, j, err)
			}
			if len(sub.Providers) == 0 {
				return fmt.Errorf("consumers[%d].subscriptions[%d]: providers required", i, j)
			}
			if len(sub.EventTypes) == 0 {
				return fmt.Errorf("consumers[%d].subscriptions[%d]: event_types required", i, j)
			}
		}
	}
	return nil
}

// LoadStreamingConfigV2 loads the V2 streaming configuration layout.
func LoadStreamingConfigV2(ctx context.Context, path string) (StreamingConfigV2, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = os.Getenv("MELTICA_CONFIG")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		path = "config/streaming.yaml"
	}

	reader, closer, err := openStreamingFile(path)
	if err != nil {
		return StreamingConfigV2{}, err
	}
	defer closer()

	bytes, err := io.ReadAll(reader)
	if err != nil {
		return StreamingConfigV2{}, fmt.Errorf("read streaming config: %w", err)
	}

	var cfg StreamingConfigV2
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return StreamingConfigV2{}, fmt.Errorf("unmarshal streaming config: %w", err)
	}

	if err := cfg.Validate(ctx); err != nil {
		return StreamingConfigV2{}, err
	}
	return cfg, nil
}

func openStreamingFile(path string) (io.Reader, func(), error) {
	safePath := filepath.Clean(path)
	var closeFn func()
	file, err := os.Open(safePath) // #nosec G304 -- configuration paths are controlled by operators.
	if err == nil {
		closeFn = func() { _ = file.Close() }
		return file, closeFn, nil
	}
	if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("open streaming config: %w", err)
	}
	fallback := "config/streaming.example.yaml"
	file, err = os.Open(fallback)
	if err != nil {
		return nil, nil, fmt.Errorf("open streaming config: %w", err)
	}
	closeFn = func() { _ = file.Close() }
	return file, closeFn, nil
}
