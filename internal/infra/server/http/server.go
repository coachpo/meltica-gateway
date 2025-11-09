// Package httpserver exposes HTTP handlers for managing lambda strategies and risk settings.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/app/lambda/js"
	"github.com/coachpo/meltica/internal/app/lambda/runtime"
	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/app/risk"
	"github.com/coachpo/meltica/internal/domain/orderstore"
	"github.com/coachpo/meltica/internal/domain/outboxstore"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
)

const (
	maxJSONBodyBytes int64 = 1 << 20 // 1 MiB

	strategiesPath       = "/strategies"
	strategyDetailPrefix = strategiesPath + "/"
	strategyModulesPath  = strategiesPath + "/modules"
	strategyModulePrefix = strategyModulesPath + "/"
	strategyRefreshPath  = strategiesPath + "/refresh"
	strategyRegistryPath = strategiesPath + "/registry"
	strategySourceSuffix = "/source"
	strategyUsageSuffix  = "/usage"

	providersPath        = "/providers"
	providerDetailPrefix = providersPath + "/"

	adaptersPath        = "/adapters"
	adapterDetailPrefix = adaptersPath + "/"

	instancesPath        = "/strategy/instances"
	instanceDetailPrefix = instancesPath + "/"

	riskLimitsPath    = "/risk/limits"
	contextBackupPath = "/context/backup"

	instanceOrdersSuffix     = "orders"
	instanceExecutionsSuffix = "executions"
	providerBalancesSuffix   = "balances"
	outboxPath               = "/outbox"
	outboxDetailPrefix       = outboxPath + "/"

	defaultOrdersLimit     = 50
	defaultExecutionsLimit = 100
	defaultBalancesLimit   = 100
	defaultOutboxLimit     = 100
	maxListLimit           = 500
)

type handlerFunc func(http.ResponseWriter, *http.Request)

type httpServer struct {
	manager       *runtime.Manager
	providers     *provider.Manager
	orderStore    orderstore.Store
	outboxStore   outboxstore.Store
	baseProviders map[string]struct{}
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

type strategyModulePayload struct {
	Filename      string   `json:"filename,omitempty"`
	Name          string   `json:"name,omitempty"`
	Tag           string   `json:"tag,omitempty"`
	Aliases       []string `json:"aliases,omitempty"`
	ReassignTags  []string `json:"reassignTags,omitempty"`
	PromoteLatest *bool    `json:"promoteLatest,omitempty"`
	Source        string   `json:"source"`
}

type strategyTagPayload struct {
	Hash    string `json:"hash"`
	Refresh *bool  `json:"refresh,omitempty"`
}

type strategyRefreshPayload struct {
	Hashes     []string `json:"hashes"`
	Strategies []string `json:"strategies"`
}

type instanceLinks struct {
	Self  string `json:"self,omitempty"`
	Usage string `json:"usage,omitempty"`
}

type instanceSummaryResponse struct {
	runtime.InstanceSummary
	Links instanceLinks `json:"links"`
}

type instanceSnapshotResponse struct {
	runtime.InstanceSnapshot
	Links instanceLinks `json:"links"`
}

// NewHandler creates an HTTP handler for lambda management operations.
func NewHandler(appCfg config.AppConfig, manager *runtime.Manager, providers *provider.Manager, orders orderstore.Store, outbox outboxstore.Store) http.Handler {
	baseProviders := make(map[string]struct{}, len(appCfg.Providers))
	for name := range appCfg.Providers {
		normalized := strings.ToLower(strings.TrimSpace(string(name)))
		if normalized != "" {
			baseProviders[normalized] = struct{}{}
		}
	}
	server := &httpServer{
		manager:       manager,
		providers:     providers,
		orderStore:    orders,
		outboxStore:   outbox,
		baseProviders: baseProviders,
	}
	mux := http.NewServeMux()

	mux.Handle(strategiesPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet: server.getStrategies,
	}))
	mux.Handle(strategyDetailPrefix, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet: server.getStrategy,
	}))
	mux.Handle(strategyModulesPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet:  server.listStrategyModules,
		http.MethodPost: server.createStrategyModule,
	}))
	mux.Handle(strategyModulePrefix, http.HandlerFunc(server.handleStrategyModule))
	mux.Handle(strategyRefreshPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodPost: server.refreshStrategies,
	}))
	mux.Handle(strategyRegistryPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet: server.exportStrategyRegistry,
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

	mux.Handle(outboxPath, server.methodHandlers(map[string]handlerFunc{
		http.MethodGet: server.listOutbox,
	}))
	mux.Handle(outboxDetailPrefix, http.HandlerFunc(server.handleOutboxEntry))

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

func (s *httpServer) listStrategyModules(w http.ResponseWriter, r *http.Request) {
	modules := []js.ModuleSummary{}
	if s.manager != nil {
		modules = s.manager.StrategyModules()
	}
	strategyDirectory := ""
	if s.manager != nil {
		strategyDirectory = s.manager.StrategyDirectory()
	}
	filtered, total, offset, limit, err := filterModuleSummaries(modules, r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	response := map[string]any{
		"modules":           filtered,
		"total":             total,
		"offset":            offset,
		"strategyDirectory": strategyDirectory,
	}
	if limit >= 0 {
		response["limit"] = limit
	}
	writeJSON(w, http.StatusOK, response)
}

func filterModuleSummaries(modules []js.ModuleSummary, values url.Values) ([]js.ModuleSummary, int, int, int, error) {
	if len(modules) == 0 {
		return modules, 0, 0, -1, nil
	}

	strategyFilter := strings.TrimSpace(values.Get("strategy"))
	hashFilter := strings.TrimSpace(values.Get("hash"))
	runningOnly := false
	if raw := values.Get("runningOnly"); raw != "" {
		val, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, 0, 0, 0, fmt.Errorf("runningOnly must be a boolean")
		}
		runningOnly = val
	}

	limit := -1
	if raw := values.Get("limit"); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil || val < 0 {
			return nil, 0, 0, 0, fmt.Errorf("limit must be a non-negative integer")
		}
		limit = val
	}
	offset := 0
	if raw := values.Get("offset"); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil || val < 0 {
			return nil, 0, 0, 0, fmt.Errorf("offset must be a non-negative integer")
		}
		offset = val
	}

	filtered := make([]js.ModuleSummary, 0, len(modules))
	for _, module := range modules {
		if strategyFilter != "" && !strings.EqualFold(module.Name, strategyFilter) {
			continue
		}
		if filteredModule, include := applyModuleFilters(module, hashFilter, runningOnly); include {
			filtered = append(filtered, filteredModule)
		}
	}

	total := len(filtered)
	if total == 0 {
		return filtered, 0, offset, limit, nil
	}

	if offset > total {
		offset = total
	}
	end := total
	if limit >= 0 && offset+limit < end {
		end = offset + limit
	}
	paged := filtered[offset:end]
	return paged, total, offset, limit, nil
}

func applyModuleFilters(module js.ModuleSummary, hashFilter string, runningOnly bool) (js.ModuleSummary, bool) {
	filtered := module
	filtered.Revisions = filterModuleRevisions(module.Revisions, hashFilter)
	filtered.Running = filterModuleRunning(module.Running, hashFilter)

	if strings.TrimSpace(hashFilter) != "" {
		if len(filtered.Revisions) == 0 && len(filtered.Running) == 0 {
			var empty js.ModuleSummary
			return empty, false
		}
	}
	if runningOnly && len(filtered.Running) == 0 {
		var empty js.ModuleSummary
		return empty, false
	}
	return filtered, true
}

func filterModuleRevisions(revisions []js.ModuleRevision, hashFilter string) []js.ModuleRevision {
	if len(revisions) == 0 {
		return nil
	}
	normalized := strings.TrimSpace(hashFilter)
	out := make([]js.ModuleRevision, 0, len(revisions))
	for _, revision := range revisions {
		if normalized != "" && !strings.EqualFold(revision.Hash, normalized) {
			continue
		}
		revCopy := revision
		out = append(out, revCopy)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func filterModuleRunning(running []js.ModuleUsage, hashFilter string) []js.ModuleUsage {
	if len(running) == 0 {
		return nil
	}
	normalized := strings.TrimSpace(hashFilter)
	out := make([]js.ModuleUsage, 0, len(running))
	for _, usage := range running {
		if normalized != "" && !strings.EqualFold(usage.Hash, normalized) {
			continue
		}
		usageCopy := usage
		usageCopy.Instances = append([]string(nil), usage.Instances...)
		out = append(out, usageCopy)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *httpServer) buildInstanceLinksFromSummary(summary runtime.InstanceSummary) instanceLinks {
	selector := buildUsageSelector(summary.StrategySelector, summary.StrategyIdentifier, summary.StrategyHash)
	return instanceLinks{
		Self:  instanceDetailPrefix + url.PathEscape(summary.ID),
		Usage: buildModuleUsageURL(selector),
	}
}

func (s *httpServer) buildInstanceLinksFromSnapshot(snapshot runtime.InstanceSnapshot) instanceLinks {
	selector := buildUsageSelector(snapshot.Strategy.Selector, snapshot.Strategy.Identifier, snapshot.Strategy.Hash)
	return instanceLinks{
		Self:  instanceDetailPrefix + url.PathEscape(snapshot.ID),
		Usage: buildModuleUsageURL(selector),
	}
}

func buildUsageSelector(selector, identifier, hash string) string {
	trimmed := strings.TrimSpace(selector)
	if trimmed != "" {
		return trimmed
	}
	name := strings.ToLower(strings.TrimSpace(identifier))
	normalizedHash := strings.TrimSpace(hash)
	if name == "" || normalizedHash == "" {
		return ""
	}
	return fmt.Sprintf("%s@%s", name, normalizedHash)
}

func buildModuleUsageURL(selector string) string {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return ""
	}
	return strategyModulePrefix + url.PathEscape(trimmed) + strategyUsageSuffix
}

func (s *httpServer) createStrategyModule(w http.ResponseWriter, r *http.Request) {
	if s.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "strategy manager unavailable")
		return
	}
	limitRequestBody(w, r)
	payload, err := decodeStrategyModulePayload(r)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	if strings.TrimSpace(payload.Source) == "" {
		writeError(w, http.StatusBadRequest, "source required")
		return
	}
	aliases := sanitizeAliases(payload.Aliases)
	reassign := sanitizeAliases(payload.ReassignTags)
	promote := true
	if payload.PromoteLatest != nil {
		promote = *payload.PromoteLatest
	}
	nameHint := strings.TrimSpace(payload.Name)
	opts := js.ModuleWriteOptions{
		Filename:      strings.TrimSpace(payload.Filename),
		Tag:           strings.TrimSpace(payload.Tag),
		Aliases:       aliases,
		ReassignTags:  reassign,
		PromoteLatest: promote,
	}
	if opts.Filename == "" && nameHint != "" {
		opts.Filename = fmt.Sprintf("%s.js", strings.ToLower(nameHint))
	}
	resolution, err := s.manager.UpsertStrategy([]byte(payload.Source), opts)
	if err != nil {
		s.writeStrategyModuleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"status":            "pending_refresh",
		"strategyDirectory": s.manager.StrategyDirectory(),
		"module":            moduleResolutionPayload(resolution),
	})
}

func (s *httpServer) handleStrategyModule(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, strategyModulePrefix)
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
		return
	}
	segments := splitPathSegments(trimmed)
	name := strings.TrimSpace(segments[0])
	if name == "" {
		writeError(w, http.StatusNotFound, "module identifier required")
		return
	}
	if len(segments) >= 3 && strings.EqualFold(segments[1], "tags") {
		tag := strings.TrimSpace(segments[2])
		if tag == "" {
			writeError(w, http.StatusNotFound, "tag identifier required")
			return
		}
		if len(segments) != 3 {
			writeError(w, http.StatusNotFound, "invalid module tag path")
			return
		}
		switch r.Method {
		case http.MethodPut:
			s.assignStrategyModuleTag(w, r, name, tag)
		case http.MethodDelete:
			s.deleteStrategyModuleTag(w, r, name, tag)
		default:
			methodNotAllowed(w, http.MethodPut, http.MethodDelete)
		}
		return
	}
	if len(segments) == 2 {
		switch segments[1] {
		case strings.TrimPrefix(strategySourceSuffix, "/"):
			s.getStrategyModuleSource(w, r, name)
			return
		case strings.TrimPrefix(strategyUsageSuffix, "/"):
			s.getStrategyModuleUsage(w, r, name)
			return
		default:
			writeError(w, http.StatusNotFound, "invalid module path")
			return
		}
	}
	if len(segments) != 1 {
		writeError(w, http.StatusNotFound, "invalid module path")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getStrategyModule(w, name)
	case http.MethodPut:
		s.updateStrategyModule(w, r)
	case http.MethodDelete:
		s.deleteStrategyModule(w, name)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func splitPathSegments(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func (s *httpServer) getStrategyModule(w http.ResponseWriter, name string) {
	if s.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "strategy manager unavailable")
		return
	}
	summary, err := s.manager.StrategyModule(name)
	if err != nil {
		s.writeStrategyModuleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *httpServer) getStrategyModuleUsage(w http.ResponseWriter, r *http.Request, selector string) {
	if s.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "strategy manager unavailable")
		return
	}

	query := r.URL.Query()

	includeStopped := false
	if raw := query.Get("includeStopped"); raw != "" {
		val, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "includeStopped must be a boolean")
			return
		}
		includeStopped = val
	}

	limit := -1
	if raw := query.Get("limit"); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil || val < 0 {
			writeError(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return
		}
		limit = val
	}

	offset := 0
	if raw := query.Get("offset"); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil || val < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		offset = val
	}

	usage, canonical, instances, err := s.manager.RevisionUsageDetail(selector, includeStopped)
	if err != nil {
		s.writeStrategyModuleError(w, err)
		return
	}

	total := len(instances)
	if offset > total {
		offset = total
	}
	end := total
	if limit >= 0 && offset+limit < end {
		end = offset + limit
	}
	sliced := instances[offset:end]
	responseInstances := make([]instanceSummaryResponse, 0, len(sliced))
	for _, summary := range sliced {
		responseInstances = append(responseInstances, instanceSummaryResponse{
			InstanceSummary: summary,
			Links:           s.buildInstanceLinksFromSummary(summary),
		})
	}

	var limitValue any
	if limit >= 0 {
		limitValue = limit
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"selector":  canonical,
		"strategy":  usage.Strategy,
		"hash":      usage.Hash,
		"usage":     usage,
		"instances": responseInstances,
		"total":     total,
		"offset":    offset,
		"limit":     limitValue,
	})
}

func (s *httpServer) exportStrategyRegistry(w http.ResponseWriter, _ *http.Request) {
	if s.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "strategy manager unavailable")
		return
	}
	registry, usage, err := s.manager.RegistryExport()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"registry": registry,
		"usage":    usage,
	})
}

func (s *httpServer) updateStrategyModule(w http.ResponseWriter, r *http.Request) {
	if s.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "strategy manager unavailable")
		return
	}
	limitRequestBody(w, r)
	payload, err := decodeStrategyModulePayload(r)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	source := payload.Source
	if source == "" {
		writeError(w, http.StatusBadRequest, "source required")
		return
	}
	aliases := sanitizeAliases(payload.Aliases)
	reassign := sanitizeAliases(payload.ReassignTags)
	promote := true
	if payload.PromoteLatest != nil {
		promote = *payload.PromoteLatest
	}
	nameHint := strings.TrimSpace(payload.Name)
	opts := js.ModuleWriteOptions{
		Filename:      strings.TrimSpace(payload.Filename),
		Tag:           strings.TrimSpace(payload.Tag),
		Aliases:       aliases,
		ReassignTags:  reassign,
		PromoteLatest: promote,
	}
	if opts.Filename == "" && nameHint != "" {
		opts.Filename = fmt.Sprintf("%s.js", strings.ToLower(nameHint))
	}
	resolution, err := s.manager.UpsertStrategy([]byte(source), opts)
	if err != nil {
		s.writeStrategyModuleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "pending_refresh",
		"strategyDirectory": s.manager.StrategyDirectory(),
		"module":            moduleResolutionPayload(resolution),
	})
}

func (s *httpServer) deleteStrategyModule(w http.ResponseWriter, name string) {
	if s.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "strategy manager unavailable")
		return
	}
	if err := s.manager.RemoveStrategy(name); err != nil {
		s.writeStrategyModuleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *httpServer) assignStrategyModuleTag(w http.ResponseWriter, r *http.Request, name, tag string) {
	if s.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "strategy manager unavailable")
		return
	}
	limitRequestBody(w, r)
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var payload strategyTagPayload
	if err := decoder.Decode(&payload); err != nil {
		writeDecodeError(w, err)
		return
	}
	hash := strings.TrimSpace(payload.Hash)
	if hash == "" {
		writeError(w, http.StatusBadRequest, "hash required")
		return
	}
	refresh := true
	if payload.Refresh != nil {
		refresh = *payload.Refresh
	}
	previous, err := s.manager.AssignStrategyTag(r.Context(), name, tag, hash, refresh)
	if err != nil {
		s.writeStrategyModuleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "tag_assigned",
		"strategy":     name,
		"tag":          tag,
		"hash":         hash,
		"previousHash": previous,
		"refresh":      refresh,
	})
}

func (s *httpServer) deleteStrategyModuleTag(w http.ResponseWriter, r *http.Request, name, tag string) {
	if s.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "strategy manager unavailable")
		return
	}
	allowOrphan := false
	if raw := r.URL.Query().Get("allowOrphan"); raw != "" {
		val, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "allowOrphan must be a boolean")
			return
		}
		allowOrphan = val
	}
	hash, err := s.manager.DeleteStrategyTag(name, tag, allowOrphan)
	if err != nil {
		s.writeStrategyModuleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "tag_deleted",
		"strategy":    name,
		"tag":         tag,
		"hash":        hash,
		"allowOrphan": allowOrphan,
	})
}

func (s *httpServer) getStrategyModuleSource(w http.ResponseWriter, _ *http.Request, name string) {
	if s.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "strategy manager unavailable")
		return
	}
	source, err := s.manager.StrategySource(name)
	if err != nil {
		s.writeStrategyModuleError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(source)
}

func (s *httpServer) refreshStrategies(w http.ResponseWriter, r *http.Request) {
	if s.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "strategy manager unavailable")
		return
	}
	limitRequestBody(w, r)
	defer func() { _ = r.Body.Close() }()

	var payload strategyRefreshPayload
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
			writeDecodeError(w, err)
			return
		}
	}

	if len(payload.Hashes) == 0 && len(payload.Strategies) == 0 {
		if err := s.manager.RefreshJavaScriptStrategies(r.Context()); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "refreshed"})
		return
	}

	results, err := s.manager.RefreshJavaScriptStrategiesWithTargets(r.Context(), runtime.RefreshTargets{
		Hashes:     payload.Hashes,
		Strategies: payload.Strategies,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "partial_refresh",
		"results": results,
	})
}

func sanitizeAliases(aliases []string) []string {
	if len(aliases) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(aliases))
	out := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		trimmed := strings.TrimSpace(alias)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, lower)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func moduleResolutionPayload(res js.ModuleResolution) map[string]any {
	if res.Name == "" && res.Hash == "" {
		return nil
	}
	payload := map[string]any{
		"name": res.Name,
		"hash": res.Hash,
		"tag":  res.Tag,
	}
	if res.Alias != "" {
		payload["alias"] = res.Alias
	}
	if res.Module != nil {
		payload["file"] = res.Module.Filename
		payload["path"] = res.Module.Path
	}
	return payload
}

func (s *httpServer) listProviders(w http.ResponseWriter, _ *http.Request) {
	if s.providers == nil {
		writeJSON(w, http.StatusOK, map[string]any{"providers": []provider.RuntimeMetadata{}})
		return
	}
	metadata := s.providers.ProviderMetadataSnapshot()
	usage := s.providerUsage()
	for i := range metadata {
		nameKey := strings.ToLower(strings.TrimSpace(metadata[i].Name))
		if dependents, ok := usage[nameKey]; ok {
			metadata[i].DependentInstances = cloneStringSlice(dependents)
			metadata[i].DependentInstanceCount = len(dependents)
		} else {
			metadata[i].DependentInstances = []string{}
			metadata[i].DependentInstanceCount = 0
		}
	}
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
	detail, err := s.providers.Create(r.Context(), spec, false)
	if err != nil {
		s.writeProviderError(w, err)
		return
	}
	if enabled {
		if _, err := s.providers.StartProviderAsync(spec.Name); err != nil {
			s.writeProviderError(w, err)
			return
		}
	}
	location := providerDetailPrefix + spec.Name
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusAccepted, detail)
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
	dependents := s.instancesUsingProvider(name)
	meta.DependentInstances = dependents
	meta.DependentInstanceCount = len(dependents)
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
		dependents := s.instancesUsingProvider(name)
		if len(dependents) > 0 {
			writeError(w, http.StatusConflict, fmt.Sprintf("provider %s is in use by instances: %s", name, strings.Join(dependents, ", ")))
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
	switch action {
	case "start":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if s.providers == nil {
			writeError(w, http.StatusServiceUnavailable, "provider manager unavailable")
			return
		}
		detail, err := s.providers.StartProviderAsync(name)
		if err != nil {
			s.writeProviderError(w, err)
			return
		}
		location := providerDetailPrefix + name
		w.Header().Set("Location", location)
		writeJSON(w, http.StatusAccepted, detail)
	case "stop":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if s.providers == nil {
			writeError(w, http.StatusServiceUnavailable, "provider manager unavailable")
			return
		}
		detail, err := s.providers.StopProvider(name)
		if err != nil {
			s.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	case providerBalancesSuffix:
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleProviderBalances(w, r, name)
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
	responses := make([]instanceSummaryResponse, 0, len(instances))
	for _, summary := range instances {
		responses = append(responses, instanceSummaryResponse{
			InstanceSummary: summary,
			Links:           s.buildInstanceLinksFromSummary(summary),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": responses})
}

func (s *httpServer) createInstance(w http.ResponseWriter, r *http.Request) {
	limitRequestBody(w, r)
	spec, err := decodeInstanceSpec(r)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	if _, err := s.manager.Create(spec); err != nil {
		s.writeManagerError(w, err)
		return
	}
	snapshot, _ := s.manager.Instance(spec.ID)
	response := instanceSnapshotResponse{
		InstanceSnapshot: snapshot,
		Links:            s.buildInstanceLinksFromSnapshot(snapshot),
	}
	writeJSON(w, http.StatusCreated, response)
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

func (s *httpServer) handleContextBackupExport(w http.ResponseWriter, _ *http.Request) {
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
	var result contextBackup
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
		response := instanceSnapshotResponse{
			InstanceSnapshot: snapshot,
			Links:            s.buildInstanceLinksFromSnapshot(snapshot),
		}
		writeJSON(w, http.StatusOK, response)
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
		response := instanceSnapshotResponse{
			InstanceSnapshot: snapshot,
			Links:            s.buildInstanceLinksFromSnapshot(snapshot),
		}
		writeJSON(w, http.StatusOK, response)
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
	switch action {
	case "start":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if s.manager == nil {
			writeError(w, http.StatusServiceUnavailable, "lambda manager unavailable")
			return
		}
		if err := s.manager.Start(r.Context(), id); err != nil {
			s.writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "id": id, "action": action})
	case "stop":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if s.manager == nil {
			writeError(w, http.StatusServiceUnavailable, "lambda manager unavailable")
			return
		}
		if err := s.manager.Stop(id); err != nil {
			s.writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "id": id, "action": action})
	case instanceOrdersSuffix:
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleInstanceOrders(w, r, id)
	case instanceExecutionsSuffix:
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleInstanceExecutions(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "unsupported action")
	}
}

func (s *httpServer) handleInstanceOrders(w http.ResponseWriter, r *http.Request, id string) {
	if s.orderStore == nil {
		writeError(w, http.StatusServiceUnavailable, "order store unavailable")
		return
	}
	values := r.URL.Query()
	limit, err := parseLimitParam(values.Get("limit"), defaultOrdersLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	states := values["state"]
	for i, state := range states {
		states[i] = strings.TrimSpace(state)
	}
	provider := strings.TrimSpace(values.Get("provider"))
	records, err := s.orderStore.ListOrders(r.Context(), orderstore.OrderQuery{
		StrategyInstance: id,
		Provider:         provider,
		States:           states,
		Limit:            limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response := map[string]any{
		"orders": records,
		"count":  len(records),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *httpServer) handleInstanceExecutions(w http.ResponseWriter, r *http.Request, id string) {
	if s.orderStore == nil {
		writeError(w, http.StatusServiceUnavailable, "order store unavailable")
		return
	}
	values := r.URL.Query()
	limit, err := parseLimitParam(values.Get("limit"), defaultExecutionsLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider := strings.TrimSpace(values.Get("provider"))
	orderID := strings.TrimSpace(values.Get("orderId"))
	records, err := s.orderStore.ListExecutions(r.Context(), orderstore.ExecutionQuery{
		StrategyInstance: id,
		Provider:         provider,
		OrderID:          orderID,
		Limit:            limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response := map[string]any{
		"executions": records,
		"count":      len(records),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *httpServer) handleProviderBalances(w http.ResponseWriter, r *http.Request, name string) {
	if s.orderStore == nil {
		writeError(w, http.StatusServiceUnavailable, "order store unavailable")
		return
	}
	values := r.URL.Query()
	limit, err := parseLimitParam(values.Get("limit"), defaultBalancesLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	asset := strings.TrimSpace(values.Get("asset"))
	records, err := s.orderStore.ListBalances(r.Context(), orderstore.BalanceQuery{
		Provider: strings.TrimSpace(name),
		Asset:    asset,
		Limit:    limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response := map[string]any{
		"balances": records,
		"count":    len(records),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *httpServer) listOutbox(w http.ResponseWriter, r *http.Request) {
	if s.outboxStore == nil {
		writeError(w, http.StatusServiceUnavailable, "outbox store unavailable")
		return
	}
	limit, err := parseLimitParam(r.URL.Query().Get("limit"), defaultOutboxLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	records, err := s.outboxStore.ListPending(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response := map[string]any{
		"events": records,
		"count":  len(records),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *httpServer) handleOutboxEntry(w http.ResponseWriter, r *http.Request) {
	idSegment := strings.TrimPrefix(r.URL.Path, outboxDetailPrefix)
	idSegment = strings.Trim(idSegment, "/")
	if idSegment == "" {
		writeError(w, http.StatusNotFound, "outbox entry id required")
		return
	}
	id, err := strconv.ParseInt(idSegment, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid outbox entry id")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.deleteOutboxEntry(w, r, id)
	default:
		methodNotAllowed(w, http.MethodDelete)
	}
}

func (s *httpServer) deleteOutboxEntry(w http.ResponseWriter, r *http.Request, id int64) {
	if s.outboxStore == nil {
		writeError(w, http.StatusServiceUnavailable, "outbox store unavailable")
		return
	}
	if err := s.outboxStore.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":     id,
		"status": "deleted",
	})
}

func parseLimitParam(raw string, fallback int) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid limit")
	}
	if value > maxListLimit {
		return maxListLimit, nil
	}
	return value, nil
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
	case errors.Is(err, provider.ErrProviderStarting):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, provider.ErrProviderNotRunning):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *httpServer) writeStrategyModuleError(w http.ResponseWriter, err error) {
	if diagErr, ok := js.AsDiagnosticError(err); ok {
		diagnostics := diagErr.Diagnostics()
		payload := map[string]any{
			"status":  "error",
			"error":   "strategy_validation_failed",
			"message": diagErr.Error(),
		}
		if len(diagnostics) > 0 {
			payload["diagnostics"] = diagnostics
		}
		if payload["message"] == "" {
			payload["message"] = "strategy validation failed"
		}
		writeJSON(w, http.StatusUnprocessableEntity, payload)
		return
	}
	switch {
	case errors.Is(err, js.ErrModuleNotFound):
		writeError(w, http.StatusNotFound, err.Error())
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

func decodeStrategyModulePayload(r *http.Request) (strategyModulePayload, error) {
	defer func() {
		_ = r.Body.Close()
	}()
	var payload strategyModulePayload
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
	cleanConfig := sanitizeAdapterConfig(payload.Adapter.Config)
	if len(cleanConfig) > 0 {
		adapterConfig["config"] = cleanConfig
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

func sanitizeAdapterConfig(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	clean := make(map[string]any, len(input))
	for key, value := range input {
		if key == "" {
			continue
		}
		if sanitized, ok := sanitizeConfigValue(value); ok {
			clean[key] = sanitized
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func sanitizeConfigValue(value any) (any, bool) {
	switch v := value.(type) {
	case nil:
		return nil, false
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, false
		}
		return trimmed, true
	case []any:
		clean, ok := sanitizeConfigSlice(v)
		if !ok {
			return nil, false
		}
		return clean, true
	case map[string]any:
		clean := sanitizeAdapterConfig(v)
		if len(clean) == 0 {
			return nil, false
		}
		return clean, true
	default:
		return v, true
	}
}

func sanitizeConfigSlice(values []any) ([]any, bool) {
	if len(values) == 0 {
		return nil, false
	}
	clean := make([]any, 0, len(values))
	for _, elem := range values {
		if sanitized, ok := sanitizeConfigValue(elem); ok {
			clean = append(clean, sanitized)
		}
	}
	if len(clean) == 0 {
		return nil, false
	}
	return clean, true
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
	if len(cfg.AllowedOrderTypes) > 0 {
		normalized := make([]string, 0, len(cfg.AllowedOrderTypes))
		seen := make(map[string]struct{}, len(cfg.AllowedOrderTypes))
		for _, ot := range cfg.AllowedOrderTypes {
			trimmed := strings.TrimSpace(ot)
			if trimmed == "" {
				continue
			}
			key := strings.ToLower(trimmed)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			normalized = append(normalized, trimmed)
		}
		cfg.AllowedOrderTypes = normalized
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
	if err := ensurePositiveDecimal("maxPositionSize", cfg.MaxPositionSize); err != nil {
		return err
	}
	if err := ensurePositiveDecimal("maxNotionalValue", cfg.MaxNotionalValue); err != nil {
		return err
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

func ensurePositiveDecimal(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s required", field)
	}
	dec, err := decimal.NewFromString(trimmed)
	if err != nil {
		return fmt.Errorf("%s must be a valid decimal number", field)
	}
	if dec.Cmp(decimal.Zero) <= 0 {
		return fmt.Errorf("%s must be greater than 0", field)
	}
	return nil
}

func (s *httpServer) providerUsage() map[string][]string {
	usage := make(map[string][]string)
	if s.manager == nil {
		return usage
	}
	appendProvider := func(list *[]string, seen map[string]struct{}, name string) {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return
		}
		normalized := strings.ToLower(trimmed)
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		*list = append(*list, normalized)
	}
	summaries := s.manager.Instances()
	for _, summary := range summaries {
		if s.isBaselineLambda(summary.ID) {
			continue
		}
		normalizedProviders := make([]string, 0, len(summary.Providers))
		seen := make(map[string]struct{}, len(summary.Providers))
		for _, providerName := range summary.Providers {
			appendProvider(&normalizedProviders, seen, providerName)
		}
		if len(normalizedProviders) == 0 {
			if snapshot, ok := s.manager.Instance(summary.ID); ok {
				for _, providerName := range snapshot.Providers {
					appendProvider(&normalizedProviders, seen, providerName)
				}
				if len(normalizedProviders) == 0 && len(snapshot.ProviderSymbols) > 0 {
					providerNames := make([]string, 0, len(snapshot.ProviderSymbols))
					for name := range snapshot.ProviderSymbols {
						providerNames = append(providerNames, name)
					}
					sort.Strings(providerNames)
					for _, providerName := range providerNames {
						appendProvider(&normalizedProviders, seen, providerName)
					}
				}
			}
		}
		for _, key := range normalizedProviders {
			usage[key] = append(usage[key], summary.ID)
		}
	}
	for key := range usage {
		sort.Strings(usage[key])
	}
	return usage
}

func (s *httpServer) instancesUsingProvider(name string) []string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return []string{}
	}
	usage := s.providerUsage()
	list := usage[strings.ToLower(trimmed)]
	if len(list) == 0 {
		return []string{}
	}
	return cloneStringSlice(list)
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
		key := strings.ToLower(sanitized.Name)
		targetProviders[key] = sanitized
		orderedProviders = append(orderedProviders, sanitized)
	}

	targetLambdas := make(map[string]struct{}, len(payload.Lambdas))
	restoredSpecs := make([]config.LambdaSpec, 0, len(payload.Lambdas))
	for _, spec := range payload.Lambdas {
		copied := config.LambdaSpec{
			ID: strings.TrimSpace(spec.ID),
			Strategy: config.LambdaStrategySpec{
				Identifier: strings.TrimSpace(spec.Strategy.Identifier),
				Config:     cloneAnyMap(spec.Strategy.Config),
				Selector:   strings.TrimSpace(spec.Strategy.Selector),
				Tag:        strings.TrimSpace(spec.Strategy.Tag),
				Hash:       strings.TrimSpace(spec.Strategy.Hash),
			},
			ProviderSymbols: cloneProviderSymbolsMap(spec.ProviderSymbols),
			Providers:       cloneStringSlice(spec.Providers),
		}
		if copied.ID == "" {
			return fmt.Errorf("lambda id required")
		}
		restoredSpecs = append(restoredSpecs, copied)
		targetLambdas[strings.ToLower(copied.ID)] = struct{}{}
	}
	for _, spec := range restoredSpecs {
		for _, providerName := range spec.Providers {
			trimmed := strings.TrimSpace(providerName)
			if trimmed == "" {
				return fmt.Errorf("lambda %s requires at least one provider", spec.ID)
			}
			if s.isBaselineProvider(trimmed) {
				continue
			}
			if _, ok := targetProviders[strings.ToLower(trimmed)]; !ok {
				return fmt.Errorf("lambda %s references unknown provider %s", spec.ID, trimmed)
			}
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

	existing := s.providers.SanitizedProviderSpecs()
	for _, spec := range existing {
		if s.isBaselineProvider(spec.Name) {
			continue
		}
		if _, ok := targetProviders[strings.ToLower(spec.Name)]; !ok {
			dependents := s.instancesUsingProvider(spec.Name)
			if len(dependents) > 0 {
				return fmt.Errorf("provider %s is in use by instances: %s", spec.Name, strings.Join(dependents, ", "))
			}
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

	restored := make([]config.LambdaSpec, 0, len(restoredSpecs))
	for _, spec := range restoredSpecs {
		if s.isBaselineLambda(spec.ID) {
			continue
		}
		if err := s.manager.Remove(spec.ID); err != nil && !errors.Is(err, runtime.ErrInstanceNotFound) {
			return fmt.Errorf("prepare lambda %s: %w", spec.ID, err)
		}
		restored = append(restored, spec)
	}

	for _, spec := range restored {
		if _, err := s.manager.Create(spec); err != nil {
			return fmt.Errorf("restore lambda %s: %w", spec.ID, err)
		}
	}

	if payload.Risk.MaxPositionSize != "" || payload.Risk.MaxNotionalValue != "" || payload.Risk.NotionalCurrency != "" {
		s.manager.ApplyRiskConfig(payload.Risk)
	}
	return nil
}

func lambdaSpecFromSnapshot(snapshot runtime.InstanceSnapshot) config.LambdaSpec {
	return config.LambdaSpec{
		ID: snapshot.ID,
		Strategy: config.LambdaStrategySpec{
			Identifier: snapshot.Strategy.Identifier,
			Config:     cloneAnyMap(snapshot.Strategy.Config),
			Selector:   snapshot.Strategy.Selector,
			Tag:        snapshot.Strategy.Tag,
			Hash:       snapshot.Strategy.Hash,
		},
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
	if id == "" || s.manager == nil {
		return false
	}
	return s.manager.IsBaseline(id)
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
		w.Header().Set("Access-Control-Allow-Headers", allowedCORSHeaders(r))
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func allowedCORSHeaders(r *http.Request) string {
	defaults := []string{"Content-Type", "Authorization", "X-Meltica-Request-Id"}
	seen := make(map[string]struct{}, len(defaults))
	for _, header := range defaults {
		seen[strings.ToLower(header)] = struct{}{}
	}
	requested := strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",")
	for _, raw := range requested {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		defaults = append(defaults, trimmed)
	}
	return strings.Join(defaults, ", ")
}
