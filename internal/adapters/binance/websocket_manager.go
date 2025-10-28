package binance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/coder/websocket"
	"github.com/goccy/go-json"
)

const (
	// Binance limits control messages (SUBSCRIBE/UNSUBSCRIBE, PING/PONG) to 5 per second per connection.
	// See: https://github.com/binance/binance-spot-api-docs/blob/master/web-socket-streams.md
	binanceControlMessageInterval = 250 * time.Millisecond
	// Keep subscribe payloads modest so we can throttle between them if the stream count is large.
	binanceMaxStreamsPerRequest = 100
)

// streamManager manages a single WebSocket connection with live subscribe/unsubscribe support.
type streamManager struct {
	baseURL string
	ctx     context.Context
	cancel  context.CancelFunc

	conn     *websocket.Conn
	connMu   sync.RWMutex
	msgIDGen atomic.Uint64

	subscriptions map[string]struct{}
	subsMu        sync.Mutex

	handler   func([]byte) error
	errorChan chan<- error

	ready     chan struct{}
	readyOnce sync.Once

	controlMu       sync.Mutex
	lastControlSend time.Time
}

type subscribeRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
	ID     uint64   `json:"id"`
}

type subscribeResponse struct {
	Result *json.RawMessage `json:"result"`
	ID     uint64           `json:"id"`
	Error  *wsError         `json:"error,omitempty"`
}

type wsError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// newStreamManager creates a new stream manager instance.
func newStreamManager(ctx context.Context, baseURL string, handler func([]byte) error, errorChan chan<- error) *streamManager {
	managerCtx, cancel := context.WithCancel(ctx)
	return &streamManager{
		baseURL:       baseURL,
		ctx:           managerCtx,
		cancel:        cancel,
		conn:          nil,
		connMu:        sync.RWMutex{},
		msgIDGen:      atomic.Uint64{},
		subscriptions: make(map[string]struct{}),
		subsMu:        sync.Mutex{},
		handler:       handler,
		errorChan:     errorChan,
		ready:         make(chan struct{}),
		readyOnce:     sync.Once{},
	}
}

// start establishes the WebSocket connection in a background goroutine and waits for the initial connection.
func (sm *streamManager) start() error {
	go func() {
		if err := sm.connect(); err != nil && !errors.Is(err, context.Canceled) {
			sm.reportError(fmt.Errorf("stream manager connection failed: %w", err))
		}
	}()

	// Wait for initial connection with timeout
	select {
	case <-sm.ready:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("timeout waiting for websocket connection")
	case <-sm.ctx.Done():
		return fmt.Errorf("stream manager context done: %w", sm.ctx.Err())
	}
}

// stop closes the WebSocket connection and cancels the context.
func (sm *streamManager) stop() {
	sm.cancel()
	sm.connMu.Lock()
	if sm.conn != nil {
		_ = sm.conn.Close(websocket.StatusNormalClosure, "shutdown")
		sm.conn = nil
	}
	sm.connMu.Unlock()
}

// subscribe adds one or more stream subscriptions.
func (sm *streamManager) subscribe(streams []string) error {
	if len(streams) == 0 {
		return nil
	}

	sm.subsMu.Lock()
	newStreams := make([]string, 0, len(streams))
	for _, stream := range streams {
		if _, exists := sm.subscriptions[stream]; !exists {
			newStreams = append(newStreams, stream)
			sm.subscriptions[stream] = struct{}{}
		}
	}
	sm.subsMu.Unlock()

	if len(newStreams) == 0 {
		return nil // All streams already subscribed
	}

	return sm.sendBatchedControlRequests("SUBSCRIBE", newStreams)
}

// unsubscribe removes one or more stream subscriptions.
// NOTE: Currently unused in favor of persistent subscriptions to support multi-lambda scenarios.
// Kept for potential future use (e.g., resource optimization, testing, or explicit cleanup).
func (sm *streamManager) unsubscribe(streams []string) error {
	if len(streams) == 0 {
		return nil
	}

	sm.subsMu.Lock()
	existingStreams := make([]string, 0, len(streams))
	for _, stream := range streams {
		if _, exists := sm.subscriptions[stream]; exists {
			existingStreams = append(existingStreams, stream)
			delete(sm.subscriptions, stream)
		}
	}
	sm.subsMu.Unlock()

	if len(existingStreams) == 0 {
		return nil // No streams to unsubscribe
	}

	return sm.sendBatchedControlRequests("UNSUBSCRIBE", existingStreams)
}

// connect maintains the WebSocket connection with automatic reconnection and exponential backoff.
func (sm *streamManager) connect() error {
	backoffCfg := backoff.NewExponentialBackOff()

	for {
		select {
		case <-sm.ctx.Done():
			return context.Canceled
		default:
		}

		conn, _, err := websocket.Dial(sm.ctx, sm.baseURL, nil)
		if err != nil {
			sm.reportError(fmt.Errorf("dial %s: %w", sm.baseURL, err))
			sleep := backoffCfg.NextBackOff()
			select {
			case <-sm.ctx.Done():
				return context.Canceled
			case <-time.After(sleep):
				continue
			}
		}

		sm.connMu.Lock()
		sm.conn = conn
		sm.connMu.Unlock()

		// Signal ready on first successful connection
		sm.readyOnce.Do(func() {
			close(sm.ready)
		})

		backoffCfg.Reset()

		// Resubscribe to all active streams after reconnection
		if err := sm.subscribeAll(); err != nil {
			sm.reportError(fmt.Errorf("resubscribe after reconnect: %w", err))
		}

		// Start read loop
		if err := sm.readLoop(conn); err != nil {
			if errors.Is(err, context.Canceled) {
				return context.Canceled
			}
			sm.reportError(fmt.Errorf("read loop: %w", err))
		}

		sm.connMu.Lock()
		sm.conn = nil
		sm.connMu.Unlock()

		// Reconnect with backoff
		sleep := backoffCfg.NextBackOff()
		select {
		case <-sm.ctx.Done():
			return context.Canceled
		case <-time.After(sleep):
		}
	}
}

// subscribeAll sends a bulk SUBSCRIBE request for all active subscriptions.
// This is called after reconnection to restore the subscription state.
func (sm *streamManager) subscribeAll() error {
	sm.subsMu.Lock()
	streams := make([]string, 0, len(sm.subscriptions))
	for stream := range sm.subscriptions {
		streams = append(streams, stream)
	}
	sm.subsMu.Unlock()

	if len(streams) == 0 {
		return nil
	}

	return sm.sendBatchedControlRequests("SUBSCRIBE", streams)
}

func (sm *streamManager) sendBatchedControlRequests(method string, streams []string) error {
	if len(streams) == 0 {
		return nil
	}

	sm.controlMu.Lock()
	defer sm.controlMu.Unlock()

	sm.connMu.RLock()
	conn := sm.conn
	sm.connMu.RUnlock()
	if conn == nil {
		return errors.New("websocket not connected")
	}

	chunks := chunkStreams(streams, binanceMaxStreamsPerRequest)
	for _, chunk := range chunks {
		if err := sm.waitForControlWindowLocked(method); err != nil {
			return err
		}

		req := subscribeRequest{
			Method: method,
			Params: chunk,
			ID:     sm.msgIDGen.Add(1),
		}

		data, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal %s request: %w", method, err)
		}

		writeCtx, cancel := context.WithTimeout(sm.ctx, 5*time.Second)
		err = conn.Write(writeCtx, websocket.MessageText, data)
		cancel()
		if err != nil {
			return fmt.Errorf("write %s request: %w", method, err)
		}

		sm.lastControlSend = time.Now()
	}

	return nil
}

func chunkStreams(streams []string, size int) [][]string {
	if len(streams) == 0 {
		return nil
	}

	if size <= 0 || len(streams) <= size {
		snapshot := make([]string, len(streams))
		copy(snapshot, streams)
		return [][]string{snapshot}
	}

	chunks := make([][]string, 0, (len(streams)+size-1)/size)
	for start := 0; start < len(streams); start += size {
		end := start + size
		if end > len(streams) {
			end = len(streams)
		}
		chunk := make([]string, end-start)
		copy(chunk, streams[start:end])
		chunks = append(chunks, chunk)
	}
	return chunks
}

func (sm *streamManager) waitForControlWindowLocked(method string) error {
	if sm.lastControlSend.IsZero() {
		return nil
	}

	waitUntil := sm.lastControlSend.Add(binanceControlMessageInterval)
	wait := time.Until(waitUntil)
	if wait <= 0 {
		return nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-sm.ctx.Done():
		return fmt.Errorf("context done while pacing %s requests: %w", method, sm.ctx.Err())
	}
}

// readLoop continuously reads messages from the WebSocket connection.
// It distinguishes between control messages (subscribe/unsubscribe responses) and stream data.
func (sm *streamManager) readLoop(conn *websocket.Conn) error {
	for {
		msgType, data, err := conn.Read(sm.ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return context.Canceled
			}
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return fmt.Errorf("read: %w", err)
		}

		if msgType != websocket.MessageText {
			continue
		}

		// Check if this is a response to a subscribe/unsubscribe request
		var resp subscribeResponse
		if err := json.Unmarshal(data, &resp); err == nil && resp.ID > 0 {
			if resp.Error != nil {
				sm.reportError(fmt.Errorf("websocket error (id=%d): code=%d, msg=%s", resp.ID, resp.Error.Code, resp.Error.Msg))
			}
			continue // Skip processing control messages
		}

		// Handle stream data
		if sm.handler != nil {
			if err := sm.handler(data); err != nil {
				sm.reportError(fmt.Errorf("handle message: %w", err))
			}
		}
	}
}

func (sm *streamManager) reportError(err error) {
	if err == nil || sm.errorChan == nil {
		return
	}
	select {
	case <-sm.ctx.Done():
	case sm.errorChan <- err:
	default:
	}
}
