package binance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// FrameProvider subscribes to Binance websocket topics.
type FrameProvider interface {
	Subscribe(ctx context.Context, topics []string) (<-chan []byte, <-chan error, error)
}

// WSParser converts websocket frames into canonical events.
type WSParser interface {
	Parse(ctx context.Context, frame []byte, ingestTS time.Time) ([]*schema.Event, error)
}

// WSClient consumes websocket frames and emits canonical events.
type WSClient struct {
	providerName string
	provider     FrameProvider
	parser       WSParser
	clock        func() time.Time
	pools        *pool.PoolManager
}

// NewWSClient creates a websocket client with the supplied provider and parser.
func NewWSClient(providerName string, provider FrameProvider, parser WSParser, clock func() time.Time, pools *pool.PoolManager) *WSClient {
	if clock == nil {
		clock = time.Now
	}
	return &WSClient{providerName: providerName, provider: provider, parser: parser, clock: clock, pools: pools}
}

// Stream subscribes to the given topics and returns canonical events and error notifications.
func (c *WSClient) Stream(ctx context.Context, topics []string) (<-chan *schema.Event, <-chan error) {
	events := make(chan *schema.Event)
	errs := make(chan error, 4)

	frames, providerErrs, err := c.provider.Subscribe(ctx, topics)
	if err != nil {
		go func() {
			defer close(events)
			defer close(errs)
			errs <- err
		}()
		return events, errs
	}

	go func() {
		defer close(events)
		defer close(errs)
		for {
			select {
			case <-ctx.Done():
				return
			case frame, ok := <-frames:
				if !ok {
					frames = nil
					if providerErrs == nil {
						return
					}
					continue
				}
				c.handleFrame(ctx, events, errs, frame)
			case err, ok := <-providerErrs:
				if !ok {
					providerErrs = nil
					if frames == nil {
						return
					}
					continue
				}
				if err != nil {
					select {
					case errs <- err:
					default:
					}
				}
			}
		}
	}()

	return events, errs
}

func (c *WSClient) handleFrame(ctx context.Context, events chan<- *schema.Event, errs chan<- error, payload []byte) {
	ingestTS := c.clock().UTC()
	frameCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	wsFrame, release, err := c.acquireWsFrame(frameCtx)
	cancel()
	if err != nil {
		select {
		case errs <- err:
		default:
		}
		return
	}
	if release != nil {
		defer release()
	}
	if wsFrame == nil {
		return
	}

	wsFrame.Provider = c.providerName
	wsFrame.ReceivedAt = ingestTS.UnixNano()
	wsFrame.MessageType = 0
	wsFrame.Data = append(wsFrame.Data[:0], payload...)

	parsed, err := c.parser.Parse(ctx, wsFrame.Data, ingestTS)
	if err != nil {
		select {
		case errs <- err:
		default:
		}
		return
	}

	for _, evt := range parsed {
		if evt == nil {
			continue
		}
		
		if evt.IngestTS.IsZero() {
			evt.IngestTS = ingestTS
		}
		if evt.EmitTS.IsZero() {
			evt.EmitTS = ingestTS
		}
		select {
		case <-ctx.Done():
			return
		case events <- evt:
		}
	}
}

func (c *WSClient) acquireWsFrame(ctx context.Context) (*schema.WsFrame, func(), error) {
	if c.pools == nil {
		frame := new(schema.WsFrame)
		return frame, func() {}, nil
	}
	obj, err := c.pools.Get(ctx, "WsFrame")
	if err != nil {
		return nil, nil, fmt.Errorf("acquire ws frame: %w", err)
	}
	frame, ok := obj.(*schema.WsFrame)
	if !ok {
		c.pools.Put("WsFrame", obj)
		return nil, nil, errors.New("ws frame pool returned unexpected type")
	}
	return frame, func() { c.pools.Put("WsFrame", frame) }, nil
}
