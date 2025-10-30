package binance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/schema"
)

type exchangeInfoResponse struct {
	Symbols []exchangeInfoSymbol `json:"symbols"`
}

type exchangeInfoSymbol struct {
	Symbol                 string               `json:"symbol"`
	Status                 string               `json:"status"`
	BaseAsset              string               `json:"baseAsset"`
	QuoteAsset             string               `json:"quoteAsset"`
	ContractSize           string               `json:"contractSize"`
	ContractType           string               `json:"contractType"`
	BaseAssetPrecision     int                  `json:"baseAssetPrecision"`
	QuoteAssetPrecision    int                  `json:"quotePrecision"`
	QuoteAssetPrecisionAlt int                  `json:"quoteAssetPrecision"`
	Filters                []exchangeInfoFilter `json:"filters"`
	Permissions            []string             `json:"permissions"`
	OrderTypes             []string             `json:"orderTypes"`
	IsSpotTradingAllowed   bool                 `json:"isSpotTradingAllowed"`
	IsMarginTradingAllowed bool                 `json:"isMarginTradingAllowed"`
	BaseCommissionPrec     int                  `json:"baseCommissionPrecision"`
	QuoteCommissionPrec    int                  `json:"quoteCommissionPrecision"`
}

type exchangeInfoFilter struct {
	FilterType  string `json:"filterType"`
	MinPrice    string `json:"minPrice"`
	MaxPrice    string `json:"maxPrice"`
	TickSize    string `json:"tickSize"`
	MinQty      string `json:"minQty"`
	MaxQty      string `json:"maxQty"`
	StepSize    string `json:"stepSize"`
	MinNotional string `json:"minNotional"`
}

type depthSnapshotResponse struct {
	LastUpdateID int64      `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"`
	Asks         [][]string `json:"asks"`
}

type listenKeyResponse struct {
	ListenKey string `json:"listenKey"`
}

type accountInfoResponse struct {
	Balances []accountBalance `json:"balances"`
}

type accountBalance struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

type futuresBalanceEntry struct {
	Asset              string `json:"asset"`
	Balance            string `json:"balance"`
	WalletBalance      string `json:"walletBalance"`
	CrossWalletBalance string `json:"crossWalletBalance"`
	AvailableBalance   string `json:"availableBalance"`
	WithdrawAvailable  string `json:"withdrawAvailable"`
}

func (p *Provider) fetchExchangeInfo(ctx context.Context) ([]schema.Instrument, map[string]symbolMeta, error) {
	endpoint := p.opts.exchangeInfoEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return nil, nil, errors.New("binance: exchange info endpoint not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create exchangeInfo request: %w", err)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request exchangeInfo: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, nil, fmt.Errorf("exchangeInfo unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload exchangeInfoResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&payload); err != nil {
		return nil, nil, fmt.Errorf("decode exchangeInfo: %w", err)
	}

	permitted := make(map[string]struct{})
	if len(p.opts.Symbols) > 0 {
		for _, symbol := range p.opts.Symbols {
			permitted[symbol] = struct{}{}
		}
	}

	instruments := make([]schema.Instrument, 0, len(payload.Symbols))
	metas := make(map[string]symbolMeta, len(payload.Symbols))

	for _, sym := range payload.Symbols {
		if p.opts.isFuturesMarket() {
			contract := strings.ToUpper(strings.TrimSpace(sym.ContractType))
			if contract != "" && contract != "PERPETUAL" {
				continue
			}
		}
		if !strings.EqualFold(sym.Status, "TRADING") {
			continue
		}
		if len(permitted) > 0 {
			canonical := canonicalFromAssets(sym.BaseAsset, sym.QuoteAsset)
			if _, ok := permitted[canonical]; !ok {
				continue
			}
		}
		instrument, meta, err := p.buildInstrument(sym)
		if err != nil {
			continue
		}
		if len(permitted) > 0 {
			if _, ok := permitted[instrument.Symbol]; !ok {
				continue
			}
		}
		instruments = append(instruments, instrument)
		metas[instrument.Symbol] = meta
	}

	return instruments, metas, nil
}

func (p *Provider) buildInstrument(sym exchangeInfoSymbol) (schema.Instrument, symbolMeta, error) {
	canonical := canonicalFromAssets(sym.BaseAsset, sym.QuoteAsset)
	instType := schema.InstrumentTypeSpot
	if p.opts.isFuturesMarket() {
		instType = schema.InstrumentTypePerp
	}
	contractCurrency := ""
	if p.opts.isFuturesUSDM() {
		contractCurrency = strings.ToUpper(strings.TrimSpace(sym.QuoteAsset))
	} else if p.opts.isFuturesCoinM() {
		contractCurrency = strings.ToUpper(strings.TrimSpace(sym.BaseAsset))
	}
	inst := schema.Instrument{
		Symbol:            canonical,
		Type:              instType,
		BaseCurrency:      strings.ToUpper(strings.TrimSpace(sym.BaseAsset)),
		QuoteCurrency:     strings.ToUpper(strings.TrimSpace(sym.QuoteAsset)),
		Venue:             p.opts.Venue,
		Expiry:            "",
		ContractValue:     nil,
		ContractCurrency:  contractCurrency,
		Strike:            nil,
		OptionType:        "",
		PriceIncrement:    "",
		QuantityIncrement: "",
		PricePrecision:    nil,
		QuantityPrecision: nil,
		NotionalPrecision: nil,
		MinNotional:       "",
		MinQuantity:       "",
		MaxQuantity:       "",
	}
	if sym.QuoteAssetPrecision > 0 {
		inst.PricePrecision = ptr(sym.QuoteAssetPrecision)
	} else if sym.QuoteAssetPrecisionAlt > 0 {
		inst.PricePrecision = ptr(sym.QuoteAssetPrecisionAlt)
	}
	if sym.BaseAssetPrecision > 0 {
		inst.QuantityPrecision = ptr(sym.BaseAssetPrecision)
	}
	if sym.QuoteCommissionPrec > 0 {
		inst.NotionalPrecision = ptr(sym.QuoteCommissionPrec)
	}

	for _, filter := range sym.Filters {
		switch strings.ToUpper(strings.TrimSpace(filter.FilterType)) {
		case "PRICE_FILTER":
			if strings.TrimSpace(filter.TickSize) != "" {
				inst.PriceIncrement = filter.TickSize
			}
		case "LOT_SIZE":
			if strings.TrimSpace(filter.StepSize) != "" {
				inst.QuantityIncrement = filter.StepSize
			}
			if strings.TrimSpace(filter.MinQty) != "" {
				inst.MinQuantity = filter.MinQty
			}
			if strings.TrimSpace(filter.MaxQty) != "" {
				inst.MaxQuantity = filter.MaxQty
			}
		case "MIN_NOTIONAL":
			if strings.TrimSpace(filter.MinNotional) != "" {
				inst.MinNotional = filter.MinNotional
			}
		}
	}

	if value := strings.TrimSpace(sym.ContractSize); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
			inst.ContractValue = &parsed
		}
	}

	meta := symbolMeta{
		canonical: canonical,
		rest:      strings.ToUpper(strings.TrimSpace(sym.Symbol)),
		stream:    strings.ToLower(strings.TrimSpace(sym.Symbol)),
	}

	return inst, meta, nil
}

func (p *Provider) fetchDepthSnapshot(ctx context.Context, symbol string) (depthSnapshotResponse, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("limit", fmt.Sprintf("%d", p.opts.SnapshotDepth))
	endpoint := p.opts.depthEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return depthSnapshotResponse{}, errors.New("binance: depth endpoint not configured")
	}
	fullEndpoint := endpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullEndpoint, nil)
	if err != nil {
		return depthSnapshotResponse{}, fmt.Errorf("create depth request: %w", err)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return depthSnapshotResponse{}, fmt.Errorf("request depth snapshot: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return depthSnapshotResponse{}, fmt.Errorf("depth snapshot status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var snapshot depthSnapshotResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&snapshot); err != nil {
		return depthSnapshotResponse{}, fmt.Errorf("decode depth snapshot: %w", err)
	}
	return snapshot, nil
}

func (p *Provider) createListenKey(ctx context.Context) (string, error) {
	if !p.hasTradingCredentials() {
		return "", errors.New("binance: missing api credentials for listen key")
	}
	reqCtx, cancel := context.WithTimeout(ctx, p.opts.httpTimeoutDuration())
	defer cancel()
	endpoint := p.opts.listenKeyEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return "", errors.New("binance: listen key endpoint not configured")
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create listen key request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", p.opts.APIKey)
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("request listen key: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("listen key status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload listenKeyResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&payload); err != nil {
		return "", fmt.Errorf("decode listen key: %w", err)
	}
	if strings.TrimSpace(payload.ListenKey) == "" {
		return "", errors.New("binance: empty listen key")
	}
	return payload.ListenKey, nil
}

func (p *Provider) keepAliveListenKey(ctx context.Context, listenKey string) error {
	if strings.TrimSpace(listenKey) == "" {
		return errors.New("binance: empty listen key for keepalive")
	}
	reqCtx, cancel := context.WithTimeout(ctx, p.opts.httpTimeoutDuration())
	defer cancel()
	params := url.Values{}
	params.Set("listenKey", strings.TrimSpace(listenKey))
	endpoint := p.opts.listenKeyEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return errors.New("binance: listen key endpoint not configured")
	}
	fullEndpoint := endpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPut, fullEndpoint, nil)
	if err != nil {
		return fmt.Errorf("create keepalive request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", p.opts.APIKey)
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("keepalive listen key: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("listen key keepalive status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (p *Provider) fetchAccountBalances(ctx context.Context) ([]accountBalance, error) {
	if !p.hasTradingCredentials() {
		return nil, errors.New("binance: missing api credentials for account balances")
	}
	if p.opts.isFuturesMarket() {
		return p.fetchFuturesAccountBalances(ctx)
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.opts.httpTimeoutDuration())
	defer cancel()
	params := url.Values{}
	if p.opts.recvWindowDuration() > 0 {
		params.Set("recvWindow", strconv.FormatInt(p.opts.recvWindowDuration().Milliseconds(), 10))
	}
	params.Set("timestamp", strconv.FormatInt(p.clock().UTC().UnixMilli(), 10))
	query := params.Encode()
	signature := signPayload(query, p.opts.APISecret)
	if query != "" {
		query += "&"
	}
	query += "signature=" + signature
	endpoint := p.opts.accountInfoEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("binance: account endpoint not configured")
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint+"?"+query, nil)
	if err != nil {
		return nil, fmt.Errorf("create account request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", p.opts.APIKey)
	resp, err := p.httpClient().Do(req)
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
	var payload accountInfoResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode account: %w", err)
	}
	return payload.Balances, nil
}

func (p *Provider) fetchFuturesAccountBalances(ctx context.Context) ([]accountBalance, error) {
	reqCtx, cancel := context.WithTimeout(ctx, p.opts.httpTimeoutDuration())
	defer cancel()
	params := url.Values{}
	if p.opts.recvWindowDuration() > 0 {
		params.Set("recvWindow", strconv.FormatInt(p.opts.recvWindowDuration().Milliseconds(), 10))
	}
	params.Set("timestamp", strconv.FormatInt(p.clock().UTC().UnixMilli(), 10))
	query := params.Encode()
	signature := signPayload(query, p.opts.APISecret)
	if query != "" {
		query += "&"
	}
	query += "signature=" + signature
	endpoint := p.opts.accountInfoEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("binance: futures account endpoint not configured")
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint+"?"+query, nil)
	if err != nil {
		return nil, fmt.Errorf("create futures account request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", p.opts.APIKey)
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request futures account: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("futures account status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload []futuresBalanceEntry
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode futures account: %w", err)
	}
	out := make([]accountBalance, 0, len(payload))
	for _, entry := range payload {
		asset := strings.ToUpper(strings.TrimSpace(entry.Asset))
		if asset == "" {
			continue
		}
		total := firstNonEmpty(entry.WalletBalance, entry.Balance, entry.CrossWalletBalance)
		free := firstNonEmpty(entry.AvailableBalance, entry.WithdrawAvailable)
		if free == "" {
			free = total
		}
		total = strings.TrimSpace(total)
		free = strings.TrimSpace(free)
		if total == "" && free == "" {
			continue
		}
		locked := ""
		totalDec, totalErr := decimal.NewFromString(total)
		freeDec, freeErr := decimal.NewFromString(free)
		if totalErr == nil && freeErr == nil {
			lockedDec := totalDec.Sub(freeDec)
			if lockedDec.Sign() < 0 {
				lockedDec = decimal.Zero
			}
			locked = lockedDec.String()
		}
		out = append(out, accountBalance{
			Asset:  asset,
			Free:   free,
			Locked: locked,
		})
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func ptr[T any](v T) *T {
	return &v
}

func canonicalFromAssets(base, quote string) string {
	base = strings.ToUpper(strings.TrimSpace(base))
	quote = strings.ToUpper(strings.TrimSpace(quote))
	if base == "" || quote == "" {
		return ""
	}
	return base + "-" + quote
}

type symbolMeta struct {
	canonical string
	rest      string
	stream    string
}
