package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	binance "github.com/coachpo/meltica/internal/adapters/binance"
)

func TestHTTPSnapshotFetcherSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/snapshot", r.URL.Path)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	fetcher := binance.NewHTTPSnapshotFetcher(srv.URL+"/", time.Second)
	data, err := fetcher.Fetch(context.Background(), "/snapshot")
	require.NoError(t, err)
	require.Equal(t, `{"ok":true}`, string(data))
}

func TestHTTPSnapshotFetcherErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	fetcher := binance.NewHTTPSnapshotFetcher(srv.URL, time.Millisecond)
	_, err := fetcher.Fetch(context.Background(), "")
	require.Error(t, err)
}
