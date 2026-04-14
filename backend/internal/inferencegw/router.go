package inferencegw

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
)

// ResolvedModelInfo is the policy-relevant identity of a model request —
// the vendor family and the clean canonical alias.
// Used to build the fine-grained policy scope: inference:{Family}:{Alias}:use.
type ResolvedModelInfo struct {
	Family string // e.g. "anthropic"
	Alias  string // e.g. "claude-3-5-haiku" (shortest clean alias)
}

// CatalogScope describes a scope entry derived from the live model catalog,
// ready for registration with the policy service vocabulary.
type CatalogScope struct {
	Scope        string   // e.g. "inference:anthropic:claude-3-5-haiku:use"
	PathSegments []string // e.g. ["anthropic", "claude-3-5-haiku"]
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
// without a second cache lookup.
type RouteTarget struct {
	Provider ProviderName
	ModelID  string
	Pricing  ModelPricing
}

// RoutingEngine resolves an inference request to an ordered list of provider
// targets. The service layer tries targets in order, retrying within each target
// up to MaxAttemptsPerTarget times, bounded by MaxTotalAttempts overall.
type RoutingEngine struct {
	connectors           map[ProviderName]ProviderConnector
	pricing              PricingCache
	maxAttemptsPerTarget int
	maxTotalAttempts     int

	// aliasTable maps "provider/alias" → "provider/canonicalModelID".
	// It is rebuilt on every successful ListModels call via an atomic pointer
	// swap, so RouteRequest reads are never blocked — not even briefly — by a
	// concurrent ListModels write.
	aliasTable atomic.Pointer[map[string]string]

	// modelCatalog holds the full list of ProviderModels returned by the last
	// successful ListModels call. Used by lowestCostTargets and failoverChain
	// so those strategies work without a populated pricing cache.
	modelCatalog atomic.Pointer[[]ProviderModel]

	// onCatalogUpdate is called after every successful ListModels that produces
	// a non-empty catalog. Wired in main.go to re-register scope vocabulary
	// with the policy service so new models become grantable immediately.
	// Stored atomically so SetCatalogUpdateHook is safe to call concurrent
	// with ListModels.
	onCatalogUpdate atomic.Pointer[func([]CatalogScope)]
}

// SetCatalogUpdateHook registers a function to be called after every successful
// ListModels refresh. The hook receives the current CatalogScopes and runs in
// the same goroutine as the ListModels caller — implementations should be
// non-blocking (e.g. spawn their own goroutine if they do I/O).
func (r *RoutingEngine) SetCatalogUpdateHook(fn func([]CatalogScope)) {
	if fn == nil {
		r.onCatalogUpdate.Store(nil)
		return
	}
	r.onCatalogUpdate.Store(&fn)
}

// NewRoutingEngine constructs a RoutingEngine.
// connectors is the set of enabled provider connectors keyed by ProviderName.
func NewRoutingEngine(
	connectors map[ProviderName]ProviderConnector,
	pricing PricingCache,
) *RoutingEngine {
	return &RoutingEngine{
		connectors:           connectors,
		pricing:              pricing,
		maxAttemptsPerTarget: defaultMaxAttemptsPerTarget,
		maxTotalAttempts:     defaultMaxTotalAttempts,
	}
}

// MaxAttemptsPerTarget returns the maximum retry count within a single target.
func (r *RoutingEngine) MaxAttemptsPerTarget() int { return r.maxAttemptsPerTarget }

// MaxTotalAttempts returns the maximum total call count across all targets.
func (r *RoutingEngine) MaxTotalAttempts() int { return r.maxTotalAttempts }

// Connector returns the ProviderConnector for the given provider, if enabled.
func (r *RoutingEngine) Connector(p ProviderName) (ProviderConnector, bool) {
	c, ok := r.connectors[p]
	return c, ok
}

// RouteRequest resolves a request to an ordered list of RouteTargets and the
// strategy that was used. The service layer iterates targets in order.
//
// Routing strategy is determined by the request's model field:
//
//   - "*"                   → LowestCost across all enabled providers
//   - "provider/model"      → Explicit single target
//   - "failover"            → FailoverChain in canonical provider priority order
//   - "provider/model"      → falls through to explicit if "/" is present
func (r *RoutingEngine) RouteRequest(
	ctx context.Context,
	req ChatCompletionRequest,
) ([]RouteTarget, RoutingStrategy, error) {
	_ = ctx // reserved for future policy-aware routing

	switch {
	case req.Model == "*" || req.Model == "":
		targets, err := r.lowestCostTargets()
		return targets, StrategyLowestCost, err

	case req.Model == "failover":
		targets, err := r.failoverChain()
		return targets, StrategyFailoverChain, err

	case strings.Contains(req.Model, "/"):
		// Resolve clean aliases (e.g. "bedrock/claude-sonnet-4-6") to their
		// canonical routing IDs (e.g. "bedrock/us.anthropic.claude-sonnet-4-6")
		// before handing off to explicitTarget. If the model is already
		// canonical or unknown, resolveAlias returns it unchanged.
		resolved := r.resolveAlias(req.Model)
		target, err := r.explicitTarget(resolved)
		if err != nil {
			return nil, StrategyExplicit, err
		}
		return []RouteTarget{target}, StrategyExplicit, nil

	default:
		// Bare model name — try each enabled provider in canonical order.
		return r.modelAcrossProviders(req.Model)
	}
}

// ListModels queries every enabled connector concurrently and aggregates
// the results into a deduplicated list of ProviderModels.
func (r *RoutingEngine) ListModels(ctx context.Context) ([]ProviderModel, error) {
	if len(r.connectors) == 0 {
		return nil, ErrNoProvidersEnabled
	}

	type result struct {
		provider ProviderName
		models   []ProviderModel
		err      error
	}

	ch := make(chan result, len(r.connectors))
	for _, c := range r.connectors {
		go func() {
			ms, err := c.ListModels(ctx)
			ch <- result{provider: c.Name(), models: ms, err: err}
		}()
	}

	var models []ProviderModel
	var firstErr error
	for range r.connectors {
		res := <-ch
		if res.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("list models from %s: %w", res.provider, res.err)
			}
			continue
		}
		models = append(models, res.models...)
	}

	// Return partial results if at least one provider succeeded.
	if len(models) > 0 {
		// Rebuild the alias lookup table from the live model catalog.
		// Key: "provider/alias", Value: "provider/canonicalModelID".
		newTable := make(map[string]string, len(models)*3)
		for _, m := range models {
			canonicalKey := string(m.Provider) + "/" + m.ModelID
			for _, alias := range m.Aliases {
				aliasKey := string(m.Provider) + "/" + alias
				if aliasKey != canonicalKey {
					newTable[aliasKey] = canonicalKey
				}
			}
		}
		r.aliasTable.Store(&newTable)
		r.modelCatalog.Store(&models)

		if fn := r.onCatalogUpdate.Load(); fn != nil && *fn != nil {
			go (*fn)(r.CatalogScopes())
		}

		return models, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, ErrNoProvidersEnabled
}

// resolveAlias translates a potentially-aliased "provider/model" string to
// the canonical "provider/canonicalModelID" form. Returns the input unchanged
// if it is not in the alias table (i.e. it is already canonical or unknown).
func (r *RoutingEngine) resolveAlias(model string) string {
	tbl := r.aliasTable.Load()
	if tbl == nil {
		return model
	}
	if resolved, ok := (*tbl)[model]; ok {
		return resolved
	}
	return model
}

// ─── strategy implementations ─────────────────────────────────────────────────

// lowestCostTargets returns all enabled provider/model pairs sorted ascending
// by the estimated cost to process a 1k-token prompt + 1k-token completion
// (a representative unit for comparison purposes). When pricing data is
// unavailable (nop cache) the sort is a no-op and all models are returned in
// catalog order.
func (r *RoutingEngine) lowestCostTargets() ([]RouteTarget, error) {
	if len(r.connectors) == 0 {
		return nil, ErrNoProvidersEnabled
	}

	catalog := r.modelCatalog.Load()
	if catalog == nil || len(*catalog) == 0 {
		return nil, ErrNoProvidersEnabled
	}

	targets := make([]RouteTarget, 0, len(*catalog))
	for _, m := range *catalog {
		if _, ok := r.connectors[m.Provider]; !ok {
			continue
		}
		// Only include chat-capable models. lowestCostTargets is invoked by the
		// wildcard ("*") routing path for chat completions — routing to an embedding
		// or image-generation model would produce a provider error (ADR-0027).
		if !slices.Contains(m.Capabilities, CapabilityChat) {
			continue
		}
		pricing, _ := r.pricing.Price(m.Provider, m.ModelID)
		pricing.Provider = m.Provider
		pricing.ModelID = m.ModelID
		targets = append(targets, RouteTarget{
			Provider: m.Provider,
			ModelID:  m.ModelID,
			Pricing:  pricing,
		})
	}

	if len(targets) == 0 {
		return nil, ErrNoProvidersEnabled
	}

	// Insertion-sort by representative cost (small list — O(n²) is fine).
	// When pricing is unavailable all costs are 0 and order is preserved.
	unitCost := func(t RouteTarget) float64 { return t.Pricing.EstimatedCostUSD(1000, 1000) }
	for i := 1; i < len(targets); i++ {
		for j := i; j > 0 && unitCost(targets[j]) < unitCost(targets[j-1]); j-- {
			targets[j], targets[j-1] = targets[j-1], targets[j]
		}
	}

	return targets, nil
}

// explicitTarget resolves a "provider/model" string to a single RouteTarget.
func (r *RoutingEngine) explicitTarget(model string) (RouteTarget, error) {
	provider, modelID, err := parseExplicitModel(model)
	if err != nil {
		return RouteTarget{}, err
	}

	if _, ok := r.connectors[provider]; !ok {
		return RouteTarget{}, fmt.Errorf("%w: provider %q is not enabled or recognized", ErrInvalidRequest, provider)
	}

	pricing, ok := r.pricing.Price(provider, modelID)
	if !ok {
		// Model not in pricing cache — use zero-cost pricing so the call still
		// proceeds; cost attribution will be $0.00.
		pricing = ModelPricing{Provider: provider, ModelID: modelID}
	}

	return RouteTarget{
		Provider: provider,
		ModelID:  modelID,
		Pricing:  pricing,
	}, nil
}

// failoverChain returns enabled providers in canonical priority order.
// Priority: Bedrock → Vertex AI → Azure.
// Picks the first model from the live catalog for each provider.
func (r *RoutingEngine) failoverChain() ([]RouteTarget, error) {
	priority := []ProviderName{ProviderBedrock, ProviderVertexAI, ProviderAzure}

	catalog := r.modelCatalog.Load()

	targets := make([]RouteTarget, 0, len(priority))
	for _, p := range priority {
		if _, ok := r.connectors[p]; !ok {
			continue
		}
		if catalog == nil {
			continue
		}
		// Pick the first chat-capable model from the live catalog for this provider.
		for _, m := range *catalog {
			if m.Provider != p {
				continue
			}
			if !slices.Contains(m.Capabilities, CapabilityChat) {
				continue // skip embedding/image/video models
			}
			pricing, _ := r.pricing.Price(p, m.ModelID)
			pricing.Provider = p
			pricing.ModelID = m.ModelID
			targets = append(targets, RouteTarget{
				Provider: p,
				ModelID:  m.ModelID,
				Pricing:  pricing,
			})
			break
		}
	}

	if len(targets) == 0 {
		return nil, ErrNoProvidersEnabled
	}

	return targets, nil
}

// modelAcrossProviders resolves a bare model name across all enabled providers
// in canonical failover order.
//
// When the model catalog has been populated by a prior ListModels call, only
// providers that actually advertise the model (by canonical ID or alias) are
// included as targets. This prevents spurious cross-provider calls when only a
// subset of providers supports the requested model.
//
// When the catalog is empty (cold start before the first ListModels sweep) we
// fall back to broadcasting to every enabled provider so the gateway remains
// functional without a warm-up dependency.
func (r *RoutingEngine) modelAcrossProviders(modelID string) ([]RouteTarget, RoutingStrategy, error) {
	priority := []ProviderName{ProviderBedrock, ProviderVertexAI, ProviderAzure}

	// Build a set of providers known to carry this model and resolve any alias
	// to the canonical model ID the provider expects.
	type providerEntry struct {
		canonicalID string
	}
	var catalogProviders map[ProviderName]providerEntry
	if catalog := r.modelCatalog.Load(); catalog != nil {
		catalogProviders = make(map[ProviderName]providerEntry, len(priority))
		for _, m := range *catalog {
			if m.ModelID == modelID {
				catalogProviders[m.Provider] = providerEntry{canonicalID: m.ModelID}
				continue
			}
			if slices.Contains(m.Aliases, modelID) {
				catalogProviders[m.Provider] = providerEntry{canonicalID: m.ModelID}
			}
		}
	}

	targets := make([]RouteTarget, 0, len(priority))
	for _, p := range priority {
		if _, ok := r.connectors[p]; !ok {
			continue
		}
		// When catalog data is available, skip providers that don't carry this model.
		// Resolve alias to the canonical provider model ID.
		canonicalID := modelID
		if catalogProviders != nil {
			entry, known := catalogProviders[p]
			if !known {
				continue
			}
			canonicalID = entry.canonicalID
		}
		pricing, ok := r.pricing.Price(p, canonicalID)
		if !ok {
			pricing = ModelPricing{Provider: p, ModelID: canonicalID}
		}
		targets = append(targets, RouteTarget{
			Provider: p,
			ModelID:  canonicalID,
			Pricing:  pricing,
		})
	}

	if len(targets) == 0 {
		return nil, StrategyFailoverChain, ErrNoProvidersEnabled
	}

	return targets, StrategyFailoverChain, nil
}

// ResolveModelForPolicy maps a requested model string to the canonical family
// and policy alias used to construct inference:{family}:{alias}:invoke scopes.
//
// Returns ok=false for wildcard routing ("", "*", "failover") — those paths
// require only the broad inference:use scope enforced at Layer 1 by Envoy.
// Also returns ok=false when the catalog is empty (cold start) or the model
// is not found; in that case the Layer 1 scope check still applies.
func (r *RoutingEngine) ResolveModelForPolicy(model string) (ResolvedModelInfo, bool) {
	if model == "" || model == "*" || model == "failover" {
		return ResolvedModelInfo{}, false
	}

	catalog := r.modelCatalog.Load()
	if catalog == nil {
		return ResolvedModelInfo{}, false
	}

	// Normalise to a bare model ID (no provider prefix) for catalog lookup.
	// Provider-qualified form: "bedrock/claude-sonnet-4-6" → resolve alias → strip prefix.
	// Bare form: "anthropic.claude-sonnet-4-20250514-v1:0" — check as-is, and also
	// check against catalog aliases after stripping their provider prefix, since
	// callers may omit the provider and rely on routing to pick one.
	lookupID := model
	if strings.Contains(model, "/") {
		resolved := r.resolveAlias(model)
		_, modelID, err := parseExplicitModel(resolved)
		if err != nil {
			return ResolvedModelInfo{}, false
		}
		lookupID = modelID
	}

	for _, m := range *catalog {
		// Direct match on canonical model ID (already stripped of provider prefix).
		_, bareModelID, _ := parseExplicitModel(m.ModelID)
		if bareModelID == "" {
			bareModelID = m.ModelID
		}
		if bareModelID == lookupID {
			return ResolvedModelInfo{Family: m.Family, Alias: m.PolicyAlias()}, true
		}
		// Match against aliases, stripping provider prefix from each.
		for _, alias := range m.Aliases {
			_, bareAlias, _ := parseExplicitModel(alias)
			if bareAlias == "" {
				bareAlias = alias
			}
			if bareAlias == lookupID {
				return ResolvedModelInfo{Family: m.Family, Alias: m.PolicyAlias()}, true
			}
		}
	}

	return ResolvedModelInfo{}, false
}

// CatalogScopes returns the deduplicated set of scope declarations derived
// from the current model catalog. Includes one family-level scope per vendor
// (e.g. inference:anthropic:use) and one model-level scope per distinct
// model (e.g. inference:anthropic:claude-3-5-haiku:invoke).
//
// These are registered with the policy service vocabulary so the grants/roles
// UI can show available model scopes for assignment.
func (r *RoutingEngine) CatalogScopes() []CatalogScope {
	catalog := r.modelCatalog.Load()
	if catalog == nil {
		return nil
	}

	seen := make(map[string]bool)
	var scopes []CatalogScope

	for _, m := range *catalog {
		if m.Family == "" || m.Family == "unknown" {
			continue
		}
		alias := m.PolicyAlias()

		// Family-level scope — one per vendor.
		familyScope := fmt.Sprintf("inference:%s:use", m.Family)
		if !seen[familyScope] {
			seen[familyScope] = true
			scopes = append(scopes, CatalogScope{
				Scope:        familyScope,
				PathSegments: []string{m.Family},
				Description:  fmt.Sprintf("Use any %s model", m.Family),
			})
		}

		// Model-level scope — one per distinct canonical alias.
		modelScope := fmt.Sprintf("inference:%s:%s:use", m.Family, alias)
		if !seen[modelScope] {
			seen[modelScope] = true
			scopes = append(scopes, CatalogScope{
				Scope:        modelScope,
				PathSegments: []string{m.Family, alias},
				Description:  m.DisplayName,
			})
		}
	}

	return scopes
}

// ─── helpers ───────────────────────────────────────────────────────────────────

// parseExplicitModel splits "provider/model-id" into its components.
// Returns an error if the format is invalid.
func parseExplicitModel(model string) (ProviderName, string, error) {
	idx := strings.Index(model, "/")
	if idx <= 0 || idx == len(model)-1 {
		return "", "", fmt.Errorf("%w: model %q must be in 'provider/model-id' format",
			ErrInvalidRequest, model)
	}
	provider := ProviderName(model[:idx])
	modelID := model[idx+1:]
	return provider, modelID, nil
}
