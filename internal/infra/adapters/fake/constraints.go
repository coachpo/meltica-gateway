// Package fake provides helper constraints for the synthetic adapter.
package fake

import (
	"math"
	"strconv"
	"strings"

	"github.com/coachpo/meltica/internal/domain/schema"
)

type priceTick int64

type instrumentConstraints struct {
	priceIncrement    float64
	quantityIncrement float64
	minQuantity       float64
	maxQuantity       float64
	minNotional       float64
	pricePrecision    int
	quantityPrecision int
}

func constraintsFromInstrument(inst schema.Instrument) instrumentConstraints {
	cons := instrumentConstraints{
		priceIncrement:    parseFloat(inst.PriceIncrement, 0.01),
		quantityIncrement: parseFloat(inst.QuantityIncrement, 0.0001),
		minQuantity:       parseFloat(inst.MinQuantity, 0),
		maxQuantity:       parseFloat(inst.MaxQuantity, 0),
		minNotional:       parseFloat(inst.MinNotional, 0),
		pricePrecision:    derefInt(inst.PricePrecision, 2),
		quantityPrecision: derefInt(inst.QuantityPrecision, 4),
	}
	if cons.priceIncrement <= 0 {
		cons.priceIncrement = 0.01
	}
	if cons.quantityIncrement <= 0 {
		cons.quantityIncrement = 0.0001
	}
	return cons
}

func (c instrumentConstraints) tickForPrice(price float64) priceTick {
	if price <= 0 {
		price = c.priceIncrement
	}
	if c.priceIncrement <= 0 {
		return priceTick(math.Round(price * 1e4))
	}
	return priceTick(math.Round(price / c.priceIncrement))
}

func (c instrumentConstraints) priceForTick(t priceTick) float64 {
	if c.priceIncrement <= 0 {
		return float64(t) / 1e4
	}
	return float64(t) * c.priceIncrement
}

func (c instrumentConstraints) normalizeQuantity(qty float64) float64 {
	if c.quantityIncrement <= 0 {
		return qty
	}
	steps := math.Round(qty / c.quantityIncrement)
	return steps * c.quantityIncrement
}

func formatWithPrecision(value float64, precision int) string {
	if precision < 0 {
		precision = 0
	}
	return strconv.FormatFloat(value, 'f', precision, 64)
}

func parseFloat(raw string, fallback float64) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		return v
	}
	return fallback
}

func derefInt(ptr *int, fallback int) int {
	if ptr == nil {
		return fallback
	}
	if *ptr <= 0 {
		return fallback
	}
	return *ptr
}

func normalizeInstrument(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func defaultBasePrice(symbol string) float64 {
	symbol = normalizeInstrument(symbol)
	switch {
	case strings.HasPrefix(symbol, "BTC"):
		return 30000
	case strings.HasPrefix(symbol, "ETH"):
		return 2000
	case strings.HasPrefix(symbol, "SOL"):
		return 40
	case strings.HasPrefix(symbol, "DOGE"):
		return 0.07
	default:
		return 100
	}
}
