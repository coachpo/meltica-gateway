package binance

import (
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/infra/pool"
)

const (
	defaultAPIBaseURL          = "https://api.binance.com"
	defaultWebsocketBaseURL    = "wss://stream.binance.com"
	defaultProviderName        = "binance"
	defaultVenue               = "BINANCE"
	defaultSnapshotDepth       = 1000
	defaultHTTPTimeout         = 10 * time.Second
	defaultInstrumentRefresh   = 30 * time.Minute
	defaultRecvWindow          = 5 * time.Second
	defaultUserStreamKeepAlive = 15 * time.Minute

	exchangeInfoPath = "/api/v3/exchangeInfo"
	depthPath        = "/api/v3/depth"
	listenKeyPath    = "/api/v3/userDataStream"
	accountInfoPath  = "/api/v3/account"
	orderPath        = "/api/v3/order"
)

// Options configure the Binance adapter.
type Options struct {
	Name             string
	Venue            string
	APIBaseURL       string
	WebsocketBaseURL string
	Symbols          []string
	SnapshotDepth    int
	Pools            *pool.PoolManager
	APIKey           string
	APISecret        string

	apiBaseURL          string
	websocketBaseURL    string
	instrumentRefresh   time.Duration
	httpTimeout         time.Duration
	recvWindow          time.Duration
	userStreamKeepAlive time.Duration
}

func normalizeWebsocketBase(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return base
	}
	base = strings.TrimSuffix(base, "/")
	base = strings.TrimSuffix(base, "/ws")
	base = strings.TrimSuffix(base, "/")
	return base
}

func withDefaults(in Options) Options {
	if strings.TrimSpace(in.Name) == "" {
		in.Name = defaultProviderName
	}
	if strings.TrimSpace(in.Venue) == "" {
		in.Venue = defaultVenue
	}

	baseURL := strings.TrimSpace(in.APIBaseURL)
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	in.apiBaseURL = baseURL

	wsBase := strings.TrimSpace(in.WebsocketBaseURL)
	if wsBase == "" {
		wsBase = defaultWebsocketBaseURL
	}
	in.websocketBaseURL = normalizeWebsocketBase(wsBase)

	if in.SnapshotDepth <= 0 {
		in.SnapshotDepth = defaultSnapshotDepth
	}
	if in.httpTimeout <= 0 {
		in.httpTimeout = defaultHTTPTimeout
	}
	if in.instrumentRefresh <= 0 {
		in.instrumentRefresh = defaultInstrumentRefresh
	}
	if in.recvWindow <= 0 {
		in.recvWindow = defaultRecvWindow
	}
	if in.userStreamKeepAlive <= 0 {
		in.userStreamKeepAlive = defaultUserStreamKeepAlive
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

func (o Options) restEndpoint(path string) string {
	base := strings.TrimSuffix(strings.TrimSpace(o.apiBaseURL), "/")
	if base == "" {
		return ""
	}
	if strings.TrimSpace(path) == "" {
		return base
	}
	if strings.HasPrefix(path, "/") {
		return base + path
	}
	return base + "/" + path
}

func (o Options) exchangeInfoEndpoint() string {
	return o.restEndpoint(exchangeInfoPath)
}

func (o Options) depthEndpoint() string {
	return o.restEndpoint(depthPath)
}

func (o Options) listenKeyEndpoint() string {
	return o.restEndpoint(listenKeyPath)
}

func (o Options) accountInfoEndpoint() string {
	return o.restEndpoint(accountInfoPath)
}

func (o Options) orderEndpoint() string {
	return o.restEndpoint(orderPath)
}

func (o Options) httpTimeoutDuration() time.Duration {
	return o.httpTimeout
}

func (o Options) instrumentRefreshDuration() time.Duration {
	return o.instrumentRefresh
}

func (o Options) recvWindowDuration() time.Duration {
	return o.recvWindow
}

func (o Options) userStreamKeepAliveDuration() time.Duration {
	return o.userStreamKeepAlive
}

func (o Options) websocketURL() string {
	return o.websocketBaseURL
}
