// Package binance provides adapters for Binance market data integration.
package binance

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// Parser normalises Binance payloads into canonical events.
type Parser struct {
	pools        *pool.PoolManager
	providerName string
}

const parserAcquireWarnDelay = 250 * time.Millisecond

// NewParser creates a Binance payload parser.
func NewParser() *Parser {
	return NewParserWithPool("binance", nil)
}

// NewParserWithPool creates a parser configured with pooling support.
func NewParserWithPool(providerName string, pools *pool.PoolManager) *Parser {
	if providerName == "" {
		providerName = "binance"
	}
	return &Parser{pools: pools, providerName: providerName}
}

// Parse converts a websocket frame into canonical events.
func (p *Parser) Parse(ctx context.Context, frame []byte, ingestTS time.Time) ([]*schema.Event, error) {
	raw, releaseRaw, err := p.acquireProviderRaw(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseRaw()

	raw.Provider = p.providerName
	raw.ReceivedAt = ingestTS.UnixNano()
	raw.Payload = append(raw.Payload[:0], frame...)

	var envelope wsEnvelope
	if err := json.Unmarshal(raw.Payload, &envelope); err != nil {
		return nil, fmt.Errorf("parse binance ws frame: %w", err)
	}
	raw.StreamName = envelope.Stream

	meta := make(map[string]json.RawMessage)
	if err := json.Unmarshal(envelope.Data, &meta); err != nil {
		return nil, fmt.Errorf("parse binance ws meta: %w", err)
	}
	var eventType string
	if rawType, ok := meta["e"]; ok {
		if err := json.Unmarshal(rawType, &eventType); err != nil {
			return nil, fmt.Errorf("parse binance ws event type: %w", err)
		}
	}
	if eventType == "" {
		eventType = inferStreamType(envelope.Stream)
	}

	switch strings.ToLower(eventType) {
	case "depthupdate":
		return p.parseDepthUpdate(ctx, envelope.Stream, envelope.Data, ingestTS)
	case "aggtrade":
		return p.parseAggTrade(ctx, envelope.Stream, envelope.Data, ingestTS)
	case "24hrticker", "ticker":
		return p.parseTicker(ctx, envelope.Stream, envelope.Data, ingestTS)
	default:
		return nil, fmt.Errorf("unsupported binance ws event %s", eventType)
	}
}

// ParseSnapshot converts a REST snapshot payload into canonical events based on the parser hint.
func (p *Parser) ParseSnapshot(ctx context.Context, name string, body []byte, ingestTS time.Time) ([]*schema.Event, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "orderbook":
		return p.parseOrderbookSnapshot(ctx, body, ingestTS)
	default:
		return nil, fmt.Errorf("unsupported rest parser %s", name)
	}
}

func (p *Parser) parseDepthUpdate(ctx context.Context, stream string, data []byte, ingestTS time.Time) ([]*schema.Event, error) {
	_ = stream
	var payload depthUpdate
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode depth update: %w", err)
	}
	symbol := canonicalInstrument(payload.Symbol)
	if symbol == "" {
		return nil, fmt.Errorf("missing symbol in depth update")
	}
	seq := uint64(payload.FinalUpdateID)
	evt, err := p.acquireCanonicalEvent(ctx)
	if err != nil {
		return nil, err
	}
	evt.EventID = buildEventID(p.providerName, symbol, schema.EventTypeBookUpdate, seq)
	evt.Provider = p.providerName
	evt.Symbol = symbol
	evt.Type = schema.EventTypeBookUpdate
	evt.SeqProvider = seq
	evt.IngestTS = ingestTS
	evt.EmitTS = ingestTS
	evt.Payload = schema.BookUpdatePayload{
		UpdateType: schema.BookUpdateTypeDelta,
		Bids:       toPriceLevels(payload.Bids),
		Asks:       toPriceLevels(payload.Asks),
		Checksum:   payload.Checksum,
	}
	return []*schema.Event{evt}, nil
}

func (p *Parser) parseAggTrade(ctx context.Context, stream string, data []byte, ingestTS time.Time) ([]*schema.Event, error) {
	_ = stream
	var payload aggTrade
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode agg trade: %w", err)
	}
	symbol := canonicalInstrument(payload.Symbol)
	if symbol == "" {
		return nil, fmt.Errorf("missing symbol in agg trade")
	}
	seq := uint64(payload.TradeID)
	side := schema.TradeSideBuy
	if payload.IsBuyerMaker {
		side = schema.TradeSideSell
	}
	evt, err := p.acquireCanonicalEvent(ctx)
	if err != nil {
		return nil, err
	}
	evt.EventID = buildEventID(p.providerName, symbol, schema.EventTypeTrade, seq)
	evt.Provider = p.providerName
	evt.Symbol = symbol
	evt.Type = schema.EventTypeTrade
	evt.SeqProvider = seq
	evt.IngestTS = ingestTS
	evt.EmitTS = ingestTS
	evt.Payload = schema.TradePayload{
		TradeID:   fmt.Sprintf("%d", payload.TradeID),
		Side:      side,
		Price:     payload.Price,
		Quantity:  payload.Quantity,
		Timestamp: time.UnixMilli(payload.EventTime).UTC(),
	}
	return []*schema.Event{evt}, nil
}

func (p *Parser) parseTicker(ctx context.Context, stream string, data []byte, ingestTS time.Time) ([]*schema.Event, error) {
	_ = stream
	var payload ticker24hr
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode ticker: %w", err)
	}
	symbol := canonicalInstrument(payload.Symbol)
	if symbol == "" {
		return nil, fmt.Errorf("missing symbol in ticker")
	}
	if payload.EventTime < 0 {
		return nil, fmt.Errorf("negative ticker event time: %d", payload.EventTime)
	}
	seq := uint64(payload.EventTime)
	if seq == 0 {
		ingestNano := ingestTS.UnixNano()
		if ingestNano < 0 {
			return nil, fmt.Errorf("negative ingest timestamp: %d", ingestNano)
		}
		seq = uint64(ingestNano)
	}
	evt, err := p.acquireCanonicalEvent(ctx)
	if err != nil {
		return nil, err
	}
	evt.EventID = buildEventID(p.providerName, symbol, schema.EventTypeTicker, seq)
	evt.Provider = p.providerName
	evt.Symbol = symbol
	evt.Type = schema.EventTypeTicker
	evt.SeqProvider = seq
	evt.IngestTS = ingestTS
	evt.EmitTS = ingestTS
	evt.Payload = schema.TickerPayload{
		LastPrice: payload.LastPrice,
		BidPrice:  payload.BidPrice,
		AskPrice:  payload.AskPrice,
		Volume24h: payload.Volume,
		Timestamp: time.UnixMilli(payload.EventTime).UTC(),
	}
	return []*schema.Event{evt}, nil
}

func (p *Parser) parseOrderbookSnapshot(ctx context.Context, body []byte, ingestTS time.Time) ([]*schema.Event, error) {
	var payload orderbookSnapshot
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode orderbook snapshot: %w", err)
	}
	symbol := canonicalInstrument(payload.Symbol)
	if symbol == "" {
		return nil, fmt.Errorf("missing symbol in orderbook snapshot")
	}
	seq := uint64(payload.LastUpdateID)
	evt, err := p.acquireCanonicalEvent(ctx)
	if err != nil {
		return nil, err
	}
	evt.EventID = buildEventID(p.providerName, symbol, schema.EventTypeBookSnapshot, seq)
	evt.Provider = p.providerName
	evt.Symbol = symbol
	evt.Type = schema.EventTypeBookSnapshot
	evt.SeqProvider = seq
	evt.IngestTS = ingestTS
	evt.EmitTS = ingestTS
	evt.Payload = schema.BookSnapshotPayload{
		Bids:       toPriceLevels(payload.Bids),
		Asks:       toPriceLevels(payload.Asks),
		Checksum:   payload.Checksum,
		LastUpdate: time.UnixMilli(payload.EventTime).UTC(),
	}
	return []*schema.Event{evt}, nil
}

func (p *Parser) acquireProviderRaw(ctx context.Context) (*schema.ProviderRaw, func(), error) {
	if p.pools == nil {
		raw := new(schema.ProviderRaw)
		return raw, func() {}, nil
	}
	getCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	obj, err := p.pools.Get(getCtx, "ProviderRaw")
	if err != nil {
		return nil, nil, fmt.Errorf("acquire provider raw: %w", err)
	}
	raw, ok := obj.(*schema.ProviderRaw)
	if !ok {
		p.pools.Put("ProviderRaw", obj)
		return nil, nil, errors.New("provider raw pool returned unexpected type")
	}
	return raw, func() { p.pools.Put("ProviderRaw", raw) }, nil
}

func (p *Parser) acquireCanonicalEvent(ctx context.Context) (*schema.Event, error) {
	if p.pools == nil {
		return nil, errors.New("canonical event pool unavailable")
	}
	requestCtx := ctx
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	start := time.Now()
	obj, err := p.pools.Get(requestCtx, "CanonicalEvent")
	if err != nil {
		return nil, fmt.Errorf("acquire canonical event: %w", err)
	}
	if waited := time.Since(start); waited >= parserAcquireWarnDelay {
		log.Printf("parser: waited %s for CanonicalEvent pool", waited)
	}
	evt, ok := obj.(*schema.Event)
	if !ok {
		p.pools.Put("CanonicalEvent", obj)
		return nil, errors.New("canonical event pool returned unexpected type")
	}
	evt.Reset()
	return evt, nil
}

func canonicalInstrument(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return ""
	}
	knownQuotes := []string{"USDT", "BUSD", "USDC", "BTC"}
	for _, quote := range knownQuotes {
		if strings.HasSuffix(symbol, quote) && len(symbol) > len(quote) {
			base := symbol[:len(symbol)-len(quote)]
			return fmt.Sprintf("%s-%s", base, quote)
		}
	}
	if len(symbol) > 3 {
		return fmt.Sprintf("%s-%s", symbol[:3], symbol[3:])
	}
	return symbol
}

func toPriceLevels(levels [][]string) []schema.PriceLevel {
	out := make([]schema.PriceLevel, 0, len(levels))
	for _, level := range levels {
		if len(level) < 2 {
			continue
		}
		out = append(out, schema.PriceLevel{Price: level[0], Quantity: level[1]})
	}
	return out
}

func buildEventID(provider, symbol string, typ schema.EventType, seq uint64) string {
	return fmt.Sprintf("%s:%s:%s:%d", provider, symbol, string(typ), seq)
}

func inferStreamType(stream string) string {
	stream = strings.ToLower(stream)
	switch {
	case strings.Contains(stream, "depth"):
		return "depthupdate"
	case strings.Contains(stream, "aggtrade"):
		return "aggtrade"
	case strings.Contains(stream, "ticker"):
		return "24hrticker"
	default:
		return ""
	}
}

type wsEnvelope struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

type depthUpdate struct {
	EventType     string     `json:"e"`
	EventTime     int64      `json:"E"`
	Symbol        string     `json:"s"`
	FinalUpdateID uint64     `json:"u"`
	Bids          [][]string `json:"b"`
	Asks          [][]string `json:"a"`
	Checksum      string     `json:"checksum"`
}

type aggTrade struct {
	EventType    string `json:"e"`
	EventTime    int64  `json:"E"`
	Symbol       string `json:"s"`
	TradeID      uint64 `json:"t"`
	Price        string `json:"p"`
	Quantity     string `json:"q"`
	IsBuyerMaker bool   `json:"m"`
}

type ticker24hr struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`
	LastPrice string `json:"c"`
	BidPrice  string `json:"b"`
	AskPrice  string `json:"a"`
	Volume    string `json:"v"`
}

type orderbookSnapshot struct {
	Symbol       string     `json:"s"`
	LastUpdateID uint64     `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"`
	Asks         [][]string `json:"asks"`
	Checksum     string     `json:"checksum"`
	EventTime    int64      `json:"E"`
}
