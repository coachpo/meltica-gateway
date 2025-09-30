// Package schema defines canonical event types and helper utilities.
package schema

// CloneEvent creates a deep copy of the provided event suitable for fan-out
// delivery. The returned event is detached from any pool ownership semantics
// and includes deep copies of mutable payload structures.
func CloneEvent(evt *Event) *Event {
	if evt == nil {
		return nil
	}

	clone := *evt
	clone.returned = false

	if evt.MergeID != nil {
		merge := *evt.MergeID
		clone.MergeID = &merge
	}

	clone.Payload = clonePayload(evt.Payload)
	return &clone
}

func clonePayload(payload any) any {
	switch v := payload.(type) {
	case nil:
		return nil
	case BookUpdatePayload:
		return cloneBookUpdatePayload(v)
	case *BookUpdatePayload:
		if v == nil {
			return nil
		}
		cloned := cloneBookUpdatePayload(*v)
		return &cloned
	case BookSnapshotPayload:
		return cloneBookSnapshotPayload(v)
	case *BookSnapshotPayload:
		if v == nil {
			return nil
		}
		cloned := cloneBookSnapshotPayload(*v)
		return &cloned
	case TradePayload:
		return v
	case *TradePayload:
		if v == nil {
			return nil
		}
		cloned := *v
		return &cloned
	case TickerPayload:
		return v
	case *TickerPayload:
		if v == nil {
			return nil
		}
		cloned := *v
		return &cloned
	case ExecReportPayload:
		return v
	case *ExecReportPayload:
		if v == nil {
			return nil
		}
		cloned := *v
		return &cloned
	case []byte:
		return append([]byte(nil), v...)
	case map[string]any:
		return cloneMapStringAny(v)
	case *MergedEvent:
		return cloneMergedEvent(v)
	case MergedEvent:
		cloned := cloneMergedEvent(&v)
		if cloned == nil {
			var emptyMerged MergedEvent
			return emptyMerged
		}
		return *cloned
	default:
		return v
	}
}

func cloneBookUpdatePayload(payload BookUpdatePayload) BookUpdatePayload {
	cloned := payload
	if len(payload.Bids) > 0 {
		cloned.Bids = clonePriceLevels(payload.Bids)
	}
	if len(payload.Asks) > 0 {
		cloned.Asks = clonePriceLevels(payload.Asks)
	}
	return cloned
}

func cloneBookSnapshotPayload(payload BookSnapshotPayload) BookSnapshotPayload {
	cloned := payload
	if len(payload.Bids) > 0 {
		cloned.Bids = clonePriceLevels(payload.Bids)
	}
	if len(payload.Asks) > 0 {
		cloned.Asks = clonePriceLevels(payload.Asks)
	}
	return cloned
}

func clonePriceLevels(levels []PriceLevel) []PriceLevel {
	if len(levels) == 0 {
		return nil
	}
	out := make([]PriceLevel, len(levels))
	copy(out, levels)
	return out
}

func cloneMapStringAny(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = cloneInterface(v)
	}
	return out
}

func cloneInterface(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return append([]byte(nil), v...)
	case map[string]any:
		return cloneMapStringAny(v)
	case []any:
		return cloneSliceAny(v)
	case BookUpdatePayload:
		return cloneBookUpdatePayload(v)
	case *BookUpdatePayload:
		if v == nil {
			return nil
		}
		cloned := cloneBookUpdatePayload(*v)
		return &cloned
	case BookSnapshotPayload:
		return cloneBookSnapshotPayload(v)
	case *BookSnapshotPayload:
		if v == nil {
			return nil
		}
		cloned := cloneBookSnapshotPayload(*v)
		return &cloned
	default:
		return v
	}
}

func cloneSliceAny(src []any) []any {
	if len(src) == 0 {
		return nil
	}
	out := make([]any, len(src))
	for i := range src {
		out[i] = cloneInterface(src[i])
	}
	return out
}

func cloneMergedEvent(src *MergedEvent) *MergedEvent {
	if src == nil {
		return nil
	}
	clone := *src
	clone.returned = false
	if len(src.Fragments) > 0 {
		clone.Fragments = make([]CanonicalEvent, len(src.Fragments))
		for i := range src.Fragments {
			fragmentClone := CloneEvent((*Event)(&src.Fragments[i]))
			if fragmentClone != nil {
				clone.Fragments[i] = CanonicalEvent(*fragmentClone)
			} else {
				var empty CanonicalEvent
				clone.Fragments[i] = empty
			}
		}
	}
	return &clone
}
