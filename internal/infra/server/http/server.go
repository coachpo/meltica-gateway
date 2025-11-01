// Package httpserver exposes HTTP handlers for managing lambda strategies and risk settings.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/app/lambda/runtime"
	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/app/risk"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
)

const (
	maxJSONBodyBytes int64 = 1 << 20 // 1 MiB

	strategiesPath       = "/strategies"
	strategyDetailPrefix = strategiesPath + "/"

	providersPath        = "/providers"
	providerDetailPrefix = providersPath + "/"

	adaptersPath        = "/adapters"
	adapterDetailPrefix = adaptersPath + "/"

	instancesPath        = "/strategy/instances"
	instanceDetailPrefix = instancesPath + "/"

	riskLimitsPath    = "/risk/limits"
	contextBackupPath = "/context/backup"
)

type handlerFunc func(http.ResponseWriter, *http.Request)

type httpServer struct {
	manager       *runtime.Manager
	providers     *provider.Manager
	baseProviders map[string]struct{}
	baseLambdas   map[string]struct{}
}

type providerPayload struct {
	Name    string                 `json:"name"`
	Adapter providerAdapterPayload `json:"adapter"`
	Enabled *bool                  `json:"enabled,omitempty"`
}

type providerAdapterPayload struct {
	Identifier string         `json:"identifier"`
	Config     map[string]any `json:"config"`
}

type contextBackup struct {
	Providers []config.ProviderSpec `json:"providers,omitempty"`
	Lambdas   []config.LambdaSpec   `json:"lambdas,omitempty"`
	Risk      config.RiskConfig     `json:"risk"`
}

// NewHandler creates an HTTP handler for lambda management operations.
func NewHandler(appCfg config.AppConfig, manager *runtime.Manager, providers *provider.Manager) http.Handler {
	baseProviders := make(map[string]struct{}, len(appCfg.Providers))
	for name := range appCfg.Providers {
		normalized := strings.ToLower(strings.TrimSpace(string(name)))
		if normalized != "" {
			baseProviders[normalized] = struct{}{}
		}
	}
	baseLambdas := make(map[string]struct{}, len(appCfg.LambdaManifest.Lambdas))
	for _, lambda := range appCfg.LambdaManifest.Lambdas {
		normalized := strings.ToLower(strings.TrimSpace(lambda.ID))
		if normalized != "" {
			baseLambdas[normalized] = struct{}{}
		}
	}
	server := &httpServer{
		manager:       manager,
		providers:     providers,
		baseProviders: baseProviders,
		baseLambdas:   baseLambdas,
	}
	mux := http.NewServeMux()

	mux.Handle(strategiesPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet: server.getStrategies,
	}))
	mux.Handle(strategyDetailPrefix, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet: server.getStrategy,
	}))

	mux.Handle(providersPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet:  server.listProviders,
		http.MethodPost: server.createProvider,
	}))
	mux.Handle(providerDetailPrefix, http.HandlerFunc(server.handleProvider))

	mux.Handle(adaptersPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet: server.listAdapters,
	}))
	mux.Handle(adapterDetailPrefix, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet: server.getAdapter,
	}))

	mux.Handle(instancesPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet:  server.listInstances,
		http.MethodPost: server.createInstance,
	}))
	mux.Handle(instanceDetailPrefix, http.HandlerFunc(server.handleInstance))

	mux.Handle(riskLimitsPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet: server.getRiskLimits,
		http.MethodPut: server.updateRiskLimits,
	}))

	mux.Handle(contextBackupPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet:  server.handleContextBackupExport,
		http.MethodPost: server.handleContextBackupRestore,
	}))

	return withCORS(mux)
}

func (s *httpServer) methodHandlers(handlers map[string]handlerFunc) http.Handler {
	allowed := allowedMethods(handlers)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler, ok := handlers[r.Method]; ok {
			handler(w, r)
			return
		}
		methodNotAllowed(w, allowed...)
	})
}

func allowedMethods(handlers map[string]handlerFunc) []string {
	if len(handlers) == 0 {
		return nil
	}
	allowed := make([]string, 0, len(handlers))
	for method := range handlers {
		allowed = append(allowed, method)
	}
	sort.Strings(allowed)
	return allowed
}

func (s *httpServer) getStrategies(w http.ResponseWriter, _ *http.Request) {
	catalog := s.manager.StrategyCatalog()
	writeJSON(w, http.StatusOK, map[string]any{"strategies": catalog})
}

func (s *httpServer) getStrategy(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, strategyDetailPrefix), "/")
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

func (s *httpServer) listProviders(w http.ResponseWriter, _ *http.Request) {
	if s.providers == nil {
		writeJSON(w, http.StatusOK, map[string]any{"providers": []provider.RuntimeMetadata{}})
		return
	}
	metadata := s.providers.ProviderMetadataSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{"providers": metadata})
}

func (s *httpServer) createProvider(w http.ResponseWriter, r *http.Request) {
	if s.providers == nil {
		writeError(w, http.StatusServiceUnavailable, "provider manager unavailable")
		return
	}
	limitRequestBody(w, r)
	payload, err := decodeProviderPayload(r)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	spec, enabled, err := buildProviderSpecFromPayload(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	detail, err := s.providers.Create(r.Context(), spec, enabled)
	if err != nil {
		s.writeProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, detail)
}

func (s *httpServer) writeProviderDetail(w http.ResponseWriter, name string) {
	if name == "" {
		writeError(w, http.StatusNotFound, "provider name required")
		return
	}
	if s.providers == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	meta, ok := s.providers.ProviderMetadataFor(name)
	if !ok {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *httpServer) handleProvider(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, providerDetailPrefix), "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "provider name required")
		return
	}

	name, action, hasAction := strings.Cut(rest, "/")
	name = strings.TrimSpace(name)
	if name == "" {
		writeError(w, http.StatusNotFound, "provider name required")
		return
	}

	if !hasAction {
		s.handleProviderResource(w, r, name)
		return
	}

	action = strings.TrimSpace(action)
	s.handleProviderAction(w, r, name, action)
}

func (s *httpServer) handleProviderResource(w http.ResponseWriter, r *http.Request, name string) {
	switch r.Method {
	case http.MethodGet:
		s.writeProviderDetail(w, name)
	case http.MethodPut:
		if s.providers == nil {
			writeError(w, http.StatusServiceUnavailable, "provider manager unavailable")
			return
		}
		limitRequestBody(w, r)
		payload, err := decodeProviderPayload(r)
		if err != nil {
			writeDecodeError(w, err)
			return
		}
		if strings.TrimSpace(payload.Name) == "" {
			payload.Name = name
		} else if !strings.EqualFold(strings.TrimSpace(payload.Name), name) {
			writeError(w, http.StatusBadRequest, "provider name mismatch")
			return
		}
		spec, enabled, err := buildProviderSpecFromPayload(payload)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		detail, err := s.providers.Update(r.Context(), spec, enabled)
		if err != nil {
			s.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	case http.MethodDelete:
		if s.providers == nil {
			writeError(w, http.StatusServiceUnavailable, "provider manager unavailable")
			return
		}
		if err := s.providers.Remove(name); err != nil {
			s.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed", "name": name})
	default:
		methodNotAllowed(w, http.MethodDelete, http.MethodGet, http.MethodPut)
	}
}

func (s *httpServer) handleProviderAction(w http.ResponseWriter, r *http.Request, name, action string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.providers == nil {
		writeError(w, http.StatusServiceUnavailable, "provider manager unavailable")
		return
	}

	switch action {
	case "start":
		detail, err := s.providers.StartProvider(r.Context(), name)
		if err != nil {
			s.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	case "stop":
		detail, err := s.providers.StopProvider(name)
		if err != nil {
			s.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	default:
		writeError(w, http.StatusNotFound, "unsupported action")
	}
}

func (s *httpServer) listAdapters(w http.ResponseWriter, _ *http.Request) {
	if s.providers == nil {
		writeJSON(w, http.StatusOK, map[string]any{"adapters": []provider.AdapterMetadata{}})
		return
	}
	reg := s.providers.Registry()
	if reg == nil {
		writeJSON(w, http.StatusOK, map[string]any{"adapters": []provider.AdapterMetadata{}})
		return
	}
	metadata := reg.AdapterMetadataSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{"adapters": metadata})
}

func (s *httpServer) getAdapter(w http.ResponseWriter, r *http.Request) {
	identifier := strings.Trim(strings.TrimPrefix(r.URL.Path, adapterDetailPrefix), "/")
	if identifier == "" {
		writeError(w, http.StatusNotFound, "adapter identifier required")
		return
	}
	if s.providers == nil {
		writeError(w, http.StatusNotFound, "adapter not found")
		return
	}
	reg := s.providers.Registry()
	if reg == nil {
		writeError(w, http.StatusNotFound, "adapter not found")
		return
	}
	meta, ok := reg.AdapterMetadata(identifier)
	if !ok {
		writeError(w, http.StatusNotFound, "adapter not found")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *httpServer) listInstances(w http.ResponseWriter, _ *http.Request) {
	instances := s.manager.Instances()
	writeJSON(w, http.StatusOK, map[string]any{"instances": instances})
}

func (s *httpServer) createInstance(w http.ResponseWriter, r *http.Request) {
	limitRequestBody(w, r)
	spec, err := decodeInstanceSpec(r)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	if _, err := s.manager.Create(r.Context(), spec); err != nil {
		s.writeManagerError(w, err)
		return
	}
	snapshot, _ := s.manager.Instance(spec.ID)
	writeJSON(w, http.StatusCreated, snapshot)
}

func (s *httpServer) getRiskLimits(w http.ResponseWriter, _ *http.Request) {
	limits := s.manager.RiskLimits()
	writeJSON(w, http.StatusOK, map[string]any{"limits": riskConfigFromLimits(limits)})
}

func (s *httpServer) updateRiskLimits(w http.ResponseWriter, r *http.Request) {
	limitRequestBody(w, r)
	cfg, err := decodeRiskConfig(r)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	limits := s.manager.ApplyRiskConfig(cfg)
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated", "limits": riskConfigFromLimits(limits)})
}

func (s *httpServer) handleContextBackupExport(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.buildContextBackup())
}

func (s *httpServer) handleContextBackupRestore(w http.ResponseWriter, r *http.Request) {
	limitRequestBody(w, r)
	defer func() { _ = r.Body.Close() }()
	var payload contextBackup
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		writeDecodeError(w, err)
		return
	}
	if err := s.applyContextBackup(r.Context(), payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "restored"})
}

func (s *httpServer) buildContextBackup() contextBackup {
	result := contextBackup{Risk: config.RiskConfig{}}
	if s.manager != nil {
		result.Risk = riskConfigFromLimits(s.manager.RiskLimits())
	}
	if s.providers != nil {
		sanitized := s.providers.SanitizedProviderSpecs()
		filtered := make([]config.ProviderSpec, 0, len(sanitized))
		for _, spec := range sanitized {
			if s.isBaselineProvider(spec.Name) {
				continue
			}
			filtered = append(filtered, spec)
		}
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Name < filtered[j].Name
		})
		result.Providers = filtered
	}
	if s.manager != nil {
		summaries := s.manager.Instances()
		lambdas := make([]config.LambdaSpec, 0, len(summaries))
		for _, summary := range summaries {
			if s.isBaselineLambda(summary.ID) {
				continue
			}
			if snapshot, ok := s.manager.Instance(summary.ID); ok {
				lambdas = append(lambdas, lambdaSpecFromSnapshot(snapshot))
			}
		}
		sort.Slice(lambdas, func(i, j int) bool {
			return lambdas[i].ID < lambdas[j].ID
		})
		result.Lambdas = lambdas
	}
	return result
}

func (s *httpServer) handleInstance(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, instanceDetailPrefix), "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "instance id required")
		return
	}

	id, action, hasAction := strings.Cut(rest, "/")
	id = strings.TrimSpace(id)
	if id == "" {
		writeError(w, http.StatusNotFound, "instance id required")
		return
	}

	if !hasAction {
		s.handleInstanceResource(w, r, id)
		return
	}

	action = strings.TrimSpace(action)
	s.handleInstanceAction(w, r, id, action)
}

func (s *httpServer) handleInstanceResource(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		snapshot, ok := s.manager.Instance(id)
		if !ok {
			writeError(w, http.StatusNotFound, "strategy instance not found")
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	case http.MethodPut:
		limitRequestBody(w, r)
		spec, err := decodeInstanceSpec(r)
		if err != nil {
			writeDecodeError(w, err)
			return
		}
		if spec.ID != "" && spec.ID != id {
			writeError(w, http.StatusBadRequest, "instance id mismatch")
			return
		}
		spec.ID = id
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
		methodNotAllowed(w, http.MethodDelete, http.MethodGet, http.MethodPut)
	}
}

func (s *httpServer) handleInstanceAction(w http.ResponseWriter, r *http.Request, id, action string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	switch action {
	case "start":
		if err := s.manager.Start(r.Context(), id); err != nil {
			s.writeManagerError(w, err)
			return
		}
	case "stop":
		if err := s.manager.Stop(id); err != nil {
			s.writeManagerError(w, err)
			return
		}
	default:
		writeError(w, http.StatusNotFound, "unsupported action")
		return
	}

	snapshot, _ := s.manager.Instance(id)
	writeJSON(w, http.StatusOK, snapshot)
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

func (s *httpServer) writeProviderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, provider.ErrProviderExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, provider.ErrProviderNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, provider.ErrProviderRunning):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, provider.ErrProviderNotRunning):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func decodeProviderPayload(r *http.Request) (providerPayload, error) {
	defer func() {
		_ = r.Body.Close()
	}()
	var payload providerPayload
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return payload, fmt.Errorf("decode payload: %w", err)
	}
	return payload, nil
}

func buildProviderSpecFromPayload(payload providerPayload) (config.ProviderSpec, bool, error) {
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return config.ProviderSpec{}, false, fmt.Errorf("name required")
	}

	identifier := strings.TrimSpace(payload.Adapter.Identifier)
	if identifier == "" {
		return config.ProviderSpec{}, false, fmt.Errorf("adapter.identifier required")
	}

	adapterConfig := map[string]any{
		"identifier": identifier,
	}
	if len(payload.Adapter.Config) > 0 {
		adapterConfig["config"] = payload.Adapter.Config
	}

	specs, err := config.BuildProviderSpecs(map[config.Provider]map[string]any{
		config.Provider(name): {
			"adapter": adapterConfig,
		},
	})
	if err != nil {
		return config.ProviderSpec{}, false, fmt.Errorf("build provider spec: %w", err)
	}
	if len(specs) == 0 {
		return config.ProviderSpec{}, false, fmt.Errorf("provider spec not generated")
	}
	return specs[0], enabled, nil
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
	spec.Strategy.Normalize()
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
	if spec.ID == "" {
		return spec, fmt.Errorf("id required")
	}
	if spec.Strategy.Identifier == "" {
		return spec, fmt.Errorf("strategy required")
	}
	manifest := config.LambdaManifest{
		Lambdas: []config.LambdaSpec{spec},
	}
	if err := manifest.Validate(); err != nil {
		return spec, fmt.Errorf("validate lambda manifest: %w", err)
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

func (s *httpServer) applyContextBackup(ctx context.Context, payload contextBackup) error {
	if s.providers == nil || s.manager == nil {
		return fmt.Errorf("runtime managers unavailable")
	}

	targetProviders := make(map[string]config.ProviderSpec, len(payload.Providers))
	orderedProviders := make([]config.ProviderSpec, 0, len(payload.Providers))
	for _, spec := range payload.Providers {
		sanitized := provider.SanitizeProviderSpec(spec)
		sanitized.Name = strings.TrimSpace(sanitized.Name)
		if sanitized.Name == "" {
			return fmt.Errorf("provider name required")
		}
		if s.isBaselineProvider(sanitized.Name) {
			continue
		}
		targetProviders[strings.ToLower(sanitized.Name)] = sanitized
		orderedProviders = append(orderedProviders, sanitized)
	}

	existing := s.providers.SanitizedProviderSpecs()
	for _, spec := range existing {
		if s.isBaselineProvider(spec.Name) {
			continue
		}
		if _, ok := targetProviders[strings.ToLower(spec.Name)]; !ok {
			if err := s.providers.Remove(spec.Name); err != nil && !errors.Is(err, provider.ErrProviderNotFound) {
				return fmt.Errorf("remove provider %s: %w", spec.Name, err)
			}
		}
	}

	for _, spec := range orderedProviders {
		if _, exists := s.providers.ProviderMetadataFor(spec.Name); exists {
			if _, err := s.providers.StopProvider(spec.Name); err != nil && !errors.Is(err, provider.ErrProviderNotRunning) {
				return fmt.Errorf("stop provider %s: %w", spec.Name, err)
			}
			if _, err := s.providers.Update(ctx, spec, false); err != nil {
				return fmt.Errorf("update provider %s: %w", spec.Name, err)
			}
		} else {
			if _, err := s.providers.Create(ctx, spec, false); err != nil {
				return fmt.Errorf("create provider %s: %w", spec.Name, err)
			}
		}
	}

	targetLambdas := make(map[string]struct{}, len(payload.Lambdas))
	manifest := config.LambdaManifest{Lambdas: make([]config.LambdaSpec, 0, len(payload.Lambdas))}
	for _, spec := range payload.Lambdas {
		copied := config.LambdaSpec{
			ID:              strings.TrimSpace(spec.ID),
			Strategy:        config.LambdaStrategySpec{Identifier: strings.TrimSpace(spec.Strategy.Identifier), Config: cloneAnyMap(spec.Strategy.Config)},
			ProviderSymbols: cloneProviderSymbolsMap(spec.ProviderSymbols),
			Providers:       cloneStringSlice(spec.Providers),
		}
		if copied.ID == "" {
			return fmt.Errorf("lambda id required")
		}
		manifest.Lambdas = append(manifest.Lambdas, copied)
		targetLambdas[strings.ToLower(copied.ID)] = struct{}{}
	}
	if len(manifest.Lambdas) > 0 {
		if err := manifest.Validate(); err != nil {
			return fmt.Errorf("validate lambdas: %w", err)
		}
	}

	summaries := s.manager.Instances()
	for _, summary := range summaries {
		if s.isBaselineLambda(summary.ID) {
			continue
		}
		if _, ok := targetLambdas[strings.ToLower(summary.ID)]; !ok {
			if err := s.manager.Remove(summary.ID); err != nil && !errors.Is(err, runtime.ErrInstanceNotFound) {
				return fmt.Errorf("remove lambda %s: %w", summary.ID, err)
			}
		}
	}

	restored := make([]config.LambdaSpec, 0, len(manifest.Lambdas))
	for _, spec := range manifest.Lambdas {
		if s.isBaselineLambda(spec.ID) {
			continue
		}
		if err := s.manager.Remove(spec.ID); err != nil && !errors.Is(err, runtime.ErrInstanceNotFound) {
			return fmt.Errorf("prepare lambda %s: %w", spec.ID, err)
		}
		restored = append(restored, spec)
	}

	if len(restored) > 0 {
		if err := s.manager.StartFromManifest(ctx, config.LambdaManifest{Lambdas: restored}); err != nil {
			return fmt.Errorf("restore lambdas: %w", err)
		}
	}

	if payload.Risk.MaxPositionSize != "" || payload.Risk.MaxNotionalValue != "" || payload.Risk.NotionalCurrency != "" {
		s.manager.ApplyRiskConfig(payload.Risk)
	}
	return nil
}

func lambdaSpecFromSnapshot(snapshot runtime.InstanceSnapshot) config.LambdaSpec {
	return config.LambdaSpec{
		ID:              snapshot.ID,
		Strategy:        config.LambdaStrategySpec{Identifier: snapshot.Strategy.Identifier, Config: cloneAnyMap(snapshot.Strategy.Config)},
		ProviderSymbols: cloneProviderSymbolsMap(snapshot.ProviderSymbols),
		Providers:       cloneStringSlice(snapshot.Providers),
	}
}

func cloneProviderSymbolsMap(input map[string]config.ProviderSymbols) map[string]config.ProviderSymbols {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]config.ProviderSymbols, len(input))
	for key, symbols := range input {
		out[key] = config.ProviderSymbols{Symbols: cloneStringSlice(symbols.Symbols)}
	}
	return out
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = cloneAnyMap(typed)
		case []any:
			out[key] = cloneAnySlice(typed)
		default:
			out[key] = typed
		}
	}
	return out
}

func cloneAnySlice(input []any) []any {
	if len(input) == 0 {
		return nil
	}
	out := make([]any, 0, len(input))
	for _, value := range input {
		switch typed := value.(type) {
		case map[string]any:
			out = append(out, cloneAnyMap(typed))
		case []any:
			out = append(out, cloneAnySlice(typed))
		default:
			out = append(out, typed)
		}
	}
	return out
}

func (s *httpServer) isBaselineProvider(name string) bool {
	if name == "" {
		return false
	}
	_, ok := s.baseProviders[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func (s *httpServer) isBaselineLambda(id string) bool {
	if id == "" {
		return false
	}
	_, ok := s.baseLambdas[strings.ToLower(strings.TrimSpace(id))]
	return ok
}

func limitRequestBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
}

func writeDecodeError(w http.ResponseWriter, err error) {
	if isRequestTooLarge(err) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func isRequestTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
	}
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = pool.WriteJSON(w, payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"status": "error", "error": message})
}

func withCORS(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		handler.ServeHTTP(w, r)
	})
}
