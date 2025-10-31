package okx

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
	apiBaseURL      string
	publicWSURL     string
	instrumentsPath string
	booksPath       string
	depthParam      string
}

var okxPublicMetadata = publicMetadata{
	identifier:  "okx",
	displayName: "OKX Spot",
	venue:       "OKX",
	description: "OKX spot market data adapter",
}

var okxPrivateMetadata = privateMetadata{
	apiBaseURL:      "https://www.okx.com",
	publicWSURL:     "wss://ws.okx.com:8443/ws/v5/public",
	instrumentsPath: "/api/v5/public/instruments",
	booksPath:       "/api/v5/market/books",
	depthParam:      "sz",
}

var okxAdapterMetadata = provider.AdapterMetadata{
	Identifier:  okxPublicMetadata.identifier,
	DisplayName: okxPublicMetadata.displayName,
	Venue:       okxPublicMetadata.venue,
	Description: okxPublicMetadata.description,
	Capabilities: []string{
		"market-data",
	},
	SettingsSchema: []provider.AdapterSetting{
		{Name: "snapshot_depth", Type: "int", Description: "Order book snapshot depth for initial seeding", Default: defaultSnapshotDepth, Required: false},
		{Name: "http_timeout", Type: "duration", Description: "HTTP client timeout for REST requests", Default: defaultHTTPTimeout.String(), Required: false},
		{Name: "instrument_refresh_interval", Type: "duration", Description: "Interval between instrument metadata refreshes", Default: defaultInstrumentRefresh.String(), Required: false},
	},
}

const (
	defaultSnapshotDepth     = 100
	defaultHTTPTimeout       = 10 * time.Second
	defaultInstrumentRefresh = 15 * time.Minute
)

// Config captures user-overridable OKX settings.
type Config struct {
	Name              string
	SnapshotDepth     int
	HTTPTimeout       time.Duration
	InstrumentRefresh time.Duration
}

// Options configure the OKX adapter.
type Options struct {
	Config Config
	Pools  *pool.PoolManager

	publicMeta  publicMetadata
	privateMeta privateMetadata
}

func withDefaults(in Options) Options {
	in.publicMeta = okxPublicMetadata
	in.privateMeta = okxPrivateMetadata
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
	return in
}

func (o Options) restEndpoint(path string) string {
	base := strings.TrimSuffix(strings.TrimSpace(o.privateMeta.apiBaseURL), "/")
	if base == "" {
		return ""
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return base
	}
	if strings.HasPrefix(trimmed, "/") {
		return base + trimmed
	}
	return base + "/" + trimmed
}

func (o Options) instrumentsEndpoint() string {
	return o.restEndpoint(o.privateMeta.instrumentsPath)
}

func (o Options) booksEndpoint() string {
	return o.restEndpoint(o.privateMeta.booksPath)
}

func (o Options) httpTimeoutDuration() time.Duration {
	return o.Config.HTTPTimeout
}

func (o Options) instrumentRefreshDuration() time.Duration {
	return o.Config.InstrumentRefresh
}

func (o Options) websocketURL() string {
	return strings.TrimSpace(o.privateMeta.publicWSURL)
}
