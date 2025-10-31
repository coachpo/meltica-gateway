package okx

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	Asks     [][]string  `json:"asks"`
	Bids     [][]string  `json:"bids"`
	SeqID    json.Number `json:"seqId"`
	PrevSeq  json.Number `json:"prevSeqId"`
	Checksum int32       `json:"checksum"`
	TS       string      `json:"ts"`
	Action   string      `json:"action"`
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
	
	// OKX may not return seqId for full orderbook snapshots, use timestamp as fallback
	seqStr := entry.SeqID.String()
	var seq uint64
	if seqStr != "" {
		var err error
		seq, err = strconv.ParseUint(seqStr, 10, 64)
		if err != nil {
			return schema.BookSnapshotPayload{}, 0, fmt.Errorf("parse books sequence: %w", err)
		}
	} else {
		// Use timestamp as sequence ID if seqId is missing
		timestamp := parseMilliTimestamp(entry.TS)
		if !timestamp.IsZero() {
			millis := timestamp.UnixMilli()
			if millis > 0 {
				seq = uint64(millis) // #nosec G115 -- UnixMilli is always positive for valid timestamps
			}
		} else {
			millis := time.Now().UnixMilli()
			if millis > 0 {
				seq = uint64(millis) // #nosec G115 -- UnixMilli is always positive for current time
			}
		}
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

type orderRequest struct {
	InstID  string `json:"instId"`
	TdMode  string `json:"tdMode"`
	Side    string `json:"side"`
	OrdType string `json:"ordType"`
	Sz      string `json:"sz"`
	Px      string `json:"px,omitempty"`
	ClOrdID string `json:"clOrdId,omitempty"`
}

type orderResponse struct {
	Code string           `json:"code"`
	Msg  string           `json:"msg"`
	Data []orderResultRow `json:"data"`
}

type orderResultRow struct {
	ClOrdID string `json:"clOrdId"`
	OrdID   string `json:"ordId"`
	SCode   string `json:"sCode"`
	SMsg    string `json:"sMsg"`
}

type accountResponse struct {
	Code string          `json:"code"`
	Msg  string          `json:"msg"`
	Data []accountDetail `json:"data"`
}

type accountDetail struct {
	TotalEq string        `json:"totalEq"`
	Details []balanceItem `json:"details"`
}

type balanceItem struct {
	Ccy       string `json:"ccy"`
	AvailBal  string `json:"availBal"`
	CashBal   string `json:"cashBal"`
	FrozenBal string `json:"frozenBal"`
}

func (p *Provider) submitOrder(ctx context.Context, meta symbolMeta, req schema.OrderRequest) error {
	if !p.hasTradingCredentials() {
		return errors.New("okx: missing api credentials for trading")
	}

	side, err := okxSide(req.Side)
	if err != nil {
		return err
	}

	ordType, err := okxOrderType(req.OrderType)
	if err != nil {
		return err
	}

	quantity := strings.TrimSpace(req.Quantity)
	if quantity == "" {
		return errors.New("okx: quantity required")
	}

	price := ""
	if req.Price != nil && strings.TrimSpace(*req.Price) != "" {
		price = strings.TrimSpace(*req.Price)
	}

	clientOrderID := ""
	if req.ClientOrderID != "" {
		clientOrderID = req.ClientOrderID
	}

	orderReq := orderRequest{
		InstID:  meta.instID,
		TdMode:  "cash",
		Side:    side,
		OrdType: ordType,
		Sz:      quantity,
		Px:      price,
		ClOrdID: clientOrderID,
	}

	body, err := json.Marshal(orderReq)
	if err != nil {
		return fmt.Errorf("marshal order request: %w", err)
	}

	endpoint := p.opts.orderEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return errors.New("okx: order endpoint not configured")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create order request: %w", err)
	}

	timestamp := p.clock().UTC().Format("2006-01-02T15:04:05.999Z07:00")
	httpReq.Header.Set("Content-Type", "application/json")
	p.signRequest(httpReq, timestamp, string(body))

	resp, err := p.httpClient().Do(httpReq)
	if err != nil {
		return fmt.Errorf("submit order: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read order response: %w", err)
	}

	var orderResp orderResponse
	if err := json.Unmarshal(respBody, &orderResp); err != nil {
		return fmt.Errorf("decode order response: %w", err)
	}

	if strings.TrimSpace(orderResp.Code) != "0" {
		return fmt.Errorf("okx order error code %s: %s", strings.TrimSpace(orderResp.Code), strings.TrimSpace(orderResp.Msg))
	}

	if len(orderResp.Data) == 0 {
		return errors.New("okx: empty order response data")
	}

	result := orderResp.Data[0]
	if strings.TrimSpace(result.SCode) != "0" {
		return fmt.Errorf("okx order failed %s: %s", strings.TrimSpace(result.SCode), strings.TrimSpace(result.SMsg))
	}

	return nil
}

func (p *Provider) fetchAccountBalances(ctx context.Context) ([]balanceItem, error) {
	if !p.hasTradingCredentials() {
		return nil, errors.New("okx: missing api credentials for account balances")
	}

	endpoint := p.opts.accountEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("okx: account endpoint not configured")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create account request: %w", err)
	}

	timestamp := p.clock().UTC().Format("2006-01-02T15:04:05.999Z07:00")
	p.signRequest(httpReq, timestamp, "")

	resp, err := p.httpClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request account: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("account status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload accountResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode account: %w", err)
	}

	if strings.TrimSpace(payload.Code) != "0" {
		return nil, fmt.Errorf("account error code %s: %s", strings.TrimSpace(payload.Code), strings.TrimSpace(payload.Msg))
	}

	if len(payload.Data) == 0 {
		return nil, errors.New("okx: empty account response")
	}

	return payload.Data[0].Details, nil
}

func (p *Provider) signRequest(req *http.Request, timestamp, body string) {
	method := strings.ToUpper(req.Method)
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}

	message := timestamp + method + path + body
	mac := hmac.New(sha256.New, []byte(p.opts.Config.APISecret))
	mac.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req.Header.Set("OK-ACCESS-KEY", p.opts.Config.APIKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", p.opts.Config.Passphrase)
}

func okxSide(side schema.TradeSide) (string, error) {
	switch side {
	case schema.TradeSideBuy:
		return "buy", nil
	case schema.TradeSideSell:
		return "sell", nil
	default:
		return "", fmt.Errorf("okx: unknown trade side %v", side)
	}
}

func okxOrderType(orderType schema.OrderType) (string, error) {
	switch orderType {
	case schema.OrderTypeMarket:
		return "market", nil
	case schema.OrderTypeLimit:
		return "limit", nil
	default:
		return "", fmt.Errorf("okx: unknown order type %v", orderType)
	}
}
