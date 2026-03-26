package inferencegw

import (
	"context"
	"fmt"
	"strings"
)

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
		target, err := r.explicitTarget(req.Model)
		if err != nil {
			return nil, StrategyExplicit, err
		}
		return []RouteTarget{target}, StrategyExplicit, nil

	default:
		// Bare model name — try each enabled provider in canonical order.
		return r.modelAcrossProviders(req.Model)
	}
}

// ─── strategy implementations ─────────────────────────────────────────────────

// lowestCostTargets returns all enabled provider/model pairs sorted ascending
// by the estimated cost to process a 1k-token prompt + 1k-token completion
// (a representative unit for comparison purposes).
func (r *RoutingEngine) lowestCostTargets() ([]RouteTarget, error) {
	if len(r.connectors) == 0 {
		return nil, ErrNoProvidersEnabled
	}

	pricings := r.pricing.All()
	targets := make([]RouteTarget, 0, len(pricings))

	for _, mp := range pricings {
		if _, ok := r.connectors[mp.Provider]; !ok {
			continue // provider not enabled
		}
		targets = append(targets, RouteTarget{
			Provider: mp.Provider,
			ModelID:  mp.ModelID,
			Pricing:  mp,
		})
	}

	if len(targets) == 0 {
		return nil, ErrNoProvidersEnabled
	}

	// Insertion-sort by representative cost (small list — O(n²) is fine).
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
		return RouteTarget{}, fmt.Errorf("%w: provider %q is not enabled", ErrRoutingFailed, provider)
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
func (r *RoutingEngine) failoverChain() ([]RouteTarget, error) {
	priority := []ProviderName{ProviderBedrock, ProviderVertexAI, ProviderAzure}

	targets := make([]RouteTarget, 0, len(priority))
	for _, p := range priority {
		if _, ok := r.connectors[p]; !ok {
			continue
		}
		// Pick the first (cheapest) model available for this provider.
		for _, mp := range r.pricing.All() {
			if mp.Provider == p {
				targets = append(targets, RouteTarget{
					Provider: p,
					ModelID:  mp.ModelID,
					Pricing:  mp,
				})
				break
			}
		}
	}

	if len(targets) == 0 {
		return nil, ErrNoProvidersEnabled
	}

	return targets, nil
}

// modelAcrossProviders resolves a bare model name across all enabled providers
// in canonical failover order.
func (r *RoutingEngine) modelAcrossProviders(modelID string) ([]RouteTarget, RoutingStrategy, error) {
	priority := []ProviderName{ProviderBedrock, ProviderVertexAI, ProviderAzure}

	targets := make([]RouteTarget, 0, len(priority))
	for _, p := range priority {
		if _, ok := r.connectors[p]; !ok {
			continue
		}
		pricing, ok := r.pricing.Price(p, modelID)
		if !ok {
			pricing = ModelPricing{Provider: p, ModelID: modelID}
		}
		targets = append(targets, RouteTarget{
			Provider: p,
			ModelID:  modelID,
			Pricing:  pricing,
		})
	}

	if len(targets) == 0 {
		return nil, StrategyFailoverChain, ErrNoProvidersEnabled
	}

	return targets, StrategyFailoverChain, nil
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
