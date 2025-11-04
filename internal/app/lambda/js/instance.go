package js

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// Instance represents an isolated Goja VM for a strategy module.
type Instance struct {
	module *Module
	rt     *goja.Runtime
	export *goja.Object
	queue  chan func(*goja.Runtime)
	wg     sync.WaitGroup
	mu     sync.RWMutex
	closed bool
	once   sync.Once
}

// NewInstance creates an isolated runtime for the provided module.
func NewInstance(module *Module) (*Instance, error) {
	if module == nil {
		return nil, fmt.Errorf("strategy instance: module required")
	}
	rt := goja.New()
	export, err := runModule(rt, module.Program)
	if err != nil {
		return nil, fmt.Errorf("strategy instance: execute %s: %w", module.Path, err)
	}
	instance := &Instance{
		module: module,
		rt:     rt,
		export: export,
		queue:  make(chan func(*goja.Runtime)),
		wg:     sync.WaitGroup{},
		mu:     sync.RWMutex{},
		closed: false,
		once:   sync.Once{},
	}
	instance.wg.Add(1)
	go instance.loop()
	return instance, nil
}

func (i *Instance) loop() {
	defer i.wg.Done()
	for cb := range i.queue {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					// goja panics when JS throws - propagate as runtime panic so callers can surface.
					panic(rec)
				}
			}()
			cb(i.rt)
		}()
	}
}

// Execute runs the provided function on the instance goroutine.
func (i *Instance) Execute(fn func(rt *goja.Runtime, exports *goja.Object) (goja.Value, error)) (goja.Value, error) {
	if i == nil {
		return nil, fmt.Errorf("strategy instance: nil receiver")
	}
	if fn == nil {
		return nil, fmt.Errorf("strategy instance: callback required")
	}

	wait := make(chan result, 1)

	i.mu.RLock()
	if i.closed {
		i.mu.RUnlock()
		return nil, fmt.Errorf("strategy instance: closed")
	}
	i.queue <- func(rt *goja.Runtime) {
		val, err := fn(rt, i.export)
		wait <- result{value: val, err: err}
	}
	i.mu.RUnlock()

	outcome := <-wait
	return outcome.value, outcome.err
}

// Call invokes the named export with the provided arguments on the instance goroutine.
func (i *Instance) Call(function string, args ...any) (goja.Value, error) {
	if i == nil {
		return nil, fmt.Errorf("strategy instance: nil receiver")
	}
	fn := strings.TrimSpace(function)
	if fn == "" {
		return nil, fmt.Errorf("strategy instance: function name required")
	}

	return i.Execute(func(rt *goja.Runtime, exports *goja.Object) (goja.Value, error) {
		value := exports.Get(fn)
		if goja.IsUndefined(value) || goja.IsNull(value) {
			return nil, ErrFunctionMissing
		}
		callable, ok := goja.AssertFunction(value)
		if !ok {
			return nil, fmt.Errorf("strategy instance: export %q not callable", fn)
		}
		params := make([]goja.Value, len(args))
		for idx, arg := range args {
			params[idx] = rt.ToValue(arg)
		}
		res, err := callable(goja.Undefined(), params...)
		if err != nil {
			return nil, err
		}
		return res, nil
	})
}

// CallMethod invokes a method on the provided object within the instance goroutine.
func (i *Instance) CallMethod(target *goja.Object, method string, args ...any) (goja.Value, error) {
	if i == nil {
		return nil, fmt.Errorf("strategy instance: nil receiver")
	}
	if target == nil {
		return nil, fmt.Errorf("strategy instance: target required")
	}
	name := strings.TrimSpace(method)
	if name == "" {
		return nil, fmt.Errorf("strategy instance: method name required")
	}
	return i.Execute(func(rt *goja.Runtime, _ *goja.Object) (goja.Value, error) {
		value := target.Get(name)
		if goja.IsUndefined(value) || goja.IsNull(value) {
			return nil, ErrFunctionMissing
		}
		callable, ok := goja.AssertFunction(value)
		if !ok {
			return nil, fmt.Errorf("strategy instance: method %q not callable", name)
		}
		params := make([]goja.Value, len(args))
		for idx, arg := range args {
			params[idx] = rt.ToValue(arg)
		}
		return callable(target, params...)
	})
}

// Close stops the instance goroutine and releases resources.
func (i *Instance) Close() {
	if i == nil {
		return
	}
	i.once.Do(func() {
		i.mu.Lock()
		if i.closed {
			i.mu.Unlock()
			return
		}
		i.closed = true
		close(i.queue)
		i.mu.Unlock()
		i.wg.Wait()
	})
}

type result struct {
	value goja.Value
	err   error
}
