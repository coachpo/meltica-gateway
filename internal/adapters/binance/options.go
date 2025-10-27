package binance

import (
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/pool"
)

const (
	defaultAPIBaseURL              = "https://api.binance.com"
	defaultWebsocketBaseURL        = "wss://stream.binance.com:9443/ws"
	defaultProviderName            = "binance"
	defaultVenue                   = "BINANCE"
	defaultSnapshotDepth           = 1000
	defaultHTTPTimeout             = 10 * time.Second
	defaultInstrumentRefreshPeriod = 30 * time.Minute
)

// Options configure the Binance adapter.
type Options struct {
	Name                      string
	Venue                     string
	APIBaseURL                string
	WebsocketBaseURL          string
	Symbols                   []string
	SnapshotDepth             int
	InstrumentRefreshInterval time.Duration
	Pools                     *pool.PoolManager
	HTTPTimeout               time.Duration
}

func withDefaults(in Options) Options {
	if strings.TrimSpace(in.Name) == "" {
		in.Name = defaultProviderName
	}
	if strings.TrimSpace(in.Venue) == "" {
		in.Venue = defaultVenue
	}
	if strings.TrimSpace(in.APIBaseURL) == "" {
		in.APIBaseURL = defaultAPIBaseURL
	}
	if strings.TrimSpace(in.WebsocketBaseURL) == "" {
		in.WebsocketBaseURL = defaultWebsocketBaseURL
	}
	if in.SnapshotDepth <= 0 {
		in.SnapshotDepth = defaultSnapshotDepth
	}
	if in.HTTPTimeout <= 0 {
		in.HTTPTimeout = defaultHTTPTimeout
	}
	if in.InstrumentRefreshInterval <= 0 {
		in.InstrumentRefreshInterval = defaultInstrumentRefreshPeriod
	}
	normalized := make([]string, 0, len(in.Symbols))
	seen := make(map[string]struct{}, len(in.Symbols))
	for _, symbol := range in.Symbols {
		trimmed := strings.ToUpper(strings.TrimSpace(symbol))
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	in.Symbols = normalized
	return in
}
