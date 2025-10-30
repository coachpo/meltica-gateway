package binance

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
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
	// Keepalive and reconnection tuning.
	binancePingInterval         = 30 * time.Second
	binancePingTimeout          = 5 * time.Second
	binanceControlWriteTimeout  = 5 * time.Second
	binanceMaxReconnectInterval = 30 * time.Second
	binanceReadLimit            = 2 * 1024 * 1024
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
	metrics         *streamMetrics
	streamName      string
	providerName    string
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
func newStreamManager(ctx context.Context, baseURL string, handler func([]byte) error, errorChan chan<- error, stream, providerName string) *streamManager {
	managerCtx, cancel := context.WithCancel(ctx)
	normalizedProvider := strings.TrimSpace(providerName)
	if normalizedProvider == "" {
		normalizedProvider = binanceMetadata.identifier
	}
	return &streamManager{
		baseURL:         baseURL,
		ctx:             managerCtx,
		cancel:          cancel,
		conn:            nil,
		connMu:          sync.RWMutex{},
		msgIDGen:        atomic.Uint64{},
		subscriptions:   make(map[string]struct{}),
		subsMu:          sync.Mutex{},
		handler:         handler,
		errorChan:       errorChan,
		ready:           make(chan struct{}),
		readyOnce:       sync.Once{},
		controlMu:       sync.Mutex{},
		lastControlSend: time.Time{},
		metrics:         newStreamMetrics(normalizedProvider, stream),
		streamName:      stream,
		providerName:    normalizedProvider,
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

	if sm.metrics != nil {
		sm.metrics.adjustSubscriptions(sm.ctx, len(newStreams))
	}

	return sm.sendBatchedControlRequests(sm.ctx, "SUBSCRIBE", newStreams)
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

	if sm.metrics != nil {
		sm.metrics.adjustSubscriptions(sm.ctx, -len(existingStreams))
	}

	return sm.sendBatchedControlRequests(sm.ctx, "UNSUBSCRIBE", existingStreams)
}

// connect maintains the WebSocket connection with automatic reconnection and exponential backoff.
func (sm *streamManager) connect() error {
	backoffCfg := backoff.NewExponentialBackOff()
	backoffCfg.MaxInterval = binanceMaxReconnectInterval

	// Persistently attempt to keep a single websocket session alive until the parent context terminates.
	// The loop dials, replays subscriptions, and coordinates reader/pinger goroutines for each session.
	for {
		select {
		case <-sm.ctx.Done():
			return context.Canceled
		default:
		}

		conn, _, err := websocket.Dial(sm.ctx, sm.baseURL, nil)
		if err != nil {
			if sm.metrics != nil {
				sm.metrics.recordReconnect(sm.ctx, "error")
			}
			sm.reportError(fmt.Errorf("dial %s: %w", sm.baseURL, err))
			sleep := backoffCfg.NextBackOff()
			if sleep == backoff.Stop {
				sleep = binanceMaxReconnectInterval
			}
			select {
			case <-sm.ctx.Done():
				return context.Canceled
			case <-time.After(sleep):
				continue
			}
		}

		if sm.metrics != nil {
			sm.metrics.recordReconnect(sm.ctx, "success")
		}

		sm.connMu.Lock()
		sm.conn = conn
		sm.connMu.Unlock()

		conn.SetReadLimit(binanceReadLimit)

		sm.controlMu.Lock()
		sm.lastControlSend = time.Time{}
		sm.controlMu.Unlock()

		// Signal ready on first successful connection
		sm.readyOnce.Do(func() {
			close(sm.ready)
		})

		backoffCfg.Reset()

		// Resubscribe to all active streams after reconnection
		if err := sm.subscribeAll(); err != nil {
			sm.reportError(fmt.Errorf("resubscribe after reconnect: %w", err))
		}

		// Each connection instance runs isolated read and ping loops that can cancel one another.
		connCtx, connCancel := context.WithCancel(sm.ctx)
		errCh := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			errCh <- sm.readLoop(connCtx, conn)
		}()

		go func() {
			defer wg.Done()
			errCh <- sm.pingLoop(connCtx, conn)
		}()

		firstErr := <-errCh
		connCancel()

		sm.connMu.Lock()
		if sm.conn == conn {
			sm.conn = nil
		}
		sm.connMu.Unlock()

		_ = conn.Close(websocket.StatusNormalClosure, "")

		wg.Wait()
		close(errCh)

		aggregatedErr := firstErr
		for e := range errCh {
			if aggregatedErr == nil || errors.Is(aggregatedErr, context.Canceled) || errors.Is(aggregatedErr, context.DeadlineExceeded) {
				aggregatedErr = e
			}
		}

		if aggregatedErr != nil && !errors.Is(aggregatedErr, context.Canceled) && !errors.Is(aggregatedErr, context.DeadlineExceeded) {
			sm.reportError(fmt.Errorf("connection loop: %w", aggregatedErr))
		}

		sleep := backoffCfg.NextBackOff()
		if sleep == backoff.Stop {
			sleep = binanceMaxReconnectInterval
		}
		// Back off before re-dialing to avoid hammering Binance when transient faults occur.
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

	return sm.sendBatchedControlRequests(sm.ctx, "SUBSCRIBE", streams)
}

func (sm *streamManager) sendBatchedControlRequests(ctx context.Context, method string, streams []string) error {
	if len(streams) == 0 {
		return nil
	}

	if ctx == nil {
		ctx = sm.ctx
	}

	chunks := chunkStreams(streams, binanceMaxStreamsPerRequest)
	for _, chunk := range chunks {
		req := subscribeRequest{
			Method: method,
			Params: chunk,
			ID:     sm.msgIDGen.Add(1),
		}

		data, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal %s request: %w", method, err)
		}

		sm.controlMu.Lock()
		if err := sm.waitForControlWindowLocked(ctx, method); err != nil {
			sm.controlMu.Unlock()
			return err
		}

		sm.connMu.RLock()
		conn := sm.conn
		sm.connMu.RUnlock()
		if conn == nil {
			sm.controlMu.Unlock()
			return nil
		}

		writeCtx, cancel := context.WithTimeout(ctx, binanceControlWriteTimeout)
		err = conn.Write(writeCtx, websocket.MessageText, data)
		cancel()
		if err != nil {
			sm.controlMu.Unlock()
			return fmt.Errorf("write %s request: %w", method, err)
		}

		sm.lastControlSend = time.Now()
		sm.controlMu.Unlock()

		if sm.metrics != nil {
			sm.metrics.recordControl(ctx, method, len(chunk))
		}

		log.Printf("binance stream manager [%s/%s]: %s request %+v", sm.providerName, sm.streamName, method, req)
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

func (sm *streamManager) waitForControlWindowLocked(ctx context.Context, method string) error {
	if ctx == nil {
		ctx = sm.ctx
	}

	// Binance enforces a strict control-frame rate limit; pace outbound requests accordingly.
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
	case <-ctx.Done():
		return fmt.Errorf("context done while pacing %s requests: %w", method, ctx.Err())
	case <-sm.ctx.Done():
		return fmt.Errorf("context done while pacing %s requests: %w", method, sm.ctx.Err())
	}
}

// pingLoop periodically sends ping control frames to keep the connection alive and detect stale sockets.
func (sm *streamManager) pingLoop(ctx context.Context, conn *websocket.Conn) error {
	ticker := time.NewTicker(binancePingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ping loop context done: %w", ctx.Err())
		case <-ticker.C:
			// Serialize pings with other control messages so we respect Binance control budgets.
			sm.controlMu.Lock()
			err := sm.waitForControlWindowLocked(ctx, "PING")
			if err != nil {
				sm.controlMu.Unlock()
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return context.Canceled
				}
				return err
			}

			if sm.metrics != nil {
				sm.metrics.recordControl(ctx, "PING", 1)
			}

			pingCtx, cancel := context.WithTimeout(ctx, binancePingTimeout)
			start := time.Now()
			err = conn.Ping(pingCtx)
			cancel()
			if sm.metrics != nil {
				result := "success"
				if err != nil {
					result = "error"
				}
				sm.metrics.recordPing(ctx, time.Since(start), result)
			}
			if err != nil {
				sm.controlMu.Unlock()
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return context.Canceled
				}
				if errors.Is(err, net.ErrClosed) {
					return context.Canceled
				}
				if status := websocket.CloseStatus(err); status != -1 {
					return fmt.Errorf("ping: remote closed with status %d", status)
				}
				return fmt.Errorf("ping: %w", err)
			}

			sm.lastControlSend = time.Now()
			sm.controlMu.Unlock()
		}
	}
}

// readLoop continuously reads messages from the WebSocket connection.
// It distinguishes between control messages (subscribe/unsubscribe responses) and stream data.
func (sm *streamManager) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return context.Canceled
			}
			if errors.Is(err, net.ErrClosed) {
				return context.Canceled
			}
			// Surface remote close codes for observability; otherwise bubble the transport error.
			if status := websocket.CloseStatus(err); status != -1 {
				if status == websocket.StatusNormalClosure {
					return context.Canceled
				}
				return fmt.Errorf("read: remote closed with status %d", status)
			}
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
			if sm.metrics != nil {
				sm.metrics.recordMessage(ctx, len(data))
			}
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
	if sm.providerName != "" || sm.streamName != "" {
		err = fmt.Errorf("binance stream manager [%s/%s]: %w", sm.providerName, sm.streamName, err)
	}
	select {
	case <-sm.ctx.Done():
	case sm.errorChan <- err:
	default:
	}
}
