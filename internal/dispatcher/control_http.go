package dispatcher

import (
	"net/http"
	"time"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/bus/controlbus"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// NewControlHTTPHandler returns an HTTP handler implementing the control bus REST facade.
func NewControlHTTPHandler(bus controlbus.Bus) http.Handler {
	server := &controlHTTPServer{bus: bus}
	mux := http.NewServeMux()
	mux.HandleFunc("/control/subscribe", server.handleSubscribe)
	mux.HandleFunc("/control/unsubscribe", server.handleUnsubscribe)
	return mux
}

type controlHTTPServer struct {
	bus controlbus.Bus
}

func (s *controlHTTPServer) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req schema.Subscribe
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode payload failed")
		return
	}
	msg := schema.ControlMessage{
		MessageID:  req.RequestID,
		ConsumerID: "",
		Type:       schema.ControlMessageSubscribe,
		Payload:    payload,
		Timestamp:  time.Now().UTC(),
	}
	ack, err := s.bus.Send(r.Context(), msg)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeAck(w, ack)
}

func (s *controlHTTPServer) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req schema.Unsubscribe
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode payload failed")
		return
	}
	msg := schema.ControlMessage{
		MessageID:  req.RequestID,
		ConsumerID: "",
		Type:       schema.ControlMessageUnsubscribe,
		Payload:    payload,
		Timestamp:  time.Now().UTC(),
	}
	ack, err := s.bus.Send(r.Context(), msg)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeAck(w, ack)
}

func writeAck(w http.ResponseWriter, ack schema.ControlAcknowledgement) {
	status := http.StatusAccepted
	if !ack.Success {
		status = http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := pool.AcquireJSONEncoder()
	defer pool.ReleaseJSONEncoder(encoder)
	_ = encoder.WriteTo(w, ack)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := pool.AcquireJSONEncoder()
	defer pool.ReleaseJSONEncoder(encoder)
	_ = encoder.WriteTo(w, map[string]string{"status": "error", "error": message})
}
