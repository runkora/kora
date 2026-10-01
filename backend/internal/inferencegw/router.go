package inferencegw

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ResolvedModelInfo is the policy-relevant identity of a model request —
// the vendor family and the virtual model name.
// Used to build the fine-grained policy scope: inference:{Family}:{Name}:use.
type ResolvedModelInfo struct {
	Family string // e.g. "anthropic"
	Alias  string // e.g. "claude-sonnet-4-6" (the virtual model Name)
}

// CatalogScope describes a scope entry derived from the virtual model catalog,
// ready for registration with the policy service vocabulary.
type CatalogScope struct {
	Scope        string   // e.g. "inference:anthropic:claude-sonnet-4-6:use"
	PathSegments []string // e.g. ["anthropic", "claude-sonnet-4-6"]
	Description  string   // human-readable label for the UI
}

const (
	// defaultMaxAttemptsPerTarget is the maximum number of retries for a single
	// provider before moving to the next target in the failover chain.
	defaultMaxAttemptsPerTarget = 2

	// defaultMaxTotalAttempts is the maximum number of total provider calls
	// across all targets before the request is failed.
	defaultMaxTotalAttempts = 4
)

// RouteTarget describes a single provider + model pair resolved by the routing
// engine. Pricing is attached so the service layer can compute cost attribution
// without a second cache lookup. ModelFamily and VirtualModelID carry the
// virtual model identity so the service layer can persist them in inference_usage
// without a second catalog lookup.
type RouteTarget struct {
	Provider       ProviderName
	ModelID        string // canonical provider model ID (e.g. "anthropic.claude-3-5-sonnet-20241022-v2:0")
	ModelFamily    string // virtual model family (e.g. "anthropic")
	VirtualModelID string // consumer-facing full ID: "{family}/{name}" (e.g. "anthropic/claude-sonnet-4-6")
	Pricing        ModelPricing
}

// RoutingEngine resolves an inference request to an ordered list of provider
// targets. The service layer tries targets in order, retrying within each target
// up to MaxAttemptsPerTarget times, bounded by MaxTotalAttempts overall.
type RoutingEngine struct {
	// providers is swapped whole when providers reload. A request keeps the
	// connector it resolved; a reload never interrupts it.
	providers            atomic.Pointer[providerSet]
	maxAttemptsPerTarget int
	maxTotalAttempts     int

	// virtualCatalog is the in-memory projection of the virtual model catalog.
	// It is rebuilt from the database on every admin write via an atomic pointer
	// swap, so RouteRequest reads are never blocked — not even briefly — by a
	// concurrent catalog update.
	virtualCatalog atomic.Pointer[virtualCatalogSnapshot]

	// modelCatalog holds the most recent provider-discovered models, used only
	// for the admin Provider Catalog view. Not consulted during request routing.
	modelCatalog atomic.Pointer[[]ProviderModel]

	// discovery holds the last ListModels outcome per provider.
	discovery atomic.Pointer[map[ProviderName]discoveryResult]

	// publish orders SetProviders against discovery results. A discovery that
	// started on an older provider set publishes nothing.
	publish sync.Mutex

	// sweeps numbers each discovery as it starts. published is the newest one
	// published, so a slower, older sweep never overwrites a newer one.
	sweeps    atomic.Uint64
	published atomic.Uint64

	// notify runs onDiscovered for one sweep at a time, in sweep order.
	notify sync.Mutex

	// onDiscovered receives each provider's models after a published discovery
	// that provider completed without error.
	onDiscovered DiscoveredFunc

	// onCatalogUpdate is called after every successful LoadVirtualCatalog.
	// Wired in main.go to re-register scope vocabulary with the policy service.
	onCatalogUpdate atomic.Pointer[func([]CatalogScope)]
}

// SetCatalogUpdateHook registers a function to be called after every successful
// catalog load. The hook receives the current CatalogScopes and runs in a
// background goroutine — implementations may do I/O.
func (r *RoutingEngine) SetCatalogUpdateHook(fn func([]CatalogScope)) {
	if fn == nil {
		r.onCatalogUpdate.Store(nil)
		return
	}
	r.onCatalogUpdate.Store(&fn)
}

// DiscoveredFunc receives the full model list of one provider whose discovery succeeded.
type DiscoveredFunc func(ctx context.Context, provider ProviderName, models []ProviderModel)

// NewRoutingEngine constructs a RoutingEngine serving connectors, described by
// infos. onDiscovered, when set, runs for each provider a published discovery listed.
func NewRoutingEngine(connectors map[ProviderName]ProviderConnector, infos []ProviderInfo, onDiscovered DiscoveredFunc) *RoutingEngine {
	r := &RoutingEngine{
		maxAttemptsPerTarget: defaultMaxAttemptsPerTarget,
		maxTotalAttempts:     defaultMaxTotalAttempts,
		onDiscovered:         onDiscovered,
	}
	r.SetProviders(connectors, infos)
	return r
}

// providerSet is one generation of providers: their connectors and their
// descriptions for the admin view.
type providerSet struct {
	connectors map[ProviderName]ProviderConnector
	infos      []ProviderInfo
}

// SetProviders replaces the connector set and the provider descriptions.
// Discovered models of providers that are gone are dropped.
func (r *RoutingEngine) SetProviders(connectors map[ProviderName]ProviderConnector, infos []ProviderInfo) {
	if connectors == nil {
		connectors = map[ProviderName]ProviderConnector{}
	}
	r.publish.Lock()
	defer r.publish.Unlock()
	r.providers.Store(&providerSet{connectors: connectors, infos: infos})
	if catalog := r.modelCatalog.Load(); catalog != nil {
		kept := make([]ProviderModel, 0, len(*catalog))
		for _, m := range *catalog {
			if _, ok := connectors[m.Provider]; ok {
				kept = append(kept, m)
			}
		}
		r.modelCatalog.Store(&kept)
	}
}

func (r *RoutingEngine) connectorMap() map[ProviderName]ProviderConnector {
	return r.providers.Load().connectors
}

// MaxAttemptsPerTarget returns the maximum retry count within a single target.
func (r *RoutingEngine) MaxAttemptsPerTarget() int { return r.maxAttemptsPerTarget }

// MaxTotalAttempts returns the maximum total call count across all targets.
func (r *RoutingEngine) MaxTotalAttempts() int { return r.maxTotalAttempts }

// Connector returns the ProviderConnector for the given provider, if enabled.
func (r *RoutingEngine) Connector(p ProviderName) (ProviderConnector, bool) {
	c, ok := r.connectorMap()[p]
	return c, ok
}

// LoadVirtualCatalog atomically replaces the in-memory catalog snapshot and
// fires the catalog update hook. Called at startup and after every admin write.
func (r *RoutingEngine) LoadVirtualCatalog(snap *virtualCatalogSnapshot) {
	r.virtualCatalog.Store(snap)
	if fn := r.onCatalogUpdate.Load(); fn != nil && *fn != nil {
		go (*fn)(r.CatalogScopes())
	}
}

// RouteRequest resolves a virtual model ID to an ordered list of RouteTargets
// and the routing strategy that was applied. The service layer iterates targets
// in order, retrying within each up to MaxAttemptsPerTarget times.
//
// Only virtual model IDs ("family/name", e.g. "anthropic/claude-sonnet-4-6")
// are accepted. Provider-prefixed IDs and bare model names are not valid consumer
// inputs — the provider is an internal platform concern.
func (r *RoutingEngine) RouteRequest(
	ctx context.Context,
	req ChatCompletionRequest,
) ([]RouteTarget, RoutingStrategy, error) {
	_ = ctx

	snap := r.virtualCatalog.Load()
	if snap == nil {
		return nil, "", ErrNoCatalog
	}

	vm, ok := snap.byID[req.Model]
	if !ok {
		return nil, "", fmt.Errorf("%w: model %q not found in catalog", ErrInvalidRequest, req.Model)
	}
	if !vm.Enabled {
		return nil, "", fmt.Errorf("%w: model %q is disabled", ErrInvalidRequest, req.Model)
	}

	targets, strategy, err := r.targetsForVirtualModel(vm, snap)
	if err != nil {
		return nil, "", err
	}
	return targets, strategy, nil
}

// targetsForVirtualModel builds the ordered RouteTarget slice from a virtual
// model's enabled routes, applying the configured routing strategy.
func (r *RoutingEngine) targetsForVirtualModel(
	vm *VirtualModel,
	snap *virtualCatalogSnapshot,
) ([]RouteTarget, RoutingStrategy, error) {
	enabled := r.enabledRoutes(vm, snap)
	if len(enabled) == 0 {
		return nil, "", fmt.Errorf("%w: no enabled routes for model %q", ErrNoProvidersEnabled, vm.FullID())
	}

	// The switch only determines ordering; family propagation happens once below.
	var ordered []VirtualModelRoute
	var strategy RoutingStrategy

	switch vm.Strategy {
	case VirtualStrategyPriority:
		ordered = enabled
		strategy = StrategyPriority

	case VirtualStrategyWeighted:
		ordered = weightedOrder(enabled)
		strategy = StrategyWeighted

	case VirtualStrategyLowestCost:
		// SliceStable preserves the configured priority order as a tiebreaker
		// when two routes have equal unit costs.
		sort.SliceStable(enabled, func(i, j int) bool {
			return enabled[i].unitCost() < enabled[j].unitCost()
		})
		ordered = enabled
		strategy = StrategyLowestCost

	default:
		return nil, "", fmt.Errorf("unknown virtual model strategy %q", vm.Strategy)
	}

	return routesToTargets(ordered, vm.Family, vm.FullID()), strategy, nil
}

// enabledRoutes returns the routes for vm that are individually enabled and
// whose underlying provider model has not been disabled by a platform admin.
// Routes are returned in ascending Priority order.
func (r *RoutingEngine) enabledRoutes(vm *VirtualModel, snap *virtualCatalogSnapshot) []VirtualModelRoute {
	connectors := r.connectorMap()
	out := make([]VirtualModelRoute, 0, len(vm.Routes))
	for _, route := range vm.Routes {
		if !route.Enabled {
			continue
		}
		if snap.isRouteDisabled(route.Provider, route.ModelID) {
			continue
		}
		if _, ok := connectors[route.Provider]; !ok {
			continue // provider not configured in this deployment
		}
		out = append(out, route)
	}
	// Ensure ascending priority order. The SQL query includes ORDER BY rt.priority ASC,
	// but this re-sort is cheap and guards against any future query changes.
	sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}

// weightedOrder selects one route probabilistically by weight, then appends the
// remaining routes in ascending priority order as the failover chain.
func weightedOrder(routes []VirtualModelRoute) []VirtualModelRoute {
	if len(routes) == 1 {
		return routes
	}

	total := 0
	for _, r := range routes {
		total += r.Weight
	}
	if total <= 0 {
		// Weights are misconfigured — fall back to priority order.
		return routes
	}

	pick := rand.IntN(total) // #nosec G404 — route selection is not security-sensitive
	cum := 0
	selected := 0
	for i, r := range routes {
		cum += r.Weight
		if pick < cum {
			selected = i
			break
		}
	}

	result := make([]VirtualModelRoute, 0, len(routes))
	result = append(result, routes[selected])
	for i, r := range routes {
		if i != selected {
			result = append(result, r)
		}
	}
	return result
}

// routesToTargets converts VirtualModelRoutes to RouteTargets, attaching the
// model family and virtual model full ID for usage attribution. Both come from
// the parent VirtualModel and are passed explicitly — they are not stored on
// the route row.
func routesToTargets(routes []VirtualModelRoute, family, virtualModelID string) []RouteTarget {
	targets := make([]RouteTarget, len(routes))
	for i, r := range routes {
		targets[i] = RouteTarget{
			Provider:       r.Provider,
			ModelID:        r.ModelID,
			ModelFamily:    family,
			VirtualModelID: virtualModelID,
			Pricing:        r.pricing(),
		}
	}
	return targets
}

// IsWildcardModel always returns false. Wildcard routing is not supported with
// the virtual model catalog — every request must specify a virtual model ID.
func (r *RoutingEngine) IsWildcardModel(_ string) bool { return false }

// ResolveModelForPolicy maps a virtual model ID to the family and name used to
// construct the policy scope inference:{family}:{name}:use.
// Returns ok=false when the catalog is empty or the model is not found.
func (r *RoutingEngine) ResolveModelForPolicy(model string) (ResolvedModelInfo, bool) {
	snap := r.virtualCatalog.Load()
	if snap == nil {
		return ResolvedModelInfo{}, false
	}
	vm, ok := snap.byID[model]
	if !ok {
		return ResolvedModelInfo{}, false
	}
	return ResolvedModelInfo{Family: vm.Family, Alias: vm.Name}, true
}

// ResolveTargetForPolicy maps an internal (provider, modelID) pair back to a
// virtual model's policy identity. Used by filterTargetsByPolicy when policy
// must be checked per target rather than per request.
// Uses the byRouteKey reverse index for O(1) lookup.
func (r *RoutingEngine) ResolveTargetForPolicy(provider ProviderName, modelID string) (ResolvedModelInfo, bool) {
	snap := r.virtualCatalog.Load()
	if snap == nil {
		return ResolvedModelInfo{}, false
	}
	vm, ok := snap.byRouteKey[string(provider)+"/"+modelID]
	if !ok {
		return ResolvedModelInfo{}, false
	}
	return ResolvedModelInfo{Family: vm.Family, Alias: vm.Name}, true
}

// CatalogScopes returns the deduplicated set of policy scope declarations
// derived from the virtual model catalog. Includes one family-level scope per
// vendor and one model-level scope per virtual model.
//
// Both enabled and disabled virtual models are included — disabling a model
// preserves its vocab entry so existing grants remain intact. Only deleting
// a model removes its scope from the vocabulary.
func (r *RoutingEngine) CatalogScopes() []CatalogScope {
	snap := r.virtualCatalog.Load()
	if snap == nil {
		return nil
	}

	seen := make(map[string]bool, len(snap.all)*2)
	var scopes []CatalogScope

	for _, vm := range snap.all {
		familyScope := fmt.Sprintf("inference:%s:use", vm.Family)
		if !seen[familyScope] {
			seen[familyScope] = true
			scopes = append(scopes, CatalogScope{
				Scope:        familyScope,
				PathSegments: []string{vm.Family},
				Description:  fmt.Sprintf("Use any %s model", vm.Family),
			})
		}

		modelScope := vm.PolicyScope()
		if !seen[modelScope] {
			seen[modelScope] = true
			scopes = append(scopes, CatalogScope{
				Scope:        modelScope,
				PathSegments: []string{vm.Family, vm.Name},
				Description:  vm.DisplayName,
			})
		}
	}

	return scopes
}

// ListModels queries every enabled connector concurrently and returns the
// aggregated provider model list. Used exclusively for the admin Provider
// Catalog view — it does NOT drive routing or the consumer /v1/models endpoint.
//
// A connector may return models with an error: they are kept, and the error is
// reported in its provider status. Results are published only if the provider
// set has not been replaced since discovery started.
func (r *RoutingEngine) ListModels(ctx context.Context) ([]ProviderModel, error) {
	sweep := r.sweeps.Add(1)
	set := r.providers.Load()
	connectors := set.connectors
	if len(connectors) == 0 {
		r.publishDiscovery(ctx, sweep, set, map[ProviderName]discoveryResult{}, []ProviderModel{}, nil)
		return nil, ErrNoProvidersEnabled
	}

	type result struct {
		provider ProviderName
		models   []ProviderModel
		err      error
	}

	ch := make(chan result, len(connectors))
	for name, c := range connectors {
		go func() {
			ms, err := c.ListModels(ctx)
			ch <- result{provider: name, models: ms, err: err}
		}()
	}

	var models []ProviderModel
	var firstErr error
	discovered := make(map[ProviderName]discoveryResult, len(connectors))
	listed := make(map[ProviderName][]ProviderModel, len(connectors))
	now := time.Now().UTC()
	for range connectors {
		res := <-ch
		d := discoveryResult{at: now, models: len(res.models)}
		if res.err != nil {
			d.err = res.err.Error()
			if firstErr == nil {
				firstErr = res.err
			}
		} else {
			listed[res.provider] = res.models
		}
		discovered[res.provider] = d
		models = append(models, res.models...)
	}
	r.publishDiscovery(ctx, sweep, set, discovered, models, listed)

	if len(models) > 0 {
		return models, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, ErrNoProvidersEnabled
}

// publishDiscovery stores a discovery outcome unless set has been replaced or a
// newer sweep has published. An empty model list keeps the previous catalog.
func (r *RoutingEngine) publishDiscovery(ctx context.Context, sweep uint64, set *providerSet, discovered map[ProviderName]discoveryResult, models []ProviderModel, listed map[ProviderName][]ProviderModel) {
	r.publish.Lock()
	if r.providers.Load() != set || sweep < r.published.Load() {
		r.publish.Unlock()
		return
	}
	r.published.Store(sweep)
	r.discovery.Store(&discovered)
	if len(models) > 0 || len(set.connectors) == 0 {
		r.modelCatalog.Store(&models)
	}
	r.publish.Unlock()
	if r.onDiscovered == nil {
		return
	}
	r.notify.Lock()
	defer r.notify.Unlock()
	// A newer sweep published while this one waited and will notify instead.
	if sweep != r.published.Load() {
		return
	}
	for provider, ms := range listed {
		r.onDiscovered(ctx, provider, ms)
	}
}

// ProviderModels returns a copy of the most recently discovered provider model
// list. Returns nil if no ListModels call has succeeded yet.
// A copy is returned so callers cannot accidentally mutate the shared atomic slice.
func (r *RoutingEngine) ProviderModels() []ProviderModel {
	catalog := r.modelCatalog.Load()
	if catalog == nil {
		return nil
	}
	out := make([]ProviderModel, len(*catalog))
	copy(out, *catalog)
	return out
}

// VirtualModels returns all virtual models from the current catalog snapshot,
// including disabled ones. Callers are responsible for computing availability
// based on vm.Enabled and route state. Returns nil if no catalog has been loaded.
func (r *RoutingEngine) VirtualModels() []VirtualModel {
	snap := r.virtualCatalog.Load()
	if snap == nil {
		return nil
	}
	out := make([]VirtualModel, len(snap.all))
	copy(out, snap.all)
	return out
}
