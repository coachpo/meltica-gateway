// Package async provides bounded worker pool utilities.
package async

import (
	"context"
	"fmt"
	"sync"

	"github.com/coachpo/meltica/errs"
)

// Task represents a unit of work executed by the pool workers.
type Task func(context.Context) error

// Pool defines a bounded worker pool enforcing backpressure when saturated.
type Pool struct {
	ctx    context.Context
	cancel context.CancelFunc
	jobs   chan job
	wg     sync.WaitGroup
	once   sync.Once
}

type job struct {
	ctx context.Context
	fn  Task
}

// NewPool creates a worker pool with the given concurrency and queue depth.
func NewPool(workers, queue int) (*Pool, error) {
	if workers <= 0 {
		return nil, errs.New("lib/async", errs.CodeInvalid, errs.WithMessage("workers must be >0"))
	}
	if queue < 0 {
		queue = 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := new(Pool)
	p.ctx = ctx
	p.cancel = cancel
	p.jobs = make(chan job, queue)
	for i := 0; i < workers; i++ {
		go p.worker()
	}
	return p, nil
}

// Submit schedules the provided task for execution respecting pool backpressure.
func (p *Pool) Submit(ctx context.Context, fn Task) error {
	if fn == nil {
		return errs.New("lib/async", errs.CodeInvalid, errs.WithMessage("task must not be nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.wg.Add(1)
	select {
	case <-p.ctx.Done():
		p.wg.Done()
		return errs.New("lib/async", errs.CodeUnavailable, errs.WithMessage("pool closed"))
	case <-ctx.Done():
		p.wg.Done()
		return fmt.Errorf("submit context: %w", ctx.Err())
	case p.jobs <- job{ctx: ctx, fn: fn}:
		return nil
	default:
		p.wg.Done()
		return errs.New("lib/async", errs.CodeUnavailable, errs.WithMessage("pool at capacity"))
	}
}

// Close stops accepting new tasks and cancels workers.
func (p *Pool) Close() {
	p.once.Do(func() {
		p.cancel()
		close(p.jobs)
	})
}

// Shutdown waits for in-flight tasks to complete or until the context expires.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.Close()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return fmt.Errorf("shutdown context: %w", ctx.Err())
	case <-done:
		return nil
	}
}

func (p *Pool) worker() {
	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobs:
			if !ok {
				return
			}
			ctx := job.ctx
			if ctx == nil {
				ctx = p.ctx
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						// swallow panics to keep worker alive; rely on upstream telemetry for diagnostics.
						_ = r
					}
				}()
				if err := job.fn(ctx); err != nil {
					// Task errors are propagated to caller via context; swallow to keep worker running.
					_ = err
				}
			}()
			p.wg.Done()
		}
	}
}
