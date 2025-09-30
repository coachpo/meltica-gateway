package schema

// WsFrame represents a raw websocket frame from an upstream provider.
type WsFrame struct {
	returned    bool
	Provider    string
	ConnID      string
	ReceivedAt  int64
	MessageType int
	Data        []byte
}

// Reset zeroes the frame for reuse by a pool.
func (f *WsFrame) Reset() {
	if f == nil {
		return
	}
	f.Provider = ""
	f.ConnID = ""
	f.ReceivedAt = 0
	f.MessageType = 0
	f.Data = nil
	f.returned = false
}

// SetReturned toggles the pool ownership flag.
func (f *WsFrame) SetReturned(flag bool) {
	if f == nil {
		return
	}
	f.returned = flag
}

// IsReturned reports whether the frame is currently in the pool.
func (f *WsFrame) IsReturned() bool {
	if f == nil {
		return false
	}
	return f.returned
}
