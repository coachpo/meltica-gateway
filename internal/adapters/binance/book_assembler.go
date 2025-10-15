// Package binance provides order book assembly functionality with checksum verification.
package binance

import (
	"fmt"
	"hash/crc32"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/coachpo/meltica/internal/schema"
)

const (
	bookDepthChecksum = 10
)

var (
	// ErrBookNotInitialized is returned when updates arrive before the first snapshot.
	ErrBookNotInitialized = fmt.Errorf("binance book assembler: snapshot required before updates")
	// ErrBookStaleUpdate is returned when an update with an older sequence is applied.
	ErrBookStaleUpdate = fmt.Errorf("binance book assembler: stale update")
)

// BookAssembler keeps a canonical representation of the Binance order book,
// validates checksum integrity, and produces normalised payloads for downstream consumers.
type BookAssembler struct {
	mu       sync.Mutex
	bids     map[string]string
	asks     map[string]string
	seq      uint64
	ready    bool
	checksum uint32
}

// NewBookAssembler constructs an empty order book assembler.
func NewBookAssembler() *BookAssembler {
	return &BookAssembler{
		bids: make(map[string]string),
		asks: make(map[string]string),
	}
}

// ApplySnapshot ingests a full depth snapshot, resets internal state, and verifies checksum integrity.
func (a *BookAssembler) ApplySnapshot(snapshot schema.BookSnapshotPayload, seq uint64) (schema.BookSnapshotPayload, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.resetBooks()
	for _, level := range snapshot.Bids {
		a.upsertLevel(a.bids, level)
	}
	for _, level := range snapshot.Asks {
		a.upsertLevel(a.asks, level)
	}

	checksum := a.computeChecksumLocked()
	if snapshot.Checksum != "" {
		if err := verifyChecksumValue(snapshot.Checksum, checksum); err != nil {
			return schema.BookSnapshotPayload{}, err
		}
	}

	a.seq = seq
	a.ready = true
	a.checksum = checksum

	sanitised := schema.BookSnapshotPayload{
		Bids:       a.topLevelsLocked(a.bids, true),
		Asks:       a.topLevelsLocked(a.asks, false),
		Checksum:   fmt.Sprintf("%d", checksum),
		LastUpdate: snapshot.LastUpdate,
	}
	return sanitised, nil
}

// ApplyUpdate applies a delta update to the maintained order book and validates checksum integrity.
func (a *BookAssembler) ApplyUpdate(update schema.BookUpdatePayload, seq uint64) (schema.BookUpdatePayload, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.ready {
		return schema.BookUpdatePayload{}, ErrBookNotInitialized
	}
	if seq <= a.seq {
		return schema.BookUpdatePayload{}, ErrBookStaleUpdate
	}

	for _, level := range update.Bids {
		a.upsertLevel(a.bids, level)
	}
	for _, level := range update.Asks {
		a.upsertLevel(a.asks, level)
	}

	checksum := a.computeChecksumLocked()
	if update.Checksum != "" {
		if err := verifyChecksumValue(update.Checksum, checksum); err != nil {
			return schema.BookUpdatePayload{}, err
		}
	}

	a.seq = seq
	a.checksum = checksum

	payload := schema.BookUpdatePayload{
		UpdateType: update.UpdateType,
		Bids:       a.topLevelsLocked(a.bids, true),
		Asks:       a.topLevelsLocked(a.asks, false),
		Checksum:   fmt.Sprintf("%d", checksum),
	}
	return payload, nil
}

func (a *BookAssembler) upsertLevel(book map[string]string, level schema.PriceLevel) {
	price := strings.TrimSpace(level.Price)
	qty := strings.TrimSpace(level.Quantity)
	if price == "" {
		return
	}
	if qty == "" {
		delete(book, price)
		return
	}
	value, err := strconv.ParseFloat(qty, 64)
	if err != nil {
		book[price] = qty
		return
	}
	if value == 0 {
		delete(book, price)
		return
	}
	book[price] = qty
}

func (a *BookAssembler) resetBooks() {
	a.bids = make(map[string]string, len(a.bids))
	a.asks = make(map[string]string, len(a.asks))
}

func (a *BookAssembler) computeChecksumLocked() uint32 {
	bids := a.topLevelsLocked(a.bids, true)
	asks := a.topLevelsLocked(a.asks, false)
	parts := make([]string, 0, (len(bids)+len(asks))*2)

	for _, level := range bids {
		parts = append(parts, normaliseDecimal(level.Price))
		parts = append(parts, normaliseDecimal(level.Quantity))
	}
	for _, level := range asks {
		parts = append(parts, normaliseDecimal(level.Price))
		parts = append(parts, normaliseDecimal(level.Quantity))
	}

	data := strings.Join(parts, ":")
	return crc32.ChecksumIEEE([]byte(data))
}

func (a *BookAssembler) topLevelsLocked(book map[string]string, desc bool) []schema.PriceLevel {
	if len(book) == 0 {
		return nil
	}
	type priceLevel struct {
		price float64
		raw   schema.PriceLevel
	}
	levels := make([]priceLevel, 0, len(book))
	for price, qty := range book {
		fv, err := strconv.ParseFloat(price, 64)
		if err != nil {
			continue
		}
		levels = append(levels, priceLevel{
			price: fv,
			raw: schema.PriceLevel{
				Price:    price,
				Quantity: qty,
			},
		})
	}
	sort.Slice(levels, func(i, j int) bool {
		if desc {
			return levels[i].price > levels[j].price
		}
		return levels[i].price < levels[j].price
	})

	limit := bookDepthChecksum
	if len(levels) < limit {
		limit = len(levels)
	}
	out := make([]schema.PriceLevel, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, levels[i].raw)
	}
	return out
}

func normaliseDecimal(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	value = strings.TrimLeft(value, "+")
	if !strings.Contains(value, ".") {
		return value
	}
	value = strings.TrimRight(value, "0")
	if strings.HasSuffix(value, ".") {
		value = strings.TrimSuffix(value, ".")
	}
	if value == "" || value == "-" {
		return "0"
	}
	return value
}

func verifyChecksumValue(source string, expected uint32) error {
	provided, err := strconv.ParseUint(strings.TrimSpace(source), 10, 32)
	if err != nil {
		return fmt.Errorf("binance book assembler: parse checksum %q: %w", source, err)
	}
	if uint32(provided) != expected {
		return fmt.Errorf("binance book assembler: checksum mismatch (expected %d, got %d)", expected, provided)
	}
	return nil
}
