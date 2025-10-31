package okx

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/coder/websocket"
	json "github.com/goccy/go-json"
)

const (
	okxControlMessageInterval     = 200 * time.Millisecond
	okxMaxSubscriptionsPerRequest = 20
	okxPingInterval               = 20 * time.Second
	okxPingTimeout                = 5 * time.Second
	okxControlWriteTimeout        = 5 * time.Second
	okxMaxReconnectInterval       = 20 * time.Second
	okxReadLimit                  = 2 * 1024 * 1024
)

type wsArgument struct {
	Channel string `json:"channel"`
	InstID  string `json:"instId,omitempty"`
}

func (a wsArgument) key() string {
	channel := strings.TrimSpace(strings.ToLower(a.Channel))
	inst := strings.TrimSpace(strings.ToUpper(a.InstID))
	return channel + "|" + inst
}

type wsRequest struct {
	ID   string       `json:"id,omitempty"`
	Op   string       `json:"op"`
	Args []wsArgument `json:"args"`
}

type wsEnvelope struct {
	Arg   wsArgument        `json:"arg"`
	Data  []json.RawMessage `json:"data"`
	Event string            `json:"event"`
	Code  string            `json:"code"`
	Msg   string            `json:"msg"`
}

type wsMessageHandler func(wsEnvelope) error

type wsManager struct {
	baseURL string
	ctx     context.Context
	cancel  context.CancelFunc

	conn   *websocket.Conn
	connMu sync.RWMutex

	msgID atomic.Uint64

	subsMu        sync.Mutex
	subscriptions map[string]wsArgument

	handler   wsMessageHandler
	errorChan chan<- error

	ready     chan struct{}
	readyOnce sync.Once

	controlMu       sync.Mutex
	lastControlSend time.Time
}

func newWSManager(ctx context.Context, baseURL string, handler wsMessageHandler, errs chan<- error) *wsManager {
	managerCtx, cancel := context.WithCancel(ctx)
	return &wsManager{
		baseURL:         baseURL,
		ctx:             managerCtx,
		cancel:          cancel,
		conn:            nil,
		connMu:          sync.RWMutex{},
		msgID:           atomic.Uint64{},
		subsMu:          sync.Mutex{},
		subscriptions:   make(map[string]wsArgument),
		handler:         handler,
		errorChan:       errs,
		ready:           make(chan struct{}),
		readyOnce:       sync.Once{},
		controlMu:       sync.Mutex{},
		lastControlSend: time.Time{},
	}
}

func (sm *wsManager) start() error {
	go func() {
		if err := sm.connectLoop(); err != nil && !errors.Is(err, context.Canceled) {
			sm.reportError(fmt.Errorf("okx ws manager: %w", err))
		}
	}()

	select {
	case <-sm.ready:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("timeout waiting for okx websocket connection")
	case <-sm.ctx.Done():
		return fmt.Errorf("okx websocket context done: %w", sm.ctx.Err())
	}
}

func (sm *wsManager) stop() {
	sm.cancel()
	sm.connMu.Lock()
	if sm.conn != nil {
		_ = sm.conn.Close(websocket.StatusNormalClosure, "shutdown")
		sm.conn = nil
	}
	sm.connMu.Unlock()
}

func (sm *wsManager) subscribe(subs []wsArgument) error {
	if len(subs) == 0 {
		return nil
	}
	sm.subsMu.Lock()
	newSubs := make([]wsArgument, 0, len(subs))
	for _, sub := range subs {
		key := sub.key()
		if _, exists := sm.subscriptions[key]; !exists {
			sm.subscriptions[key] = sub
			newSubs = append(newSubs, sub)
		}
	}
	sm.subsMu.Unlock()
	if len(newSubs) == 0 {
		return nil
	}
	return sm.sendBatchedControlRequests(sm.ctx, "subscribe", newSubs)
}

func (sm *wsManager) unsubscribe(subs []wsArgument) error {
	if len(subs) == 0 {
		return nil
	}
	sm.subsMu.Lock()
	removals := make([]wsArgument, 0, len(subs))
	for _, sub := range subs {
		key := sub.key()
		if _, exists := sm.subscriptions[key]; exists {
			delete(sm.subscriptions, key)
			removals = append(removals, sub)
		}
	}
	sm.subsMu.Unlock()
	if len(removals) == 0 {
		return nil
	}
	return sm.sendBatchedControlRequests(sm.ctx, "unsubscribe", removals)
}

func (sm *wsManager) connectLoop() error {
	backoffCfg := backoff.NewExponentialBackOff()
	backoffCfg.MaxInterval = okxMaxReconnectInterval

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
			if sleep == backoff.Stop {
				sleep = okxMaxReconnectInterval
			}
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

		conn.SetReadLimit(okxReadLimit)

		sm.controlMu.Lock()
		sm.lastControlSend = time.Time{}
		sm.controlMu.Unlock()

		sm.readyOnce.Do(func() {
			close(sm.ready)
		})

		backoffCfg.Reset()

		if err := sm.subscribeAll(); err != nil {
			sm.reportError(fmt.Errorf("resubscribe after reconnect: %w", err))
		}

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
			sm.reportError(fmt.Errorf("okx websocket connection loop: %w", aggregatedErr))
		}

		sleep := backoffCfg.NextBackOff()
		if sleep == backoff.Stop {
			sleep = okxMaxReconnectInterval
		}
		select {
		case <-sm.ctx.Done():
			return context.Canceled
		case <-time.After(sleep):
		}
	}
}

func (sm *wsManager) subscribeAll() error {
	sm.subsMu.Lock()
	defer sm.subsMu.Unlock()
	if len(sm.subscriptions) == 0 {
		return nil
	}
	args := make([]wsArgument, 0, len(sm.subscriptions))
	for _, arg := range sm.subscriptions {
		args = append(args, arg)
	}
	return sm.sendBatchedControlRequests(sm.ctx, "subscribe", args)
}

func (sm *wsManager) sendBatchedControlRequests(ctx context.Context, operation string, args []wsArgument) error {
	if len(args) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = sm.ctx
	}

	chunks := chunkArguments(args, okxMaxSubscriptionsPerRequest)
	for _, chunk := range chunks {
		req := wsRequest{
			ID:   fmt.Sprintf("%d", sm.msgID.Add(1)),
			Op:   strings.ToLower(operation),
			Args: chunk,
		}
		data, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal %s request: %w", operation, err)
		}

		sm.controlMu.Lock()
		if err := sm.waitForControlWindowLocked(ctx); err != nil {
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

		writeCtx, cancel := context.WithTimeout(ctx, okxControlWriteTimeout)
		err = conn.Write(writeCtx, websocket.MessageText, data)
		cancel()
		sm.controlMu.Unlock()
		if err != nil {
			return fmt.Errorf("write %s request: %w", operation, err)
		}

		log.Printf("okx ws manager: %s request %+v", operation, req)
	}
	return nil
}

func chunkArguments(args []wsArgument, size int) [][]wsArgument {
	if len(args) == 0 {
		return nil
	}
	if size <= 0 || len(args) <= size {
		snapshot := make([]wsArgument, len(args))
		copy(snapshot, args)
		return [][]wsArgument{snapshot}
	}
	chunks := make([][]wsArgument, 0, (len(args)+size-1)/size)
	for start := 0; start < len(args); start += size {
		end := start + size
		if end > len(args) {
			end = len(args)
		}
		chunk := make([]wsArgument, end-start)
		copy(chunk, args[start:end])
		chunks = append(chunks, chunk)
	}
	return chunks
}

func (sm *wsManager) waitForControlWindowLocked(ctx context.Context) error {
	deadline := sm.lastControlSend.Add(okxControlMessageInterval)
	if time.Now().Before(deadline) {
		wait := time.Until(deadline)
		if wait > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("control window wait canceled: %w", ctx.Err())
			case <-time.After(wait):
			}
		}
	}
	sm.lastControlSend = time.Now()
	return nil
}

func (sm *wsManager) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return context.Canceled
		default:
		}
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read websocket: %w", err)
		}
		trimmed := strings.TrimSpace(string(data))
		if trimmed == "" {
			continue
		}
		if trimmed == "pong" {
			continue
		}
		if strings.Contains(trimmed, "\"event\":\"ping\"") {
			_ = sm.writePong(ctx, conn)
			continue
		}
		var envelope wsEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			sm.reportError(fmt.Errorf("decode websocket message: %w", err))
			continue
		}
		if envelope.Event == "error" {
			sm.reportError(fmt.Errorf("okx websocket error %s: %s", strings.TrimSpace(envelope.Code), strings.TrimSpace(envelope.Msg)))
			continue
		}
		if len(envelope.Data) == 0 {
			continue
		}
		if sm.handler != nil {
			if err := sm.handler(envelope); err != nil {
				sm.reportError(fmt.Errorf("handle websocket message: %w", err))
			}
		}
	}
}

func (sm *wsManager) pingLoop(ctx context.Context, conn *websocket.Conn) error {
	ticker := time.NewTicker(okxPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return context.Canceled
		case <-ticker.C:
			if err := sm.writePing(ctx, conn); err != nil {
				return err
			}
		}
	}
}

func (sm *wsManager) writePing(ctx context.Context, conn *websocket.Conn) error {
	pingPayload := wsRequest{ID: "", Op: "ping", Args: nil}
	data, err := json.Marshal(pingPayload)
	if err != nil {
		return fmt.Errorf("marshal ping: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, okxPingTimeout)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("write ping: %w", err)
	}
	return nil
}

func (sm *wsManager) writePong(ctx context.Context, conn *websocket.Conn) error {
	pongPayload := wsRequest{ID: "", Op: "pong", Args: nil}
	data, err := json.Marshal(pongPayload)
	if err != nil {
		return fmt.Errorf("marshal pong: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, okxPingTimeout)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("write pong: %w", err)
	}
	return nil
}

func (sm *wsManager) reportError(err error) {
	if err == nil {
		return
	}
	select {
	case <-sm.ctx.Done():
	case sm.errorChan <- err:
	default:
	}
}
