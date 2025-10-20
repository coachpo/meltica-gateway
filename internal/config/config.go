// Package config centralises runtime configuration helpers for Meltica services.
package config

import (
	"os"
	"strings"
	"time"
)

// Environment identifies the runtime environment where Meltica operates.
type Environment string

// Exchange names a supported exchange integration.
type Exchange string

const (
	// EnvDev marks the development environment.
	EnvDev Environment = "dev"
	// EnvStaging marks the staging environment.
	EnvStaging Environment = "staging"
	// EnvProd marks the production environment.
	EnvProd Environment = "prod"
)

// Credentials captures API credentials used for authenticated requests.
type Credentials struct {
	APIKey    string
	APISecret string
}

// WebsocketSettings configures websocket endpoints per exchange.
type WebsocketSettings struct {
	PublicURL  string
	PrivateURL string
}

// ExchangeSettings aggregates transport and credential configuration.
type ExchangeSettings struct {
	REST                  map[string]string
	Websocket             WebsocketSettings
	Credentials           Credentials
	HTTPTimeout           time.Duration
	HandshakeTimeout      time.Duration
	SymbolRefreshInterval time.Duration
}

// Settings contains the Meltica configuration tree loaded from defaults and overrides.
type Settings struct {
	Environment Environment
	Exchanges   map[Exchange]ExchangeSettings
}

// Default returns the default Meltica configuration.
func Default() Settings {
	return Settings{
		Environment: EnvProd,
		Exchanges:   make(map[Exchange]ExchangeSettings),
	}
}

// FromEnv loads configuration values from environment variables, overriding defaults.
func FromEnv() Settings {
	cfg := Default()
	if env := strings.TrimSpace(os.Getenv("MELTICA_ENV")); env != "" {
		cfg.Environment = Environment(strings.ToLower(env))
	}

	return cfg
}

// Option mutates Settings when applied via Apply.
type Option func(*Settings)

// Apply applies the provided Option set to a copy of the base Settings.
func Apply(base Settings, opts ...Option) Settings {
	cfg := base.clone()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// Exchange returns the exchange-specific configuration if present.
func (s Settings) Exchange(name Exchange) (ExchangeSettings, bool) {
	if len(s.Exchanges) == 0 {
		return emptyExchangeSettings(), false
	}
	key := Exchange(normalizeExchangeName(string(name)))
	cfg, ok := s.Exchanges[key]
	if !ok {
		return emptyExchangeSettings(), false
	}
	return cloneExchangeSettings(cfg), true
}

// DefaultExchangeSettings exposes the default configuration snapshot for an exchange.
func DefaultExchangeSettings(name Exchange) (ExchangeSettings, bool) {
	def := Default()
	cfg, ok := def.Exchanges[Exchange(normalizeExchangeName(string(name)))]
	if !ok {
		return emptyExchangeSettings(), false
	}
	return cloneExchangeSettings(cfg), true
}

// WithEnvironment configures the top-level environment.
func WithEnvironment(env Environment) Option {
	return func(s *Settings) {
		if env != "" {
			s.Environment = env
		}
	}
}

// WithExchangeRESTEndpoint overrides the REST endpoint for the given exchange surface.
func WithExchangeRESTEndpoint(exchange, surface, baseURL string) Option {
	surface = strings.TrimSpace(surface)
	baseURL = strings.TrimSpace(baseURL)
	return mutateExchangeOption(exchange, func(es *ExchangeSettings) {
		if surface == "" || baseURL == "" {
			return
		}
		es.REST[surface] = baseURL
	})
}

// WithExchangeWebsocketEndpoints overrides websocket endpoints and handshake timeout.
func WithExchangeWebsocketEndpoints(exchange, public, private string, handshake time.Duration) Option {
	public = strings.TrimSpace(public)
	private = strings.TrimSpace(private)
	return mutateExchangeOption(exchange, func(es *ExchangeSettings) {
		if public != "" {
			es.Websocket.PublicURL = public
		}
		if private != "" {
			es.Websocket.PrivateURL = private
		}
		if handshake > 0 {
			es.HandshakeTimeout = handshake
		}
	})
}

// WithExchangeHTTPTimeout overrides the HTTP timeout for the given exchange.
func WithExchangeHTTPTimeout(exchange string, timeout time.Duration) Option {
	return mutateExchangeOption(exchange, func(es *ExchangeSettings) {
		if timeout > 0 {
			es.HTTPTimeout = timeout
		}
	})
}

// WithExchangeCredentials overrides the API credentials for the given exchange.
func WithExchangeCredentials(exchange, key, secret string) Option {
	key = strings.TrimSpace(key)
	secret = strings.TrimSpace(secret)
	return mutateExchangeOption(exchange, func(es *ExchangeSettings) {
		if key != "" {
			es.Credentials.APIKey = key
		}
		if secret != "" {
			es.Credentials.APISecret = secret
		}
	})
}

func mutateExchangeOption(exchange string, fn func(*ExchangeSettings)) Option {
	key := Exchange(normalizeExchangeName(exchange))
	if string(key) == "" || fn == nil {
		return func(*Settings) {}
	}
	return func(s *Settings) {
		if s.Exchanges == nil {
			s.Exchanges = make(map[Exchange]ExchangeSettings)
		}
		cfg, ok := s.Exchanges[key]
		if !ok {
			cfg = ExchangeSettings{
				REST: make(map[string]string),
				Websocket: WebsocketSettings{
					PublicURL:  "",
					PrivateURL: "",
				},
				Credentials:           Credentials{APIKey: "", APISecret: ""},
				HTTPTimeout:           0,
				HandshakeTimeout:      0,
				SymbolRefreshInterval: 0,
			}
		}
		cfg = cloneExchangeSettings(cfg)
		fn(&cfg)
		s.Exchanges[key] = cfg
	}
}

func (s Settings) clone() Settings {
	clone := Settings{
		Environment: s.Environment,
		Exchanges:   cloneExchangeSettingsMap(s.Exchanges),
	}
	return clone
}

func cloneExchangeSettingsMap(src map[Exchange]ExchangeSettings) map[Exchange]ExchangeSettings {
	if len(src) == 0 {
		return make(map[Exchange]ExchangeSettings)
	}
	out := make(map[Exchange]ExchangeSettings, len(src))
	for k, v := range src {
		out[k] = cloneExchangeSettings(v)
	}
	return out
}

func cloneExchangeSettings(cfg ExchangeSettings) ExchangeSettings {
	out := cfg
	if cfg.REST != nil {
		out.REST = make(map[string]string, len(cfg.REST))
		for k, v := range cfg.REST {
			out.REST[k] = v
		}
	} else {
		out.REST = make(map[string]string)
	}
	return out
}

func emptyExchangeSettings() ExchangeSettings {
	return ExchangeSettings{
		REST: make(map[string]string),
		Websocket: WebsocketSettings{
			PublicURL:  "",
			PrivateURL: "",
		},
		Credentials:           Credentials{APIKey: "", APISecret: ""},
		HTTPTimeout:           0,
		HandshakeTimeout:      0,
		SymbolRefreshInterval: 0,
	}
}

func normalizeExchangeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
