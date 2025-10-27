package fake

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

type instrumentState struct {
	mu           sync.Mutex
	instrument   string
	basePrice    float64
	lastPrice    float64
	volume24h    float64
	constraints  instrumentConstraints
	bookLevels   int
	bids         map[priceTick]*bookDepth
	asks         map[priceTick]*bookDepth
	currentKline *klineWindow
	completed    []klineWindow
	orderIndex   map[string]*activeOrder
}

func newInstrumentState(symbol string, basePrice float64, cons instrumentConstraints, levels int) *instrumentState {
	if levels <= 0 {
		levels = defaultBookLevels
	}
	return &instrumentState{
		instrument:  symbol,
		basePrice:   basePrice,
		lastPrice:   basePrice,
		constraints: cons,
		bookLevels:  levels,
		bids:        make(map[priceTick]*bookDepth),
		asks:        make(map[priceTick]*bookDepth),
		orderIndex:  make(map[string]*activeOrder),
	}
}

func (s *instrumentState) restOrder(order *activeOrder) {
	if order == nil {
		return
	}
	depthMap := s.bids
	if order.side == schema.TradeSideSell {
		depthMap = s.asks
	}
	depth, ok := depthMap[order.priceTick]
	if !ok {
		depth = &bookDepth{}
		depthMap[order.priceTick] = depth
	}
	depth.orders = append(depth.orders, order)
}

func (s *instrumentState) consumeLiquidity(side schema.TradeSide, quantity float64, limit float64, ts time.Time) (float64, []orderFill, float64) {
	if quantity <= floatTolerance {
		return 0, nil, 0
	}

	depthMap := s.asks
	ascending := true
	if side == schema.TradeSideSell {
		depthMap = s.bids
		ascending = false
	}

	ticks := orderedTicks(depthMap, ascending)
	if len(ticks) == 0 {
		return 0, nil, 0
	}

	filled := 0.0
	notional := 0.0
	fills := make([]orderFill, 0, len(ticks))

	for _, tick := range ticks {
		depth := depthMap[tick]
		if depth == nil {
			continue
		}
		price := s.constraints.priceForTick(tick)
		if limit > 0 {
			if side == schema.TradeSideBuy && price > limit+floatTolerance {
				continue
			}
			if side == schema.TradeSideSell && price+floatTolerance < limit {
				continue
			}
		}
		for len(depth.orders) > 0 && filled+floatTolerance < quantity {
			ord := depth.orders[0]
			if ord == nil || ord.remaining <= floatTolerance {
				depth.orders = depth.orders[1:]
				continue
			}
			needed := quantity - filled
			take := math.Min(needed, ord.remaining)
			ord.recordFill(take, price, ts)
			filled += take
			notional += take * price
			fills = append(fills, orderFill{order: ord, quantity: take, price: price})
			if ord.remaining <= floatTolerance {
				depth.orders = depth.orders[1:]
			}
		}
		depth.orders = pruneOrders(depth.orders)
		if filled+floatTolerance >= quantity {
			break
		}
	}

	if filled <= floatTolerance {
		return 0, nil, 0
	}
	avg := notional / filled
	return avg, fills, filled
}

func (s *instrumentState) bestBid() (float64, bool) {
	return bestPrice(s.bids, false, s.constraints)
}

func (s *instrumentState) bestAsk() (float64, bool) {
	return bestPrice(s.asks, true, s.constraints)
}

func bestPrice(levels map[priceTick]*bookDepth, ascending bool, cons instrumentConstraints) (float64, bool) {
	ticks := orderedTicks(levels, ascending)
	for _, tick := range ticks {
		depth := levels[tick]
		if depth == nil {
			continue
		}
		qty := userQuantity(depth.orders)
		if qty <= floatTolerance {
			continue
		}
		return cons.priceForTick(tick), true
	}
	return 0, false
}

func orderedTicks(levels map[priceTick]*bookDepth, ascending bool) []priceTick {
	ticks := make([]priceTick, 0, len(levels))
	for tick, depth := range levels {
		if depth == nil {
			continue
		}
		if depth.synthetic <= floatTolerance && userQuantity(depth.orders) <= floatTolerance {
			continue
		}
		ticks = append(ticks, tick)
	}
	sort.Slice(ticks, func(i, j int) bool {
		if ascending {
			return ticks[i] < ticks[j]
		}
		return ticks[i] > ticks[j]
	})
	return ticks
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
	ready := make([]klineWindow, 0)
	for len(s.completed) > 0 && !now.Before(s.completed[0].closeTime) {
		ready = append(ready, s.completed[0])
		s.completed = s.completed[1:]
	}
	if s.currentKline != nil && !now.Before(s.currentKline.closeTime) {
		ready = append(ready, *s.currentKline)
		start := s.currentKline.closeTime
		s.currentKline = newKlineWindow(start, interval, s.currentKline.close)
	}
	return ready
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
		high:      price,
		low:       price,
		close:     price,
	}
}

func (k *klineWindow) update(price float64, qty float64) {
	if k == nil {
		return
	}
	if k.high < price {
		k.high = price
	}
	if k.low == 0 || k.low > price {
		k.low = price
	}
	k.close = price
	if qty > 0 {
		k.volume += qty
	}
}
