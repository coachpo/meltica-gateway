// Package runtime provides HTTP handlers and runtime management for lambda functions.
package runtime

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/config"
	"github.com/coachpo/meltica/internal/pool"
)

// NewHTTPHandler creates an HTTP handler for lambda management operations.
func NewHTTPHandler(manager *Manager) http.Handler {
	server := &httpServer{manager: manager}
	mux := http.NewServeMux()
	mux.HandleFunc("/strategies", server.handleStrategies)
	mux.HandleFunc("/strategies/", server.handleStrategy)
	mux.HandleFunc("/strategy-instances", server.handleInstances)
	mux.HandleFunc("/strategy-instances/", server.handleInstance)
	return mux
}

type httpServer struct {
	manager *Manager
}

func (s *httpServer) handleStrategies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	catalog := s.manager.StrategyCatalog()
	writeJSON(w, http.StatusOK, map[string]any{"strategies": catalog})
}

func (s *httpServer) handleStrategy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/strategies/")
	name = strings.Trim(name, "/")
	if name == "" {
		writeError(w, http.StatusNotFound, "strategy name required")
		return
	}
	meta, ok := s.manager.StrategyDetail(name)
	if !ok {
		writeError(w, http.StatusNotFound, "strategy not found")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *httpServer) handleInstances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		instances := s.manager.Instances()
		writeJSON(w, http.StatusOK, map[string]any{"instances": instances})
	case http.MethodPost:
		spec, err := decodeInstanceSpec(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		spec.AutoStart = false
		if _, err := s.manager.Create(r.Context(), spec); err != nil {
			s.writeManagerError(w, err)
			return
		}
		snapshot, _ := s.manager.Instance(spec.ID)
		writeJSON(w, http.StatusCreated, snapshot)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *httpServer) handleInstance(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/strategy-instances/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "instance id required")
		return
	}
	parts := strings.Split(rest, "/")
	id := parts[0]

	if len(parts) == 2 {
		action := parts[1]
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch action {
		case "start":
			if err := s.manager.Start(r.Context(), id); err != nil {
				s.writeManagerError(w, err)
				return
			}
			snapshot, _ := s.manager.Instance(id)
			writeJSON(w, http.StatusOK, snapshot)
		case "stop":
			if err := s.manager.Stop(id); err != nil {
				s.writeManagerError(w, err)
				return
			}
			snapshot, _ := s.manager.Instance(id)
			writeJSON(w, http.StatusOK, snapshot)
		default:
			writeError(w, http.StatusNotFound, "unsupported action")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		snapshot, ok := s.manager.Instance(id)
		if !ok {
			writeError(w, http.StatusNotFound, "strategy instance not found")
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	case http.MethodPut:
		spec, err := decodeInstanceSpec(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if spec.ID != "" && spec.ID != id {
			writeError(w, http.StatusBadRequest, "instance id mismatch")
			return
		}
		spec.ID = id
		spec.AutoStart = false
		if err := s.manager.Update(r.Context(), spec); err != nil {
			s.writeManagerError(w, err)
			return
		}
		snapshot, _ := s.manager.Instance(id)
		writeJSON(w, http.StatusOK, snapshot)
	case http.MethodDelete:
		if err := s.manager.Remove(id); err != nil {
			s.writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed", "id": id})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *httpServer) writeManagerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInstanceExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInstanceAlreadyRunning):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInstanceNotRunning):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInstanceNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func decodeInstanceSpec(r *http.Request) (config.LambdaSpec, error) {
	defer func() {
		_ = r.Body.Close()
	}()
	var spec config.LambdaSpec
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&spec); err != nil {
		return spec, fmt.Errorf("decode payload: %w", err)
	}
	spec.ID = strings.TrimSpace(spec.ID)
	spec.Provider = strings.TrimSpace(spec.Provider)
	spec.Symbol = strings.TrimSpace(spec.Symbol)
	spec.Strategy = strings.TrimSpace(spec.Strategy)
	if spec.Config == nil {
		spec.Config = make(map[string]any)
	}
	if spec.ID == "" {
		return spec, fmt.Errorf("id required")
	}
	if spec.Provider == "" {
		return spec, fmt.Errorf("provider required")
	}
	if spec.Symbol == "" {
		return spec, fmt.Errorf("symbol required")
	}
	if spec.Strategy == "" {
		return spec, fmt.Errorf("strategy required")
	}
	return spec, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = pool.WriteJSON(w, payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"status": "error", "error": message})
}
