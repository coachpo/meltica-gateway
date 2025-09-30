// Package template defines adapter interfaces for provider implementations.
package template

import (
	"context"

	"github.com/coachpo/meltica/internal/schema"
)

// ProviderAdapter represents a generic market data provider implementation.
type ProviderAdapter interface {
	Start(ctx context.Context) error
	Events() <-chan *schema.Event
	Errors() <-chan error
}

// AdapterFactory builds provider adapters from configured clients.
type AdapterFactory interface {
	NewProvider(name string, ws WSClient, rest RESTClient, opts ProviderOptions) ProviderAdapter
}

// ProviderOptions define transport configuration shared across adapters.
type ProviderOptions struct {
	Topics    []string
	Snapshots []RESTPoller
}
