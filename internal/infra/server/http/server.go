// Package httpserver exposes HTTP handlers for managing lambda strategies and risk settings.
package httpserver

import (
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
	runtimeConfigPath = "/config/runtime"
	configBackupPath  = "/config/backup"
	swaggerSpecPath   = "/docs/openapi.json"
	swaggerUIPath     = "/docs"
)

type handlerFunc func(http.ResponseWriter, *http.Request)

type httpServer struct {
	environment  config.Environment
	meta         config.MetaConfig
	manager      *runtime.Manager
	providers    *provider.Manager
	runtimeStore *config.RuntimeStore
	configStore  *config.AppConfigStore
}

func (s *httpServer) persistProviders(w http.ResponseWriter) bool {
	if s.configStore == nil || s.providers == nil {
		return true
	}
	specs := s.providers.ProviderSpecsSnapshot()
	if err := s.configStore.SetProviders(specs); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("persist providers: %v", err))
		return false
	}
	return true
}

func (s *httpServer) persistLambdaManifest(w http.ResponseWriter) bool {
	if s.configStore == nil || s.manager == nil {
		return true
	}
	manifest := s.manager.ManifestSnapshot()
	if err := s.configStore.SetLambdaManifest(manifest); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("persist lambda manifest: %v", err))
		return false
	}
	return true
}

func (s *httpServer) persistRuntimeConfig(w http.ResponseWriter, cfg config.RuntimeConfig) bool {
	if s.configStore == nil {
		return true
	}
	if err := s.configStore.SetRuntime(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("persist runtime config: %v", err))
		return false
	}
	return true
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

// NewHandler creates an HTTP handler for lambda management operations.
func NewHandler(environment config.Environment, meta config.MetaConfig, manager *runtime.Manager, providers *provider.Manager, runtimeStore *config.RuntimeStore, configStore *config.AppConfigStore) http.Handler {
	server := &httpServer{environment: environment, meta: meta, manager: manager, providers: providers, runtimeStore: runtimeStore, configStore: configStore}
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

	mux.Handle(runtimeConfigPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet: server.exportRuntimeConfig,
		http.MethodPut: server.importRuntimeConfig,
	}))

	mux.Handle(configBackupPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet:  server.exportConfigBackup,
		http.MethodPost: server.restoreConfigBackup,
	}))

	if environment == config.EnvDev {
		mux.Handle(swaggerSpecPath, http.HandlerFunc(server.serveSwaggerSpec))
		mux.Handle(swaggerUIPath, http.HandlerFunc(server.serveSwaggerUI))
	}

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
	if !s.persistProviders(w) {
		_ = s.providers.Remove(spec.Name)
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
		prevSpecs := s.providers.ProviderSpecsSnapshot()
		prevMeta, _ := s.providers.ProviderMetadataFor(name)
		detail, err := s.providers.Update(r.Context(), spec, enabled)
		if err != nil {
			s.writeProviderError(w, err)
			return
		}
		if !s.persistProviders(w) {
			for _, prev := range prevSpecs {
				if strings.EqualFold(prev.Name, name) {
					_, _ = s.providers.Update(r.Context(), prev, prevMeta.Running)
					break
				}
			}
			return
		}
		writeJSON(w, http.StatusOK, detail)
	case http.MethodDelete:
		if s.providers == nil {
			writeError(w, http.StatusServiceUnavailable, "provider manager unavailable")
			return
		}
		prevSpecs := s.providers.ProviderSpecsSnapshot()
		prevMeta, _ := s.providers.ProviderMetadataFor(name)
		if err := s.providers.Remove(name); err != nil {
			s.writeProviderError(w, err)
			return
		}
		if !s.persistProviders(w) {
			for _, prev := range prevSpecs {
				if strings.EqualFold(prev.Name, name) {
					_, _ = s.providers.Create(r.Context(), prev, prevMeta.Running)
					break
				}
			}
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
	spec.AutoStart = false
	if _, err := s.manager.Create(r.Context(), spec); err != nil {
		s.writeManagerError(w, err)
		return
	}
	snapshot, _ := s.manager.Instance(spec.ID)
	if !s.persistLambdaManifest(w) {
		_ = s.manager.Remove(spec.ID)
		return
	}
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
	limits, err := s.manager.ApplyRiskConfig(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.runtimeStore != nil {
		snapshot := s.runtimeStore.Snapshot()
		if !s.persistRuntimeConfig(w, snapshot) {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated", "limits": riskConfigFromLimits(limits)})
}

func (s *httpServer) exportRuntimeConfig(w http.ResponseWriter, _ *http.Request) {
	if s.runtimeStore == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime config store unavailable")
		return
	}
	snapshot := s.runtimeStore.Snapshot()
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *httpServer) importRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	if s.runtimeStore == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime config store unavailable")
		return
	}
	limitRequestBody(w, r)
	cfg, err := decodeRuntimeConfig(r)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	updated, err := s.runtimeStore.Replace(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.manager.ApplyRuntimeConfig(updated); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.persistRuntimeConfig(w, updated) {
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *httpServer) exportConfigBackup(w http.ResponseWriter, _ *http.Request) {
	payload, err := buildBackupPayload(s)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *httpServer) restoreConfigBackup(w http.ResponseWriter, r *http.Request) {
	if s.runtimeStore == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime config store unavailable")
		return
	}
	limitRequestBody(w, r)
	defer func() {
		_ = r.Body.Close()
	}()

	var payload ConfigBackup
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		writeDecodeError(w, err)
		return
	}
	if strings.TrimSpace(payload.Version) == "" {
		payload.Version = backupVersion
	} else if payload.Version != backupVersion {
		writeError(w, http.StatusBadRequest, "unsupported backup version")
		return
	}

	if err := s.applyBackup(r.Context(), payload); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	lambdaCount := 0
	if payload.Lambdas.Manifest.Lambdas != nil {
		lambdaCount = len(payload.Lambdas.Manifest.Lambdas)
	}
	providerCount := 0
	if payload.Providers.Config != nil {
		providerCount = len(payload.Providers.Config)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "restored",
		"providers": providerCount,
		"lambdas":   lambdaCount,
	})
}

func (s *httpServer) serveSwaggerSpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(swaggerSpec))
}

func (s *httpServer) serveSwaggerUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != swaggerUIPath {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerUIHTML))
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
		spec.AutoStart = false
		if err := s.manager.Update(r.Context(), spec); err != nil {
			s.writeManagerError(w, err)
			return
		}
		snapshot, _ := s.manager.Instance(id)
		if !s.persistLambdaManifest(w) {
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	case http.MethodDelete:
		if err := s.manager.Remove(id); err != nil {
			s.writeManagerError(w, err)
			return
		}
		if !s.persistLambdaManifest(w) {
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

func decodeRuntimeConfig(r *http.Request) (config.RuntimeConfig, error) {
	defer func() {
		_ = r.Body.Close()
	}()
	var cfg config.RuntimeConfig
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode payload: %w", err)
	}
	cfg.Normalise()
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
	cfg := config.RiskConfig{
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
	cfg.MarkAllFieldsSet()
	return cfg
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

func buildProviderConfigSnapshot(specs []config.ProviderSpec) map[string]map[string]any {
	if len(specs) == 0 {
		return nil
	}
	out := make(map[string]map[string]any, len(specs))
	for _, spec := range specs {
		adapter := make(map[string]any)
		adapter["identifier"] = spec.Adapter
		cleanConfig := provider.SanitizeProviderConfig(spec.Config)
		if cleanConfig == nil {
			cleanConfig = map[string]any{}
		}
		for key, value := range cleanConfig {
			switch key {
			case "provider_name":
				continue
			case "identifier":
				adapter["identifier"] = cloneValue(value)
			default:
				adapter[key] = cloneValue(value)
			}
		}
		if id, ok := adapter["identifier"].(string); !ok || strings.TrimSpace(id) == "" {
			adapter["identifier"] = spec.Adapter
		}
		out[spec.Name] = map[string]any{
			"adapter": adapter,
		}
	}
	return out
}

func cloneValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(v))
		for key, val := range v {
			clone[key] = cloneValue(val)
		}
		return clone
	case []any:
		clone := make([]any, len(v))
		for i, item := range v {
			clone[i] = cloneValue(item)
		}
		return clone
	case []string:
		return append([]string(nil), v...)
	default:
		return value
	}
}

const swaggerSpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Meltica Control API",
    "version": "1.0.0",
    "description": "Runtime control surface for Meltica gateway."
  },
  "servers": [
    { "url": "http://localhost:8880", "description": "Local development" }
  ],
  "paths": {
    "/strategies": {
      "get": {
        "summary": "List available strategies",
        "responses": {
          "200": { "description": "Successful response" }
        }
      }
    },
    "/strategies/{name}": {
      "get": {
        "summary": "Get strategy metadata",
        "parameters": [
          { "name": "name", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Strategy metadata" },
          "404": { "description": "Strategy not found" }
        }
      }
    },
    "/providers": {
      "get": {
        "summary": "List providers",
        "responses": {
          "200": { "description": "Provider list" }
        }
      },
      "post": {
        "summary": "Create provider",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "type": "object" }
            }
          }
        },
        "responses": {
          "201": { "description": "Provider created" },
          "400": { "description": "Validation error" }
        }
      }
    },
    "/providers/{name}": {
      "get": {
        "summary": "Get provider details",
        "parameters": [
          { "name": "name", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Provider metadata" },
          "404": { "description": "Provider not found" }
        }
      },
      "put": {
        "summary": "Update provider",
        "parameters": [
          { "name": "name", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "type": "object" }
            }
          }
        },
        "responses": {
          "200": { "description": "Provider updated" },
          "400": { "description": "Validation error" }
        }
      },
      "delete": {
        "summary": "Delete provider",
        "parameters": [
          { "name": "name", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Provider removed" },
          "404": { "description": "Provider not found" }
        }
      }
    },
    "/providers/{name}/start": {
      "post": {
        "summary": "Start provider",
        "parameters": [
          { "name": "name", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Provider started" }
        }
      }
    },
    "/providers/{name}/stop": {
      "post": {
        "summary": "Stop provider",
        "parameters": [
          { "name": "name", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Provider stopped" }
        }
      }
    },
    "/adapters": {
      "get": {
        "summary": "List adapter metadata",
        "responses": {
          "200": { "description": "Adapter list" }
        }
      }
    },
    "/adapters/{identifier}": {
      "get": {
        "summary": "Get adapter metadata",
        "parameters": [
          { "name": "identifier", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Adapter metadata" },
          "404": { "description": "Adapter not found" }
        }
      }
    },
    "/strategy/instances": {
      "get": {
        "summary": "List strategy instances",
        "responses": {
          "200": { "description": "Instance list" }
        }
      },
      "post": {
        "summary": "Create strategy instance",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "type": "object" }
            }
          }
        },
        "responses": {
          "201": { "description": "Instance created" },
          "400": { "description": "Validation error" }
        }
      }
    },
    "/strategy/instances/{id}": {
      "get": {
        "summary": "Get instance snapshot",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Instance snapshot" },
          "404": { "description": "Instance not found" }
        }
      },
      "put": {
        "summary": "Update instance",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "type": "object" }
            }
          }
        },
        "responses": {
          "200": { "description": "Instance updated" }
        }
      },
      "delete": {
        "summary": "Delete instance",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Instance removed" }
        }
      }
    },
    "/strategy/instances/{id}/start": {
      "post": {
        "summary": "Start instance",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Instance started" }
        }
      }
    },
    "/strategy/instances/{id}/stop": {
      "post": {
        "summary": "Stop instance",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Instance stopped" }
        }
      }
    },
    "/risk/limits": {
      "get": {
        "summary": "Get risk limits",
        "responses": {
          "200": { "description": "Risk limits" }
        }
      },
      "put": {
        "summary": "Update risk limits",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "type": "object" }
            }
          }
        },
        "responses": {
          "200": { "description": "Risk limits updated" }
        }
      }
    },
    "/config/runtime": {
      "get": {
        "summary": "Export runtime configuration",
        "responses": {
          "200": { "description": "Runtime configuration" }
        }
      },
      "put": {
        "summary": "Import runtime configuration",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "type": "object" }
            }
          }
        },
        "responses": {
          "200": { "description": "Runtime configuration updated" }
        }
      }
    },
    "/config/backup": {
      "get": {
        "summary": "Export configuration backup",
        "responses": {
          "200": { "description": "Configuration backup" }
        }
      }
    }
  }
}`

var swaggerUIHTML = fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Meltica API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    body { margin:0; background: #fafafa; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.addEventListener('load', function() {
      SwaggerUIBundle({
        url: '%s',
        dom_id: '#swagger-ui',
        presets: [SwaggerUIBundle.presets.apis],
        layout: 'BaseLayout'
      });
    });
  </script>
</body>
</html>`, swaggerSpecPath)

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
