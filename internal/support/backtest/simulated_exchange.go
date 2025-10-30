package backtest

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/app/lambda"
	"github.com/coachpo/meltica/internal/domain/schema"
)

// ExecutionResult captures the outcome of an order processed by the simulated exchange.
type ExecutionResult struct {
	Report schema.ExecReportPayload
	Fee    decimal.Decimal
}

// FillObserver receives notifications when simulated fills occur.
type FillObserver interface {
	OnOrderSubmitted(req schema.OrderRequest)
	OnFill(symbol string, report schema.ExecReportPayload, fee decimal.Decimal)
}

// SimulatedExchange models an exchange capable of matching orders within the backtest environment.
type SimulatedExchange interface {
	SubmitOrder(ctx context.Context, req schema.OrderRequest) (ExecutionResult, error)
}

// ExchangeOption configures optional behaviour for a simulated exchange instance.
type ExchangeOption func(*simulatedExchange)

var defaultFeeRate = decimal.RequireFromString("0.002")

type simulatedExchange struct {
	mu            sync.RWMutex
	orderBooks    map[string]*OrderBook
	lambda        lambda.TradingStrategy
	feeModel      FeeModel
	slippageModel SlippageModel
	observer      FillObserver
	clock         Clock
}

// WithFeeModel overrides the default fee model used by the simulated exchange.
func WithFeeModel(model FeeModel) ExchangeOption {
	return func(se *simulatedExchange) {
		se.feeModel = model
	}
}

// WithSlippageModel sets the slippage model for simulated executions.
func WithSlippageModel(model SlippageModel) ExchangeOption {
	return func(se *simulatedExchange) {
		se.slippageModel = model
	}
}

// WithFillObserver subscribes an observer to execution notifications.
func WithFillObserver(observer FillObserver) ExchangeOption {
	return func(se *simulatedExchange) {
		se.observer = observer
	}
}

// WithExchangeClock injects a custom clock for timestamping simulated executions.
func WithExchangeClock(clock Clock) ExchangeOption {
	return func(se *simulatedExchange) {
		se.clock = clock
	}
}

// NewSimulatedExchange creates a new simulated exchange instance.
func NewSimulatedExchange(strategy lambda.TradingStrategy, opts ...ExchangeOption) SimulatedExchange {
	se := &simulatedExchange{
		mu:            sync.RWMutex{},
		orderBooks:    make(map[string]*OrderBook),
		lambda:        strategy,
		feeModel:      ProportionalFee{Rate: defaultFeeRate},
		slippageModel: nil,
		observer:      nil,
		clock:         NewVirtualClock(time.Unix(0, 0)),
	}
	for _, opt := range opts {
		opt(se)
	}
	return se
}

// SubmitOrder submits an order to the simulated exchange and returns an execution result.
func (se *simulatedExchange) SubmitOrder(ctx context.Context, req schema.OrderRequest) (ExecutionResult, error) {
	se.mu.Lock()
	defer se.mu.Unlock()

	ob, ok := se.orderBooks[req.Symbol]
	if !ok {
		ob = NewOrderBook()
		se.orderBooks[req.Symbol] = ob
	}

	if se.slippageModel != nil {
		req = se.applySlippage(req, ob)
	}

	if se.observer != nil {
		se.observer.OnOrderSubmitted(req)
	}

	trades := ob.Match(&req)
	if len(trades) == 0 {
		var empty schema.ExecReportPayload
		return ExecutionResult{Report: empty, Fee: decimal.Zero}, nil
	}

	totalQty := decimal.Zero
	totalNotional := decimal.Zero
	for _, trade := range trades {
		qty, err := decimal.NewFromString(trade.Quantity)
		if err != nil {
			continue
		}
		price, err := decimal.NewFromString(trade.Price)
		if err != nil {
			continue
		}
		totalQty = totalQty.Add(qty)
		totalNotional = totalNotional.Add(qty.Mul(price))
	}
	if totalQty.Equal(decimal.Zero) {
		var empty schema.ExecReportPayload
		return ExecutionResult{Report: empty, Fee: decimal.Zero}, nil
	}
	avgPrice := totalNotional.Div(totalQty)
	fee := decimal.Zero
	if se.feeModel != nil {
		fee = se.feeModel.Fee(req, totalQty, avgPrice)
	}
	report := schema.ExecReportPayload{
		ClientOrderID:    req.ClientOrderID,
		ExchangeOrderID:  "",
		State:            schema.ExecReportStateFILLED,
		Side:             req.Side,
		OrderType:        req.OrderType,
		Price:            avgPrice.String(),
		Quantity:         req.Quantity,
		FilledQuantity:   totalQty.String(),
		RemainingQty:     decimal.Zero.String(),
		AvgFillPrice:     avgPrice.String(),
		CommissionAmount: "",
		CommissionAsset:  "",
		Timestamp:        se.clock.Now(),
		RejectReason:     nil,
	}

	if fee.GreaterThan(decimal.Zero) {
		if amount := commissionAmountBase(fee, avgPrice); amount != "" {
			report.CommissionAmount = amount
			if asset := extractBaseAsset(req.Symbol); asset != "" {
				report.CommissionAsset = asset
			}
		}
	}

	if se.lambda != nil {
		now := se.clock.Now()
		evt := &schema.Event{
			EventID:        "",
			RoutingVersion: 0,
			Provider:       req.Provider,
			Symbol:         req.Symbol,
			Type:           schema.EventTypeExecReport,
			SeqProvider:    0,
			IngestTS:       now,
			EmitTS:         now,
			Payload:        report,
		}
		se.lambda.OnOrderFilled(ctx, evt, report)
	}
	if se.observer != nil {
		se.observer.OnFill(req.Symbol, report, fee)
	}

	return ExecutionResult{Report: report, Fee: fee}, nil
}

func (se *simulatedExchange) applySlippage(req schema.OrderRequest, ob *OrderBook) schema.OrderRequest {
	adjustment := se.slippageModel.Adjust(req, ob)
	if adjustment.Equal(decimal.Zero) {
		return req
	}
	if req.Price == nil {
		return req
	}
	price, err := decimal.NewFromString(*req.Price)
	if err != nil {
		return req
	}
	if req.Side == schema.TradeSideSell {
		price = price.Sub(adjustment.Abs())
	} else {
		price = price.Add(adjustment.Abs())
	}
	priceStr := price.String()
	req.Price = &priceStr
	return req
}

func (se *simulatedExchange) setObserver(observer FillObserver) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.observer = observer
}

func (se *simulatedExchange) setClock(clock Clock) {
	if clock == nil {
		return
	}
	se.mu.Lock()
	se.clock = clock
	se.mu.Unlock()
}

func commissionAmountBase(fee, price decimal.Decimal) string {
	if fee.LessThanOrEqual(decimal.Zero) {
		return ""
	}
	if price.LessThanOrEqual(decimal.Zero) {
		return ""
	}
	amount := fee.DivRound(price, 8)
	if amount.LessThanOrEqual(decimal.Zero) {
		return ""
	}
	return amount.String()
}

func extractBaseAsset(symbol string) string {
	trimmed := strings.TrimSpace(symbol)
	if trimmed == "" {
		return ""
	}
	upper := strings.ToUpper(trimmed)
	for _, sep := range []string{"-", "/", "_", ":"} {
		if idx := strings.Index(upper, sep); idx > 0 {
			return strings.TrimSpace(upper[:idx])
		}
	}
	return ""
}
