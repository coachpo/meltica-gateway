package shared

import (
	"math"
	"sync"

	"github.com/coachpo/meltica/internal/schema"
)

// BalanceState represents the total and available balance tracked for a currency.
type BalanceState struct {
	Total     float64
	Available float64
}

// BalanceManager coordinates BalanceState updates in a concurrency-safe manner.
// Adapters can reuse it to model fills and generate deterministic balance events.
type BalanceManager struct {
	mu           sync.Mutex
	balances     map[string]BalanceState
	defaultState BalanceState
}

// NewBalanceManager constructs a manager with the supplied default state for newly
// observed currencies.
func NewBalanceManager(defaultTotal, defaultAvailable float64) *BalanceManager {
	state := normalizeBalanceState(BalanceState{
		Total:     defaultTotal,
		Available: defaultAvailable,
	})
	return &BalanceManager{
		mu:           sync.Mutex{},
		balances:     make(map[string]BalanceState),
		defaultState: state,
	}
}

// EnsureInstrument seeds balances for the base and quote currencies defined on the instrument.
func (m *BalanceManager) EnsureInstrument(inst schema.Instrument) {
	base := schema.NormalizeCurrencyCode(inst.BaseCurrency)
	quote := schema.NormalizeCurrencyCode(inst.QuoteCurrency)

	m.mu.Lock()
	if base != "" {
		m.ensureBalanceLocked(base)
	}
	if quote != "" && quote != base {
		m.ensureBalanceLocked(quote)
	}
	m.mu.Unlock()
}

// AdjustInstrumentFill mutates balances to reflect a trade fill and returns the currencies that changed.
func (m *BalanceManager) AdjustInstrumentFill(inst schema.Instrument, side schema.TradeSide, quantity, price float64) []string {
	if quantity <= 0 {
		return nil
	}

	base := schema.NormalizeCurrencyCode(inst.BaseCurrency)
	quote := schema.NormalizeCurrencyCode(inst.QuoteCurrency)
	notional := clampNonNegative(quantity * price)

	updated := make([]string, 0, 2)

	m.mu.Lock()
	if base != "" {
		state := m.ensureBalanceLocked(base)
		switch side {
		case schema.TradeSideBuy:
			state.Total += quantity
			state.Available += quantity
		case schema.TradeSideSell:
			state.Total = clampNonNegative(state.Total - quantity)
			state.Available = clampNonNegative(state.Available - quantity)
		}
		state = normalizeBalanceState(state)
		m.balances[base] = state
		updated = append(updated, base)
	}

	if quote != "" {
		state := m.ensureBalanceLocked(quote)
		switch side {
		case schema.TradeSideBuy:
			state.Total = clampNonNegative(state.Total - notional)
			state.Available = clampNonNegative(state.Available - notional)
		case schema.TradeSideSell:
			state.Total += notional
			state.Available += notional
		}
		state = normalizeBalanceState(state)
		m.balances[quote] = state
		if quote != base {
			updated = append(updated, quote)
		}
	}
	m.mu.Unlock()

	return dedupeCurrencies(updated)
}

// Update applies a mutation function to the named currency and returns the resulting state.
func (m *BalanceManager) Update(currency string, fn func(BalanceState) BalanceState) BalanceState {
	normalized := schema.NormalizeCurrencyCode(currency)
	if normalized == "" {
		return BalanceState{Total: 0, Available: 0}
	}

	m.mu.Lock()
	state := m.ensureBalanceLocked(normalized)
	if fn != nil {
		state = fn(state)
	}
	state = normalizeBalanceState(state)
	m.balances[normalized] = state
	m.mu.Unlock()

	return state
}

// Snapshot returns a copy of the tracked state for the given currency.
func (m *BalanceManager) Snapshot(currency string) (BalanceState, bool) {
	normalized := schema.NormalizeCurrencyCode(currency)
	if normalized == "" {
		return BalanceState{Total: 0, Available: 0}, false
	}

	m.mu.Lock()
	state, ok := m.balances[normalized]
	if ok {
		state = normalizeBalanceState(state)
		m.balances[normalized] = state
	}
	m.mu.Unlock()

	return state, ok
}

// Range iterates over the tracked balances, invoking fn with a consistent snapshot.
func (m *BalanceManager) Range(fn func(currency string, state BalanceState)) {
	if fn == nil {
		return
	}

	snapshot := m.snapshotAll()
	for currency, state := range snapshot {
		fn(currency, state)
	}
}

func (m *BalanceManager) snapshotAll() map[string]BalanceState {
	m.mu.Lock()
	out := make(map[string]BalanceState, len(m.balances))
	for currency, state := range m.balances {
		out[currency] = normalizeBalanceState(state)
	}
	m.mu.Unlock()
	return out
}

func (m *BalanceManager) ensureBalanceLocked(currency string) BalanceState {
	if state, ok := m.balances[currency]; ok {
		state = normalizeBalanceState(state)
		m.balances[currency] = state
		return state
	}
	m.balances[currency] = m.defaultState
	return m.defaultState
}

func normalizeBalanceState(state BalanceState) BalanceState {
	if math.IsNaN(state.Total) {
		state.Total = 0
	}
	if math.IsNaN(state.Available) {
		state.Available = 0
	}
	state.Total = clampNonNegative(state.Total)
	state.Available = clampNonNegative(state.Available)
	if state.Available > state.Total {
		state.Available = state.Total
	}
	return state
}

func clampNonNegative(value float64) float64 {
	if value < 0 {
		return 0
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func dedupeCurrencies(list []string) []string {
	if len(list) <= 1 {
		return list
	}
	seen := make(map[string]struct{}, len(list))
	out := make([]string, 0, len(list))
	for _, entry := range list {
		entry = schema.NormalizeCurrencyCode(entry)
		if entry == "" {
			continue
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		out = append(out, entry)
	}
	return out
}

// MergeBalances copies BalanceState entries from src into dst without overwriting existing currencies.
func MergeBalances(dst map[string]BalanceState, src map[string]BalanceState) {
	if len(src) == 0 {
		return
	}
	for currency, state := range src {
		currency = schema.NormalizeCurrencyCode(currency)
		if currency == "" {
			continue
		}
		if _, ok := dst[currency]; ok {
			continue
		}
		dst[currency] = normalizeBalanceState(state)
	}
}
