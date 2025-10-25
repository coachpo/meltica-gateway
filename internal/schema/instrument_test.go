package schema

import "testing"

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

func TestInstrumentValidate(t *testing.T) {
	tests := []struct {
		name       string
		instrument Instrument
		wantErr    bool
	}{
		{
			name: "valid spot instrument",
			instrument: Instrument{
				Symbol:            "BTC-USDT",
				Type:              InstrumentTypeSpot,
				BaseCurrency:      "BTC",
				QuoteCurrency:     "USDT",
				Venue:             "BINANCE",
				PriceIncrement:    "0.01",
				QuantityIncrement: "0.0001",
				PricePrecision:    intPtr(2),
				QuantityPrecision: intPtr(6),
				NotionalPrecision: intPtr(2),
				MinNotional:       "10",
				MinQuantity:       "0.0001",
				MaxQuantity:       "100",
			},
			wantErr: false,
		},
		{
			name: "valid perp instrument with contract",
			instrument: Instrument{
				Symbol:           "BTC-USD-PERP",
				Type:             InstrumentTypePerp,
				BaseCurrency:     "BTC",
				QuoteCurrency:    "USD",
				Venue:            "OKX",
				ContractValue:    floatPtr(100),
				ContractCurrency: "USD",
			},
			wantErr: false,
		},
		{
			name: "valid futures instrument",
			instrument: Instrument{
				Symbol:           "BTC-USD-20251227",
				Type:             InstrumentTypeFutures,
				BaseCurrency:     "BTC",
				QuoteCurrency:    "USD",
				Venue:            "CME",
				Expiry:           "2025-12-27",
				ContractValue:    floatPtr(5),
				ContractCurrency: "BTC",
			},
			wantErr: false,
		},
		{
			name: "valid options instrument with marker",
			instrument: Instrument{
				Symbol:            "BTC-USD-20251227-70000-C",
				Type:              InstrumentTypeOptions,
				BaseCurrency:      "BTC",
				QuoteCurrency:     "USD",
				Venue:             "DERIBIT",
				Expiry:            "2025-12-27",
				Strike:            floatPtr(70000),
				OptionType:        OptionTypeCall,
				ContractValue:     floatPtr(1),
				ContractCurrency:  "BTC",
				PriceIncrement:    "0.1",
				QuantityIncrement: "0.1",
				PricePrecision:    intPtr(1),
				QuantityPrecision: intPtr(1),
			},
			wantErr: false,
		},
		{
			name: "options instrument missing strike",
			instrument: Instrument{
				Symbol:        "BTC-USD-20251227-70000-C",
				Type:          InstrumentTypeOptions,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "DERIBIT",
				OptionType:    OptionTypeCall,
				Expiry:        "2025-12-27",
			},
			wantErr: true,
		},
		{
			name: "options instrument missing option type",
			instrument: Instrument{
				Symbol:        "BTC-USD-20251227-70000-C",
				Type:          InstrumentTypeOptions,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "DERIBIT",
				Expiry:        "2025-12-27",
				Strike:        floatPtr(70000),
			},
			wantErr: true,
		},
		{
			name: "spot instrument with contract value",
			instrument: Instrument{
				Symbol:           "ETH-USDT",
				Type:             InstrumentTypeSpot,
				BaseCurrency:     "ETH",
				QuoteCurrency:    "USDT",
				Venue:            "BINANCE",
				ContractValue:    floatPtr(1),
				ContractCurrency: "USDT",
			},
			wantErr: true,
		},
		{
			name: "futures instrument missing expiry",
			instrument: Instrument{
				Symbol:        "BTC-USD-20251227",
				Type:          InstrumentTypeFutures,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "CME",
			},
			wantErr: true,
		},
		{
			name: "perp instrument with expiry provided",
			instrument: Instrument{
				Symbol:        "BTC-USD-PERP",
				Type:          InstrumentTypePerp,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "OKX",
				Expiry:        "2025-12-27",
			},
			wantErr: true,
		},
		{
			name: "options instrument option type mismatch",
			instrument: Instrument{
				Symbol:        "BTC-USD-20251227-70000-C",
				Type:          InstrumentTypeOptions,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "DERIBIT",
				Expiry:        "2025-12-27",
				OptionType:    OptionTypePut,
			},
			wantErr: true,
		},
		{
			name: "options instrument strike mismatch symbol",
			instrument: Instrument{
				Symbol:        "BTC-USD-20251227-70000-C",
				Type:          InstrumentTypeOptions,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "DERIBIT",
				Expiry:        "2025-12-27",
				Strike:        floatPtr(65000),
				OptionType:    OptionTypeCall,
			},
			wantErr: true,
		},
		{
			name: "non options instrument with option type set",
			instrument: Instrument{
				Symbol:        "BTC-USD-PERP",
				Type:          InstrumentTypePerp,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "OKX",
				OptionType:    OptionTypeCall,
			},
			wantErr: true,
		},
		{
			name: "base currency mismatch symbol",
			instrument: Instrument{
				Symbol:        "BTC-USD",
				Type:          InstrumentTypeSpot,
				BaseCurrency:  "ETH",
				QuoteCurrency: "USD",
				Venue:         "BINANCE",
			},
			wantErr: true,
		},
		{
			name: "invalid venue format",
			instrument: Instrument{
				Symbol:        "BTC-USD",
				Type:          InstrumentTypeSpot,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "Binance",
			},
			wantErr: true,
		},
		{
			name: "invalid price increment format",
			instrument: Instrument{
				Symbol:         "BTC-USD",
				Type:           InstrumentTypeSpot,
				BaseCurrency:   "BTC",
				QuoteCurrency:  "USD",
				Venue:          "BINANCE",
				PriceIncrement: "abc",
			},
			wantErr: true,
		},
		{
			name: "negative quantity increment",
			instrument: Instrument{
				Symbol:            "BTC-USD",
				Type:              InstrumentTypeSpot,
				BaseCurrency:      "BTC",
				QuoteCurrency:     "USD",
				Venue:             "BINANCE",
				QuantityIncrement: "-0.1",
			},
			wantErr: true,
		},
		{
			name: "negative price precision",
			instrument: Instrument{
				Symbol:         "BTC-USD",
				Type:           InstrumentTypeSpot,
				BaseCurrency:   "BTC",
				QuoteCurrency:  "USD",
				Venue:          "BINANCE",
				PricePrecision: intPtr(-1),
			},
			wantErr: true,
		},
		{
			name: "price precision exceeds limit",
			instrument: Instrument{
				Symbol:         "BTC-USD",
				Type:           InstrumentTypeSpot,
				BaseCurrency:   "BTC",
				QuoteCurrency:  "USD",
				Venue:          "BINANCE",
				PricePrecision: intPtr(25),
			},
			wantErr: true,
		},
		{
			name: "min quantity greater than max",
			instrument: Instrument{
				Symbol:        "BTC-USD",
				Type:          InstrumentTypeSpot,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "BINANCE",
				MinQuantity:   "1",
				MaxQuantity:   "0.5",
			},
			wantErr: true,
		},
		{
			name: "min notional zero",
			instrument: Instrument{
				Symbol:        "BTC-USD",
				Type:          InstrumentTypeSpot,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "BINANCE",
				MinNotional:   "0",
			},
			wantErr: true,
		},
		{
			name: "max quantity invalid decimal",
			instrument: Instrument{
				Symbol:        "BTC-USD",
				Type:          InstrumentTypeSpot,
				BaseCurrency:  "BTC",
				QuoteCurrency: "USD",
				Venue:         "BINANCE",
				MaxQuantity:   "foo",
			},
			wantErr: true,
		},
		{
			name: "lowercase base currency",
			instrument: Instrument{
				Symbol:        "BTC-USD",
				Type:          InstrumentTypeSpot,
				BaseCurrency:  "btc",
				QuoteCurrency: "USD",
				Venue:         "BINANCE",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := tt.instrument
			err := inst.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
