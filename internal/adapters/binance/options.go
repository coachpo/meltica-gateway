package binance

import (
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/pool"
)

const (
	defaultAPIBaseURLSpot        = "https://api.binance.com"
	defaultAPIBaseURLFuturesUSDM = "https://fapi.binance.com"
	defaultAPIBaseURLFuturesCoin = "https://dapi.binance.com"
	defaultWebsocketSpot         = "wss://stream.binance.com"
	defaultWebsocketUSDM         = "wss://fstream.binance.com"
	defaultWebsocketCoin         = "wss://dstream.binance.com"
	defaultProviderName          = "binance"
	defaultVenue                 = "BINANCE"
	defaultSnapshotDepth         = 1000
	defaultHTTPTimeout           = 10 * time.Second
	defaultInstrumentRefresh     = 30 * time.Minute
	defaultRecvWindow            = 5 * time.Second
	defaultUserStreamKeepAlive   = 15 * time.Minute
)

type marketKind int

const (
	marketUnset marketKind = iota
	marketSpot
	marketFuturesUSDM
	marketFuturesCoinM
)

type apiPathConfig struct {
	exchangeInfo string
	depth        string
	listenKey    string
	accountInfo  string
	order        string
}

type marketProfile struct {
	apiBase   string
	wsBase    string
	apiPaths  apiPathConfig
	isFutures bool
}

var marketProfiles = map[marketKind]marketProfile{
	marketSpot: {
		apiBase: defaultAPIBaseURLSpot,
		wsBase:  defaultWebsocketSpot,
		apiPaths: apiPathConfig{
			exchangeInfo: "/api/v3/exchangeInfo",
			depth:        "/api/v3/depth",
			listenKey:    "/api/v3/userDataStream",
			accountInfo:  "/api/v3/account",
			order:        "/api/v3/order",
		},
		isFutures: false,
	},
	marketFuturesUSDM: {
		apiBase: defaultAPIBaseURLFuturesUSDM,
		wsBase:  defaultWebsocketUSDM,
		apiPaths: apiPathConfig{
			exchangeInfo: "/fapi/v1/exchangeInfo",
			depth:        "/fapi/v1/depth",
			listenKey:    "/fapi/v1/listenKey",
			accountInfo:  "/fapi/v2/balance",
			order:        "/fapi/v1/order",
		},
		isFutures: true,
	},
	marketFuturesCoinM: {
		apiBase: defaultAPIBaseURLFuturesCoin,
		wsBase:  defaultWebsocketCoin,
		apiPaths: apiPathConfig{
			exchangeInfo: "/dapi/v1/exchangeInfo",
			depth:        "/dapi/v1/depth",
			listenKey:    "/dapi/v1/listenKey",
			accountInfo:  "/dapi/v1/balance",
			order:        "/dapi/v1/order",
		},
		isFutures: true,
	},
}

// Options configure the Binance adapter.
type Options struct {
	Name          string
	Venue         string
	Symbols       []string
	SnapshotDepth int
	Pools         *pool.PoolManager
	APIKey        string
	APISecret     string

	market              marketKind
	apiPaths            apiPathConfig
	apiBaseURL          string
	websocketBaseURL    string
	instrumentRefresh   time.Duration
	httpTimeout         time.Duration
	recvWindow          time.Duration
	userStreamKeepAlive time.Duration
}

func detectMarket(venue string) marketKind {
	normalized := strings.ToLower(strings.TrimSpace(venue))
	switch {
	case strings.Contains(normalized, "coin"):
		return marketFuturesCoinM
	case strings.Contains(normalized, "future"):
		return marketFuturesUSDM
	default:
		return marketSpot
	}
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

	market := in.market
	if market == marketUnset {
		market = detectMarket(in.Venue)
	}
	if market == marketUnset {
		market = marketSpot
	}
	in.market = market

	profile, ok := marketProfiles[market]
	if !ok {
		profile = marketProfiles[marketSpot]
	}

	in.apiBaseURL = profile.apiBase
	in.websocketBaseURL = normalizeWebsocketBase(profile.wsBase)

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

	in.apiPaths = profile.apiPaths

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
	return o.restEndpoint(o.apiPaths.exchangeInfo)
}

func (o Options) depthEndpoint() string {
	return o.restEndpoint(o.apiPaths.depth)
}

func (o Options) listenKeyEndpoint() string {
	return o.restEndpoint(o.apiPaths.listenKey)
}

func (o Options) accountInfoEndpoint() string {
	if o.apiPaths.accountInfo == "" {
		return ""
	}
	return o.restEndpoint(o.apiPaths.accountInfo)
}

func (o Options) orderEndpoint() string {
	return o.restEndpoint(o.apiPaths.order)
}

func (o Options) isFuturesMarket() bool {
	profile, ok := marketProfiles[o.market]
	if !ok {
		return false
	}
	return profile.isFutures
}

func (o Options) isFuturesUSDM() bool {
	return o.market == marketFuturesUSDM
}

func (o Options) isFuturesCoinM() bool {
	return o.market == marketFuturesCoinM
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
