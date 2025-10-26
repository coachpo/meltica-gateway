// Package risk provides runtime enforcement of trading risk limits and controls.
package risk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"golang.org/x/time/rate"

	"github.com/coachpo/meltica/internal/schema"
)

var (
	// ErrKillSwitchEngaged is returned when trading is halted by the kill switch.
	ErrKillSwitchEngaged = errors.New("risk: kill switch engaged")
	// ErrCircuitBreakerOpen is returned while the circuit breaker cooldown is active.
	ErrCircuitBreakerOpen = errors.New("risk: circuit breaker open")
)

// BreachType identifies the category of a risk breach.
type BreachType string

const (
	// BreachTypeRateLimit indicates rate limiting was exceeded.
	BreachTypeRateLimit BreachType = "RATE_LIMIT"
	// BreachTypeOrderType denotes an unsupported order type was requested.
	BreachTypeOrderType BreachType = "ORDER_TYPE"
	// BreachTypeOrderValidation captures general validation failures.
	BreachTypeOrderValidation BreachType = "ORDER_VALIDATION"
	// BreachTypePriceBand signals that price bands were violated.
	BreachTypePriceBand BreachType = "PRICE_BAND"
	// BreachTypePositionLimit represents breaches of position limits.
	BreachTypePositionLimit BreachType = "POSITION_LIMIT"
	// BreachTypeNotionalLimit indicates notional exposure exceeded limits.
	BreachTypeNotionalLimit BreachType = "NOTIONAL_LIMIT"
	// BreachTypeConcurrency denotes concurrency limits were breached.
	BreachTypeConcurrency BreachType = "CONCURRENCY"
	// BreachTypeKillSwitch indicates a manual or automatic kill switch activation.
	BreachTypeKillSwitch BreachType = "KILL_SWITCH"
)

// BreachError captures structured metadata about a risk breach.
type BreachError struct {
	Type               BreachType
	Reason             string
	Details            map[string]string
	KillSwitchEngaged  bool
	CircuitBreakerOpen bool
	Err                error
}

// Error implements the error interface.
func (b *BreachError) Error() string {
	if b == nil {
		return ""
	}
	if b.Err != nil {
		return fmt.Sprintf("%s: %v", b.Reason, b.Err)
	}
	return b.Reason
}

// Unwrap returns the wrapped error.
func (b *BreachError) Unwrap() error {
	if b == nil {
		return nil
	}
	return b.Err
}

func newBreachError(t BreachType, reason string, err error, details map[string]string) *BreachError {
	var copied map[string]string
	if len(details) > 0 {
		copied = make(map[string]string, len(details))
		for k, v := range details {
			copied[k] = v
		}
	}
	return &BreachError{
		Type:               t,
		Reason:             reason,
		Details:            copied,
		KillSwitchEngaged:  false,
		CircuitBreakerOpen: false,
		Err:                err,
	}
}

// CircuitBreaker defines cascading halt behaviour for repeated breaches.
type CircuitBreaker struct {
	Enabled   bool
	Threshold int
	Cooldown  time.Duration
}

// Limits defines risk parameters for a single strategy.
type Limits struct {
	MaxPositionSize     decimal.Decimal
	MaxNotionalValue    decimal.Decimal
	NotionalCurrency    string
	OrderThrottle       float64
	OrderBurst          int
	MaxConcurrentOrders int
	PriceBandPercent    float64
	AllowedOrderTypes   []schema.OrderType
	KillSwitchEnabled   bool
	MaxRiskBreaches     int
	CircuitBreaker      CircuitBreaker
}

type orderState struct {
	symbol   string
	side     schema.TradeSide
	quantity decimal.Decimal
	filled   decimal.Decimal
	limitPx  decimal.Decimal
}

// Manager enforces risk limits for trading strategies.
type Manager struct {
	limits Limits

	mu            sync.RWMutex
	limiter       *rate.Limiter
	symbolLimiter map[string]*rate.Limiter
	positions     map[string]decimal.Decimal
	notionals     map[string]decimal.Decimal
	marketPrices  map[string]decimal.Decimal
	orders        map[string]*orderState
	inflight      map[string]int
	allowedTypes  map[schema.OrderType]struct{}
	failureCount  int
	killSwitch    bool
	killReason    string
	cooldownUntil time.Time
}

// NewManager creates a new risk manager with the given limits.
func NewManager(limits Limits) *Manager {
	limitCopy := limits
	if len(limitCopy.AllowedOrderTypes) > 0 {
		limitCopy.AllowedOrderTypes = append([]schema.OrderType(nil), limits.AllowedOrderTypes...)
	}
	burst := limitCopy.OrderBurst
	if burst <= 0 {
		burst = 1
	}
	allowed := make(map[schema.OrderType]struct{}, len(limitCopy.AllowedOrderTypes))
	for _, ot := range limitCopy.AllowedOrderTypes {
		allowed[ot] = struct{}{}
	}
	return &Manager{
		limits:        limitCopy,
		mu:            sync.RWMutex{},
		limiter:       rate.NewLimiter(rate.Limit(limitCopy.OrderThrottle), burst),
		symbolLimiter: make(map[string]*rate.Limiter),
		positions:     make(map[string]decimal.Decimal),
		notionals:     make(map[string]decimal.Decimal),
		marketPrices:  make(map[string]decimal.Decimal),
		orders:        make(map[string]*orderState),
		inflight:      make(map[string]int),
		allowedTypes:  allowed,
		failureCount:  0,
		killSwitch:    false,
		killReason:    "",
		cooldownUntil: time.Time{},
	}
}

// UpdateLimits replaces the current limits and resets throttling state.
func (m *Manager) UpdateLimits(limits Limits) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limitCopy := limits
	if len(limitCopy.AllowedOrderTypes) > 0 {
		limitCopy.AllowedOrderTypes = append([]schema.OrderType(nil), limits.AllowedOrderTypes...)
	}
	m.limits = limitCopy
	burst := limitCopy.OrderBurst
	if burst <= 0 {
		burst = 1
	}
	m.limiter = rate.NewLimiter(rate.Limit(limitCopy.OrderThrottle), burst)
	m.symbolLimiter = make(map[string]*rate.Limiter)
	m.allowedTypes = make(map[schema.OrderType]struct{}, len(limitCopy.AllowedOrderTypes))
	for _, ot := range limitCopy.AllowedOrderTypes {
		m.allowedTypes[ot] = struct{}{}
	}
}

// CheckOrder evaluates an order request against the configured risk limits.
func (m *Manager) CheckOrder(ctx context.Context, req *schema.OrderRequest) error {
	if req == nil {
		return fmt.Errorf("nil order request")
	}

	if err := m.limiter.Wait(ctx); err != nil {
		return newBreachError(BreachTypeRateLimit, "order throttle limit exceeded", err, map[string]string{
			"scope": "global",
		})
	}

	symLimiter, err := m.symbolLimiterFor(req.Provider, req.Symbol)
	if err != nil {
		return err
	}
	if err := symLimiter.Wait(ctx); err != nil {
		return newBreachError(BreachTypeRateLimit, "symbol throttle limit exceeded", err, map[string]string{
			"scope":  "symbol",
			"symbol": req.Symbol,
		})
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureTradingActiveLocked(); err != nil {
		return err
	}

	if err := m.enforceOrderTypeLocked(req.OrderType); err != nil {
		m.recordRiskBreachLocked(err)
		return err
	}

	quantity, err := decimal.NewFromString(req.Quantity)
	if err != nil {
		breach := newBreachError(BreachTypeOrderValidation, "invalid order quantity", err, map[string]string{
			"quantity": req.Quantity,
		})
		m.recordRiskBreachLocked(breach)
		return breach
	}
	if quantity.LessThanOrEqual(decimal.Zero) {
		breach := newBreachError(BreachTypeOrderValidation, "invalid order quantity", nil, map[string]string{
			"quantity": req.Quantity,
		})
		m.recordRiskBreachLocked(breach)
		return breach
	}

	price, err := m.resolveOrderPriceLocked(req)
	if err != nil {
		m.recordRiskBreachLocked(err)
		return err
	}

	signedQty := signedQuantity(req.Side, quantity)
	currentPos := m.positions[req.Symbol]
	projected := currentPos.Add(signedQty)

	if m.limits.MaxPositionSize.GreaterThan(decimal.Zero) {
		if projected.Abs().GreaterThan(m.limits.MaxPositionSize) {
			breach := newBreachError(BreachTypePositionLimit, "projected position exceeds maximum", nil, map[string]string{
				"projected_position": projected.Abs().String(),
				"max_position":       m.limits.MaxPositionSize.String(),
			})
			m.recordRiskBreachLocked(breach)
			return breach
		}
	}

	notional := projected.Abs().Mul(price)
	if m.limits.MaxNotionalValue.GreaterThan(decimal.Zero) && notional.GreaterThan(m.limits.MaxNotionalValue) {
		breach := newBreachError(BreachTypeNotionalLimit, "projected notional exceeds maximum", nil, map[string]string{
			"projected_notional": notional.String(),
			"max_notional":       m.limits.MaxNotionalValue.String(),
		})
		m.recordRiskBreachLocked(breach)
		return breach
	}

	if err := m.enforceConcurrencyLocked(req.Symbol); err != nil {
		m.recordRiskBreachLocked(err)
		return err
	}

	m.orders[req.ClientOrderID] = &orderState{
		symbol:   req.Symbol,
		side:     req.Side,
		quantity: quantity,
		filled:   decimal.Zero,
		limitPx:  price,
	}
	return nil
}

// ObserveMarketPrice updates the cached reference price used for notional checks.
func (m *Manager) ObserveMarketPrice(symbol string, price decimal.Decimal) {
	if price.LessThanOrEqual(decimal.Zero) {
		return
	}
	m.mu.Lock()
	m.marketPrices[symbol] = price
	m.mu.Unlock()
}

// HandleExecution updates position state in response to execution reports.
func (m *Manager) HandleExecution(symbol string, payload schema.ExecReportPayload) {
	m.mu.Lock()
	defer m.mu.Unlock()

	order, ok := m.orders[payload.ClientOrderID]
	if !ok {
		return
	}

	cumFilled, err := decimal.NewFromString(payload.FilledQuantity)
	if err != nil {
		return
	}
	if cumFilled.LessThan(decimal.Zero) {
		cumFilled = decimal.Zero
	}
	delta := cumFilled.Sub(order.filled)
	if delta.GreaterThan(decimal.Zero) {
		price := order.limitPx
		if payload.AvgFillPrice != "" {
			if avg, convErr := decimal.NewFromString(payload.AvgFillPrice); convErr == nil && avg.GreaterThan(decimal.Zero) {
				price = avg
			}
		}
		m.applyFillLocked(symbol, payload.Side, delta, price)
		order.filled = cumFilled
	}

	if isTerminalState(payload.State) {
		m.releaseOrderLocked(order.symbol)
		delete(m.orders, payload.ClientOrderID)
	}

	if payload.State == schema.ExecReportStateREJECTED && m.limits.KillSwitchEnabled {
		reason := "order rejected"
		if payload.RejectReason != nil {
			reason = fmt.Sprintf("order rejected: %s", *payload.RejectReason)
		}
		m.recordRiskBreachLocked(errors.New(reason))
	}
}

// ResetKillSwitch clears the kill switch and circuit breaker state.
func (m *Manager) ResetKillSwitch() {
	m.mu.Lock()
	m.killSwitch = false
	m.killReason = ""
	m.failureCount = 0
	m.cooldownUntil = time.Time{}
	m.mu.Unlock()
}

// KillSwitchStatus returns the current halt flag and reason.
func (m *Manager) KillSwitchStatus() (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.killSwitch, m.killReason
}

// Limits returns a copy of the currently enforced limits.
func (m *Manager) Limits() Limits {
	m.mu.RLock()
	defer m.mu.RUnlock()
	limitCopy := m.limits
	if len(limitCopy.AllowedOrderTypes) > 0 {
		limitCopy.AllowedOrderTypes = append([]schema.OrderType(nil), limitCopy.AllowedOrderTypes...)
	}
	return limitCopy
}

func (m *Manager) symbolLimiterFor(provider, symbol string) (*rate.Limiter, error) {
	key := provider + "::" + symbol
	m.mu.Lock()
	defer m.mu.Unlock()
	lim, ok := m.symbolLimiter[key]
	if !ok {
		burst := m.limits.OrderBurst
		if burst <= 0 {
			burst = 1
		}
		lim = rate.NewLimiter(rate.Limit(m.limits.OrderThrottle), burst)
		m.symbolLimiter[key] = lim
	}
	return lim, nil
}

func (m *Manager) ensureTradingActiveLocked() error {
	if m.killSwitch {
		if m.limits.CircuitBreaker.Enabled && !m.cooldownUntil.IsZero() && time.Now().After(m.cooldownUntil) {
			m.killSwitch = false
			m.killReason = ""
		}
	}
	if m.killSwitch {
		if m.limits.CircuitBreaker.Enabled && time.Now().Before(m.cooldownUntil) {
			return ErrCircuitBreakerOpen
		}
		return ErrKillSwitchEngaged
	}
	return nil
}

func (m *Manager) enforceOrderTypeLocked(orderType schema.OrderType) error {
	if len(m.allowedTypes) == 0 {
		return nil
	}
	if _, ok := m.allowedTypes[orderType]; ok {
		return nil
	}
	return newBreachError(BreachTypeOrderType, fmt.Sprintf("order type %s not allowed", orderType), nil, map[string]string{
		"order_type": string(orderType),
	})
}

func (m *Manager) resolveOrderPriceLocked(req *schema.OrderRequest) (decimal.Decimal, error) {
	if req.OrderType == schema.OrderTypeLimit {
		if req.Price == nil {
			return decimal.Zero, newBreachError(BreachTypeOrderValidation, "limit order requires price", nil, nil)
		}
		price, err := decimal.NewFromString(*req.Price)
		if err != nil {
			return decimal.Zero, newBreachError(BreachTypeOrderValidation, "invalid limit price", err, map[string]string{"price": *req.Price})
		}
		return m.validatePriceBandLocked(req.Symbol, price)
	}

	if req.Price != nil {
		price, err := decimal.NewFromString(*req.Price)
		if err != nil {
			return decimal.Zero, newBreachError(BreachTypeOrderValidation, "invalid price", err, map[string]string{"price": *req.Price})
		}
		return m.validatePriceBandLocked(req.Symbol, price)
	}

	market, ok := m.marketPrices[req.Symbol]
	if !ok || market.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, newBreachError(BreachTypeOrderValidation, "market price unavailable", nil, map[string]string{"symbol": req.Symbol})
	}
	return m.validatePriceBandLocked(req.Symbol, market)
}

func (m *Manager) validatePriceBandLocked(symbol string, price decimal.Decimal) (decimal.Decimal, error) {
	if m.limits.PriceBandPercent <= 0 {
		return price, nil
	}
	reference, ok := m.marketPrices[symbol]
	if !ok || reference.LessThanOrEqual(decimal.Zero) {
		m.marketPrices[symbol] = price
		return price, nil
	}
	upper := reference.Mul(decimal.NewFromFloat(1 + m.limits.PriceBandPercent/100))
	lower := reference.Mul(decimal.NewFromFloat(1 - m.limits.PriceBandPercent/100))
	if price.GreaterThan(upper) || price.LessThan(lower) {
		return decimal.Zero, newBreachError(BreachTypePriceBand, "price outside allowable band", nil, map[string]string{
			"price":        price.String(),
			"band_lower":   lower.String(),
			"band_upper":   upper.String(),
			"symbol":       symbol,
			"band_percent": fmt.Sprintf("%.4f", m.limits.PriceBandPercent),
		})
	}
	return price, nil
}

func (m *Manager) enforceConcurrencyLocked(symbol string) error {
	if m.limits.MaxConcurrentOrders <= 0 {
		m.inflight[symbol]++
		return nil
	}
	current := m.inflight[symbol]
	if current >= m.limits.MaxConcurrentOrders {
		return newBreachError(BreachTypeConcurrency, "max concurrent orders exceeded", nil, map[string]string{
			"current": fmt.Sprintf("%d", current),
			"max":     fmt.Sprintf("%d", m.limits.MaxConcurrentOrders),
			"symbol":  symbol,
		})
	}
	m.inflight[symbol] = current + 1
	return nil
}

func (m *Manager) releaseOrderLocked(symbol string) {
	if current := m.inflight[symbol]; current > 0 {
		m.inflight[symbol] = current - 1
	}
}

func (m *Manager) applyFillLocked(symbol string, side schema.TradeSide, fillQty, fillPrice decimal.Decimal) {
	if fillQty.LessThanOrEqual(decimal.Zero) {
		return
	}
	change := signedQuantity(side, fillQty)
	position := m.positions[symbol]
	position = position.Add(change)
	m.positions[symbol] = position
	notional := position.Abs().Mul(fillPrice)
	m.notionals[symbol] = notional
	if m.limits.MaxPositionSize.GreaterThan(decimal.Zero) && position.Abs().GreaterThan(m.limits.MaxPositionSize) {
		breach := newBreachError(BreachTypePositionLimit, "post-fill position exceeds maximum", nil, map[string]string{
			"position":     position.Abs().String(),
			"max_position": m.limits.MaxPositionSize.String(),
			"symbol":       symbol,
		})
		m.recordRiskBreachLocked(breach)
		m.engageKillSwitchLocked(breach.Error())
	}
	if m.limits.MaxNotionalValue.GreaterThan(decimal.Zero) && notional.GreaterThan(m.limits.MaxNotionalValue) {
		breach := newBreachError(BreachTypeNotionalLimit, "post-fill notional exceeds maximum", nil, map[string]string{
			"notional":     notional.String(),
			"max_notional": m.limits.MaxNotionalValue.String(),
			"symbol":       symbol,
		})
		m.recordRiskBreachLocked(breach)
		m.engageKillSwitchLocked(breach.Error())
	}
}

func (m *Manager) recordRiskBreachLocked(err error) {
	if err == nil {
		return
	}
	m.failureCount++
	var breach *BreachError
	if errors.As(err, &breach) {
		if breach != nil && len(breach.Details) == 0 {
			breach.Details = make(map[string]string)
		}
	}
	reason := err.Error()
	if m.limits.KillSwitchEnabled && m.limits.MaxRiskBreaches > 0 && m.failureCount >= m.limits.MaxRiskBreaches {
		m.engageKillSwitchLocked(fmt.Sprintf("risk breach threshold reached: %s", reason))
	}
	if m.limits.CircuitBreaker.Enabled && m.limits.CircuitBreaker.Threshold > 0 && m.failureCount >= m.limits.CircuitBreaker.Threshold {
		m.engageKillSwitchLocked(fmt.Sprintf("circuit breaker threshold reached: %s", reason))
	}
	if breach != nil {
		breach.KillSwitchEngaged = m.killSwitch
		if m.limits.CircuitBreaker.Enabled && !m.cooldownUntil.IsZero() {
			breach.CircuitBreakerOpen = m.killSwitch && time.Now().Before(m.cooldownUntil)
		}
	}
}

func (m *Manager) engageKillSwitchLocked(reason string) {
	if !m.limits.KillSwitchEnabled {
		return
	}
	m.killSwitch = true
	m.killReason = reason
	if m.limits.CircuitBreaker.Enabled {
		cooldown := m.limits.CircuitBreaker.Cooldown
		if cooldown > 0 {
			m.cooldownUntil = time.Now().Add(cooldown)
		}
	}
}

func signedQuantity(side schema.TradeSide, qty decimal.Decimal) decimal.Decimal {
	switch side {
	case schema.TradeSideSell:
		return qty.Neg()
	case schema.TradeSideBuy:
		return qty
	default:
		return qty
	}
}

func isTerminalState(state schema.ExecReportState) bool {
	switch state {
	case schema.ExecReportStateFILLED,
		schema.ExecReportStateCANCELLED,
		schema.ExecReportStateREJECTED,
		schema.ExecReportStateEXPIRED:
		return true
	case schema.ExecReportStateACK,
		schema.ExecReportStatePARTIAL:
		return false
	default:
		return false
	}
}
