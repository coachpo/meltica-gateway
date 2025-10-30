package binance

import (
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/infra/pool"
)

type metadata struct {
	apiBaseURL       string
	websocketBaseURL string
	identifier       string
	venue            string
	exchangeInfoPath string
	depthPath        string
	listenKeyPath    string
	accountInfoPath  string
	orderPath        string
}

var binanceMetadata = metadata{
	apiBaseURL:       "https://api.binance.com",
	websocketBaseURL: "wss://stream.binance.com/ws",
	identifier:       "binance",
	venue:            "BINANCE",
	exchangeInfoPath: "/api/v3/exchangeInfo",
	depthPath:        "/api/v3/depth",
	listenKeyPath:    "/api/v3/userDataStream",
	accountInfoPath:  "/api/v3/account",
	orderPath:        "/api/v3/order",
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

	metadata metadata
}

func withDefaults(in Options) Options {
	in.metadata = binanceMetadata
	if strings.TrimSpace(in.Config.Name) == "" {
		in.Config.Name = in.metadata.identifier
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
	base := strings.TrimSuffix(strings.TrimSpace(o.metadata.apiBaseURL), "/")
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
	return o.restEndpoint(o.metadata.exchangeInfoPath)
}

func (o Options) depthEndpoint() string {
	return o.restEndpoint(o.metadata.depthPath)
}

func (o Options) listenKeyEndpoint() string {
	return o.restEndpoint(o.metadata.listenKeyPath)
}

func (o Options) accountInfoEndpoint() string {
	return o.restEndpoint(o.metadata.accountInfoPath)
}

func (o Options) orderEndpoint() string {
	return o.restEndpoint(o.metadata.orderPath)
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
	return o.metadata.websocketBaseURL
}
