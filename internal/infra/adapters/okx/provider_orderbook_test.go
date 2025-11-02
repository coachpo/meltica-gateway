package okx

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/adapters/shared"
	"github.com/coachpo/meltica/internal/infra/pool"
)

func newTestOKXProvider(t *testing.T) *Provider {
	t.Helper()
	pm := pool.NewPoolManager()
	t.Cleanup(func() {
		_ = pm.Shutdown(context.Background())
	})
	if err := pm.RegisterPool("Event", 16, 0, func() any { return &schema.Event{} }); err != nil {
		t.Fatalf("register pool: %v", err)
	}
	prov := NewProvider(Options{Pools: pm})
	prov.ctx = context.Background()
	return prov
}

func TestHandleBooksPublishesSnapshotsAndDiffs(t *testing.T) {
	prov := newTestOKXProvider(t)

	symbol := "BTC-USDT"
	instID := "BTC-USDT"
	prov.instrumentsMu.Lock()
	prov.metas[strings.ToUpper(symbol)] = symbolMeta{canonical: symbol, instID: instID}
	prov.instIDToSym[strings.ToUpper(instID)] = symbol
	prov.instrumentsMu.Unlock()

	prov.bookMu.Lock()
	prov.bookHandles[symbol] = &bookHandle{
		assembler: shared.NewOrderBookAssembler(0),
		mu:        sync.Mutex{},
		seeded:    false,
		lastSeq:   0,
		lastCRC:   0,
		resyncing: false,
	}
	prov.bookMu.Unlock()

	baseSnapshot := schema.BookSnapshotPayload{
		Bids: []schema.PriceLevel{{Price: "100", Quantity: "1"}, {Price: "99", Quantity: "2"}},
		Asks: []schema.PriceLevel{{Price: "101", Quantity: "1"}, {Price: "102", Quantity: "3"}},
	}
	checksum := computeOKXChecksum(baseSnapshot)
	snapshotTS := time.UnixMilli(1700000000000).UTC()

	snapshotEvent := bookEvent{
		InstID:    instID,
		Asks:      [][]string{{"101", "1"}, {"102", "3"}},
		Bids:      [][]string{{"100", "1"}, {"99", "2"}},
		SeqID:     json.Number(strconv.FormatUint(123, 10)),
		PrevSeqID: json.Number("-1"),
		Checksum:  checksum,
		Timestamp: strconv.FormatInt(snapshotTS.UnixMilli(), 10),
	}

	rawSnapshot, err := json.Marshal(snapshotEvent)
	if err != nil {
		t.Fatalf("marshal snapshot event: %v", err)
	}

	if err := prov.handleBooks(wsEnvelope{
		Arg:    wsArgument{Channel: "books", InstID: instID},
		Action: "snapshot",
		Data:   []json.RawMessage{rawSnapshot},
	}); err != nil {
		t.Fatalf("handle snapshot: %v", err)
	}

	select {
	case evt := <-prov.events:
		if evt.Type != schema.EventTypeBookSnapshot {
			t.Fatalf("unexpected event type: %s", evt.Type)
		}
		payload, ok := evt.Payload.(schema.BookSnapshotPayload)
		if !ok {
			t.Fatalf("expected BookSnapshotPayload, got %T", evt.Payload)
		}
		if payload.Checksum != strconv.FormatInt(int64(checksum), 10) {
			t.Fatalf("unexpected checksum: %s", payload.Checksum)
		}
		if len(payload.Bids) != 2 || payload.Bids[0].Price != "100" {
			t.Fatalf("unexpected bids payload: %+v", payload.Bids)
		}
		if !payload.LastUpdate.Equal(snapshotTS) {
			t.Fatalf("unexpected last update: %s", payload.LastUpdate)
		}
		prov.pools.ReturnEventInst(evt)
	case <-time.After(2 * time.Second):
		t.Fatal("expected snapshot event")
	}

	updatedSnapshot := schema.BookSnapshotPayload{
		Bids: []schema.PriceLevel{{Price: "100", Quantity: "1"}},
		Asks: []schema.PriceLevel{{Price: "101", Quantity: "1.5"}, {Price: "102", Quantity: "3"}},
	}
	diffChecksum := computeOKXChecksum(updatedSnapshot)
	diffTS := snapshotTS.Add(2 * time.Second)

	diffEvent := bookEvent{
		InstID:    instID,
		Asks:      [][]string{{"101", "1.5"}},
		Bids:      [][]string{{"99", "0"}},
		SeqID:     json.Number(strconv.FormatUint(124, 10)),
		PrevSeqID: json.Number(strconv.FormatUint(123, 10)),
		Checksum:  diffChecksum,
		Timestamp: strconv.FormatInt(diffTS.UnixMilli(), 10),
	}

	rawDiff, err := json.Marshal(diffEvent)
	if err != nil {
		t.Fatalf("marshal diff event: %v", err)
	}

	if err := prov.handleBooks(wsEnvelope{
		Arg:    wsArgument{Channel: "books", InstID: instID},
		Action: "update",
		Data:   []json.RawMessage{rawDiff},
	}); err != nil {
		t.Fatalf("handle diff: %v", err)
	}

	select {
	case evt := <-prov.events:
		if evt.Type != schema.EventTypeBookSnapshot {
			t.Fatalf("unexpected event type: %s", evt.Type)
		}
		payload, ok := evt.Payload.(schema.BookSnapshotPayload)
		if !ok {
			t.Fatalf("expected BookSnapshotPayload, got %T", evt.Payload)
		}
		if payload.Checksum != strconv.FormatInt(int64(diffChecksum), 10) {
			t.Fatalf("unexpected diff checksum: %s", payload.Checksum)
		}
		if len(payload.Bids) != 1 || payload.Bids[0].Price != "100" {
			t.Fatalf("unexpected bids after diff: %+v", payload.Bids)
		}
		if len(payload.Asks) != 2 || payload.Asks[0].Quantity != "1.5" {
			t.Fatalf("unexpected asks after diff: %+v", payload.Asks)
		}
		if payload.FirstUpdateID != 124 || payload.FinalUpdateID != 124 {
			t.Fatalf("unexpected sequence window: first=%d final=%d", payload.FirstUpdateID, payload.FinalUpdateID)
		}
		if !payload.LastUpdate.Equal(diffTS) {
			t.Fatalf("unexpected diff timestamp: %s", payload.LastUpdate)
		}
		prov.pools.ReturnEventInst(evt)
	case <-time.After(2 * time.Second):
		t.Fatal("expected diff event")
	}
}

func TestHandleBooksUsesEnvelopeInstIDWhenMissing(t *testing.T) {
	prov := newTestOKXProvider(t)

	symbol := "BTC-USDT"
	instID := "BTC-USDT"
	prov.instrumentsMu.Lock()
	prov.metas[strings.ToUpper(symbol)] = symbolMeta{canonical: symbol, instID: instID}
	prov.instIDToSym[strings.ToUpper(instID)] = symbol
	prov.instrumentsMu.Unlock()

	prov.bookMu.Lock()
	prov.bookHandles[symbol] = &bookHandle{
		assembler: shared.NewOrderBookAssembler(0),
		mu:        sync.Mutex{},
		seeded:    false,
		lastSeq:   0,
		lastCRC:   0,
		resyncing: false,
	}
	prov.bookMu.Unlock()

	checksum := computeOKXChecksum(schema.BookSnapshotPayload{
		Bids: []schema.PriceLevel{{Price: "100", Quantity: "1"}},
		Asks: []schema.PriceLevel{{Price: "101", Quantity: "2"}},
	})
	snapshotTS := time.UnixMilli(1700000000000).UTC()

	snapshotEvent := bookEvent{
		InstID:    "",
		Asks:      [][]string{{"101", "2"}},
		Bids:      [][]string{{"100", "1"}},
		SeqID:     json.Number(strconv.FormatUint(42, 10)),
		PrevSeqID: json.Number("-1"),
		Checksum:  checksum,
		Timestamp: strconv.FormatInt(snapshotTS.UnixMilli(), 10),
	}

	rawSnapshot, err := json.Marshal(snapshotEvent)
	if err != nil {
		t.Fatalf("marshal snapshot event: %v", err)
	}

	if err := prov.handleBooks(wsEnvelope{
		Arg:    wsArgument{Channel: "books", InstID: instID},
		Action: "snapshot",
		Data:   []json.RawMessage{rawSnapshot},
	}); err != nil {
		t.Fatalf("handle snapshot: %v", err)
	}

	select {
	case evt := <-prov.events:
		if evt.Type != schema.EventTypeBookSnapshot {
			t.Fatalf("unexpected event type: %s", evt.Type)
		}
		payload, ok := evt.Payload.(schema.BookSnapshotPayload)
		if !ok {
			t.Fatalf("expected BookSnapshotPayload, got %T", evt.Payload)
		}
		if payload.FirstUpdateID != 42 || payload.FinalUpdateID != 42 {
			t.Fatalf("unexpected sequence ids: first=%d final=%d", payload.FirstUpdateID, payload.FinalUpdateID)
		}
		if payload.Checksum != strconv.FormatInt(int64(checksum), 10) {
			t.Fatalf("unexpected checksum: %s", payload.Checksum)
		}
		prov.pools.ReturnEventInst(evt)
	case <-time.After(2 * time.Second):
		t.Fatal("expected snapshot event")
	}
}
