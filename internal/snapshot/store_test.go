package snapshot

import (
	"testing"
)

func TestKeyValidate(t *testing.T) {
	tests := []struct {
		name    string
		key     Key
		wantErr bool
	}{
		{
			name: "valid key",
			key: Key{
				Market:     "BINANCE-SPOT",
				Instrument: "BTC-USD",
				Type:       "TICKER",
			},
			wantErr: false,
		},
		{
			name: "empty market",
			key: Key{
				Market:     "",
				Instrument: "BTC-USD",
				Type:       "TICKER",
			},
			wantErr: true,
		},
		{
			name: "invalid instrument",
			key: Key{
				Market:     "BINANCE-SPOT",
				Instrument: "BTCUSD", // Missing dash
				Type:       "TICKER",
			},
			wantErr: true,
		},
		{
			name: "invalid type",
			key: Key{
				Market:     "BINANCE-SPOT",
				Instrument: "BTC-USD",
				Type:       "ticker", // Lowercase
			},
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.key.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRecordCloneEmpty(t *testing.T) {
	record := Record{
		Key:  Key{Market: "TEST", Instrument: "BTC-USD", Type: "TICKER"},
		Seq:  1,
		Data: nil,
	}
	
	clone := record.Clone()
	
	if clone.Data == nil {
		t.Error("expected empty map, got nil")
	}
}
