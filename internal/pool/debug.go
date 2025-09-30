//go:build debug

package pool

import (
	"math"
	"reflect"
	"runtime/debug"
	"sync"
	"time"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/schema"
)

const (
	poisonString = "<<poison>>"
)

type debugState struct {
	name   string
	mu     sync.Mutex
	stacks map[uintptr]string
}

func newDebugState(name string) *debugState {
	return &debugState{
		name:   name,
		stacks: make(map[uintptr]string),
	}
}

func (d *debugState) recordAcquire(obj PooledObject) {
	if d == nil {
		return
	}
	key := pointerKey(obj)
	if key == 0 {
		return
	}
	stack := string(debug.Stack())
	d.mu.Lock()
	d.stacks[key] = stack
	d.mu.Unlock()
}

func (d *debugState) recordRelease(obj PooledObject) {
	if d == nil {
		return
	}
	key := pointerKey(obj)
	if key == 0 {
		return
	}
	d.mu.Lock()
	delete(d.stacks, key)
	d.mu.Unlock()
}

func (d *debugState) activeStacks() []string {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.stacks) == 0 {
		return nil
	}
	out := make([]string, 0, len(d.stacks))
	for _, stack := range d.stacks {
		out = append(out, stack)
	}
	return out
}

func (d *debugState) poison(obj PooledObject) {
	if d == nil || obj == nil {
		return
	}
	switch v := obj.(type) {
	case *schema.WsFrame:
		v.Provider = poisonString
		v.ConnID = poisonString
		v.ReceivedAt = -1
		v.MessageType = -1
		v.Data = append(v.Data[:0], 0xDE, 0xAD, 0xBE, 0xEF)
	case *schema.ProviderRaw:
		v.Provider = poisonString
		v.StreamName = poisonString
		v.ReceivedAt = -1
		v.Payload = json.RawMessage(`"poison"`)
	case *schema.Event:
		v.EventID = poisonString
		if v.MergeID != nil {
			*v.MergeID = poisonString
		}
		v.RoutingVersion = -1
		v.Provider = poisonString
		v.Symbol = poisonString
		v.Type = schema.EventType("POISON")
		v.SeqProvider = math.MaxUint64
		v.IngestTS = time.Unix(1, 0)
		v.EmitTS = time.Unix(1, 0)
		v.Payload = map[string]any{"poison": true}
		v.TraceID = poisonString
	case *schema.MergedEvent:
		v.MergeID = poisonString
		v.Symbol = poisonString
		v.EventType = schema.EventType("POISON")
		v.WindowOpen = -1
		v.WindowClose = -1
		v.Fragments = nil
		v.IsComplete = false
		v.TraceID = poisonString
	case *schema.OrderRequest:
		v.ClientOrderID = poisonString
		v.ConsumerID = poisonString
		v.Provider = poisonString
		v.Symbol = poisonString
		v.Side = schema.TradeSide("POISON")
		v.OrderType = schema.OrderType("POISON")
		price := poisonString
		v.Price = &price
		v.Quantity = poisonString
		v.Timestamp = time.Unix(1, 0)
	case *schema.ExecReport:
		v.ClientOrderID = poisonString
		v.ExchangeOrderID = poisonString
		v.Provider = poisonString
		v.Symbol = poisonString
		v.Status = schema.ExecReportState("POISON")
		v.FilledQty = poisonString
		v.RemainingQty = poisonString
		v.AvgPrice = poisonString
		v.TransactTime = -1
		v.ReceivedAt = -1
		v.TraceID = poisonString
		v.DecisionID = poisonString
	default:
		poisonWithReflection(v)
	}
}

func (d *debugState) clear(obj PooledObject) {
	if d == nil || obj == nil {
		return
	}
	obj.Reset()
}

func poisonWithReflection(obj any) {
	v := reflect.ValueOf(obj)
	if !v.IsValid() || v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	poisonValue(v.Elem())
}

func poisonValue(v reflect.Value) {
	if !v.IsValid() || !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(poisonString)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(-1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v.SetUint(math.MaxUint64)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(math.MaxFloat64)
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
	case reflect.Map:
		v.Set(reflect.MakeMapWithSize(v.Type(), 0))
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			poisonValue(v.Field(i))
		}
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		poisonValue(v.Elem())
	}
}

func pointerKey(obj PooledObject) uintptr {
	if obj == nil {
		return 0
	}
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return 0
	}
	return v.Pointer()
}
