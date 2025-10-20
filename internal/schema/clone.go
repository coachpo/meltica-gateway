// Package schema defines canonical event types and helper utilities.
package schema

// CopyEvent copies the contents of src into dst, performing deep copies of
// mutable payload fields. dst must not be nil.
func CopyEvent(dst, src *Event) {
	if dst == nil || src == nil {
		return
	}
	*dst = *src
	dst.returned = false
	dst.Payload = clonePayload(src.Payload)
}

func clonePayload(payload any) any {
	switch v := payload.(type) {
	case nil:
		return nil
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
	default:
		return v
	}
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
