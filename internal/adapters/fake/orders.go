package fake

import (
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

type tifMode int

const (
	tifGTC tifMode = iota
	tifIOC
	tifFOK
	tifPostOnly
)

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
	updatedAt  time.Time
}

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

type orderFill struct {
	order    *activeOrder
	quantity float64
	price    float64
}

type bookDepth struct {
	synthetic float64
	orders    []*activeOrder
}

func userQuantity(orders []*activeOrder) float64 {
	sum := 0.0
	for _, ord := range orders {
		if ord == nil {
			continue
		}
		if ord.remaining > 0 {
			sum += ord.remaining
		}
	}
	return sum
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
