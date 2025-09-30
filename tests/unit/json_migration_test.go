package unit

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	gojson "github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

type samplePayload struct {
	Provider string            `json:"provider"`
	Symbol   string            `json:"symbol"`
	Values   []int             `json:"values"`
	Meta     map[string]string `json:"meta,omitempty"`
	Blob     string            `json:"blob,omitempty"`
}

func makeLargePayload() samplePayload {
	values := make([]int, 0, 2048)
	for i := 0; i < 2048; i++ {
		values = append(values, i*i)
	}
	meta := make(map[string]string, 32)
	for i := 0; i < 32; i++ {
		meta[fmt.Sprintf("key_%03d", i)] = strings.Repeat("value", 6)
	}
	return samplePayload{
		Provider: "binance",
		Symbol:   "BTC-USDT",
		Values:   values,
		Meta:     meta,
		Blob:     strings.Repeat("data", 4096),
	}
}

func largePayloadJSON(tb testing.TB) []byte {
	tb.Helper()
	payload := makeLargePayload()
	bytes, err := gojson.Marshal(payload)
	if err != nil {
		tb.Fatalf("marshal large payload: %v", err)
	}
	return bytes
}

func TestGoJSONMarshalMatchesEncoding(t *testing.T) {
	payload := samplePayload{
		Provider: "binance",
		Symbol:   "BTC-USDT",
		Values:   []int{1, 2, 3},
		Meta: map[string]string{
			"trace_id": "abc-123",
		},
		Blob: strings.Repeat("ref", 16),
	}

	stdBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	fastBytes, err := gojson.Marshal(payload)
	require.NoError(t, err)

	require.JSONEq(t, string(stdBytes), string(fastBytes))
}

func TestGoJSONUnmarshalMatchesEncoding(t *testing.T) {
	input := []byte(`{"provider":"binance","symbol":"BTC-USDT","values":[5,8,13],"meta":{"trace_id":"xyz"},"blob":"` + strings.Repeat("ref", 16) + `"}`)

	var stdPayload samplePayload
	require.NoError(t, json.Unmarshal(input, &stdPayload))

	var fastPayload samplePayload
	require.NoError(t, gojson.Unmarshal(input, &fastPayload))

	require.Equal(t, stdPayload, fastPayload)
}

func BenchmarkGoJSONMarshal(b *testing.B) {
	payload := makeLargePayload()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := gojson.Marshal(payload); err != nil {
			b.Fatalf("marshal failed: %v", err)
		}
	}
}

func BenchmarkEncodingJSONMarshal(b *testing.B) {
	payload := makeLargePayload()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(payload); err != nil {
			b.Fatalf("marshal failed: %v", err)
		}
	}
}

func BenchmarkGoJSONUnmarshal(b *testing.B) {
	input := largePayloadJSON(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var out samplePayload
		if err := gojson.Unmarshal(input, &out); err != nil {
			b.Fatalf("unmarshal failed: %v", err)
		}
	}
}

func BenchmarkEncodingJSONUnmarshal(b *testing.B) {
	input := largePayloadJSON(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var out samplePayload
		if err := json.Unmarshal(input, &out); err != nil {
			b.Fatalf("unmarshal failed: %v", err)
		}
	}
}

func TestGoJSONMarshalIsFasterThanEncoding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmarks in short mode")
	}
	if raceEnabled {
		t.Skip("skipping speed comparison under -race")
	}

	fast := testing.Benchmark(BenchmarkGoJSONMarshal)
	slow := testing.Benchmark(BenchmarkEncodingJSONMarshal)
	require.Greater(t, slow.NsPerOp(), int64(0), "unexpected zero ns/op for encoding/json marshal")
	require.Greater(t, fast.NsPerOp(), int64(0), "unexpected zero ns/op for go-json marshal")
	speedup := float64(slow.NsPerOp()) / float64(fast.NsPerOp())
	require.GreaterOrEqualf(t, speedup, 1.3, "expected go-json marshal to be ≥30%% faster (ratio %.2fx)", speedup)
}

func TestGoJSONUnmarshalIsFasterThanEncoding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmarks in short mode")
	}
	if raceEnabled {
		t.Skip("skipping speed comparison under -race")
	}

	fast := testing.Benchmark(BenchmarkGoJSONUnmarshal)
	slow := testing.Benchmark(BenchmarkEncodingJSONUnmarshal)
	require.Greater(t, slow.NsPerOp(), int64(0), "unexpected zero ns/op for encoding/json unmarshal")
	require.Greater(t, fast.NsPerOp(), int64(0), "unexpected zero ns/op for go-json unmarshal")
	speedup := float64(slow.NsPerOp()) / float64(fast.NsPerOp())
	require.GreaterOrEqualf(t, speedup, 1.3, "expected go-json unmarshal to be ≥30%% faster (ratio %.2fx)", speedup)
}
