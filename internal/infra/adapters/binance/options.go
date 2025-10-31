package binance

import (
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/infra/pool"
)

type publicMetadata struct {
	identifier  string
	displayName string
	venue       string
	description string
}

type privateMetadata struct {
	apiBaseURL       string
	websocketBaseURL string
	exchangeInfoPath string
	depthPath        string
	listenKeyPath    string
	accountInfoPath  string
	orderPath        string
}

var binancePublicMetadata = publicMetadata{
	identifier:  "binance",
	displayName: "Binance Spot",
	venue:       "BINANCE",
	description: "Binance spot market data and order routing adapter",
}

var binancePrivateMetadata = privateMetadata{
	apiBaseURL:       "https://api.binance.com",
	websocketBaseURL: "wss://stream.binance.com/ws",
	exchangeInfoPath: "/api/v3/exchangeInfo",
	depthPath:        "/api/v3/depth",
	listenKeyPath:    "/api/v3/userDataStream",
	accountInfoPath:  "/api/v3/account",
	orderPath:        "/api/v3/order",
}

var binanceAdapterMetadata = provider.AdapterMetadata{
	Identifier:   binancePublicMetadata.identifier,
	DisplayName:  binancePublicMetadata.displayName,
	Venue:        binancePublicMetadata.venue,
	Description:  binancePublicMetadata.description,
	Capabilities: []string{"market-data", "orders"},
	SettingsSchema: []provider.AdapterSetting{
		{Name: "api_key", Type: "string", Description: "API key used for authenticated REST and user data streams", Required: false},
		{Name: "api_secret", Type: "string", Description: "API secret used to sign REST requests", Required: false},
		{Name: "snapshot_depth", Type: "int", Description: "Order book snapshot depth used when seeding local books", Default: defaultSnapshotDepth, Required: false},
		{Name: "http_timeout", Type: "duration", Description: "HTTP client timeout for REST requests", Default: defaultHTTPTimeout.String(), Required: false},
		{Name: "instrument_refresh_interval", Type: "duration", Description: "Interval between instrument metadata refreshes", Default: defaultInstrumentRefresh.String(), Required: false},
		{Name: "recv_window", Type: "duration", Description: "REST recvWindow applied to signed requests", Default: defaultRecvWindow.String(), Required: false},
		{Name: "user_stream_keepalive", Type: "duration", Description: "Interval between user data stream keepalive heartbeats", Default: defaultUserStreamKeepAlive.String(), Required: false},
	},
}

const (
	defaultSnapshotDepth       = 1000
	defaultHTTPTimeout         = 10 * time.Second
	defaultInstrumentRefresh   = 30 * time.Minute
	defaultRecvWindow          = 5 * time.Second
	defaultUserStreamKeepAlive = 15 * time.Minute
)

// Config captures user-overridable Binance settings.
type Config struct {
	Name                string
	APIKey              string
	APISecret           string
	SnapshotDepth       int
	HTTPTimeout         time.Duration
	InstrumentRefresh   time.Duration
	RecvWindow          time.Duration
	UserStreamKeepAlive time.Duration
}

// Options configure the Binance adapter.
type Options struct {
	Config Config
	Pools  *pool.PoolManager

	privateMeta privateMetadata
	publicMeta  publicMetadata
}

func withDefaults(in Options) Options {
	in.privateMeta = binancePrivateMetadata
	in.publicMeta = binancePublicMetadata
	if strings.TrimSpace(in.Config.Name) == "" {
		in.Config.Name = in.publicMeta.identifier
	}
	if in.Config.SnapshotDepth <= 0 {
		in.Config.SnapshotDepth = defaultSnapshotDepth
	}
	if in.Config.HTTPTimeout <= 0 {
		in.Config.HTTPTimeout = defaultHTTPTimeout
	}
	if in.Config.InstrumentRefresh <= 0 {
		in.Config.InstrumentRefresh = defaultInstrumentRefresh
	}
	if in.Config.RecvWindow <= 0 {
		in.Config.RecvWindow = defaultRecvWindow
	}
	if in.Config.UserStreamKeepAlive <= 0 {
		in.Config.UserStreamKeepAlive = defaultUserStreamKeepAlive
	}
	return in
}

func (o Options) restEndpoint(path string) string {
	base := strings.TrimSuffix(strings.TrimSpace(o.privateMeta.apiBaseURL), "/")
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
	return o.restEndpoint(o.privateMeta.exchangeInfoPath)
}

func (o Options) depthEndpoint() string {
	return o.restEndpoint(o.privateMeta.depthPath)
}

func (o Options) listenKeyEndpoint() string {
	return o.restEndpoint(o.privateMeta.listenKeyPath)
}

func (o Options) accountInfoEndpoint() string {
	return o.restEndpoint(o.privateMeta.accountInfoPath)
}

func (o Options) orderEndpoint() string {
	return o.restEndpoint(o.privateMeta.orderPath)
}

func (o Options) httpTimeoutDuration() time.Duration {
	return o.Config.HTTPTimeout
}

func (o Options) instrumentRefreshDuration() time.Duration {
	return o.Config.InstrumentRefresh
}

func (o Options) recvWindowDuration() time.Duration {
	return o.Config.RecvWindow
}

func (o Options) userStreamKeepAliveDuration() time.Duration {
	return o.Config.UserStreamKeepAlive
}

func (o Options) websocketURL() string {
	return o.privateMeta.websocketBaseURL
}
