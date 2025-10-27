package shared

import (
	"fmt"
	"math"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

// ExecReportSnapshot captures the state required to build a canonical execution report payload.
type ExecReportSnapshot struct {
	ClientOrderID     string
	ExchangeOrderID   string
	State             schema.ExecReportState
	Side              schema.TradeSide
	OrderType         schema.OrderType
	Price             float64
	Quantity          float64
	Filled            float64
	Remaining         float64
	AvgFillPrice      float64
	PricePrecision    int
	QuantityPrecision int
	BaseCurrency      string
	CommissionRate    float64
	Timestamp         time.Time
	RejectReason      *string
}

// BuildExecReportPayload normalizes the snapshot into a schema.ExecReportPayload.
func BuildExecReportPayload(s ExecReportSnapshot) schema.ExecReportPayload {
	priceValue := s.Price
	if s.OrderType == schema.OrderTypeMarket && s.AvgFillPrice > 0 {
		priceValue = s.AvgFillPrice
	}

	priceStr := formatWithPrecision(priceValue, s.PricePrecision)
	qtyStr := formatWithPrecision(s.Quantity, s.QuantityPrecision)
	filledStr := formatWithPrecision(s.Filled, s.QuantityPrecision)
	remainingStr := formatWithPrecision(s.Remaining, s.QuantityPrecision)
	avgFillStr := formatWithPrecision(s.AvgFillPrice, s.PricePrecision)

	var commissionAmount string
	if s.CommissionRate > 0 && s.BaseCurrency != "" && s.Filled > 0 {
		commission := s.Filled * s.CommissionRate
		if commission > 0 && !math.IsNaN(commission) && !math.IsInf(commission, 0) {
			commissionAmount = formatWithPrecision(commission, s.QuantityPrecision)
		}
	}

	return schema.ExecReportPayload{
		ClientOrderID:    s.ClientOrderID,
		ExchangeOrderID:  s.ExchangeOrderID,
		State:            s.State,
		Side:             s.Side,
		OrderType:        s.OrderType,
		Price:            priceStr,
		Quantity:         qtyStr,
		FilledQuantity:   filledStr,
		RemainingQty:     remainingStr,
		AvgFillPrice:     avgFillStr,
		CommissionAmount: commissionAmount,
		CommissionAsset:  schema.NormalizeCurrencyCode(s.BaseCurrency),
		Timestamp:        s.Timestamp,
		RejectReason:     s.RejectReason,
	}
}

func formatWithPrecision(value float64, precision int) string {
	if precision <= 0 {
		precision = 2
	}
	return fmt.Sprintf("%.*f", precision, value)
}
