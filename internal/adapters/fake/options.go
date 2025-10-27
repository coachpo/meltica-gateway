package fake

import (
	"context"
	"time"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

const (
	floatTolerance          = 1e-9
	defaultTickerInterval   = time.Second
	defaultTradeInterval    = 500 * time.Millisecond
	defaultSnapshotInterval = 5 * time.Second
	defaultKlineInterval    = time.Minute
	defaultBookLevels       = 10
	defaultRefreshInterval  = 30 * time.Minute
	defaultTradeMinQty      = 0.01
	defaultTradeMaxQty      = 1.5
	defaultVenueLatencyMin  = 5 * time.Millisecond
	defaultVenueLatencyMax  = 35 * time.Millisecond
	defaultVenueErrorRate   = 0.005
	defaultDisconnectChance = 0.0005
	defaultDisconnectFor    = 5 * time.Second
	defaultBalanceInterval  = 3 * time.Second
	defaultShockProbability = 0.045
	defaultShockMagnitude   = 0.02
	defaultPriceDrift       = 0.00025
	defaultPriceVolatility  = 0.0125
)

// DefaultInstruments enumerates the built-in catalogue used when callers do not
// supply an explicit list.
var DefaultInstruments = []schema.Instrument{
	newSpotInstrument("BTC-USDT", "BTC", "USDT"),
	newSpotInstrument("ETH-USDT", "ETH", "USDT"),
	newSpotInstrument("XRP-USDT", "XRP", "USDT"),
	newSpotInstrument("SOL-USDT", "SOL", "USDT"),
	newSpotInstrument("ADA-USDT", "ADA", "USDT"),
	newSpotInstrument("DOGE-USDT", "DOGE", "USDT"),
	newSpotInstrument("BNB-USDT", "BNB", "USDT"),
	newSpotInstrument("LTC-USDT", "LTC", "USDT"),
	newSpotInstrument("DOT-USDT", "DOT", "USDT"),
	newSpotInstrument("AVAX-USDT", "AVAX", "USDT"),
}

// Options configures the fake provider runtime.
type Options struct {
	Name                      string
	TickerInterval            time.Duration
	TradeInterval             time.Duration
	BookSnapshotInterval      time.Duration
	KlineInterval             time.Duration
	InstrumentRefreshInterval time.Duration
	InstrumentRefresh         func(context.Context) ([]schema.Instrument, error)
	Instruments               []schema.Instrument
	Pools                     *pool.PoolManager
	PriceModel                marketModelOptions
	TradeModel                tradeModelOptions
	OrderBook                 orderBookOptions
	VenueBehavior             venueBehaviorOptions
	BalanceUpdateInterval     time.Duration
}

type orderBookOptions struct {
	Levels           int
	MaxMutationWidth int
}

type marketModelOptions struct {
	Drift            float64
	Volatility       float64
	ShockProbability float64
	ShockMagnitude   float64
}

type tradeModelOptions struct {
	MinQuantity float64
	MaxQuantity float64
}

type venueBehaviorOptions struct {
	LatencyMin       time.Duration
	LatencyMax       time.Duration
	TransientError   float64
	DisconnectChance float64
	DisconnectFor    time.Duration
}

func withDefaults(in Options) Options {
	if in.TickerInterval <= 0 {
		in.TickerInterval = defaultTickerInterval
	}
	if in.TradeInterval <= 0 {
		in.TradeInterval = defaultTradeInterval
	}
	if in.BookSnapshotInterval <= 0 {
		in.BookSnapshotInterval = defaultSnapshotInterval
	}
	if in.KlineInterval <= 0 {
		in.KlineInterval = defaultKlineInterval
	}
	if in.OrderBook.Levels <= 0 {
		in.OrderBook.Levels = defaultBookLevels
	}
	if in.OrderBook.MaxMutationWidth <= 0 {
		in.OrderBook.MaxMutationWidth = in.OrderBook.Levels
	}
	if in.InstrumentRefreshInterval <= 0 {
		in.InstrumentRefreshInterval = defaultRefreshInterval
	}
	if in.TradeModel.MinQuantity <= 0 {
		in.TradeModel.MinQuantity = defaultTradeMinQty
	}
	if in.TradeModel.MaxQuantity <= 0 {
		in.TradeModel.MaxQuantity = defaultTradeMaxQty
	}
	if in.PriceModel.Drift == 0 {
		in.PriceModel.Drift = defaultPriceDrift
	}
	if in.PriceModel.Volatility == 0 {
		in.PriceModel.Volatility = defaultPriceVolatility
	}
	if in.PriceModel.ShockProbability == 0 {
		in.PriceModel.ShockProbability = defaultShockProbability
	}
	if in.PriceModel.ShockMagnitude == 0 {
		in.PriceModel.ShockMagnitude = defaultShockMagnitude
	}
	if in.VenueBehavior.LatencyMin <= 0 {
		in.VenueBehavior.LatencyMin = defaultVenueLatencyMin
	}
	if in.VenueBehavior.LatencyMax <= 0 {
		in.VenueBehavior.LatencyMax = defaultVenueLatencyMax
	}
	if in.VenueBehavior.TransientError <= 0 {
		in.VenueBehavior.TransientError = defaultVenueErrorRate
	}
	if in.VenueBehavior.DisconnectChance <= 0 {
		in.VenueBehavior.DisconnectChance = defaultDisconnectChance
	}
	if in.VenueBehavior.DisconnectFor <= 0 {
		in.VenueBehavior.DisconnectFor = defaultDisconnectFor
	}
	if in.BalanceUpdateInterval <= 0 {
		in.BalanceUpdateInterval = defaultBalanceInterval
	}
	return in
}

func newSpotInstrument(symbol, base, quote string) schema.Instrument {
	pricePrecision := 2
	quantityPrecision := 4
	notionalPrecision := 2
	return schema.Instrument{
		Symbol:            symbol,
		Type:              schema.InstrumentTypeSpot,
		BaseCurrency:      base,
		QuoteCurrency:     quote,
		Venue:             "FAKE",
		PriceIncrement:    "0.01",
		QuantityIncrement: "0.0001",
		PricePrecision:    &pricePrecision,
		QuantityPrecision: &quantityPrecision,
		NotionalPrecision: &notionalPrecision,
		MinNotional:       "10",
		MinQuantity:       "0.0001",
		MaxQuantity:       "1000",
	}
}
