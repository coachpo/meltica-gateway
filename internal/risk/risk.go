package risk

import (
	"context"
	"fmt"
	"sync"

	"github.com/shopspring/decimal"
	"golang.org/x/time/rate"

	"github.com/coachpo/meltica/internal/schema"
)

// Position describes the size of a single instrument held by a strategy.
type Position struct {
	Instrument string
	Quantity   decimal.Decimal
	Side       schema.TradeSide
}

// Notional describes the total value of a position in a given currency.
type Notional struct {
	Currency string
	Value    decimal.Decimal
}

// Limits defines risk parameters for a single strategy.
type Limits struct {
	// MaxPositionSize is the maximum quantity of a single instrument
	// that a strategy can hold.
	MaxPositionSize decimal.Decimal `yaml:"maxPositionSize"`

	// MaxNotionalValue is the maximum total value of a single position,
	// expressed in a given currency (e.g., USD).
	MaxNotionalValue decimal.Decimal `yaml:"maxNotionalValue"`

	// NotionalCurrency is the currency for notional value calculations.
	NotionalCurrency string `yaml:"notionalCurrency"`

	// OrderThrottle is the maximum rate of orders per second.
	OrderThrottle float64 `yaml:"orderThrottle"`
}

// Manager enforces risk limits for trading strategies.
type Manager struct {
	limits    Limits
	limiter   *rate.Limiter
	mu        sync.RWMutex
	positions map[string]Position
	notionals map[string]Notional
}

// NewManager creates a new risk manager with the given limits.
func NewManager(limits Limits) *Manager {
	return &Manager{
		limits:    limits,
		limiter:   rate.NewLimiter(rate.Limit(limits.OrderThrottle), 1),
		positions: make(map[string]Position),
		notionals: make(map[string]Notional),
	}
}

// CheckOrder evaluates an order request against the configured risk limits.
func (m *Manager) CheckOrder(ctx context.Context, req *schema.OrderRequest) error {
	if err := m.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("order throttle limit exceeded")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// This is a simplified check. A real implementation would need to
	// consider the order's impact on the current position and notional value.
	// For now, we'll just check the order size against the max position size.
	quantity, err := decimal.NewFromString(req.Quantity)
	if err != nil {
		return fmt.Errorf("invalid order quantity: %w", err)
	}

	if quantity.GreaterThan(m.limits.MaxPositionSize) {
		return fmt.Errorf("order quantity %s exceeds max position size %s",
			quantity, m.limits.MaxPositionSize)
	}

	return nil
}
