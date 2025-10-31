package okx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/domain/schema"
)

type instrumentsResponse struct {
	Code string             `json:"code"`
	Msg  string             `json:"msg"`
	Data []instrumentRecord `json:"data"`
}

type instrumentRecord struct {
	InstID   string `json:"instId"`
	InstType string `json:"instType"`
	BaseCcy  string `json:"baseCcy"`
	QuoteCcy string `json:"quoteCcy"`
	TickSz   string `json:"tickSz"`
	LotSz    string `json:"lotSz"`
	MinSz    string `json:"minSz"`
	State    string `json:"state"`
}

type booksResponse struct {
	Code string          `json:"code"`
	Msg  string          `json:"msg"`
	Data []booksSnapshot `json:"data"`
}

type booksSnapshot struct {
	Asks     [][]string `json:"asks"`
	Bids     [][]string `json:"bids"`
	SeqID    string     `json:"seqId"`
	PrevSeq  string     `json:"prevSeqId"`
	Checksum int32      `json:"checksum"`
	TS       string     `json:"ts"`
	Action   string     `json:"action"`
}

type symbolMeta struct {
	canonical string
	instID    string
}

func (p *Provider) fetchInstruments(ctx context.Context) ([]schema.Instrument, map[string]symbolMeta, error) {
	endpoint := p.opts.instrumentsEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return nil, nil, errors.New("okx: instruments endpoint not configured")
	}
	params := url.Values{}
	params.Set("instType", "SPOT")
	fullEndpoint := endpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullEndpoint, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create instruments request: %w", err)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request instruments: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, nil, fmt.Errorf("instruments status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload instrumentsResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&payload); err != nil {
		return nil, nil, fmt.Errorf("decode instruments: %w", err)
	}
	if strings.TrimSpace(payload.Code) != "0" {
		return nil, nil, fmt.Errorf("instruments error code %s: %s", strings.TrimSpace(payload.Code), strings.TrimSpace(payload.Msg))
	}
	if len(payload.Data) == 0 {
		return nil, nil, errors.New("okx: no instruments returned")
	}
	instruments := make([]schema.Instrument, 0, len(payload.Data))
	metas := make(map[string]symbolMeta, len(payload.Data))
	for _, record := range payload.Data {
		if !strings.EqualFold(strings.TrimSpace(record.InstType), "SPOT") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(record.State), "live") {
			continue
		}
		inst, meta, err := buildInstrument(record, p.opts.publicMeta.venue)
		if err != nil {
			continue
		}
		instruments = append(instruments, inst)
		metas[inst.Symbol] = meta
	}
	if len(instruments) == 0 {
		return nil, nil, errors.New("okx: no active instruments available")
	}
	return instruments, metas, nil
}

func buildInstrument(record instrumentRecord, venue string) (schema.Instrument, symbolMeta, error) {
	instID := strings.TrimSpace(record.InstID)
	if instID == "" {
		return schema.Instrument{}, symbolMeta{}, errors.New("okx: instrument id empty")
	}
	base := strings.ToUpper(strings.TrimSpace(record.BaseCcy))
	quote := strings.ToUpper(strings.TrimSpace(record.QuoteCcy))
	if base == "" || quote == "" {
		return schema.Instrument{}, symbolMeta{}, errors.New("okx: base or quote currency missing")
	}
	canonical := base + "-" + quote

	inst := schema.Instrument{
		Symbol:            canonical,
		Type:              schema.InstrumentTypeSpot,
		BaseCurrency:      base,
		QuoteCurrency:     quote,
		Venue:             strings.ToUpper(strings.TrimSpace(venue)),
		Expiry:            "",
		ContractValue:     nil,
		ContractCurrency:  "",
		Strike:            nil,
		OptionType:        "",
		PriceIncrement:    strings.TrimSpace(record.TickSz),
		QuantityIncrement: strings.TrimSpace(record.LotSz),
		PricePrecision:    nil,
		QuantityPrecision: nil,
		NotionalPrecision: nil,
		MinNotional:       "",
		MinQuantity:       strings.TrimSpace(record.MinSz),
		MaxQuantity:       "",
	}

	if pricePrec, ok := precisionFromStep(record.TickSz); ok {
		inst.PricePrecision = &pricePrec
	}
	if qtyPrec, ok := precisionFromStep(record.LotSz); ok {
		inst.QuantityPrecision = &qtyPrec
	}

	meta := symbolMeta{
		canonical: canonical,
		instID:    instID,
	}
	return inst, meta, nil
}

func precisionFromStep(step string) (int, bool) {
	trimmed := strings.TrimSpace(step)
	if trimmed == "" {
		return 0, false
	}
	if !strings.Contains(trimmed, ".") {
		return 0, true
	}
	parts := strings.Split(trimmed, ".")
	decimals := strings.TrimRight(parts[1], "0")
	if decimals == "" {
		return 0, true
	}
	return len(decimals), true
}

func (p *Provider) fetchOrderBookSnapshot(ctx context.Context, instID string) (schema.BookSnapshotPayload, uint64, error) {
	endpoint := p.opts.booksEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return schema.BookSnapshotPayload{}, 0, errors.New("okx: books endpoint not configured")
	}
	depth := p.opts.Config.SnapshotDepth
	if depth <= 0 {
		depth = defaultSnapshotDepth
	}
	if depth > 400 {
		depth = 400
	}
	params := url.Values{}
	params.Set("instId", strings.TrimSpace(instID))
	params.Set(p.opts.privateMeta.depthParam, strconv.Itoa(depth))
	fullEndpoint := endpoint + "?" + params.Encode()
	requestCtx, cancel := context.WithTimeout(ctx, p.opts.httpTimeoutDuration())
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, fullEndpoint, nil)
	if err != nil {
		return schema.BookSnapshotPayload{}, 0, fmt.Errorf("create books request: %w", err)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return schema.BookSnapshotPayload{}, 0, fmt.Errorf("request books: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return schema.BookSnapshotPayload{}, 0, fmt.Errorf("books status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload booksResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&payload); err != nil {
		return schema.BookSnapshotPayload{}, 0, fmt.Errorf("decode books: %w", err)
	}
	if strings.TrimSpace(payload.Code) != "0" {
		return schema.BookSnapshotPayload{}, 0, fmt.Errorf("books error code %s: %s", strings.TrimSpace(payload.Code), strings.TrimSpace(payload.Msg))
	}
	if len(payload.Data) == 0 {
		return schema.BookSnapshotPayload{}, 0, errors.New("okx: empty books response")
	}
	entry := payload.Data[0]
	seq, err := strconv.ParseUint(strings.TrimSpace(entry.SeqID), 10, 64)
	if err != nil {
		return schema.BookSnapshotPayload{}, 0, fmt.Errorf("parse books sequence: %w", err)
	}
	timestamp := parseMilliTimestamp(entry.TS)
	snapshot := schema.BookSnapshotPayload{
		Bids:          convertPriceLevels(entry.Bids),
		Asks:          convertPriceLevels(entry.Asks),
		Checksum:      strconv.Itoa(int(entry.Checksum)),
		LastUpdate:    timestamp,
		FirstUpdateID: seq,
		FinalUpdateID: seq,
	}
	return snapshot, seq, nil
}

func convertPriceLevels(levels [][]string) []schema.PriceLevel {
	if len(levels) == 0 {
		return nil
	}
	out := make([]schema.PriceLevel, 0, len(levels))
	for _, level := range levels {
		if len(level) < 2 {
			continue
		}
		price := strings.TrimSpace(level[0])
		qty := strings.TrimSpace(level[1])
		if price == "" || qty == "" {
			continue
		}
		out = append(out, schema.PriceLevel{Price: price, Quantity: qty})
	}
	return out
}

func parseMilliTimestamp(ts string) time.Time {
	trimmed := strings.TrimSpace(ts)
	if trimmed == "" {
		return time.Time{}
	}
	millis, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(millis).UTC()
}
