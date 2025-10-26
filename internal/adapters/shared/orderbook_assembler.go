package shared

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/schema"
)

// ErrSnapshotIDRequired indicates that a snapshot operation was attempted without a sequence identifier.
var ErrSnapshotIDRequired = errors.New("orderbook assembler: snapshot sequence id required")

// DiffLevel represents a single price level update from an order book diff stream.
type DiffLevel struct {
	Price    string
	Quantity string
}

// OrderBookDiff wraps an order book diff update along with its sequencing metadata.
type OrderBookDiff struct {
	SequenceID uint64
	Bids       []DiffLevel
	Asks       []DiffLevel
	Timestamp  time.Time
}

// OrderBookAssembler maintains an in-memory order book by combining REST snapshots with streaming diffs.
type OrderBookAssembler struct {
	mu          sync.Mutex
	depth       int
	initialized bool
	bids        map[string]decimal.Decimal
	asks        map[string]decimal.Decimal
	pending     []OrderBookDiff
	lastSeq     uint64
	lastUpdate  time.Time
}

// NewOrderBookAssembler constructs a new assembler limited to depth price levels (<=0 keeps full depth).
func NewOrderBookAssembler(depth int) *OrderBookAssembler {
	return &OrderBookAssembler{
		depth: depth,
		bids:  make(map[string]decimal.Decimal),
		asks:  make(map[string]decimal.Decimal),
	}
}

// HasSnapshot reports whether the assembler has applied an initial REST snapshot.
func (a *OrderBookAssembler) HasSnapshot() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.initialized
}

// ApplySnapshot resets the book state using a REST snapshot and applies any queued diffs.
func (a *OrderBookAssembler) ApplySnapshot(seq uint64, snapshot schema.BookSnapshotPayload) (schema.BookSnapshotPayload, error) {
	if seq == 0 {
		if snapshot.FinalUpdateID != 0 {
			seq = snapshot.FinalUpdateID
		} else if snapshot.FirstUpdateID != 0 {
			seq = snapshot.FirstUpdateID
		}
	}
	if seq == 0 {
		return schema.BookSnapshotPayload{}, ErrSnapshotIDRequired
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.resetLocked()
	if err := a.replaceSideLocked(a.bids, snapshot.Bids); err != nil {
		return schema.BookSnapshotPayload{}, err
	}
	if err := a.replaceSideLocked(a.asks, snapshot.Asks); err != nil {
		return schema.BookSnapshotPayload{}, err
	}
	a.initialized = true
	a.lastSeq = seq
	if !snapshot.LastUpdate.IsZero() {
		a.lastUpdate = snapshot.LastUpdate
	} else {
		a.lastUpdate = time.Now()
	}

	result := a.buildSnapshotLocked(seq, seq)
	if len(a.pending) == 0 {
		return result, nil
	}

	sort.SliceStable(a.pending, func(i, j int) bool {
		return a.pending[i].SequenceID < a.pending[j].SequenceID
	})

	for _, diff := range a.pending {
		if diff.SequenceID <= a.lastSeq {
			continue
		}
		updated, err := a.applyDiffLocked(diff)
		if err != nil {
			return schema.BookSnapshotPayload{}, err
		}
		result = updated
	}
	a.pending = a.pending[:0]
	return result, nil
}

// ApplyDiff applies a single diff update. It returns the resulting full snapshot and whether the diff was applied.
func (a *OrderBookAssembler) ApplyDiff(diff OrderBookDiff) (schema.BookSnapshotPayload, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.initialized {
		a.pending = append(a.pending, diff)
		return schema.BookSnapshotPayload{}, false, nil
	}
	if diff.SequenceID == 0 || diff.SequenceID <= a.lastSeq {
		return schema.BookSnapshotPayload{}, false, nil
	}

	payload, err := a.applyDiffLocked(diff)
	if err != nil {
		return schema.BookSnapshotPayload{}, false, err
	}
	return payload, true, nil
}

func (a *OrderBookAssembler) applyDiffLocked(diff OrderBookDiff) (schema.BookSnapshotPayload, error) {
	if err := a.updateSideLocked(a.bids, diff.Bids); err != nil {
		return schema.BookSnapshotPayload{}, err
	}
	if err := a.updateSideLocked(a.asks, diff.Asks); err != nil {
		return schema.BookSnapshotPayload{}, err
	}
	a.lastSeq = diff.SequenceID
	if !diff.Timestamp.IsZero() {
		a.lastUpdate = diff.Timestamp
	} else {
		a.lastUpdate = time.Now()
	}
	return a.buildSnapshotLocked(diff.SequenceID, diff.SequenceID), nil
}

func (a *OrderBookAssembler) resetLocked() {
	for price := range a.bids {
		delete(a.bids, price)
	}
	for price := range a.asks {
		delete(a.asks, price)
	}
	a.pending = a.pending[:0]
	a.initialized = false
}

func (a *OrderBookAssembler) replaceSideLocked(target map[string]decimal.Decimal, levels []schema.PriceLevel) error {
	for price := range target {
		delete(target, price)
	}
	if len(levels) == 0 {
		return nil
	}
	for _, level := range levels {
		qty, err := decimal.NewFromString(strings.TrimSpace(level.Quantity))
		if err != nil {
			return err
		}
		if qty.Sign() <= 0 {
			continue
		}
		priceKey := strings.TrimSpace(level.Price)
		target[priceKey] = qty
	}
	return nil
}

func (a *OrderBookAssembler) updateSideLocked(target map[string]decimal.Decimal, updates []DiffLevel) error {
	for _, update := range updates {
		priceKey := strings.TrimSpace(update.Price)
		if priceKey == "" {
			continue
		}
		qtyStr := strings.TrimSpace(update.Quantity)
		if qtyStr == "" {
			delete(target, priceKey)
			continue
		}
		qty, err := decimal.NewFromString(qtyStr)
		if err != nil {
			return err
		}
		if qty.Sign() <= 0 {
			delete(target, priceKey)
			continue
		}
		target[priceKey] = qty
	}
	return nil
}

func (a *OrderBookAssembler) buildSnapshotLocked(firstSeq, finalSeq uint64) schema.BookSnapshotPayload {
	return schema.BookSnapshotPayload{
		Bids:          a.buildSideSnapshotLocked(a.bids, true),
		Asks:          a.buildSideSnapshotLocked(a.asks, false),
		LastUpdate:    a.lastUpdate,
		FirstUpdateID: firstSeq,
		FinalUpdateID: finalSeq,
	}
}

func (a *OrderBookAssembler) buildSideSnapshotLocked(source map[string]decimal.Decimal, isBid bool) []schema.PriceLevel {
	if len(source) == 0 {
		return nil
	}
	levels := make([]struct {
		price decimal.Decimal
		qty   decimal.Decimal
		key   string
	}, 0, len(source))
	for key, qty := range source {
		price, err := decimal.NewFromString(key)
		if err != nil {
			continue
		}
		if qty.Sign() <= 0 {
			continue
		}
		levels = append(levels, struct {
			price decimal.Decimal
			qty   decimal.Decimal
			key   string
		}{price: price, qty: qty, key: key})
	}
	sort.Slice(levels, func(i, j int) bool {
		if isBid {
			cmp := levels[i].price.Cmp(levels[j].price)
			if cmp == 0 {
				return levels[i].key < levels[j].key
			}
			return cmp > 0
		}
		cmp := levels[i].price.Cmp(levels[j].price)
		if cmp == 0 {
			return levels[i].key < levels[j].key
		}
		return cmp < 0
	})

	limit := len(levels)
	if a.depth > 0 && limit > a.depth {
		limit = a.depth
	}
	out := make([]schema.PriceLevel, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, schema.PriceLevel{
			Price:    levels[i].price.String(),
			Quantity: levels[i].qty.String(),
		})
	}
	return out
}
