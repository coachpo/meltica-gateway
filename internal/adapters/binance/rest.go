package binance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	json "github.com/goccy/go-json"

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

func (p *Provider) fetchExchangeInfo(ctx context.Context) ([]schema.Instrument, map[string]symbolMeta, error) {
	endpoint := strings.TrimSuffix(p.opts.APIBaseURL, "/") + "/api/v3/exchangeInfo"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create exchangeInfo request: %w", err)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request exchangeInfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, nil, fmt.Errorf("exchangeInfo unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	payload := exchangeInfoResponse{}
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
	inst := schema.Instrument{
		Symbol:            canonical,
		Type:              schema.InstrumentTypeSpot,
		BaseCurrency:      strings.ToUpper(strings.TrimSpace(sym.BaseAsset)),
		QuoteCurrency:     strings.ToUpper(strings.TrimSpace(sym.QuoteAsset)),
		Venue:             p.opts.Venue,
		PriceIncrement:    "",
		QuantityIncrement: "",
		MinQuantity:       "",
		MaxQuantity:       "",
		MinNotional:       "",
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

	meta := symbolMeta{
		canonical: canonical,
		rest:      strings.ToUpper(strings.TrimSpace(sym.Symbol)),
	}
	meta.stream = strings.ToLower(meta.rest)

	return inst, meta, nil
}

func (p *Provider) fetchDepthSnapshot(ctx context.Context, symbol string) (depthSnapshotResponse, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("limit", fmt.Sprintf("%d", p.opts.SnapshotDepth))
	endpoint := strings.TrimSuffix(p.opts.APIBaseURL, "/") + "/api/v3/depth?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return depthSnapshotResponse{}, fmt.Errorf("create depth request: %w", err)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return depthSnapshotResponse{}, fmt.Errorf("request depth snapshot: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return depthSnapshotResponse{}, fmt.Errorf("depth snapshot status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	snapshot := depthSnapshotResponse{}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&snapshot); err != nil {
		return depthSnapshotResponse{}, fmt.Errorf("decode depth snapshot: %w", err)
	}
	return snapshot, nil
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
