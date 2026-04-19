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
const scopesQuery = "x := data.orbit.authz.principal_all_scopes"
const grantDetailQuery = "x := data.orbit.authz.principal_grant_detail"
const roleDetailQuery = "x := data.orbit.authz.principal_role_detail"

// tenantBundle holds compiled PreparedEvalQueries for a single tenant.
type tenantBundle struct {
	query            rego.PreparedEvalQuery
	scopesQuery      rego.PreparedEvalQuery
	grantDetailQuery rego.PreparedEvalQuery
	roleDetailQuery  rego.PreparedEvalQuery
	loadedAt         time.Time
	revision         string
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

	// Compile the decision query (allow/reason).
	r := rego.New(
		rego.Query(decisionQuery),
		rego.ParsedBundle(tenantID, &b),
	)
	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return fmt.Errorf("prepare rego eval: %w", err)
	}

	// Reuse the same parsed bundle — rego.New reads from it without mutation.
	rs := rego.New(
		rego.Query(scopesQuery),
		rego.ParsedBundle(tenantID, &b),
	)
	sq, err := rs.PrepareForEval(ctx)
	if err != nil {
		return fmt.Errorf("prepare scopes rego eval: %w", err)
	}

	gd := rego.New(
		rego.Query(grantDetailQuery),
		rego.ParsedBundle(tenantID, &b),
	)
	gdq, err := gd.PrepareForEval(ctx)
	if err != nil {
		return fmt.Errorf("prepare grant detail rego eval: %w", err)
	}

	rd := rego.New(
		rego.Query(roleDetailQuery),
		rego.ParsedBundle(tenantID, &b),
	)
	rdq, err := rd.PrepareForEval(ctx)
	if err != nil {
		return fmt.Errorf("prepare role detail rego eval: %w", err)
	}

	e.mu.Lock()
	e.bundles[tenantID] = &tenantBundle{
		query:            pq,
		scopesQuery:      sq,
		grantDetailQuery: gdq,
		roleDetailQuery:  rdq,
		loadedAt:         time.Now().UTC(),
		revision:         revision,
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

// Scopes returns all scopes the principal holds (grants + roles). Implements Evaluator.
func (e *engine) Scopes(ctx context.Context, input ScopesInput) ([]string, error) {
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

	opaInput := principalInput{
		Type:     string(input.Claims.Orbit.PrincipalType),
		TenantID: input.Claims.Orbit.TenantID,
		ID:       principalID(input.Claims),
		Roles:    extractRoles(input.Claims),
	}

	rs, err := tb.scopesQuery.Eval(ctx, rego.EvalInput(map[string]any{
		"principal": opaInput,
	}))
	if err != nil {
		return nil, fmt.Errorf("rego scopes eval: %w", err)
	}

	return evalScopesResult(rs), nil
}

// ScopesExplain returns the principal's scopes broken down by source. Implements Evaluator.
func (e *engine) ScopesExplain(ctx context.Context, input ScopesInput) (*ScopesExplainResult, error) {
	if input.Claims == nil {
		return &ScopesExplainResult{}, nil
	}
	tenantID := input.Claims.Orbit.TenantID

	tb, err := e.bundleForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if tb == nil {
		return &ScopesExplainResult{}, nil
	}

	opaInput := map[string]any{
		"principal": principalInput{
			Type:     string(input.Claims.Orbit.PrincipalType),
			TenantID: input.Claims.Orbit.TenantID,
			ID:       principalID(input.Claims),
			Roles:    extractRoles(input.Claims),
		},
	}

	// Query grant details.
	grantRS, err := tb.grantDetailQuery.Eval(ctx, rego.EvalInput(opaInput))
	if err != nil {
		return nil, fmt.Errorf("rego grant detail eval: %w", err)
	}
	fromGrants := evalGrantDetailResult(grantRS)

	// Query role details.
	roleRS, err := tb.roleDetailQuery.Eval(ctx, rego.EvalInput(opaInput))
	if err != nil {
		return nil, fmt.Errorf("rego role detail eval: %w", err)
	}
	fromRoles := evalRoleDetailResult(roleRS)

	// Get effective scopes (flat union).
	scopesRS, err := tb.scopesQuery.Eval(ctx, rego.EvalInput(opaInput))
	if err != nil {
		return nil, fmt.Errorf("rego scopes eval: %w", err)
	}
	effective := evalScopesResult(scopesRS)
	if effective == nil {
		effective = []string{}
	}

	return &ScopesExplainResult{
		FromGrants:      fromGrants,
		FromRoles:       fromRoles,
		EffectiveScopes: effective,
	}, nil
}

func evalGrantDetailResult(rs rego.ResultSet) []GrantDetail {
	if len(rs) == 0 || len(rs[0].Bindings) == 0 {
		return []GrantDetail{}
	}
	raw, ok := rs[0].Bindings["x"]
	if !ok {
		return []GrantDetail{}
	}
	rawSlice, ok := raw.([]any)
	if !ok {
		return []GrantDetail{}
	}
	details := make([]GrantDetail, 0, len(rawSlice))
	for _, v := range rawSlice {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		grantID, _ := m["grant_id"].(string)
		scopesRaw, _ := m["scopes"].([]any)
		scopes := make([]string, 0, len(scopesRaw))
		for _, s := range scopesRaw {
			if str, ok := s.(string); ok {
				scopes = append(scopes, str)
			}
		}
		details = append(details, GrantDetail{GrantID: grantID, Scopes: scopes})
	}
	return details
}

func evalRoleDetailResult(rs rego.ResultSet) []RoleDetail {
	if len(rs) == 0 || len(rs[0].Bindings) == 0 {
		return []RoleDetail{}
	}
	raw, ok := rs[0].Bindings["x"]
	if !ok {
		return []RoleDetail{}
	}
	rawSlice, ok := raw.([]any)
	if !ok {
		return []RoleDetail{}
	}
	details := make([]RoleDetail, 0, len(rawSlice))
	for _, v := range rawSlice {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		scopesRaw, _ := m["scopes"].([]any)
		scopes := make([]string, 0, len(scopesRaw))
		for _, s := range scopesRaw {
			if str, ok := s.(string); ok {
				scopes = append(scopes, str)
			}
		}
		details = append(details, RoleDetail{Role: role, Scopes: scopes})
	}
	return details
}

func evalScopesResult(rs rego.ResultSet) []string {
	if len(rs) == 0 || len(rs[0].Bindings) == 0 {
		return nil
	}
	raw, ok := rs[0].Bindings["x"]
	if !ok {
		return nil
	}
	// OPA returns sets as []interface{}.
	rawSlice, ok := raw.([]any)
	if !ok {
		return nil
	}
	scopes := make([]string, 0, len(rawSlice))
	for _, v := range rawSlice {
		if s, ok := v.(string); ok {
			scopes = append(scopes, s)
		}
	}
	return scopes
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
			ID:       principalID(claims),
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

// principalID returns the correct principal identifier for OPA grant lookups.
// For PAT principals, sub holds the issuing user's OIDC subject for attribution;
// the grant is keyed on the PAT UUID stored in PatID instead.
func principalID(claims *token.PlatformClaims) string {
	if claims.Orbit.PrincipalType == token.PrincipalTypePAT && claims.Orbit.PatID != "" {
		return claims.Orbit.PatID
	}
	return claims.Subject
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
