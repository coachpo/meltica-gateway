package unit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coder/websocket"

	"github.com/coachpo/meltica/internal/adapters/binance"
)

const (
	legacyFrameNsPerOp     = 120000.0
	legacyFrameAllocsPerOp = 220.0
	benchmarkTradeFrame    = `{"stream":"btcusdt@trade","data":{"e":"aggTrade","E":1717171717,"s":"BTCUSDT","t":12345,"p":"123.45","q":"0.001","m":false}}`
)

func TestDefaultFrameProviderWithCoderWebsocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subscribePayload := make(chan []byte, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		require.NoError(t, err)
		defer conn.Close(websocket.StatusNormalClosure, "shutdown")

		readCtx, readCancel := context.WithTimeout(ctx, time.Second)
		typ, data, err := conn.Read(readCtx)
		readCancel()
		require.NoError(t, err)
		require.Equal(t, websocket.MessageText, typ)
		subscribePayload <- append([]byte(nil), data...)

		writeCtx, writeCancel := context.WithTimeout(ctx, time.Second)
		err = conn.Write(writeCtx, websocket.MessageText, []byte(`{"stream":"btcusdt@trade"}`))
		writeCancel()
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	wsURL, err := toWebsocketURL(server.URL)
	require.NoError(t, err)

	provider := binance.NewDefaultFrameProvider(wsURL, 100*time.Millisecond)

	frames, errs, err := provider.Subscribe(ctx, []string{"btcusdt@trade"})
	require.NoError(t, err)
	require.NotNil(t, frames)
	require.NotNil(t, errs)

	select {
	case got := <-frames:
		require.JSONEq(t, `{"stream":"btcusdt@trade"}`, string(got))
	case <-time.After(2 * time.Second):
		t.Fatal("expected frame from provider")
	}

	select {
	case raw := <-subscribePayload:
		var payload map[string]any
		require.NoError(t, json.Unmarshal(raw, &payload))
		require.Equal(t, "SUBSCRIBE", payload["method"])
		params, ok := payload["params"].([]any)
		require.True(t, ok)
		require.ElementsMatch(t, []any{"btcusdt@trade"}, params)
	case <-time.After(time.Second):
		t.Fatal("expected subscribe payload")
	}

	cancel()
}

func TestDefaultFrameProviderReadTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		require.NoError(t, err)
		defer conn.Close(websocket.StatusNormalClosure, "shutdown")

		// consume subscribe request to keep handshake symmetrical
		readCtx, readCancel := context.WithTimeout(context.Background(), time.Second)
		_, _, err = conn.Read(readCtx)
		readCancel()
		require.NoError(t, err)

		// block without sending further frames so client read times out
		<-ctx.Done()
	}))
	t.Cleanup(server.Close)

	wsURL, err := toWebsocketURL(server.URL)
	require.NoError(t, err)

	provider := binance.NewDefaultFrameProvider(wsURL, 50*time.Millisecond)

	_, errs, err := provider.Subscribe(ctx, []string{"btcusdt@trade"})
	require.NoError(t, err)

	select {
	case err := <-errs:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(2 * time.Second):
		t.Fatal("expected deadline exceeded error from provider")
	}
}

func toWebsocketURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		if !strings.HasPrefix(u.Scheme, "ws") {
			return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
		}
	}
	return u.String(), nil
}

func BenchmarkDefaultFrameProviderCoder(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	writes := make(chan []byte, 2048)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		require.NoError(b, err)
		defer conn.Close(websocket.StatusNormalClosure, "shutdown")

		readCtx, readCancel := context.WithTimeout(ctx, time.Second)
		_, _, err = conn.Read(readCtx)
		readCancel()
		require.NoError(b, err)

		for {
			select {
			case payload, ok := <-writes:
				if !ok {
					return
				}
				writeCtx, writeCancel := context.WithTimeout(ctx, time.Second)
				err = conn.Write(writeCtx, websocket.MessageText, payload)
				writeCancel()
				if err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}))
	b.Cleanup(server.Close)
	b.Cleanup(func() {
		close(writes)
	})

	wsURL, err := toWebsocketURL(server.URL)
	require.NoError(b, err)

	provider := binance.NewDefaultFrameProvider(wsURL, 100*time.Millisecond)

	frames, errs, err := provider.Subscribe(ctx, []string{"btcusdt@trade"})
	require.NoError(b, err)
	require.NotNil(b, frames)
	require.NotNil(b, errs)

	payload := []byte(benchmarkTradeFrame)
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	waitForFrame := func() {
		if !deadline.Stop() {
			select {
			case <-deadline.C:
			default:
			}
		}
		deadline.Reset(time.Second)
		select {
		case <-frames:
			if !deadline.Stop() {
				select {
				case <-deadline.C:
				default:
				}
			}
		case err := <-errs:
			if !deadline.Stop() {
				select {
				case <-deadline.C:
				default:
				}
			}
			require.NoError(b, err)
		case <-deadline.C:
			b.Fatalf("timeout waiting for frame delivery")
		}
	}

	one := func() {
		select {
		case writes <- payload:
		case <-ctx.Done():
			b.Fatalf("context canceled before frame send")
		}
		waitForFrame()
	}

	// Warm-up to ensure channels and timers ready.
	one()

	allocs := testing.AllocsPerRun(1, func() {
		one()
	})
	if allocs > legacyFrameAllocsPerOp {
		b.Fatalf("expected ≤%.2f allocs/op, got %.2f", legacyFrameAllocsPerOp, allocs)
	}

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		one()
	}
	elapsed := time.Since(start)
	b.StopTimer()

	if b.N == 0 {
		return
	}
	nsPerOp := float64(elapsed.Nanoseconds()) / float64(b.N)
	if nsPerOp > legacyFrameNsPerOp {
		b.Fatalf("expected ≤%.0f ns/op, got %.0f ns/op", legacyFrameNsPerOp, nsPerOp)
	}

	b.ReportMetric(nsPerOp, "ns/op")
	b.ReportMetric(nsPerOp/legacyFrameNsPerOp, "ratio_vs_legacy")
}
