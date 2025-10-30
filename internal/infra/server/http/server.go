package httpserver

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/app/lambda/runtime"
	"github.com/coachpo/meltica/internal/app/risk"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
)

// NewHandler creates an HTTP handler for lambda management operations.
func NewHandler(manager *runtime.Manager) http.Handler {
	server := &httpServer{manager: manager}
	mux := http.NewServeMux()
	mux.HandleFunc("/strategies", server.handleStrategies)
	mux.HandleFunc("/strategies/", server.handleStrategy)
	mux.HandleFunc("/strategy-instances", server.handleInstances)
	mux.HandleFunc("/strategy-instances/", server.handleInstance)
	mux.HandleFunc("/risk/limits", server.handleRiskLimits)
	return mux
}

type httpServer struct {
	manager *runtime.Manager
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

func (s *httpServer) handleRiskLimits(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limits := s.manager.RiskLimits()
		writeJSON(w, http.StatusOK, map[string]any{"limits": riskConfigFromLimits(limits)})
	case http.MethodPut:
		cfg, err := decodeRiskConfig(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		limits := s.manager.ApplyRiskConfig(cfg)
		writeJSON(w, http.StatusOK, map[string]any{"status": "updated", "limits": riskConfigFromLimits(limits)})
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
	case errors.Is(err, runtime.ErrInstanceExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, runtime.ErrInstanceAlreadyRunning):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, runtime.ErrInstanceNotRunning):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, runtime.ErrInstanceNotFound):
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
	spec.Strategy = strings.TrimSpace(spec.Strategy)
	if len(spec.ProviderSymbols) > 0 {
		symbolSets := make(map[string]config.ProviderSymbols, len(spec.ProviderSymbols))
		for name, symbolSpec := range spec.ProviderSymbols {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				continue
			}
			symbolSpec.Normalize()
			symbolSets[trimmed] = symbolSpec
		}
		spec.ProviderSymbols = symbolSets
	}
	spec.RefreshProviders()
	if spec.Config == nil {
		spec.Config = make(map[string]any)
	}
	if spec.ID == "" {
		return spec, fmt.Errorf("id required")
	}
	if spec.Strategy == "" {
		return spec, fmt.Errorf("strategy required")
	}
	manifest := config.LambdaManifest{
		Lambdas: []config.LambdaSpec{spec},
	}
	if err := manifest.Validate(); err != nil {
		return spec, err
	}
	spec = manifest.Lambdas[0]
	if len(spec.AllSymbols()) == 0 {
		return spec, fmt.Errorf("symbols required")
	}
	return spec, nil
}

func decodeRiskConfig(r *http.Request) (config.RiskConfig, error) {
	defer func() {
		_ = r.Body.Close()
	}()
	var cfg config.RiskConfig
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode payload: %w", err)
	}
	cfg.MaxPositionSize = strings.TrimSpace(cfg.MaxPositionSize)
	cfg.MaxNotionalValue = strings.TrimSpace(cfg.MaxNotionalValue)
	cfg.NotionalCurrency = strings.TrimSpace(cfg.NotionalCurrency)
	if cfg.OrderBurst <= 0 {
		cfg.OrderBurst = 1
	}
	if cfg.MaxRiskBreaches < 0 {
		cfg.MaxRiskBreaches = 0
	}
	if cfg.CircuitBreaker.Threshold < 0 {
		cfg.CircuitBreaker.Threshold = 0
	}
	for i, ot := range cfg.AllowedOrderTypes {
		cfg.AllowedOrderTypes[i] = strings.TrimSpace(ot)
	}
	if err := validateRiskConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func riskConfigFromLimits(limits risk.Limits) config.RiskConfig {
	allowed := make([]string, 0, len(limits.AllowedOrderTypes))
	for _, ot := range limits.AllowedOrderTypes {
		allowed = append(allowed, string(ot))
	}
	cooldown := ""
	if limits.CircuitBreaker.Cooldown > 0 {
		cooldown = limits.CircuitBreaker.Cooldown.String()
	}
	return config.RiskConfig{
		MaxPositionSize:     limits.MaxPositionSize.String(),
		MaxNotionalValue:    limits.MaxNotionalValue.String(),
		NotionalCurrency:    limits.NotionalCurrency,
		OrderThrottle:       limits.OrderThrottle,
		OrderBurst:          limits.OrderBurst,
		MaxConcurrentOrders: limits.MaxConcurrentOrders,
		PriceBandPercent:    limits.PriceBandPercent,
		AllowedOrderTypes:   allowed,
		KillSwitchEnabled:   limits.KillSwitchEnabled,
		MaxRiskBreaches:     limits.MaxRiskBreaches,
		CircuitBreaker: config.CircuitBreakerConfig{
			Enabled:   limits.CircuitBreaker.Enabled,
			Threshold: limits.CircuitBreaker.Threshold,
			Cooldown:  cooldown,
		},
	}
}

func validateRiskConfig(cfg config.RiskConfig) error {
	if cfg.MaxPositionSize == "" {
		return fmt.Errorf("maxPositionSize required")
	}
	if cfg.MaxNotionalValue == "" {
		return fmt.Errorf("maxNotionalValue required")
	}
	if cfg.NotionalCurrency == "" {
		return fmt.Errorf("notionalCurrency required")
	}
	if cfg.OrderThrottle <= 0 {
		return fmt.Errorf("orderThrottle must be > 0")
	}
	if cfg.OrderBurst <= 0 {
		return fmt.Errorf("orderBurst must be > 0")
	}
	if cfg.MaxConcurrentOrders < 0 {
		return fmt.Errorf("maxConcurrentOrders must be >= 0")
	}
	if cfg.PriceBandPercent < 0 {
		return fmt.Errorf("priceBandPercent must be >= 0")
	}
	if cfg.MaxRiskBreaches < 0 {
		return fmt.Errorf("maxRiskBreaches must be >= 0")
	}
	if cfg.CircuitBreaker.Threshold < 0 {
		return fmt.Errorf("circuitBreaker.threshold must be >= 0")
	}
	if cfg.CircuitBreaker.Enabled && strings.TrimSpace(cfg.CircuitBreaker.Cooldown) == "" {
		return fmt.Errorf("circuitBreaker.cooldown required when enabled")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = pool.WriteJSON(w, payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"status": "error", "error": message})
}
