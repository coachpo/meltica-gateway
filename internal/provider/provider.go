package provider

import (
	"context"

	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/schema"
)

// Instance represents a runtime provider with event streaming capabilities.
type Instance interface {
	Name() string
	Start(ctx context.Context) error
	Events() <-chan *schema.Event
	Errors() <-chan error
	SubmitOrder(ctx context.Context, req schema.OrderRequest) error
	SubscribeRoute(route dispatcher.Route) error
	UnsubscribeRoute(typ schema.CanonicalType) error
	Instruments() []schema.Instrument
}
