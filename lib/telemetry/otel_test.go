package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/config"
)

func TestParseEndpoint(t *testing.T) {
	host, insecure, err := parseEndpoint("https://example.com:4318")
	require.NoError(t, err)
	require.Equal(t, "example.com:4318", host)
	require.False(t, insecure)

	host, insecure, err = parseEndpoint("http://localhost:4318")
	require.NoError(t, err)
	require.Equal(t, "localhost:4318", host)
	require.True(t, insecure)
}

func TestInitNoEndpointUsesNoop(t *testing.T) {
	providers, shutdown, err := Init(context.Background(), config.TelemetryConfig{})
	require.NoError(t, err)
	require.NotNil(t, providers.TracerProvider)
	require.NotNil(t, providers.MeterProvider)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}

func TestInitInvalidEndpoint(t *testing.T) {
	_, _, err := Init(context.Background(), config.TelemetryConfig{OTLPEndpoint: "://bad"})
	require.Error(t, err)
}

func TestInitWithEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	providers, shutdown, err := Init(context.Background(), config.TelemetryConfig{OTLPEndpoint: srv.URL, ServiceName: "gateway"})
	require.NoError(t, err)
	require.NotNil(t, providers.TracerProvider)
	require.NotNil(t, providers.MeterProvider)
	require.NoError(t, shutdown(context.Background()))
}
