package binance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPSnapshotFetcher retrieves Binance REST snapshots over HTTP.
type HTTPSnapshotFetcher struct {
	client  *http.Client
	baseURL string
}

// NewHTTPSnapshotFetcher creates a snapshot fetcher with the provided base URL and timeout.
func NewHTTPSnapshotFetcher(baseURL string, timeout time.Duration) *HTTPSnapshotFetcher {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	baseURL = strings.TrimRight(baseURL, "/")
	client := new(http.Client)
	client.Timeout = timeout
	return &HTTPSnapshotFetcher{
		client:  client,
		baseURL: baseURL,
	}
}

// Fetch requests the snapshot at the configured endpoint.
func (f *HTTPSnapshotFetcher) Fetch(ctx context.Context, endpoint string) ([]byte, error) {
	url := f.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch snapshot: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("snapshot status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("snapshot read: %w", err)
	}
	return body, nil
}
