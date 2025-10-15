// Package snapshot provides in-memory storage for canonical snapshots.
package snapshot

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/errs"
)

// MemoryStore is an in-memory implementation of the snapshot Store.
type MemoryStore struct {
	mu         sync.RWMutex
	records    map[Key]*entry
	shutdown   chan struct{}
	casRetries atomic.Uint64
}

type entry struct {
	mu     sync.Mutex
	record Record
}

// NewMemoryStore creates a memory-backed snapshot store.
func NewMemoryStore() *MemoryStore {
	store := new(MemoryStore)
	store.records = make(map[Key]*entry)
	store.shutdown = make(chan struct{})
	go store.sweepExpired()
	return store
}

// Get returns the current snapshot for the provided key.
func (s *MemoryStore) Get(ctx context.Context, key Key) (Record, error) {
	if err := key.Validate(); err != nil {
		return Record{}, err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return Record{}, fmt.Errorf("memory store get context: %w", ctx.Err())
		default:
		}
	}
	s.mu.RLock()
	e, ok := s.records[key]
	s.mu.RUnlock()
	if !ok {
		return Record{}, errs.New("snapshot/not-found", errs.CodeNotFound, errs.WithMessage("snapshot not found"))
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.record.Clone()
	if isExpired(rec) {
		markStale(&rec)
	}
	return rec, nil
}

// Put stores a new snapshot, initialising the version counter if necessary.
func (s *MemoryStore) Put(ctx context.Context, record Record) (Record, error) {
	if err := record.Key.Validate(); err != nil {
		return Record{}, err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return Record{}, fmt.Errorf("memory store put context: %w", ctx.Err())
		default:
		}
	}
	s.mu.Lock()
	e, exists := s.records[record.Key]
	if !exists {
		e = new(entry)
		s.records[record.Key] = e
	}
	s.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	record.Version = 1
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	e.record = record.Clone()
	return e.record.Clone(), nil
}

// CompareAndSwap replaces the snapshot if the previous version matches.
func (s *MemoryStore) CompareAndSwap(ctx context.Context, prevVersion uint64, record Record) (Record, error) {
	if err := record.Key.Validate(); err != nil {
		return Record{}, err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return Record{}, fmt.Errorf("memory store cas context: %w", ctx.Err())
		default:
		}
	}
	s.mu.RLock()
	e, ok := s.records[record.Key]
	s.mu.RUnlock()
	if !ok {
		return Record{}, errs.New("snapshot/not-found", errs.CodeNotFound, errs.WithMessage("snapshot not found"))
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.record.Version != prevVersion {
		s.casRetries.Add(1)
		return Record{}, errs.New("snapshot/conflict", errs.CodeConflict, errs.WithMessage("version mismatch"))
	}
	record.Version = prevVersion + 1
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	e.record = record.Clone()
	return e.record.Clone(), nil
}

// Close stops background maintenance routines.
func (s *MemoryStore) Close() {
	close(s.shutdown)
}

func (s *MemoryStore) sweepExpired() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.shutdown:
			return
		case <-ticker.C:
			s.pruneExpired()
		}
	}
}

func (s *MemoryStore) pruneExpired() {
	now := time.Now().UTC()
	s.mu.Lock()
	for key, entry := range s.records {
		entry.mu.Lock()
		rec := entry.record
		entry.mu.Unlock()
		if rec.TTL <= 0 {
			continue
		}
		if rec.UpdatedAt.Add(rec.TTL).Before(now) {
			delete(s.records, key)
		}
	}
	s.mu.Unlock()
}

func isExpired(record Record) bool {
	if record.TTL <= 0 {
		return false
	}
	return record.UpdatedAt.Add(record.TTL).Before(time.Now().UTC())
}

func markStale(record *Record) {
	if record.Data == nil {
		record.Data = make(map[string]any)
	}
	record.Data["stale"] = true
}
