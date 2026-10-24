package inferencegw

import (
	"fmt"
	"time"
)

// ─── VirtualModelStrategy ─────────────────────────────────────────────────────

// VirtualModelStrategy controls how the routing engine selects among the
// configured routes for a virtual model at dispatch time.
type VirtualModelStrategy string

const (
	// VirtualStrategyPriority tries routes in ascending priority order.
	// On a retryable provider error the engine advances to the next route.
	VirtualStrategyPriority VirtualModelStrategy = "priority"

	// VirtualStrategyWeighted selects a route probabilistically by weight at
	// request time. The remaining routes serve as a failover chain in priority
	// order if the selected route errors.
	VirtualStrategyWeighted VirtualModelStrategy = "weighted"

	// VirtualStrategyLowestCost selects the enabled route with the lowest
	// combined (input + output) cost per million tokens at request time.
	// Remaining routes serve as failover in ascending cost order.
	VirtualStrategyLowestCost VirtualModelStrategy = "lowest_cost"
)

// ─── VirtualModel ─────────────────────────────────────────────────────────────

// VirtualModel is a platform-curated model definition. It is the only model
// identity exposed to consumers — provider details are encapsulated in Routes
// and never surfaced externally.
type VirtualModel struct {
	ID          string // UUID
	Family      string // e.g. "anthropic"
	Name        string // e.g. "claude-sonnet-4-6"
	DisplayName string // e.g. "Claude Sonnet 4.6"
	Description string
	Strategy    VirtualModelStrategy
	Routes      []VirtualModelRoute // ordered by Priority ascending
	Enabled     bool
	// Source is declarative.SourceAPI or SourceCRD. ResourceName and
	// ResourceUID name the projecting resource of a SourceCRD row.
	Source       string
	ResourceName string
	ResourceUID  string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// FullID returns the consumer-facing model identifier: "{family}/{name}".
func (v VirtualModel) FullID() string { return v.Family + "/" + v.Name }

// PolicyScope returns the OPA scope for this virtual model.
func (v VirtualModel) PolicyScope() string {
	return fmt.Sprintf("inference:%s:%s:use", v.Family, v.Name)
}

// ─── VirtualModelRoute ────────────────────────────────────────────────────────

// VirtualModelRoute is a single provider target within a VirtualModel.
type VirtualModelRoute struct {
	ID              string
	VirtualModelID  string
	Provider        ProviderName
	ModelID         string  // canonical provider model ID, no provider/ prefix
	Priority        int     // 1-based; ascending order is the dispatch order
	Weight          int     // 0–100; used by weighted strategy; must sum to 100 across routes
	InputCostPer1M  float64 // USD per 1M input tokens; used by lowest_cost strategy
	OutputCostPer1M float64 // USD per 1M output tokens
	Enabled         bool
}

// unitCost returns a representative cost for sorting: input + output cost per 1M tokens.
func (r VirtualModelRoute) unitCost() float64 {
	return r.InputCostPer1M + r.OutputCostPer1M
}

// pricing converts the route's stored cost data to a ModelPricing for usage attribution.
func (r VirtualModelRoute) pricing() ModelPricing {
	return ModelPricing{
		Provider:             r.Provider,
		ModelID:              r.ModelID,
		InputCostPerMillion:  r.InputCostPer1M,
		OutputCostPerMillion: r.OutputCostPer1M,
	}
}

// ─── ProviderModelOverride ────────────────────────────────────────────────────

// ProviderModelOverride records a platform admin's enable/disable decision for
// a specific provider-discovered model. Absence of a row means enabled.
type ProviderModelOverride struct {
	Provider  ProviderName
	ModelID   string
	Enabled   bool
	UpdatedAt time.Time
}

// ─── virtualCatalogSnapshot ───────────────────────────────────────────────────

// virtualCatalogSnapshot is the in-memory projection of the virtual model
// catalog. It is rebuilt from the database on every admin write and held
// behind an atomic.Pointer — the routing hot path never touches the DB.
type virtualCatalogSnapshot struct {
	// byID maps full ID → VirtualModel for O(1) routing lookup.
	// key: "anthropic/claude-sonnet-4-6"
	byID map[string]*VirtualModel

	// byRouteKey maps "provider/modelID" → *VirtualModel for O(1) reverse lookup.
	// Used by ResolveTargetForPolicy to map a provider+model pair back to its
	// virtual model's policy identity without a linear scan.
	// key: "bedrock/us.anthropic.claude-sonnet-4-6"
	byRouteKey map[string]*VirtualModel

	// list is the ordered set of enabled virtual models used by GET /v1/models.
	list []VirtualModel

	// all is the complete set of virtual models (enabled and disabled) used for
	// policy scope generation. Disabled models retain their vocab entries.
	all []VirtualModel

	// disabledRoutes is the set of provider models disabled by platform admins.
	// key: "{provider}/{modelID}", e.g. "bedrock/us.anthropic.claude-sonnet-4-6"
	disabledRoutes map[string]bool
}

// isRouteDisabled reports whether a provider+model pair has been disabled.
func (s *virtualCatalogSnapshot) isRouteDisabled(provider ProviderName, modelID string) bool {
	if s == nil {
		return false
	}
	return s.disabledRoutes[string(provider)+"/"+modelID]
}
