// Package fake provides a synthetic market data provider for testing and development.
package fake

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
	"github.com/coachpo/meltica/internal/telemetry"
	"github.com/sourcegraph/conc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DefaultInstruments lists canonical instruments used when no explicit catalogue is provided.
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

const (
	floatTolerance                   = 1e-9
	defaultInstrumentRefreshInterval = 30 * time.Minute
	defaultBookLevels                = 10
	defaultPriceDrift                = 0.00025
	defaultPriceVolatility           = 0.0125
	defaultShockProbability          = 0.045
	defaultShockMagnitude            = 0.02
	defaultTradeMinQty               = 0.01
	defaultTradeMaxQty               = 1.5
	defaultVenueLatencyMin           = 5 * time.Millisecond
	defaultVenueLatencyMax           = 35 * time.Millisecond
	defaultVenueErrorRate            = 0.005
	defaultVenueDisconnectChance     = 0.0005
	defaultVenueDisconnectDuration   = 5 * time.Second
	defaultKlineInterval             = time.Minute
	defaultBalanceUpdateInterval     = 3 * time.Second
)

type nativeInstrument struct {
	symbol string
}

func (n nativeInstrument) Symbol() string {
	return n.symbol
}

type instrumentInterpreter struct{}

func (instrumentInterpreter) ToNative(inst schema.Instrument) (nativeInstrument, error) {
	if strings.TrimSpace(inst.Symbol) == "" {
		return nativeInstrument{symbol: ""}, fmt.Errorf("instrument symbol required")
	}
	symbol := normalizeInstrument(inst.Symbol)
	return nativeInstrument{symbol: symbol}, nil
}

func (instrumentInterpreter) FromNative(symbol string, catalog map[string]schema.Instrument) (schema.Instrument, error) {
	normalized := normalizeInstrument(symbol)
	inst, ok := catalog[normalized]
	if !ok {
		return schema.Instrument{}, fmt.Errorf("instrument %s not supported", normalized)
	}
	return inst, nil
}

type instrumentSupplier func() []nativeInstrument

type currencySupplier func() []string

type providerMetrics struct {
	eventsEmitted    metric.Int64Counter
	ordersReceived   metric.Int64Counter
	ordersRejected   metric.Int64Counter
	orderLatency     metric.Float64Histogram
	venueDisruptions metric.Int64Counter
	venueErrors      metric.Int64Counter
}

type instrumentConstraints struct {
	priceIncrement    float64
	quantityIncrement float64
	minQuantity       float64
	maxQuantity       float64
	minNotional       float64
	pricePrecision    int
	quantityPrecision int
}

type venueState struct {
	mu           sync.Mutex
	disconnected bool
	reconnectAt  time.Time
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

type orderBookOptions struct {
	Levels           int
	MaxMutationWidth int
}

type venueBehaviorOptions struct {
	LatencyMin       time.Duration
	LatencyMax       time.Duration
	TransientError   float64
	DisconnectChance float64
	DisconnectFor    time.Duration
}

func applyMarketDefaults(in marketModelOptions) marketModelOptions {
	if in.Drift == 0 {
		in.Drift = defaultPriceDrift
	}
	if in.Volatility == 0 {
		in.Volatility = defaultPriceVolatility
	}
	if in.ShockProbability == 0 {
		in.ShockProbability = defaultShockProbability
	}
	if in.ShockMagnitude == 0 {
		in.ShockMagnitude = defaultShockMagnitude
	}
	return in
}

func applyTradeDefaults(in tradeModelOptions) tradeModelOptions {
	if in.MinQuantity == 0 {
		in.MinQuantity = defaultTradeMinQty
	}
	if in.MaxQuantity == 0 {
		in.MaxQuantity = defaultTradeMaxQty
	}
	if in.MaxQuantity < in.MinQuantity {
		in.MaxQuantity = in.MinQuantity * 2
	}
	return in
}

func applyBookDefaults(in orderBookOptions) orderBookOptions {
	if in.Levels <= 0 {
		in.Levels = defaultBookLevels
	}
	if in.MaxMutationWidth <= 0 || in.MaxMutationWidth > in.Levels {
		in.MaxMutationWidth = in.Levels / 2
		if in.MaxMutationWidth == 0 {
			in.MaxMutationWidth = 1
		}
	}
	return in
}

func applyVenueDefaults(in venueBehaviorOptions) venueBehaviorOptions {
	if in.LatencyMin <= 0 {
		in.LatencyMin = defaultVenueLatencyMin
	}
	if in.LatencyMax <= 0 || in.LatencyMax < in.LatencyMin {
		in.LatencyMax = defaultVenueLatencyMax
	}
	if in.TransientError <= 0 {
		in.TransientError = defaultVenueErrorRate
	}
	if in.DisconnectChance < 0 {
		in.DisconnectChance = 0
	}
	if in.DisconnectChance == 0 {
		in.DisconnectChance = defaultVenueDisconnectChance
	}
	if in.DisconnectFor <= 0 {
		in.DisconnectFor = defaultVenueDisconnectDuration
	}
	return in
}

func (p *Provider) initMetrics() {
	meter := otel.Meter("provider.fake")
	var err error
	p.metrics.eventsEmitted, err = meter.Int64Counter("provider.fake.events.emitted",
		metric.WithDescription("Number of synthetic events emitted"),
		metric.WithUnit("{event}"))
	if err != nil {
		p.metrics.eventsEmitted = nil
	}
	p.metrics.ordersReceived, err = meter.Int64Counter("provider.fake.orders.received",
		metric.WithDescription("Orders received by the fake provider"),
		metric.WithUnit("{order}"))
	if err != nil {
		p.metrics.ordersReceived = nil
	}
	p.metrics.ordersRejected, err = meter.Int64Counter("provider.fake.orders.rejected",
		metric.WithDescription("Orders rejected by the fake provider"),
		metric.WithUnit("{order}"))
	if err != nil {
		p.metrics.ordersRejected = nil
	}
	p.metrics.orderLatency, err = meter.Float64Histogram("provider.fake.order.latency",
		metric.WithDescription("End-to-end order handling latency"),
		metric.WithUnit("ms"))
	if err != nil {
		p.metrics.orderLatency = nil
	}
	p.metrics.venueDisruptions, err = meter.Int64Counter("provider.fake.venue.disruptions",
		metric.WithDescription("Venue connectivity disruptions triggered by the fake provider"),
		metric.WithUnit("{event}"))
	if err != nil {
		p.metrics.venueDisruptions = nil
	}
	p.metrics.venueErrors, err = meter.Int64Counter("provider.fake.venue.errors",
		metric.WithDescription("Injected venue errors"),
		metric.WithUnit("{event}"))
	if err != nil {
		p.metrics.venueErrors = nil
	}
}

func intPtr(v int) *int {
	value := v
	return &value
}

func newSpotInstrument(symbol, base, quote string) schema.Instrument {
	return schema.Instrument{
		Symbol:            symbol,
		Type:              schema.InstrumentTypeSpot,
		BaseCurrency:      base,
		QuoteCurrency:     quote,
		Venue:             "FAKE",
		Expiry:            "",
		ContractValue:     nil,
		ContractCurrency:  "",
		Strike:            nil,
		OptionType:        schema.OptionType(""),
		PriceIncrement:    "0.01",
		QuantityIncrement: "0.0001",
		PricePrecision:    intPtr(2),
		QuantityPrecision: intPtr(4),
		NotionalPrecision: intPtr(2),
		MinNotional:       "10",
		MinQuantity:       "0.0001",
		MaxQuantity:       "1000",
	}
}

// Options configures the fake provider runtime.
type Options struct {
	Name                      string
	TickerInterval            time.Duration
	TradeInterval             time.Duration
	BookSnapshotInterval      time.Duration
	Pools                     *pool.PoolManager
	Instruments               []schema.Instrument
	InstrumentRefreshInterval time.Duration
	InstrumentRefresh         func(context.Context) ([]schema.Instrument, error)
	PriceModel                marketModelOptions
	TradeModel                tradeModelOptions
	OrderBook                 orderBookOptions
	VenueBehavior             venueBehaviorOptions
	KlineInterval             time.Duration
	BalanceUpdateInterval     time.Duration
}

// Provider emits synthetic market data covering tickers, trades, and order book events.
type Provider struct {
	name                  string
	tickerInterval        time.Duration
	tradeInterval         time.Duration
	bookSnapshotInterval  time.Duration
	klineInterval         time.Duration
	balanceUpdateInterval time.Duration

	events chan *schema.Event
	errs   chan error
	orders chan schema.OrderRequest

	ctx    context.Context
	cancel context.CancelFunc

	started atomic.Bool

	mu     sync.Mutex
	routes map[schema.CanonicalType]*routeHandle

	pools *pool.PoolManager

	seqMu sync.Mutex
	seq   map[string]uint64

	stateMu sync.Mutex
	state   map[string]*instrumentState

	clock func() time.Time

	instrumentMu              sync.RWMutex
	instrumentCodec           instrumentInterpreter
	instruments               map[string]schema.Instrument
	instrumentConstraints     map[string]instrumentConstraints
	defaultNativeInstruments  []nativeInstrument
	instrumentRefreshInterval time.Duration
	instrumentRefresh         func(context.Context) ([]schema.Instrument, error)

	priceModel   marketModelOptions
	tradeModel   tradeModelOptions
	bookOptions  orderBookOptions
	venueCfg     venueBehaviorOptions
	rng          *rand.Rand
	randMu       sync.Mutex
	venueState   venueState
	orderCounter atomic.Uint64

	balanceMu sync.Mutex
	balances  map[string]balanceState

	metrics providerMetrics
}

type routeHandle struct {
	cancel context.CancelFunc
	wg     conc.WaitGroup
}

type priceTick int64

type instrumentState struct {
	mu           sync.Mutex
	instrument   string
	basePrice    float64
	lastPrice    float64
	volume       float64
	hasSnapshot  bool
	constraints  instrumentConstraints
	bookLevels   int
	bids         map[priceTick]*bookDepth
	asks         map[priceTick]*bookDepth
	orderIndex   map[string]*activeOrder
	lastDiff     bookDiff
	currentKline *klineWindow
	completed    []klineWindow
}

type balanceState struct {
	total     float64
	available float64
}

type bookDepth struct {
	synthetic float64
	orders    []*activeOrder
}

type bookLevel struct {
	price    float64
	quantity float64
}

type bookLevelChange struct {
	price    float64
	quantity float64
}

type bookDiff struct {
	bids []bookLevelChange
	asks []bookLevelChange
}

type orderFill struct {
	order    *activeOrder
	quantity float64
	price    float64
}

type execReportEvent struct {
	instrument nativeInstrument
	payload    schema.ExecReportPayload
	ts         time.Time
}

func (s *instrumentState) snapshotLevels(side map[priceTick]*bookDepth, limit int, isBid bool) []bookLevel {
	if limit <= 0 {
		limit = s.bookLevels
	}
	type pair struct {
		tick priceTick
		qty  float64
	}
	pairs := make([]pair, 0, len(side))
	for tick, depth := range side {
		if depth == nil {
			continue
		}
		totalQty := depth.synthetic + userQuantity(depth.orders)
		if totalQty <= floatTolerance {
			continue
		}
		pairs = append(pairs, pair{tick: tick, qty: totalQty})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if isBid {
			return pairs[i].tick > pairs[j].tick
		}
		return pairs[i].tick < pairs[j].tick
	})
	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	levels := make([]bookLevel, len(pairs))
	for i, entry := range pairs {
		levels[i] = bookLevel{
			price:    s.constraints.priceForTick(entry.tick),
			quantity: entry.qty,
		}
	}
	return levels
}

func (s *instrumentState) mutateBook(p *Provider, mid float64) bookDiff {
	if mid <= 0 {
		mid = s.lastPrice
	}
	diff := bookDiff{bids: nil, asks: nil}
	diff.bids = s.mutateSide(p, s.bids, true)
	diff.asks = s.mutateSide(p, s.asks, false)
	s.recenterBook(p, mid)
	return diff
}

func (s *instrumentState) mutateSide(p *Provider, side map[priceTick]*bookDepth, _ bool) []bookLevelChange {
	mutations := make([]bookLevelChange, 0, p.bookOptions.MaxMutationWidth)
	count := p.bookOptions.MaxMutationWidth
	if count <= 0 {
		count = 1
	}
	for i := 0; i < count; i++ {
		if len(side) == 0 {
			break
		}
		var chosen priceTick
		for tick := range side {
			chosen = tick
			if p.randomFloat() < 0.35 {
				break
			}
		}
		depth := side[chosen]
		if depth == nil {
			depth = &bookDepth{synthetic: 0, orders: nil}
			side[chosen] = depth
		}
		baseQty := depth.synthetic
		if baseQty <= 0 {
			baseQty = math.Max(s.constraints.quantityIncrement, 0.5)
		}
		delta := (p.randomFloat() - 0.5) * baseQty * 0.2
		depth.synthetic = math.Max(s.constraints.quantityIncrement, baseQty+delta)
		mutations = append(mutations, bookLevelChange{
			price:    s.constraints.priceForTick(chosen),
			quantity: depth.synthetic + userQuantity(depth.orders),
		})
	}
	return mutations
}

func (s *instrumentState) recenterBook(p *Provider, mid float64) {
	step := math.Max(s.constraints.priceIncrement, 0.01)
	ensureLevels := func(side map[priceTick]*bookDepth, isBid bool) {
		for len(side) < s.bookLevels {
			offset := float64(len(side)+1) * step
			var price float64
			if isBid {
				price = mid - offset
			} else {
				price = mid + offset
			}
			tick := s.constraints.tickForPrice(price)
			if depth, ok := side[tick]; ok {
				if depth.synthetic <= 0 {
					depth.synthetic = math.Max(s.constraints.quantityIncrement, 0.5)
				}
				continue
			}
			qty := math.Max(s.constraints.quantityIncrement, 0.5+p.randomFloat())
			side[tick] = &bookDepth{synthetic: qty, orders: nil}
		}
	}
	ensureLevels(s.bids, true)
	ensureLevels(s.asks, false)
}

func userQuantity(orders []*activeOrder) float64 {
	sum := 0.0
	for _, ord := range orders {
		if ord == nil {
			continue
		}
		sum += math.Max(ord.remaining, 0)
	}
	return sum
}

func (s *instrumentState) bestBid() (float64, bool) {
	return s.bestPrice(s.bids, true)
}

func (s *instrumentState) bestAsk() (float64, bool) {
	return s.bestPrice(s.asks, false)
}

func (s *instrumentState) bestPrice(side map[priceTick]*bookDepth, isBid bool) (float64, bool) {
	var (
		bestTick priceTick
		has      bool
	)
	bestQty := 0.0
	for tick, depth := range side {
		if depth == nil {
			continue
		}
		qty := depth.synthetic + userQuantity(depth.orders)
		if qty <= floatTolerance {
			continue
		}
		if !has {
			has = true
			bestTick = tick
			bestQty = qty
			continue
		}
		if isBid {
			if tick > bestTick {
				bestTick = tick
				bestQty = qty
			}
			continue
		}
		if tick < bestTick {
			bestTick = tick
			bestQty = qty
		}
	}
	if !has || bestQty <= floatTolerance {
		return 0, false
	}
	return s.constraints.priceForTick(bestTick), true
}

func (s *instrumentState) availableLiquidity(side schema.TradeSide, limit float64) float64 {
	sum := 0.0
	switch side {
	case schema.TradeSideBuy:
		for tick, depth := range s.asks {
			if depth == nil {
				continue
			}
			price := s.constraints.priceForTick(tick)
			if limit > 0 && price-limit > floatTolerance {
				continue
			}
			sum += depth.synthetic + userQuantity(depth.orders)
		}
	case schema.TradeSideSell:
		for tick, depth := range s.bids {
			if depth == nil {
				continue
			}
			price := s.constraints.priceForTick(tick)
			if limit > 0 && limit-price > floatTolerance {
				continue
			}
			sum += depth.synthetic + userQuantity(depth.orders)
		}
	}
	return sum
}

func (s *instrumentState) isMarketable(side schema.TradeSide, price float64) bool {
	switch side {
	case schema.TradeSideBuy:
		ask, ok := s.bestAsk()
		return ok && price+floatTolerance >= ask
	case schema.TradeSideSell:
		bid, ok := s.bestBid()
		return ok && price-floatTolerance <= bid
	default:
		return false
	}
}

func (s *instrumentState) consumeLiquidity(side schema.TradeSide, quantity float64, limit float64, ts time.Time) (float64, []orderFill, float64) {
	if quantity <= 0 {
		return 0, nil, 0
	}
	fills := make([]orderFill, 0, 4)
	filled := 0.0
	avgPrice := 0.0
	for filled+floatTolerance < quantity {
		tick, depth, ok := s.pickLevel(side, limit)
		if !ok || depth == nil {
			break
		}
		price := s.constraints.priceForTick(tick)
		remaining := quantity - filled
		levelQty := depth.synthetic + userQuantity(depth.orders)
		if levelQty <= floatTolerance {
			delete(s.levelMap(side), tick)
			continue
		}
		consume := math.Min(remaining, levelQty)
		var consumedUser float64
		if len(depth.orders) > 0 {
			depth.orders, fills, consumedUser = consumeUserOrders(depth.orders, consume, price, ts, fills)
			filled += consumedUser
			avgPrice += consumedUser * price
			consume -= consumedUser
		}
		if consume > 0 && depth.synthetic > 0 {
			useSynthetic := math.Min(consume, depth.synthetic)
			depth.synthetic -= useSynthetic
			filled += useSynthetic
			avgPrice += useSynthetic * price
			consume -= useSynthetic
		}
		if depth.synthetic <= floatTolerance && len(depth.orders) == 0 {
			delete(s.levelMap(side), tick)
		}
		if consume > floatTolerance {
			continue
		}
	}
	if filled > floatTolerance {
		avgPrice /= filled
	}
	return avgPrice, fills, filled
}

func (s *instrumentState) levelMap(side schema.TradeSide) map[priceTick]*bookDepth {
	if side == schema.TradeSideBuy {
		return s.asks
	}
	return s.bids
}

func (s *instrumentState) pickLevel(side schema.TradeSide, limit float64) (priceTick, *bookDepth, bool) {
	switch side {
	case schema.TradeSideBuy:
		return s.pickFromSide(s.asks, limit, false)
	case schema.TradeSideSell:
		return s.pickFromSide(s.bids, limit, true)
	default:
		return 0, nil, false
	}
}

func (s *instrumentState) pickFromSide(side map[priceTick]*bookDepth, limit float64, isBid bool) (priceTick, *bookDepth, bool) {
	var (
		selected priceTick
		depth    *bookDepth
		has      bool
	)
	for tick, lvl := range side {
		if lvl == nil {
			continue
		}
		qty := lvl.synthetic + userQuantity(lvl.orders)
		if qty <= floatTolerance {
			continue
		}
		price := s.constraints.priceForTick(tick)
		if limit > 0 {
			if isBid {
				if limit-price > floatTolerance {
					continue
				}
			} else {
				if price-limit > floatTolerance {
					continue
				}
			}
		}
		if !has {
			selected = tick
			depth = lvl
			has = true
			continue
		}
		if isBid {
			if tick > selected {
				selected = tick
				depth = lvl
			}
			continue
		}
		if tick < selected {
			selected = tick
			depth = lvl
		}
	}
	return selected, depth, has
}

func consumeUserOrders(orders []*activeOrder, target float64, price float64, ts time.Time, fills []orderFill) ([]*activeOrder, []orderFill, float64) {
	consumed := 0.0
	i := 0
	for i < len(orders) && consumed+floatTolerance < target {
		ord := orders[i]
		if ord == nil || ord.remaining <= floatTolerance {
			i++
			continue
		}
		need := target - consumed
		fillQty := math.Min(need, ord.remaining)
		ord.recordFill(fillQty, price, ts)
		consumed += fillQty
		fills = append(fills, orderFill{order: ord, quantity: fillQty, price: price})
		if ord.remaining <= floatTolerance {
			i++
		} else {
			break
		}
	}
	orders = pruneOrders(orders)
	return orders, fills, consumed
}

func pruneOrders(orders []*activeOrder) []*activeOrder {
	out := orders[:0]
	for _, ord := range orders {
		if ord == nil || ord.remaining <= floatTolerance {
			continue
		}
		out = append(out, ord)
	}
	return out
}

func (s *instrumentState) restOrder(order *activeOrder) {
	if order == nil {
		return
	}
	depthMap := s.bids
	if order.side == schema.TradeSideSell {
		depthMap = s.asks
	}
	depth := depthMap[order.priceTick]
	if depth == nil {
		depth = &bookDepth{synthetic: 0, orders: nil}
		depthMap[order.priceTick] = depth
	}
	depth.orders = append(depth.orders, order)
}

type klineWindow struct {
	openTime  time.Time
	closeTime time.Time
	open      float64
	high      float64
	low       float64
	close     float64
	volume    float64
}

func newKlineWindow(start time.Time, interval time.Duration, price float64) *klineWindow {
	if interval <= 0 {
		interval = time.Minute
	}
	return &klineWindow{
		openTime:  start,
		closeTime: start.Add(interval),
		open:      price,
		close:     price,
		high:      price,
		low:       price,
		volume:    0,
	}
}

func (k *klineWindow) update(price float64, volume float64) {
	if k == nil {
		return
	}
	if k.high == 0 || price > k.high {
		k.high = price
	}
	if k.low == 0 || price < k.low {
		k.low = price
	}
	k.close = price
	if volume > 0 {
		k.volume += volume
	}
}

func (s *instrumentState) updateKline(ts time.Time, price, qty float64, interval time.Duration) {
	if interval <= 0 {
		return
	}
	if s.currentKline == nil {
		start := ts.Truncate(interval)
		s.currentKline = newKlineWindow(start, interval, price)
	}
	for ts.After(s.currentKline.closeTime) {
		s.completed = append(s.completed, *s.currentKline)
		start := s.currentKline.closeTime
		s.currentKline = newKlineWindow(start, interval, s.currentKline.close)
	}
	s.currentKline.update(price, qty)
}

func (s *instrumentState) finalizeKlines(now time.Time, interval time.Duration) []klineWindow {
	if interval <= 0 {
		return nil
	}
	completed := make([]klineWindow, 0, len(s.completed)+1)
	for len(s.completed) > 0 && !now.Before(s.completed[0].closeTime) {
		completed = append(completed, s.completed[0])
		s.completed = s.completed[1:]
	}
	if s.currentKline != nil && !now.Before(s.currentKline.closeTime) {
		completed = append(completed, *s.currentKline)
		start := s.currentKline.closeTime
		s.currentKline = newKlineWindow(start, interval, s.currentKline.close)
	}
	return completed
}

type activeOrder struct {
	clientID   string
	exchangeID string
	instrument string
	side       schema.TradeSide
	orderType  schema.OrderType
	tif        tifMode
	price      float64
	quantity   float64
	remaining  float64
	filled     float64
	notional   float64
	priceTick  priceTick
	createdAt  time.Time
	updatedAt  time.Time
}

type tifMode int

const (
	tifGTC tifMode = iota
	tifIOC
	tifFOK
	tifPostOnly
)

func (o *activeOrder) recordFill(qty, price float64, ts time.Time) {
	if o == nil || qty <= 0 {
		return
	}
	o.remaining -= qty
	if o.remaining < 0 {
		o.remaining = 0
	}
	o.filled += qty
	o.notional += qty * price
	o.updatedAt = ts
}

func (o *activeOrder) avgFillPrice() float64 {
	if o == nil {
		return 0
	}
	if o.filled <= floatTolerance {
		if o.price > 0 {
			return o.price
		}
		return 0
	}
	return o.notional / o.filled
}

func parseTIF(value string) tifMode {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "IOC":
		return tifIOC
	case "FOK":
		return tifFOK
	case "POST", "POST_ONLY", "PO":
		return tifPostOnly
	default:
		return tifGTC
	}
}

// NewProvider constructs a fake data provider with sane defaults.
func NewProvider(opts Options) *Provider {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "fake"
	}
	tickerInterval := opts.TickerInterval
	if tickerInterval <= 0 {
		tickerInterval = time.Second
	}
	tradeInterval := opts.TradeInterval
	if tradeInterval <= 0 {
		tradeInterval = 500 * time.Millisecond
	}
	bookSnapshotInterval := opts.BookSnapshotInterval
	if bookSnapshotInterval <= 0 {
		bookSnapshotInterval = 5 * time.Second
	}
	balanceInterval := opts.BalanceUpdateInterval
	if balanceInterval <= 0 {
		balanceInterval = defaultBalanceUpdateInterval
	}

	p := &Provider{ //nolint:exhaustruct // zero values for internal synchronization fields are acceptable
		name:                  name,
		tickerInterval:        tickerInterval,
		tradeInterval:         tradeInterval,
		bookSnapshotInterval:  bookSnapshotInterval,
		klineInterval:         opts.KlineInterval,
		balanceUpdateInterval: balanceInterval,
		events:                make(chan *schema.Event, 128),
		errs:                  make(chan error, 8),
		orders:                make(chan schema.OrderRequest, 64),
		routes:                make(map[schema.CanonicalType]*routeHandle),
		seq:                   make(map[string]uint64),
		state:                 make(map[string]*instrumentState),
		clock:                 time.Now,
		pools:                 opts.Pools,
		instrumentConstraints: make(map[string]instrumentConstraints),
		rng:                   rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // pseudo-randomness is acceptable for simulations
		balances:              make(map[string]balanceState),
	}
	if p.klineInterval <= 0 {
		p.klineInterval = defaultKlineInterval
	}
	p.priceModel = applyMarketDefaults(opts.PriceModel)
	p.tradeModel = applyTradeDefaults(opts.TradeModel)
	p.bookOptions = applyBookDefaults(opts.OrderBook)
	p.venueCfg = applyVenueDefaults(opts.VenueBehavior)
	catalogue := append([]schema.Instrument(nil), opts.Instruments...)
	if len(catalogue) == 0 {
		catalogue = append(catalogue, DefaultInstruments...)
	}
	p.setSupportedInstruments(catalogue)

	refreshInterval := opts.InstrumentRefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = defaultInstrumentRefreshInterval
	}
	p.instrumentRefreshInterval = refreshInterval
	if refreshInterval > 0 {
		if opts.InstrumentRefresh != nil {
			p.instrumentRefresh = opts.InstrumentRefresh
		} else {
			p.instrumentRefresh = func(context.Context) ([]schema.Instrument, error) {
				return schema.CloneInstruments(p.Instruments()), nil
			}
		}
	}
	p.initMetrics()
	return p
}

// Start begins emitting synthetic events until the context is cancelled.
func (p *Provider) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("provider requires context")
	}
	if !p.started.CompareAndSwap(false, true) {
		return errors.New("provider already started")
	}

	ctx, cancel := context.WithCancel(ctx)
	p.ctx = ctx
	p.cancel = cancel

	go func() {
		<-ctx.Done()
		p.stopAll()
		close(p.orders)
		close(p.events)
		close(p.errs)
	}()

	go p.consumeOrders(ctx)
	if p.instrumentRefreshInterval > 0 && p.instrumentRefresh != nil {
		go p.runInstrumentRefresh(ctx)
	}
	return nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return p.name
}

// Events returns the canonical event stream.
func (p *Provider) Events() <-chan *schema.Event {
	return p.events
}

// Errors returns asynchronous provider errors.
func (p *Provider) Errors() <-chan error {
	return p.errs
}

// Instruments returns the current catalogue of supported instruments.
func (p *Provider) Instruments() []schema.Instrument {
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	if len(p.defaultNativeInstruments) == 0 {
		return nil
	}
	out := make([]schema.Instrument, 0, len(p.defaultNativeInstruments))
	for _, native := range p.defaultNativeInstruments {
		inst, ok := p.instruments[native.symbol]
		if !ok {
			continue
		}
		out = append(out, schema.CloneInstrument(inst))
	}
	return out
}

// SubmitOrder enqueues a synthetic order acknowledgement.
func (p *Provider) SubmitOrder(ctx context.Context, req schema.OrderRequest) error {
	if !p.started.Load() {
		return errors.New("provider not started")
	}
	if req.Provider == "" {
		req.Provider = p.name
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("submit order context: %w", ctx.Err())
	case <-p.ctx.Done():
		return errors.New("provider shutting down")
	case p.orders <- req:
		return nil
	}
}

// SubscribeRoute activates a dispatcher route.
func (p *Provider) SubscribeRoute(route dispatcher.Route) error {
	if route.Type == "" {
		return errors.New("route type required")
	}
	if p.ctx == nil {
		return errors.New("provider not started")
	}
	evtType, ok := canonicalToEventType(route.Type)
	if !ok {
		return fmt.Errorf("unsupported canonical type %s", route.Type)
	}
	key := route.Type
	p.mu.Lock()
	if _, exists := p.routes[key]; exists {
		p.mu.Unlock()
		return nil
	}
	handle := p.startRouteLocked(route, evtType)
	p.routes[key] = handle
	p.mu.Unlock()
	return nil
}

// UnsubscribeRoute stops streaming for the canonical type.
func (p *Provider) UnsubscribeRoute(typ schema.CanonicalType) error {
	if typ == "" {
		return errors.New("route type required")
	}
	p.mu.Lock()
	handle, ok := p.routes[typ]
	if ok {
		delete(p.routes, typ)
	}
	p.mu.Unlock()
	if ok && handle != nil {
		handle.cancel()
		handle.wg.Wait()
	}
	return nil
}

func (p *Provider) startRouteLocked(route dispatcher.Route, evtType schema.EventType) *routeHandle {
	routeCtx, cancel := context.WithCancel(p.ctx)
	handle := &routeHandle{cancel: cancel} //nolint:exhaustruct // wait group zero value is acceptable
	switch evtType {
	case schema.EventTypeBalanceUpdate:
		supply := p.currencySupplier(route)
		handle.wg.Go(func() {
			p.streamBalanceUpdates(routeCtx, supply)
		})
	default:
		supplier := p.instrumentSupplier(route)
		handle.wg.Go(func() {
			p.runGenerator(routeCtx, evtType, supplier)
		})
	}
	return handle
}

func (p *Provider) runGenerator(ctx context.Context, evtType schema.EventType, supply instrumentSupplier) {
	switch evtType {
	case schema.EventTypeTicker:
		p.streamTickers(ctx, supply)
	case schema.EventTypeTrade:
		p.streamTrades(ctx, supply)
	case schema.EventTypeBookSnapshot:
		p.streamBookSnapshots(ctx, supply)
	case schema.EventTypeInstrumentUpdate:
		<-ctx.Done()
	case schema.EventTypeKlineSummary:
		p.streamKlines(ctx, supply)
	case schema.EventTypeExecReport:
		<-ctx.Done()
	default:
		<-ctx.Done()
	}
}

func (p *Provider) instrumentSupplier(route dispatcher.Route) instrumentSupplier {
	filtered := p.instrumentsFromRoute(route)
	if len(filtered) == 0 {
		return p.currentDefaultNative
	}
	static := snapshotNative(filtered)
	return func() []nativeInstrument {
		return static
	}
}

func (p *Provider) instrumentsFromRoute(route dispatcher.Route) []nativeInstrument {
	if len(route.Filters) == 0 {
		return nil
	}
	set := make(map[string]struct{})
	for _, filter := range route.Filters {
		if strings.EqualFold(filter.Field, "instrument") {
			p.collectInstrumentValues(filter.Value, set)
		}
	}
	if len(set) == 0 {
		return nil
	}
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	natives := make([]nativeInstrument, 0, len(set))
	for symbol := range set {
		if _, err := p.instrumentCodec.FromNative(symbol, p.instruments); err == nil {
			natives = append(natives, nativeInstrument{symbol: symbol})
		}
	}
	sort.Slice(natives, func(i, j int) bool { return natives[i].symbol < natives[j].symbol })
	return natives
}

func (p *Provider) currencySupplier(route dispatcher.Route) currencySupplier {
	currencies := p.currenciesFromRoute(route)
	if len(currencies) == 0 {
		currencies = p.defaultCurrencies()
	}
	static := snapshotCurrencies(currencies)
	return func() []string {
		return static
	}
}

func (p *Provider) currenciesFromRoute(route dispatcher.Route) []string {
	if len(route.Filters) == 0 {
		return nil
	}
	set := make(map[string]struct{})
	for _, filter := range route.Filters {
		if !strings.EqualFold(filter.Field, "currency") {
			continue
		}
		switch v := filter.Value.(type) {
		case string:
			if currency := normalizeCurrency(v); currency != "" {
				set[currency] = struct{}{}
			}
		case []string:
			for _, entry := range v {
				if currency := normalizeCurrency(entry); currency != "" {
					set[currency] = struct{}{}
				}
			}
		case []any:
			for _, entry := range v {
				if currency := normalizeCurrency(fmt.Sprint(entry)); currency != "" {
					set[currency] = struct{}{}
				}
			}
		default:
			if currency := normalizeCurrency(fmt.Sprint(filter.Value)); currency != "" {
				set[currency] = struct{}{}
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for currency := range set {
		out = append(out, currency)
	}
	sort.Strings(out)
	return out
}

func (p *Provider) defaultCurrencies() []string {
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	if len(p.instruments) == 0 {
		return nil
	}
	set := make(map[string]struct{})
	for _, inst := range p.instruments {
		if currency := normalizeCurrency(inst.BaseCurrency); currency != "" {
			set[currency] = struct{}{}
		}
		if currency := normalizeCurrency(inst.QuoteCurrency); currency != "" {
			set[currency] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for currency := range set {
		out = append(out, currency)
	}
	sort.Strings(out)
	return out
}

func (p *Provider) collectInstrumentValues(value any, set map[string]struct{}) {
	switch v := value.(type) {
	case string:
		if symbol := normalizeInstrument(v); symbol != "" {
			set[symbol] = struct{}{}
		}
	case []string:
		for _, entry := range v {
			p.collectInstrumentValues(entry, set)
		}
	case []any:
		for _, entry := range v {
			p.collectInstrumentValues(entry, set)
		}
	}
}

func (p *Provider) currentDefaultNative() []nativeInstrument {
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	return snapshotNative(p.defaultNativeInstruments)
}

func (p *Provider) streamTickers(ctx context.Context, supply instrumentSupplier) {
	ticker := time.NewTicker(p.tickerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !p.applyVenueLatency(ctx) {
				return
			}
			for _, inst := range supply() {
				p.emitTicker(inst)
			}
		}
	}
}

func (p *Provider) streamTrades(ctx context.Context, supply instrumentSupplier) {
	ticker := time.NewTicker(p.tradeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !p.applyVenueLatency(ctx) {
				return
			}
			for _, inst := range supply() {
				p.emitTrade(inst)
			}
		}
	}
}

func (p *Provider) streamBookSnapshots(ctx context.Context, supply instrumentSupplier) {
	p.emitSnapshots(supply())
	ticker := time.NewTicker(p.bookSnapshotInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !p.applyVenueLatency(ctx) {
				return
			}
			p.emitSnapshots(supply())
		}
	}
}

func (p *Provider) streamKlines(ctx context.Context, supply instrumentSupplier) {
	if p.klineInterval <= 0 {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(p.klineInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !p.applyVenueLatency(ctx) {
				return
			}
			for _, inst := range supply() {
				p.emitKline(inst)
			}
		}
	}
}

func (p *Provider) streamBalanceUpdates(ctx context.Context, supply currencySupplier) {
	if p.balanceUpdateInterval <= 0 {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(p.balanceUpdateInterval)
	defer ticker.Stop()
	p.emitBalanceSnapshots(supply())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !p.applyVenueLatency(ctx) {
				return
			}
			p.emitBalanceSnapshots(supply())
		}
	}
}

func (p *Provider) emitSnapshots(instruments []nativeInstrument) {
	for _, inst := range instruments {
		p.emitBookSnapshot(inst)
	}
}

func (p *Provider) emitBalanceSnapshots(currencies []string) {
	for _, currency := range currencies {
		p.emitBalanceUpdate(currency)
	}
}

func (p *Provider) emitKline(instrument nativeInstrument) {
	if p.klineInterval <= 0 {
		return
	}
	state := p.getInstrumentState(instrument)
	ts := p.clock().UTC()
	if !p.venueOperational(ts) {
		return
	}
	if p.venueShouldError() {
		p.emitError(fmt.Errorf("fake provider %s: kline stream error", p.name))
		return
	}
	state.mu.Lock()
	state.updateKline(ts, state.lastPrice, 0, p.klineInterval)
	completed := state.finalizeKlines(ts, p.klineInterval)
	cons := state.constraints
	state.mu.Unlock()
	for _, bucket := range completed {
		payload := schema.KlineSummaryPayload{
			OpenPrice:  formatWithPrecision(bucket.open, cons.pricePrecision),
			ClosePrice: formatWithPrecision(bucket.close, cons.pricePrecision),
			HighPrice:  formatWithPrecision(bucket.high, cons.pricePrecision),
			LowPrice:   formatWithPrecision(bucket.low, cons.pricePrecision),
			Volume:     formatWithPrecision(bucket.volume, cons.quantityPrecision),
			OpenTime:   bucket.openTime,
			CloseTime:  bucket.closeTime,
		}
		seq := p.nextSeq(schema.EventTypeKlineSummary, instrument)
		evt := p.newEvent(schema.EventTypeKlineSummary, instrument, seq, payload, bucket.closeTime)
		if evt == nil {
			continue
		}
		p.emitEvent(evt)
	}
}

func (p *Provider) emitBalanceUpdate(currency string) {
	normalized := normalizeCurrency(currency)
	if normalized == "" {
		return
	}
	ts := p.clock().UTC()
	p.balanceMu.Lock()
	state := p.balances[normalized]
	if state.total <= 0 {
		state.total = 500 + 500*p.randomFloat()
	}
	delta := (p.randomFloat() - 0.5) * 25
	state.total += delta
	if state.total < 0 {
		state.total = 0
	}
	reserve := 0.3 + 0.6*p.randomFloat()
	state.available = state.total * reserve
	p.balances[normalized] = state
	p.balanceMu.Unlock()
	payload := schema.BalanceUpdatePayload{
		Currency:  normalized,
		Total:     formatBalance(state.total),
		Available: formatBalance(state.available),
		Timestamp: ts,
	}
	seq := p.nextSeq(schema.EventTypeBalanceUpdate, nativeInstrument{symbol: normalized})
	evt := p.newEvent(schema.EventTypeBalanceUpdate, nativeInstrument{symbol: normalized}, seq, payload, ts)
	if evt == nil {
		return
	}
	p.emitEvent(evt)
}

func (p *Provider) emitTicker(instrument nativeInstrument) {
	state := p.getInstrumentState(instrument)
	ts := p.clock().UTC()
	if !p.venueOperational(ts) {
		return
	}
	if p.venueShouldError() {
		p.emitError(fmt.Errorf("fake provider %s: ticker channel transient error", p.name))
		return
	}
	seq := p.nextSeq(schema.EventTypeTicker, instrument)
	state.mu.Lock()
	price := p.nextModelPrice(state)
	state.lastPrice = price
	state.lastDiff = state.mutateBook(p, price)
	bid, okBid := state.bestBid()
	ask, okAsk := state.bestAsk()
	if !okBid {
		bid = price * 0.999
	}
	if !okAsk {
		ask = price * 1.001
	}
	state.volume += 20 + 50*p.randomFloat()
	payload := schema.TickerPayload{
		LastPrice: formatPrice(price),
		BidPrice:  formatWithPrecision(bid, state.constraints.pricePrecision),
		AskPrice:  formatWithPrecision(ask, state.constraints.pricePrecision),
		Volume24h: formatWithPrecision(state.volume, state.constraints.quantityPrecision),
		Timestamp: ts,
	}
	state.mu.Unlock()
	evt := p.newEvent(schema.EventTypeTicker, instrument, seq, payload, ts)
	if evt == nil {
		return
	}
	p.emitEvent(evt)
}

func (p *Provider) emitTrade(instrument nativeInstrument) {
	state := p.getInstrumentState(instrument)
	ts := p.clock().UTC()
	if !p.venueOperational(ts) {
		return
	}
	if p.venueShouldError() {
		p.emitError(fmt.Errorf("fake provider %s: trade channel transient error", p.name))
		return
	}
	seq := p.nextSeq(schema.EventTypeTrade, instrument)
	state.mu.Lock()
	side := schema.TradeSideBuy
	if p.randomFloat() < 0.5 {
		side = schema.TradeSideSell
	}
	qty := p.randomTradeQuantity(state.constraints)
	price, fills, filled := state.consumeLiquidity(side, qty, 0, ts)
	if filled <= floatTolerance {
		price = p.nextModelPrice(state)
		filled = qty
	}
	state.lastPrice = price
	state.volume += filled
	state.updateKline(ts, price, filled, p.klineInterval)
	state.recenterBook(p, price)
	execEvents := make([]execReportEvent, 0, len(fills))
	for _, fill := range fills {
		if fill.order == nil {
			continue
		}
		if fill.order.remaining <= floatTolerance {
			delete(state.orderIndex, fill.order.exchangeID)
		}
		reportState := schema.ExecReportStatePARTIAL
		if fill.order.remaining <= floatTolerance {
			reportState = schema.ExecReportStateFILLED
		}
		payload := buildExecPayload(fill.order, reportState, nil, state.constraints, ts)
		execEvents = append(execEvents, execReportEvent{instrument: instrument, payload: payload, ts: ts})
	}
	state.mu.Unlock()
	tradeID := fmt.Sprintf("%s-%d", strings.ReplaceAll(instrument.Symbol(), "-", ""), seq)
	payload := schema.TradePayload{
		TradeID:   tradeID,
		Side:      side,
		Price:     formatWithPrecision(price, state.constraints.pricePrecision),
		Quantity:  formatWithPrecision(filled, state.constraints.quantityPrecision),
		Timestamp: ts,
	}
	evt := p.newEvent(schema.EventTypeTrade, instrument, seq, payload, ts)
	if evt == nil {
		return
	}
	p.emitEvent(evt)
	if len(execEvents) > 0 {
		p.emitExecReportEvents(execEvents)
	}
}

func (p *Provider) emitBookSnapshot(instrument nativeInstrument) {
	state := p.getInstrumentState(instrument)
	ts := p.clock().UTC()
	if !p.venueOperational(ts) {
		return
	}
	if p.venueShouldError() {
		p.emitError(fmt.Errorf("fake provider %s: orderbook snapshot error", p.name))
		return
	}
	seq := p.nextSeq(schema.EventTypeBookSnapshot, instrument)
	state.mu.Lock()
	state.lastDiff = state.mutateBook(p, state.lastPrice)
	levelsBids := formatLevels(state.snapshotLevels(state.bids, p.bookOptions.Levels, true), state.constraints)
	levelsAsks := formatLevels(state.snapshotLevels(state.asks, p.bookOptions.Levels, false), state.constraints)
	state.hasSnapshot = true
	checksum := checksum(instrument.Symbol(), schema.EventTypeBookSnapshot, seq)
	state.mu.Unlock()
	payload := schema.BookSnapshotPayload{ //nolint:exhaustruct // optional update fields are intentionally omitted
		Bids:       levelsBids,
		Asks:       levelsAsks,
		Checksum:   checksum,
		LastUpdate: ts,
	}
	evt := p.newEvent(schema.EventTypeBookSnapshot, instrument, seq, payload, ts)
	if evt == nil {
		return
	}
	p.emitEvent(evt)
}

func (p *Provider) newEvent(evtType schema.EventType, instrument nativeInstrument, seq uint64, payload any, ts time.Time) *schema.Event {
	if ts.IsZero() {
		ts = p.clock().UTC()
	}
	symbol := normalizeInstrument(instrument.Symbol())
	eventID := fmt.Sprintf("%s:%s:%s:%d", p.name, strings.ReplaceAll(symbol, "-", ""), evtType, seq)
	evt := p.borrowEvent(p.ctx)
	if evt == nil {
		return nil
	}
	evt.EventID = eventID
	evt.Provider = p.name
	evt.Symbol = symbol
	evt.Type = evtType
	evt.SeqProvider = seq
	evt.IngestTS = ts
	evt.EmitTS = ts
	evt.Payload = payload
	return evt
}

func (p *Provider) borrowEvent(ctx context.Context) *schema.Event {
	requestCtx := ctx
	if requestCtx == nil {
		requestCtx = p.ctx
	}
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	evt, err := p.pools.BorrowEventInst(requestCtx)
	if err != nil {
		log.Printf("fake provider %s: borrow canonical event failed: %v", p.name, err)
		p.emitError(fmt.Errorf("borrow canonical event: %w", err))
		return nil
	}
	return evt
}

func (p *Provider) emitEvent(evt *schema.Event) {
	if evt == nil {
		return
	}
	if p.ctx == nil {
		return
	}
	p.recordEventMetric(evt.Type, evt.Symbol)
	select {
	case <-p.ctx.Done():
		return
	case p.events <- evt:
	}
}

func (p *Provider) emitExecReportEvents(events []execReportEvent) {
	for _, entry := range events {
		if entry.instrument.Symbol() == "" {
			continue
		}
		seq := p.nextSeq(schema.EventTypeExecReport, entry.instrument)
		evt := p.newEvent(schema.EventTypeExecReport, entry.instrument, seq, entry.payload, entry.ts)
		if evt == nil {
			continue
		}
		p.emitEvent(evt)
	}
}

func (p *Provider) emitError(err error) {
	if err == nil {
		return
	}
	if p.ctx == nil {
		return
	}
	select {
	case <-p.ctx.Done():
		return
	case p.errs <- err:
	default:
	}
}

func (p *Provider) consumeOrders(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case order, ok := <-p.orders:
			if !ok {
				return
			}
			p.handleOrder(order)
		}
	}
}

func (p *Provider) handleOrder(order schema.OrderRequest) {
	start := p.clock()
	if strings.TrimSpace(order.Symbol) == "" {
		order.Symbol = p.defaultInstrumentSymbol()
	}
	nativeInst, ok := p.nativeInstrumentForSymbol(order.Symbol)
	if !ok {
		order.Symbol = p.defaultInstrumentSymbol()
		nativeInst, ok = p.nativeInstrumentForSymbol(order.Symbol)
		if !ok {
			p.emitError(fmt.Errorf("fake provider %s: unsupported instrument %q", p.name, order.Symbol))
			return
		}
	}
	order.Symbol = nativeInst.Symbol()
	if order.Timestamp.IsZero() {
		order.Timestamp = p.clock().UTC()
	}
	if order.Side != schema.TradeSideBuy && order.Side != schema.TradeSideSell {
		p.rejectOrder(nativeInst, order, "side required", start, order.Timestamp)
		return
	}
	if order.OrderType != schema.OrderTypeLimit && order.OrderType != schema.OrderTypeMarket {
		p.rejectOrder(nativeInst, order, "unsupported order type", start, order.Timestamp)
		return
	}
	state := p.getInstrumentState(nativeInst)
	cons := state.constraints
	p.recordOrderReceived(order)
	qty, err := strconv.ParseFloat(order.Quantity, 64)
	if err != nil || qty <= 0 {
		p.rejectOrder(nativeInst, order, "invalid quantity", start, order.Timestamp)
		return
	}
	qty = cons.normalizeQuantity(qty)
	if !cons.validQuantity(qty) {
		p.rejectOrder(nativeInst, order, "quantity violates instrument constraints", start, order.Timestamp)
		return
	}
	var limitPrice float64
	if order.OrderType == schema.OrderTypeLimit {
		if order.Price == nil {
			p.rejectOrder(nativeInst, order, "limit price required", start, order.Timestamp)
			return
		}
		limitPrice, err = strconv.ParseFloat(*order.Price, 64)
		if err != nil || limitPrice <= 0 {
			p.rejectOrder(nativeInst, order, "invalid limit price", start, order.Timestamp)
			return
		}
		limitPrice = cons.normalizePrice(limitPrice)
	} else {
		limitPrice = state.lastPrice
	}
	if limitPrice <= 0 {
		limitPrice = state.basePrice
	}
	if !cons.enforceNotional(limitPrice, qty) {
		p.rejectOrder(nativeInst, order, "min notional not met", start, order.Timestamp)
		return
	}
	if !p.applyVenueLatency(p.ctx) {
		return
	}
	now := p.clock().UTC()
	if !p.venueOperational(now) {
		p.rejectOrder(nativeInst, order, "venue unavailable", start, now)
		return
	}
	state.mu.Lock()
	tifMode := parseTIF(order.TIF)
	limitConstraint := limitPrice
	if order.OrderType == schema.OrderTypeMarket {
		limitConstraint = 0
	}
	if tifMode == tifFOK {
		available := state.availableLiquidity(order.Side, limitConstraint)
		if available+floatTolerance < qty {
			state.mu.Unlock()
			p.rejectOrder(nativeInst, order, "FOK insufficient liquidity", start, now)
			return
		}
	}
	if tifMode == tifPostOnly && state.isMarketable(order.Side, limitPrice) {
		state.mu.Unlock()
		p.rejectOrder(nativeInst, order, "post-only order would cross the book", start, now)
		return
	}
	active := &activeOrder{
		clientID:   order.ClientOrderID,
		exchangeID: p.nextExchangeOrderID(order.Symbol),
		instrument: order.Symbol,
		side:       order.Side,
		orderType:  order.OrderType,
		tif:        tifMode,
		price:      limitPrice,
		priceTick:  cons.tickForPrice(limitPrice),
		quantity:   qty,
		remaining:  qty,
		filled:     0,
		notional:   0,
		createdAt:  now,
		updatedAt:  now,
	}
	events := []execReportEvent{{
		instrument: nativeInst,
		payload:    buildExecPayload(active, schema.ExecReportStateACK, nil, cons, now),
		ts:         now,
	}}
	avgPrice := 0.0
	var fills []orderFill
	filled := 0.0
	if order.OrderType == schema.OrderTypeMarket || state.isMarketable(order.Side, limitPrice) {
		avgPrice, fills, filled = state.consumeLiquidity(order.Side, qty, limitConstraint, now)
		if filled > floatTolerance {
			active.recordFill(filled, avgPrice, now)
			fillState := schema.ExecReportStateFILLED
			if active.remaining > floatTolerance {
				fillState = schema.ExecReportStatePARTIAL
			}
			events = append(events, execReportEvent{
				instrument: nativeInst,
				payload:    buildExecPayload(active, fillState, nil, cons, now),
				ts:         now,
			})
		}
	}
	for _, fill := range fills {
		if fill.order == nil {
			continue
		}
		if fill.order.remaining <= floatTolerance {
			delete(state.orderIndex, fill.order.exchangeID)
		}
		stateCargo := schema.ExecReportStatePARTIAL
		if fill.order.remaining <= floatTolerance {
			stateCargo = schema.ExecReportStateFILLED
		}
		events = append(events, execReportEvent{
			instrument: nativeInst,
			payload:    buildExecPayload(fill.order, stateCargo, nil, cons, now),
			ts:         now,
		})
	}
	switch tifMode {
	case tifIOC, tifFOK:
		if active.remaining > floatTolerance {
			reason := "IOC remainder cancelled"
			if tifMode == tifFOK {
				reason = "FOK remainder cancelled"
			}
			events = append(events, execReportEvent{
				instrument: nativeInst,
				payload:    buildExecPayload(active, schema.ExecReportStateCANCELLED, ptr(reason), cons, now),
				ts:         now,
			})
			active.remaining = 0
		}
		delete(state.orderIndex, active.exchangeID)
	case tifGTC, tifPostOnly:
		if active.remaining > floatTolerance {
			state.restOrder(active)
			state.orderIndex[active.exchangeID] = active
		} else {
			delete(state.orderIndex, active.exchangeID)
		}
	}
	state.recenterBook(p, state.lastPrice)
	state.mu.Unlock()
	p.emitExecReportEvents(events)
	finalState := schema.ExecReportState("")
	if len(events) > 0 {
		finalState = events[len(events)-1].payload.State
	}
	p.recordOrderOutcome(order, finalState, start, "")
}

func (p *Provider) defaultInstrumentSymbol() string {
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	if len(p.defaultNativeInstruments) == 0 {
		return ""
	}
	return p.defaultNativeInstruments[0].Symbol()
}

func (p *Provider) nativeInstrumentForSymbol(symbol string) (nativeInstrument, bool) {
	normalized := normalizeInstrument(symbol)
	if normalized == "" {
		return nativeInstrument{symbol: ""}, false
	}
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	if _, ok := p.instruments[normalized]; !ok {
		return nativeInstrument{symbol: ""}, false
	}
	return nativeInstrument{symbol: normalized}, true
}

func (p *Provider) constraintsFor(symbol string) instrumentConstraints {
	normalized := normalizeInstrument(symbol)
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	if meta, ok := p.instrumentConstraints[normalized]; ok {
		return meta
	}
	return instrumentConstraints{
		priceIncrement:    0.01,
		quantityIncrement: 0.0001,
		minQuantity:       0.0001,
		maxQuantity:       0,
		minNotional:       0,
		pricePrecision:    2,
		quantityPrecision: 4,
	}
}

func (p *Provider) stopAll() {
	p.mu.Lock()
	handles := make([]*routeHandle, 0, len(p.routes))
	for typ, handle := range p.routes {
		handles = append(handles, handle)
		delete(p.routes, typ)
	}
	p.mu.Unlock()
	for _, handle := range handles {
		if handle == nil {
			continue
		}
		handle.cancel()
		handle.wg.Wait()
	}
}

func (p *Provider) nextSeq(evtType schema.EventType, instrument nativeInstrument) uint64 {
	key := fmt.Sprintf("%s|%s", evtType, instrument.Symbol())
	p.seqMu.Lock()
	seq := p.seq[key] + 1
	p.seq[key] = seq
	p.seqMu.Unlock()
	return seq
}

func (p *Provider) nextExchangeOrderID(symbol string) string {
	count := p.orderCounter.Add(1)
	clean := strings.ReplaceAll(strings.ToUpper(symbol), "-", "")
	return fmt.Sprintf("%s-%06d", clean, count)
}

func (p *Provider) randomTradeQuantity(cons instrumentConstraints) float64 {
	rangeSize := p.tradeModel.MaxQuantity - p.tradeModel.MinQuantity
	if rangeSize <= 0 {
		rangeSize = p.tradeModel.MinQuantity
	}
	qty := p.tradeModel.MinQuantity + p.randomFloat()*rangeSize
	if cons.minQuantity > 0 && qty < cons.minQuantity {
		qty = cons.minQuantity
	}
	if cons.maxQuantity > 0 && qty > cons.maxQuantity {
		qty = cons.maxQuantity
	}
	return cons.normalizeQuantity(qty)
}

func (p *Provider) getInstrumentState(inst nativeInstrument) *instrumentState {
	symbol := inst.Symbol()
	p.stateMu.Lock()
	state, ok := p.state[symbol]
	if !ok {
		state = newInstrumentState(symbol, defaultBasePrice(symbol), p.constraintsFor(symbol), p.bookOptions.Levels)
		p.state[symbol] = state
	}
	p.stateMu.Unlock()
	return state
}

func newInstrumentState(symbol string, basePrice float64, meta instrumentConstraints, levels int) *instrumentState {
	if levels <= 0 {
		levels = defaultBookLevels
	}
	state := &instrumentState{
		mu:           sync.Mutex{},
		instrument:   symbol,
		basePrice:    basePrice,
		lastPrice:    basePrice,
		volume:       1000,
		hasSnapshot:  false,
		constraints:  meta,
		bookLevels:   levels,
		bids:         make(map[priceTick]*bookDepth, levels),
		asks:         make(map[priceTick]*bookDepth, levels),
		orderIndex:   make(map[string]*activeOrder),
		lastDiff:     bookDiff{bids: nil, asks: nil},
		currentKline: nil,
		completed:    nil,
	}
	state.seedOrderBook()
	return state
}

func (s *instrumentState) seedOrderBook() {
	if s.bookLevels <= 0 {
		s.bookLevels = defaultBookLevels
	}
	step := math.Max(s.constraints.priceIncrement, 0.01)
	qty := math.Max(s.constraints.quantityIncrement, 0.1)
	for i := 1; i <= s.bookLevels; i++ {
		delta := float64(i) * step
		bidTick := s.constraints.tickForPrice(s.lastPrice - delta)
		askTick := s.constraints.tickForPrice(s.lastPrice + delta)
		s.bids[bidTick] = &bookDepth{synthetic: qty + 0.25*float64(i), orders: nil}
		s.asks[askTick] = &bookDepth{synthetic: qty + 0.2*float64(i), orders: nil}
	}
}

func snapshotNative(src []nativeInstrument) []nativeInstrument {
	if len(src) == 0 {
		return nil
	}
	out := make([]nativeInstrument, len(src))
	copy(out, src)
	return out
}

func snapshotCurrencies(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

func (p *Provider) setSupportedInstruments(list []schema.Instrument) {
	catalog := make(map[string]schema.Instrument, len(list))
	constraints := make(map[string]instrumentConstraints, len(list))
	seen := make(map[string]struct{}, len(list))
	natives := make([]nativeInstrument, 0, len(list))
	for _, inst := range list {
		clone := schema.CloneInstrument(inst)
		if err := clone.Validate(); err != nil {
			log.Printf("fake provider %s: dropping instrument %q: %v", p.name, inst.Symbol, err)
			continue
		}
		if clone.Type != schema.InstrumentTypeSpot {
			log.Printf("fake provider %s: instrument %q is %s; fake provider only supports spot", p.name, inst.Symbol, clone.Type)
			continue
		}
		native, err := p.instrumentCodec.ToNative(clone)
		if err != nil {
			log.Printf("fake provider %s: normalize instrument %q failed: %v", p.name, inst.Symbol, err)
			continue
		}
		meta, err := constraintsFromInstrument(clone)
		if err != nil {
			log.Printf("fake provider %s: instrument %q constraints invalid: %v", p.name, inst.Symbol, err)
			continue
		}
		catalog[native.symbol] = clone
		constraints[native.symbol] = meta
		if _, exists := seen[native.symbol]; !exists {
			seen[native.symbol] = struct{}{}
			natives = append(natives, native)
		}
	}
	if len(catalog) == 0 {
		log.Printf("fake provider %s: instrument catalogue empty; retaining previous set", p.name)
		return
	}
	sort.Slice(natives, func(i, j int) bool { return natives[i].symbol < natives[j].symbol })
	ordered := make([]schema.Instrument, 0, len(natives))
	for _, native := range natives {
		if inst, ok := catalog[native.symbol]; ok {
			ordered = append(ordered, schema.CloneInstrument(inst))
		}
	}
	p.instrumentMu.Lock()
	prev := p.instruments
	p.instruments = catalog
	p.instrumentConstraints = constraints
	p.defaultNativeInstruments = natives
	p.instrumentMu.Unlock()
	log.Printf("fake provider %s: instrument catalogue updated: %d instruments", p.name, len(ordered))
	p.emitInstrumentDiff(prev, catalog)
}

func (p *Provider) runInstrumentRefresh(ctx context.Context) {
	ticker := time.NewTicker(p.instrumentRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.refreshInstruments(ctx)
		}
	}
}

func (p *Provider) refreshInstruments(ctx context.Context) {
	if p.instrumentRefresh == nil {
		return
	}
	list, err := p.instrumentRefresh(ctx)
	if err != nil {
		p.emitError(fmt.Errorf("instrument refresh: %w", err))
		return
	}
	if len(list) == 0 {
		return
	}
	p.setSupportedInstruments(list)
}

func (p *Provider) emitInstrumentDiff(previous map[string]schema.Instrument, current map[string]schema.Instrument) {
	for symbol, inst := range current {
		if prev, ok := previous[symbol]; ok && instrumentsEqual(prev, inst) {
			continue
		}
		p.emitInstrumentUpdate(inst)
	}
}

func buildExecPayload(order *activeOrder, state schema.ExecReportState, reason *string, cons instrumentConstraints, ts time.Time) schema.ExecReportPayload {
	if order == nil {
		var empty schema.ExecReportPayload
		return empty
	}
	priceValue := order.price
	if order.orderType == schema.OrderTypeMarket && order.avgFillPrice() > 0 {
		priceValue = order.avgFillPrice()
	}
	priceStr := formatWithPrecision(priceValue, cons.pricePrecision)
	filled := formatWithPrecision(order.filled, cons.quantityPrecision)
	remaining := formatWithPrecision(order.remaining, cons.quantityPrecision)
	qtyStr := formatWithPrecision(order.quantity, cons.quantityPrecision)
	avgFill := formatWithPrecision(order.avgFillPrice(), cons.pricePrecision)
	return schema.ExecReportPayload{
		ClientOrderID:   order.clientID,
		ExchangeOrderID: order.exchangeID,
		State:           state,
		Side:            order.side,
		OrderType:       order.orderType,
		Price:           priceStr,
		Quantity:        qtyStr,
		FilledQuantity:  filled,
		RemainingQty:    remaining,
		AvgFillPrice:    avgFill,
		Timestamp:       ts,
		RejectReason:    reason,
	}
}

func (p *Provider) randomFloat() float64 {
	p.randMu.Lock()
	defer p.randMu.Unlock()
	return p.rng.Float64()
}

func (p *Provider) randomNorm() float64 {
	p.randMu.Lock()
	defer p.randMu.Unlock()
	return p.rng.NormFloat64()
}

func (p *Provider) randomDuration(minDur, maxDur time.Duration) time.Duration {
	if maxDur <= minDur {
		return minDur
	}
	rangeDur := maxDur - minDur
	factor := p.randomFloat()
	return minDur + time.Duration(float64(rangeDur)*factor)
}

func (p *Provider) emitInstrumentUpdate(inst schema.Instrument) {
	if strings.TrimSpace(inst.Symbol) == "" {
		return
	}
	now := p.clock().UTC()
	native := nativeInstrument{symbol: inst.Symbol}
	seq := p.nextSeq(schema.EventTypeInstrumentUpdate, native)
	payload := schema.InstrumentUpdatePayload{Instrument: schema.CloneInstrument(inst)}
	evt := p.borrowEvent(p.ctx)
	if evt == nil {
		return
	}
	evt.EventID = fmt.Sprintf("%s:INST:%s:%d", p.name, strings.ReplaceAll(inst.Symbol, "-", ""), seq)
	evt.Provider = p.name
	evt.Symbol = inst.Symbol
	evt.Type = schema.EventTypeInstrumentUpdate
	evt.SeqProvider = seq
	evt.IngestTS = now
	evt.EmitTS = now
	evt.Payload = payload
	p.emitEvent(evt)
}

func instrumentsEqual(a, b schema.Instrument) bool {
	return reflect.DeepEqual(a, b)
}

func constraintsFromInstrument(inst schema.Instrument) (instrumentConstraints, error) {
	var meta instrumentConstraints
	var err error
	if meta.priceIncrement, err = parseDecimal(inst.PriceIncrement, 0.01); err != nil {
		return instrumentConstraints{}, fmt.Errorf("price increment: %w", err)
	}
	if meta.quantityIncrement, err = parseDecimal(inst.QuantityIncrement, 0.0001); err != nil {
		return instrumentConstraints{}, fmt.Errorf("quantity increment: %w", err)
	}
	if meta.minQuantity, err = parseDecimal(inst.MinQuantity, meta.quantityIncrement); err != nil {
		return instrumentConstraints{}, fmt.Errorf("min quantity: %w", err)
	}
	if meta.maxQuantity, err = parseDecimal(inst.MaxQuantity, 0); err != nil {
		return instrumentConstraints{}, fmt.Errorf("max quantity: %w", err)
	}
	if meta.minNotional, err = parseDecimal(inst.MinNotional, 0); err != nil {
		return instrumentConstraints{}, fmt.Errorf("min notional: %w", err)
	}
	if inst.PricePrecision != nil {
		meta.pricePrecision = *inst.PricePrecision
	}
	if meta.pricePrecision <= 0 {
		meta.pricePrecision = 2
	}
	if inst.QuantityPrecision != nil {
		meta.quantityPrecision = *inst.QuantityPrecision
	}
	if meta.quantityPrecision <= 0 {
		meta.quantityPrecision = 4
	}
	if meta.priceIncrement <= 0 {
		meta.priceIncrement = 0.01
	}
	if meta.quantityIncrement <= 0 {
		meta.quantityIncrement = 0.0001
	}
	return meta, nil
}

func parseDecimal(input string, fallback float64) (float64, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float %q: %w", value, err)
	}
	return f, nil
}

func (ic instrumentConstraints) tickForPrice(price float64) priceTick {
	if ic.priceIncrement <= 0 {
		return priceTick(math.Round(price / 0.01))
	}
	return priceTick(math.Round(price / ic.priceIncrement))
}

func (ic instrumentConstraints) priceForTick(t priceTick) float64 {
	if ic.priceIncrement <= 0 {
		return float64(t) * 0.01
	}
	return float64(t) * ic.priceIncrement
}

func (ic instrumentConstraints) normalizePrice(price float64) float64 {
	if ic.priceIncrement <= 0 {
		return price
	}
	steps := math.Round(price / ic.priceIncrement)
	return steps * ic.priceIncrement
}

func (ic instrumentConstraints) normalizeQuantity(q float64) float64 {
	if ic.quantityIncrement <= 0 {
		return q
	}
	steps := math.Round(q / ic.quantityIncrement)
	return steps * ic.quantityIncrement
}

func (ic instrumentConstraints) validQuantity(q float64) bool {
	if q <= 0 {
		return false
	}
	if ic.minQuantity > 0 && q+floatTolerance < ic.minQuantity {
		return false
	}
	if ic.maxQuantity > 0 && q-ic.maxQuantity > floatTolerance {
		return false
	}
	if ic.quantityIncrement > 0 {
		steps := math.Round(q / ic.quantityIncrement)
		return math.Abs(q-steps*ic.quantityIncrement) < 1e-9
	}
	return true
}

func (ic instrumentConstraints) enforceNotional(price, qty float64) bool {
	if ic.minNotional <= 0 {
		return true
	}
	return price*qty+floatTolerance >= ic.minNotional
}

func (p *Provider) applyVenueLatency(ctx context.Context) bool {
	if p.venueCfg.LatencyMin <= 0 {
		return true
	}
	delay := p.randomDuration(p.venueCfg.LatencyMin, p.venueCfg.LatencyMax)
	if delay <= 0 {
		return true
	}
	if ctx == nil {
		ctx = p.ctx
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (p *Provider) venueOperational(now time.Time) bool {
	p.venueState.mu.Lock()
	defer p.venueState.mu.Unlock()
	if p.venueState.disconnected {
		if now.After(p.venueState.reconnectAt) {
			p.venueState.disconnected = false
			p.recordVenueDisruption("reconnected")
		} else {
			return false
		}
	}
	if p.venueCfg.DisconnectChance > 0 && p.randomFloat() < p.venueCfg.DisconnectChance {
		p.venueState.disconnected = true
		p.venueState.reconnectAt = now.Add(p.venueCfg.DisconnectFor)
		p.emitError(fmt.Errorf("fake provider %s: venue link temporarily unavailable", p.name))
		p.recordVenueDisruption("disconnected")
		return false
	}
	return true
}

func (p *Provider) venueShouldError() bool {
	if p.venueCfg.TransientError > 0 && p.randomFloat() < p.venueCfg.TransientError {
		p.recordVenueError("transient")
		return true
	}
	return false
}

func normalizeInstrument(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func normalizeCurrency(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) < 2 || len(code) > 10 {
		return ""
	}
	for _, r := range code {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return code
}

func defaultBasePrice(symbol string) float64 {
	switch symbol {
	case "BTC-USDT":
		return 60000
	case "ETH-USDT":
		return 2000
	case "SOL-USDT":
		return 150
	default:
		return 100
	}
}

func (p *Provider) nextModelPrice(state *instrumentState) float64 {
	base := state.lastPrice
	if base <= 0 {
		base = state.basePrice
	}
	drift := base * p.priceModel.Drift
	vol := base * p.priceModel.Volatility * p.randomNorm()
	shock := 0.0
	if p.randomFloat() < p.priceModel.ShockProbability {
		direction := 1.0
		if p.randomFloat() < 0.5 {
			direction = -1
		}
		shock = direction * p.priceModel.ShockMagnitude * base
	}
	price := base + drift + vol + shock
	if price <= 0 {
		price = state.basePrice
	}
	return price
}

func formatLevels(levels []bookLevel, cons instrumentConstraints) []schema.PriceLevel {
	if len(levels) == 0 {
		return nil
	}
	out := make([]schema.PriceLevel, len(levels))
	for i, lvl := range levels {
		out[i] = schema.PriceLevel{
			Price:    formatWithPrecision(lvl.price, cons.pricePrecision),
			Quantity: formatWithPrecision(lvl.quantity, cons.quantityPrecision),
		}
	}
	return out
}

func canonicalToEventType(c schema.CanonicalType) (schema.EventType, bool) {
	switch c {
	case schema.CanonicalType("ORDERBOOK.SNAPSHOT"), schema.CanonicalType("ORDERBOOK.DELTA"), schema.CanonicalType("ORDERBOOK.UPDATE"):
		return schema.EventTypeBookSnapshot, true
	case schema.CanonicalType("TRADE"):
		return schema.EventTypeTrade, true
	case schema.CanonicalType("TICKER"):
		return schema.EventTypeTicker, true
	case schema.CanonicalType("EXECUTION.REPORT"):
		return schema.EventTypeExecReport, true
	case schema.CanonicalType("KLINE.SUMMARY"), schema.CanonicalType("KLINE"):
		return schema.EventTypeKlineSummary, true
	case schema.CanonicalTypeAccountBalance:
		return schema.EventTypeBalanceUpdate, true
	default:
		return "", false
	}
}

func checksum(instrument string, evtType schema.EventType, seq uint64) string {
	base := fmt.Sprintf("%s|%s|%d", instrument, evtType, seq)
	sum := uint32(0)
	for _, r := range base {
		sum = sum*33 + uint32(r)
	}
	return fmt.Sprintf("%08d", sum%100000000)
}

func (p *Provider) recordEventMetric(evtType schema.EventType, symbol string) {
	if p.metrics.eventsEmitted == nil || p.ctx == nil {
		return
	}
	attrs := telemetry.EventAttributes(telemetry.Environment(), string(evtType), p.name, symbol)
	p.metrics.eventsEmitted.Add(p.ctx, 1, metric.WithAttributes(attrs...))
}

func (p *Provider) recordOrderReceived(order schema.OrderRequest) {
	if p.metrics.ordersReceived == nil || p.ctx == nil {
		return
	}
	attrs := telemetry.OrderAttributes(telemetry.Environment(), p.name, order.Symbol, string(order.Side), string(order.OrderType), order.TIF)
	p.metrics.ordersReceived.Add(p.ctx, 1, metric.WithAttributes(attrs...))
}

func (p *Provider) recordOrderOutcome(order schema.OrderRequest, state schema.ExecReportState, start time.Time, reason string) {
	if p.ctx == nil {
		return
	}
	attrs := telemetry.OrderAttributes(telemetry.Environment(), p.name, order.Symbol, string(order.Side), string(order.OrderType), order.TIF)
	if p.metrics.orderLatency != nil {
		latencyAttrs := append([]attribute.KeyValue(nil), attrs...)
		if state != "" {
			latencyAttrs = append(latencyAttrs, telemetry.AttrOrderState.String(string(state)))
		}
		duration := float64(0)
		if !start.IsZero() {
			delta := p.clock().Sub(start).Milliseconds()
			if delta > 0 {
				duration = float64(delta)
			}
		}
		p.metrics.orderLatency.Record(p.ctx, duration, metric.WithAttributes(latencyAttrs...))
	}
	if state == schema.ExecReportStateREJECTED && p.metrics.ordersRejected != nil {
		rejectAttrs := append([]attribute.KeyValue(nil), attrs...)
		if reason != "" {
			rejectAttrs = append(rejectAttrs, telemetry.AttrReason.String(reason))
		}
		if state != "" {
			rejectAttrs = append(rejectAttrs, telemetry.AttrOrderState.String(string(state)))
		}
		p.metrics.ordersRejected.Add(p.ctx, 1, metric.WithAttributes(rejectAttrs...))
	}
}

func (p *Provider) recordVenueDisruption(result string) {
	if p.metrics.venueDisruptions == nil || p.ctx == nil {
		return
	}
	attrs := telemetry.OperationResultAttributes(telemetry.Environment(), p.name, "venue_link", result)
	p.metrics.venueDisruptions.Add(p.ctx, 1, metric.WithAttributes(attrs...))
}

func (p *Provider) recordVenueError(result string) {
	if p.metrics.venueErrors == nil || p.ctx == nil {
		return
	}
	attrs := telemetry.OperationResultAttributes(telemetry.Environment(), p.name, "venue_error", result)
	p.metrics.venueErrors.Add(p.ctx, 1, metric.WithAttributes(attrs...))
}

func formatPrice(value float64) string {
	return fmt.Sprintf("%.2f", value)
}

func formatWithPrecision(value float64, precision int) string {
	if precision <= 0 {
		precision = 2
	}
	return fmt.Sprintf("%.*f", precision, value)
}

func formatBalance(value float64) string {
	if value < 0 {
		value = 0
	}
	return fmt.Sprintf("%.8f", value)
}

func stringOrDefault(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (p *Provider) emitReject(nativeInst nativeInstrument, req schema.OrderRequest, reason string, ts time.Time) {
	payload := schema.ExecReportPayload{
		ClientOrderID:   req.ClientOrderID,
		ExchangeOrderID: p.nextExchangeOrderID(req.Symbol),
		State:           schema.ExecReportStateREJECTED,
		Side:            req.Side,
		OrderType:       req.OrderType,
		Price:           stringOrDefault(req.Price),
		Quantity:        req.Quantity,
		FilledQuantity:  "0",
		RemainingQty:    req.Quantity,
		AvgFillPrice:    "0",
		Timestamp:       ts,
		RejectReason:    ptr(reason),
	}
	p.emitExecReportEvents([]execReportEvent{{instrument: nativeInst, payload: payload, ts: ts}})
}

func (p *Provider) rejectOrder(nativeInst nativeInstrument, order schema.OrderRequest, reason string, start time.Time, ts time.Time) {
	p.emitReject(nativeInst, order, reason, ts)
	p.recordOrderOutcome(order, schema.ExecReportStateREJECTED, start, reason)
}

func ptr[T any](value T) *T {
	return &value
}
