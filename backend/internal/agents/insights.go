package agents

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// Agent insights: the read-only aggregates behind the Agents page. They answer
// "which agents can this caller see, and how many of their runs finished", and
// they follow the same visibility rules as the run list so a delegated agent's
// activity stays private to the person who ran it
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 6).

// AgentTypeRef is the slice of an agent type that visibility needs.
type AgentTypeRef struct {
	ID        string
	Slug      string
	Delegated bool
}

// AgentVisibility lists the agent slugs a caller can see, split by how much of
// each agent's activity is theirs to see. The inference gateway applies it to
// usage rows, which carry no run owner other than the delegator.
type AgentVisibility struct {
	// AllRows lists agents whose activity is visible in full.
	AllRows []string `json:"all_rows"`
	// OwnRows lists delegated agents whose activity is visible only where the
	// caller started the run.
	OwnRows []string `json:"own_rows"`
}

// RunOutcomes counts the terminal runs in a window.
type RunOutcomes struct {
	Completed int64 `json:"completed"`
	Failed    int64 `json:"failed"`
}

// RunOutcomeScope selects the runs a CountRunOutcomes call may count: every run
// of an AllRows agent type, and the runs of an OwnRows agent type that Subject
// started.
type RunOutcomeScope struct {
	TenantID string
	Since    time.Time
	AllRows  []string
	OwnRows  []string
	Subject  string
}

// AgentBudgetLimit is the spend ceiling on an agent's grant. The grant, held by
// the policy service, is the source of truth for it.
type AgentBudgetLimit struct {
	AgentSlug    string  `json:"agent_slug"`
	LimitUSD     float64 `json:"limit_usd"`
	ResetCadence string  `json:"reset_cadence"`
}

// budgetReader reads the budget carried by an agent's grant.
type budgetReader interface {
	GetAgentBudget(ctx context.Context, at AgentType, namespace string) (*AgentBudget, error)
}

// budgetLookupConcurrency bounds the policy-service reads one request makes.
const budgetLookupConcurrency = 8

// insightsStore is the persistence the insights service reads.
type insightsStore interface {
	// ListAgentTypeRefs returns the id, slug and identity model of each agent
	// type in the tenant, without the rest of the row.
	ListAgentTypeRefs(ctx context.Context, tenantID string) ([]AgentTypeRef, error)
	// CountRunOutcomes counts completed and failed runs started at or after
	// scope.Since, within scope's agent types.
	CountRunOutcomes(ctx context.Context, scope RunOutcomeScope) (RunOutcomes, error)
}

// InsightsService answers the agent visibility and run outcome questions.
type InsightsService struct {
	store       insightsStore
	authz       *Authz
	budgets     budgetReader
	namespaceFn func(tenantID string) string
}

// NewInsightsService creates an InsightsService. namespaceFn maps a tenant to
// the namespace its agents' grants are keyed under.
func NewInsightsService(store insightsStore, authz *Authz, budgets budgetReader, namespaceFn func(tenantID string) string) *InsightsService {
	return &InsightsService{store: store, authz: authz, budgets: budgets, namespaceFn: namespaceFn}
}

// partition keeps the refs the caller may read under resource ("types" or
// "runs") and splits them into the agents seen in full and the delegated agents
// seen only for the caller's own runs. A platform admin sees every delegated
// agent in full, as for the run list.
func (s *InsightsService) partition(ctx context.Context, resource string) (all, own []AgentTypeRef, err error) {
	refs, err := s.store.ListAgentTypeRefs(ctx, tenantFromContext(ctx))
	if err != nil {
		return nil, nil, fmt.Errorf("list agent type refs: %w", err)
	}
	allowed, err := s.authz.filterRefs(ctx, refs, resource)
	if err != nil {
		return nil, nil, err
	}
	admin := isPlatformAdmin(ctx)
	for _, ref := range allowed {
		if ref.Delegated && !admin {
			own = append(own, ref)
		} else {
			all = append(all, ref)
		}
	}
	return all, own, nil
}

func slugs(refs []AgentTypeRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Slug
	}
	return out
}

func ids(refs []AgentTypeRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.ID
	}
	return out
}

// VisibleAgents returns the agents whose usage the caller may see.
func (s *InsightsService) VisibleAgents(ctx context.Context) (AgentVisibility, error) {
	all, own, err := s.partition(ctx, "types")
	if err != nil {
		return AgentVisibility{}, err
	}
	return AgentVisibility{AllRows: slugs(all), OwnRows: slugs(own)}, nil
}

// RunOutcomes counts the caller's visible runs that completed or failed in the
// window, using the run list's visibility rule.
func (s *InsightsService) RunOutcomes(ctx context.Context, period usagewindow.Period) (RunOutcomes, error) {
	all, own, err := s.partition(ctx, "runs")
	if err != nil {
		return RunOutcomes{}, err
	}
	if len(all) == 0 && len(own) == 0 {
		return RunOutcomes{}, nil
	}
	return s.store.CountRunOutcomes(ctx, RunOutcomeScope{
		TenantID: tenantFromContext(ctx),
		Since:    usagewindow.For(period, time.Now()).Start,
		AllRows:  ids(all),
		OwnRows:  ids(own),
		Subject:  callerSubject(ctx),
	})
}

// BudgetLimits returns the spend ceilings of the agents whose spend the caller
// may see in full, for the agents that have one. A delegated agent seen only
// for the caller's own runs is left out: its ceiling applies to everyone's runs
// together, so the caller's share beside it would read as the whole.
func (s *InsightsService) BudgetLimits(ctx context.Context) ([]AgentBudgetLimit, error) {
	all, _, err := s.partition(ctx, "types")
	if err != nil {
		return nil, err
	}
	tenantID := tenantFromContext(ctx)
	namespace := s.namespaceFn(tenantID)
	limits := make([]*AgentBudgetLimit, len(all))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(budgetLookupConcurrency)
	for i, ref := range all {
		g.Go(func() error {
			b, err := s.budgets.GetAgentBudget(gctx, AgentType{TenantID: tenantID, Slug: ref.Slug}, namespace)
			if err != nil {
				return fmt.Errorf("read budget of %s: %w", ref.Slug, err)
			}
			if b != nil && b.LimitUSD > 0 {
				cadence := b.ResetCadence
				if cadence == "" {
					cadence = "monthly"
				}
				limits[i] = &AgentBudgetLimit{AgentSlug: ref.Slug, LimitUSD: b.LimitUSD, ResetCadence: cadence}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out := []AgentBudgetLimit{}
	for _, l := range limits {
		if l != nil {
			out = append(out, *l)
		}
	}
	return out, nil
}

// filterRefs returns the refs the principal may read under resource, with one
// policy query for the whole set.
func (a *Authz) filterRefs(ctx context.Context, refs []AgentTypeRef, resource string) ([]AgentTypeRef, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	candidates := make([]policy.Candidate, len(refs))
	for i, ref := range refs {
		candidates[i] = policy.Candidate{ID: ref.ID, PathSegments: []string{resource, ref.Slug}}
	}
	allowed, err := a.eval.Query(ctx, policy.QueryInput{
		Claims:       claimsFromContext(ctx),
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "agents",
		Action:       "read",
		Candidates:   candidates,
	})
	if err != nil {
		return nil, fmt.Errorf("authz query %s: %w", resource, err)
	}
	set := stringSet(allowed)
	return slices.DeleteFunc(slices.Clone(refs), func(ref AgentTypeRef) bool {
		_, ok := set[ref.ID]
		return !ok
	}), nil
}

// InsightsController serves the insights routes. It is a sibling of Controller
// so the main controller's constructor stays as it is, and it shares that
// controller's route-to-scope table.
type InsightsController struct {
	svc      *InsightsService
	verifier *token.Verifier
	eval     policy.Evaluator
	routes   []routescopes.RouteScope
	basePath string
	logger   logging.Logger
}

// NewInsightsController creates the controller. routes and basePath are the
// main controller's, so scope enforcement reads one table.
func NewInsightsController(
	svc *InsightsService,
	verifier *token.Verifier,
	eval policy.Evaluator,
	routes []routescopes.RouteScope,
	basePath string,
	logger logging.Logger,
) *InsightsController {
	return &InsightsController{svc: svc, verifier: verifier, eval: eval, routes: routes, basePath: basePath, logger: logger}
}

// GroupedBy returns the route group prefix.
func (c *InsightsController) GroupedBy() string { return "" }

// Middleware applies the public chain of the main controller: authentication,
// delegation parsing and route scope enforcement.
func (c *InsightsController) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(
		mw.AuthMiddleware(c.verifier, c.logger),
		mw.DelegationMiddleware(c.logger, c.verifier),
		mw.ScopeEnforcementMiddleware(c.eval, c.routes, c.basePath, c.logger),
	)
}

// Boot registers the insights routes.
func (c *InsightsController) Boot(mux *api.Mux) {
	mux.Handle("GET /api/v1/agent-visibility", c.handleVisibility)
	mux.Handle("GET /api/v1/run-outcomes", c.handleRunOutcomes)
	mux.Handle("GET /api/v1/agent-budgets", c.handleBudgetLimits)
}

func (c *InsightsController) handleBudgetLimits(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out, err := c.svc.BudgetLimits(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to read agent budgets", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": out})
}

func (c *InsightsController) handleVisibility(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vis, err := c.svc.VisibleAgents(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to resolve visible agents", nil), err)
		return
	}
	// Empty lists encode as [] so a client never reads null as "unknown".
	if vis.AllRows == nil {
		vis.AllRows = []string{}
	}
	if vis.OwnRows == nil {
		vis.OwnRows = []string{}
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, vis)
}

func (c *InsightsController) handleRunOutcomes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period, err := usagewindow.Parse(r.URL.Query().Get("period"))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidField, err.Error(), nil), err)
		return
	}
	out, err := c.svc.RunOutcomes(ctx, period)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to count run outcomes", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}
