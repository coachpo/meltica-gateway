package schema

import (
	"testing"
	"time"
)

func TestWsFrameReset(t *testing.T) {
	frame := &WsFrame{
		Provider:    "binance",
		ConnID:      "conn-123",
		ReceivedAt:  time.Now().UnixNano(),
		MessageType: 1,
		Data:        []byte("test data"),
	}
	
	frame.Reset()
	
	if frame.Provider != "" {
		t.Error("expected Provider to be reset")
	}
	if frame.ConnID != "" {
		t.Error("expected ConnID to be reset")
	}
	if frame.ReceivedAt != 0 {
		t.Error("expected ReceivedAt to be reset")
	}
	if frame.MessageType != 0 {
		t.Error("expected MessageType to be reset")
	}
    if len(frame.Data) != 0 {
        t.Error("expected Data to be reset")
    }
}

func TestWsFrameSetReturned(t *testing.T) {
	frame := &WsFrame{}
	
	frame.SetReturned(true)
	if !frame.IsReturned() {
		t.Error("expected frame to be marked as returned")
	}
	
	frame.SetReturned(false)
	if frame.IsReturned() {
		t.Error("expected frame to be marked as not returned")
	}
}

func TestWsFrameIsReturned(t *testing.T) {
	frame := &WsFrame{}
	
	if frame.IsReturned() {
		t.Error("expected new frame to not be returned")
	}
	
	frame.returned = true
	if !frame.IsReturned() {
		t.Error("expected frame to be returned")
	}
}

func TestWsFrameNilHandling(t *testing.T) {
	var frame *WsFrame
	
	frame.Reset() // Should not panic
	frame.SetReturned(true) // Should not panic
	
	if frame.IsReturned() {
		t.Error("expected nil frame to return false for IsReturned")
	}
}


