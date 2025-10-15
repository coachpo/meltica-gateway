// Package snapshot defines canonical snapshot storage primitives.
package snapshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/errs"
	"github.com/coachpo/meltica/internal/schema"
)

// Key identifies a snapshot record.
type Key struct {
	Market     string
	Instrument string
	Type       schema.CanonicalType
}

// Record represents a canonical snapshot entry.
type Record struct {
	Key       Key
	Seq       uint64
	Version   uint64
	Data      map[string]any
	UpdatedAt time.Time
	TTL       time.Duration
}

// Store defines the snapshot store contract.
type Store interface {
	Get(ctx context.Context, key Key) (Record, error)
	Put(ctx context.Context, record Record) (Record, error)
	CompareAndSwap(ctx context.Context, prevVersion uint64, record Record) (Record, error)
}

// Validate ensures the key conforms to canonical rules.
func (k Key) Validate() error {
	if strings.TrimSpace(k.Market) == "" {
		return errs.New("snapshot/key", errs.CodeInvalid, errs.WithMessage("market required"))
	}
	if err := schema.ValidateInstrument(k.Instrument); err != nil {
		return fmt.Errorf("validate instrument: %w", err)
	}
	if err := k.Type.Validate(); err != nil {
		return fmt.Errorf("validate canonical type: %w", err)
	}
	return nil
}

// Clone returns a deep copy of the record payload.
func (r Record) Clone() Record {
	clone := r
	if r.Data != nil {
		clone.Data = make(map[string]any, len(r.Data))
		for k, v := range r.Data {
			clone.Data[k] = v
		}
	} else {
		// Initialize empty map instead of leaving nil
		clone.Data = make(map[string]any)
	}
	return clone
}
