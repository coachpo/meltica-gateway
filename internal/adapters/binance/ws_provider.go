package binance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coder/websocket"
	json "github.com/goccy/go-json"
)

// DefaultFrameProvider connects to Binance public websocket endpoints.
type DefaultFrameProvider struct {
	url          string
	timeout      time.Duration
	readTimeout  time.Duration
	pingInterval time.Duration
}

// NewDefaultFrameProvider constructs a websocket frame provider.
func NewDefaultFrameProvider(url string, timeout time.Duration) *DefaultFrameProvider {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &DefaultFrameProvider{
		url:          url,
		timeout:      timeout,
		readTimeout:  100 * time.Millisecond,
		pingInterval: 30 * time.Second,
	}
}

// Subscribe opens a websocket connection and subscribes to the provided topics.
func (p *DefaultFrameProvider) Subscribe(ctx context.Context, topics []string) (<-chan []byte, <-chan error, error) {
	dialCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	conn, _, err := websocket.Dial(dialCtx, p.url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("dial binance ws: %w", err)
	}

	if len(topics) > 0 {
		payload, err := EncodeSubscribeMessage(topics)
		if err != nil {
			closeErr := conn.Close(websocket.StatusInternalError, "encode subscribe")
			wrapped := fmt.Errorf("encode subscribe payload: %w", err)
			if closeErr != nil {
				wrapped = errors.Join(wrapped, fmt.Errorf("close websocket: %w", closeErr))
			}
			return nil, nil, wrapped
		}
		writeCtx, writeCancel := context.WithTimeout(ctx, p.timeout)
		err = conn.Write(writeCtx, websocket.MessageText, payload)
		writeCancel()
		if err != nil {
			closeErr := conn.Close(websocket.StatusPolicyViolation, "subscribe topics")
			wrapped := fmt.Errorf("subscribe topics: %w", err)
			if closeErr != nil {
				wrapped = errors.Join(wrapped, fmt.Errorf("close websocket: %w", closeErr))
			}
			return nil, nil, wrapped
		}
	}

	frames := make(chan []byte)
	errCh := make(chan error, 1)

	go func() {
		defer close(frames)
		defer close(errCh)
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "shutdown")
		}()

		var ticker *time.Ticker
		if p.pingInterval > 0 {
			ticker = time.NewTicker(p.pingInterval)
			defer ticker.Stop()
		}

		for {
			if ctx.Err() != nil {
				return
			}

			if ticker != nil {
				select {
				case <-ticker.C:
					pingCtx, pingCancel := context.WithTimeout(ctx, p.timeout)
					_ = conn.Ping(pingCtx)
					pingCancel()
				default:
				}
			}

			rt := p.readTimeout
			if rt <= 0 {
				rt = 100 * time.Millisecond
			}
			readCtx, readCancel := context.WithTimeout(ctx, rt)
			msgType, message, err := conn.Read(readCtx)
			readCancel()
			if err != nil {
				if errors.Is(err, context.Canceled) && ctx.Err() != nil {
					return
				}
				select {
				case errCh <- err:
				default:
				}
				return
			}

			if msgType != websocket.MessageText && msgType != websocket.MessageBinary {
				continue
			}

			frameCopy := append([]byte(nil), message...)
			select {
			case <-ctx.Done():
				return
			case frames <- frameCopy:
			}
		}
	}()

	return frames, errCh, nil
}

// EncodeSubscribeMessage renders the Binance subscribe request body.
func EncodeSubscribeMessage(topics []string) ([]byte, error) {
	payload := map[string]any{
		"method": "SUBSCRIBE",
		"params": topics,
		"id":     time.Now().UnixNano(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode subscribe message: %w", err)
	}
	return encoded, nil
}
