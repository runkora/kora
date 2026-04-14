package policy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"
)

// ErrBundleUnexpectedStatus is returned when the bundle endpoint responds
// with a non-200 status code.
var ErrBundleUnexpectedStatus = errors.New("bundle endpoint returned unexpected status")

const decisionQuery = "x := {\"allow\": data.orbit.authz.allow, \"reason\": data.orbit.authz.reason}"

// tenantBundle holds a compiled PreparedEvalQuery for a single tenant.
type tenantBundle struct {
	query    rego.PreparedEvalQuery
	loadedAt time.Time
	revision string
}

// engine manages per-tenant OPA bundle loading and policy evaluation.
// It polls the Policy Service bundle endpoint for each active tenant and
// caches a PreparedEvalQuery per tenant. All evaluation is in-process.
type engine struct {
	*httpclient.Base
	mu             sync.RWMutex
	bundles        map[string]*tenantBundle // tenant_id → compiled bundle
	bundleEndpoint string
	pollInterval   time.Duration
}

func newEngine(bundleEndpoint string, pollInterval time.Duration, logger logging.Logger) *engine {
	return &engine{
		Base:           httpclient.New("orbit.policy-engine-bundle-client", logger, 10*time.Second),
		bundles:        make(map[string]*tenantBundle),
		bundleEndpoint: bundleEndpoint,
		pollInterval:   pollInterval,
	}
}

// Start begins background bundle polling. Implements Evaluator.
func (e *engine) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(e.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.refreshAll(ctx)
			}
		}
	}()
}

func (e *engine) refreshAll(ctx context.Context) {
	e.mu.RLock()
	tenants := make([]string, 0, len(e.bundles))
	for t := range e.bundles {
		tenants = append(tenants, t)
	}
	e.mu.RUnlock()

	for _, tenantID := range tenants {
		if err := e.loadBundle(ctx, tenantID); err != nil {
			e.Logger().Error(ctx, "bundle poll failed",
				logging.Field{Key: "tenant_id", Value: tenantID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}
}

func (e *engine) loadBundle(ctx context.Context, tenantID string) error {
	url := e.bundleEndpoint + "/" + tenantID

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create bundle request: %w", err)
	}

	_, span, resp, err := e.Do(ctx, "policy-engine.Engine.LoadBundle", req)
	defer span.End()
	if err != nil {
		return fmt.Errorf("fetch bundle for tenant %s: %w", tenantID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		e.mu.Lock()
		delete(e.bundles, tenantID)
		e.mu.Unlock()
		e.Logger().Info(ctx, "bundle evicted for tenant (no grants configured)",
			logging.Field{Key: "tenant_id", Value: tenantID},
		)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return e.HandleErrorResponse(span, resp, ErrBundleUnexpectedStatus)
	}

	bundleBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read bundle for tenant %s: %w", tenantID, err)
	}

	revision := resp.Header.Get("ETag")

	if err := e.compileBundle(ctx, tenantID, bundleBytes, revision); err != nil {
		return fmt.Errorf("compile bundle for tenant %s: %w", tenantID, err)
	}

	e.Logger().Info(ctx, "bundle loaded",
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "revision", Value: revision},
	)
	return nil
}

func (e *engine) compileBundle(ctx context.Context, tenantID string, bundleBytes []byte, revision string) error {
	b, err := bundle.NewReader(bytes.NewReader(bundleBytes)).Read()
	if err != nil {
		return fmt.Errorf("parse bundle: %w", err)
	}

	r := rego.New(
		rego.Query(decisionQuery),
		rego.ParsedBundle(tenantID, &b),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return fmt.Errorf("prepare rego eval: %w", err)
	}

	e.mu.Lock()
	e.bundles[tenantID] = &tenantBundle{
		query:    pq,
		loadedAt: time.Now().UTC(),
		revision: revision,
	}
	e.mu.Unlock()

	return nil
}

// Check evaluates the allow rule for the given input. Implements Evaluator.
func (e *engine) Check(ctx context.Context, input CheckInput) (Result, error) {
	if input.Claims == nil {
		return Result{Reason: "no claims provided"}, nil
	}
	tenantID := input.Claims.Orbit.TenantID

	tb, err := e.bundleForTenant(ctx, tenantID)
	if err != nil {
		return Result{}, err
	}
	if tb == nil {
		return Result{Reason: "no policy configured for tenant"}, nil
	}

	if input.Delegation != nil && input.Delegation.ActSub != "" && input.Delegation.ActSub != input.Claims.Subject {
		return Result{Reason: "deny:delegation_act_sub_mismatch"}, nil
	}

	opaInput := buildOPAInput(input.Claims, input.Delegation, input.ResourceType, input.PathSegments, input.Action, input.Attributes)

	rs, err := tb.query.Eval(ctx, rego.EvalInput(opaInput))
	if err != nil {
		return Result{}, fmt.Errorf("rego eval: %w", err)
	}

	return evalResult(rs), nil
}

// Query filters candidates to those the principal may access. Implements Evaluator.
func (e *engine) Query(ctx context.Context, input QueryInput) ([]string, error) {
	if input.Claims == nil {
		return nil, nil
	}
	tenantID := input.Claims.Orbit.TenantID

	tb, err := e.bundleForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if tb == nil {
		return nil, nil
	}

	allowed := make([]string, 0, len(input.Candidates))
	for _, c := range input.Candidates {
		opaInput := buildOPAInput(input.Claims, input.Delegation, input.ResourceType, c.PathSegments, input.Action, c.Attributes)
		rs, err := tb.query.Eval(ctx, rego.EvalInput(opaInput))
		if err != nil {
			return nil, fmt.Errorf("rego eval for candidate %s: %w", c.ID, err)
		}
		if evalResult(rs).Allow {
			allowed = append(allowed, c.ID)
		}
	}
	return allowed, nil
}

func (e *engine) bundleForTenant(ctx context.Context, tenantID string) (*tenantBundle, error) {
	e.mu.RLock()
	tb, ok := e.bundles[tenantID]
	e.mu.RUnlock()
	if ok {
		return tb, nil
	}

	// Lazy load on first request for this tenant.
	if err := e.loadBundle(ctx, tenantID); err != nil {
		return nil, fmt.Errorf("no bundle loaded for tenant %s: %w", tenantID, err)
	}
	e.mu.RLock()
	tb = e.bundles[tenantID]
	e.mu.RUnlock()
	return tb, nil // nil means no grants configured — caller handles
}

func evalResult(rs rego.ResultSet) Result {
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return Result{Reason: "deny:no_policy_result"}
	}
	// The query produces: x := {"allow": bool, "reason": string}
	// The binding is in Bindings, not Expressions.
	if len(rs[0].Bindings) > 0 {
		if decision, ok := rs[0].Bindings["x"].(map[string]any); ok {
			allow, _ := decision["allow"].(bool)
			reason, _ := decision["reason"].(string)
			return Result{Allow: allow, Reason: reason}
		}
	}
	return Result{Reason: "deny:no_policy_result"}
}

// opaInput is the JSON structure consumed by authz.rego.
type opaInput struct {
	Principal         principalInput `json:"principal"`
	DelegationPresent bool           `json:"delegation_present"`
	DelegationScopes  []string       `json:"delegation_scopes"`
	DelegatorID       string         `json:"delegator_id,omitempty"`
	RequiredScope     string         `json:"required_scope"`
	Resource          resourceInput  `json:"resource"`
	Action            string         `json:"action"`
}

type principalInput struct {
	Type     string   `json:"type"`
	TenantID string   `json:"tenant_id"`
	ID       string   `json:"id"`
	Roles    []string `json:"roles"`
}

type resourceInput struct {
	Type       string            `json:"type"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

func buildOPAInput(
	claims *token.PlatformClaims,
	delegation *DelegationClaims,
	resourceType string,
	pathSegments []string,
	action string,
	attrs map[string]string,
) opaInput {
	requiredScope := DeriveRequiredScope(resourceType, pathSegments, action)

	input := opaInput{
		Principal: principalInput{
			Type:     string(claims.Orbit.PrincipalType),
			TenantID: claims.Orbit.TenantID,
			ID:       claims.Subject,
			Roles:    extractRoles(claims),
		},
		RequiredScope: requiredScope,
		Resource:      resourceInput{Type: resourceType, Attributes: attrs},
		Action:        action,
	}

	if delegation != nil {
		input.DelegationPresent = true
		input.DelegationScopes = delegation.Scopes
		input.DelegatorID = delegation.Sub
	}

	return input
}

func extractRoles(claims *token.PlatformClaims) []string {
	if claims.Orbit.Claims == nil {
		return []string{}
	}
	raw, ok := claims.Orbit.Claims["roles"]
	if !ok || raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	roles := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			roles = append(roles, trimmed)
		}
	}
	return roles
}
