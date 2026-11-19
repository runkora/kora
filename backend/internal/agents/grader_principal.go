package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// graderTokenTTL bounds a grading token to one grading call with margin.
const graderTokenTTL = 5 * time.Minute

// graderInvokeScope lets the grading principal call the inference gateway at all.
const graderInvokeScope = "inference:invoke"

var errInvalidGraderScope = errors.New("grader grant scope must be a model use scope")

// graderGrants is the part of the policy client the grading principal writes with.
type graderGrants interface {
	ListGrants(ctx context.Context, tenantID, principalType, principalID string) ([]policyclient.Grant, error)
	CreateGrant(ctx context.Context, tenantID, principalType, principalID string, scopes []string, budget *policyclient.BudgetAssignment, delegationRequired bool) (string, error)
	DeleteGrant(ctx context.Context, grantID string) error
}

// serviceTokenMinter asks identity for a SERVICE token. kora-agents is on identity's mint allowlist for this.
type serviceTokenMinter interface {
	MintServiceToken(ctx context.Context, tenantID, principalID, displayName string, ttl time.Duration) (string, error)
}

// GraderPrincipal owns each tenant's grading SERVICE principal grader:<tenant>.
// Like a trigger principal it has no budget of its own. The tenant ceiling bounds its spend.
type GraderPrincipal struct {
	grants graderGrants
	minter serviceTokenMinter
	logger logging.Logger
}

// NewGraderPrincipal builds the grading principal over kora-agents' policy and identity clients.
func NewGraderPrincipal(grants graderGrants, minter serviceTokenMinter, logger logging.Logger) *GraderPrincipal {
	return &GraderPrincipal{grants: grants, minter: minter, logger: logger}
}

func graderPrincipalID(tenantID string) string {
	return "grader:" + tenantID
}

// Ensure leaves the tenant's grading principal with one grant holding inference:invoke,
// modelScopes and every model scope it already held.
// Ensure never sets a budget. A replacement grant keeps any budget the old grant carried.
func (g *GraderPrincipal) Ensure(ctx context.Context, tenantID string, modelScopes []string) error {
	if tenantID == "" {
		return errors.New("grader principal: tenant id is required")
	}
	for _, s := range modelScopes {
		if !isModelUseScope(s) {
			return fmt.Errorf("%w: %q", errInvalidGraderScope, s)
		}
	}
	principalID := graderPrincipalID(tenantID)
	existing, err := g.grants.ListGrants(ctx, tenantID, string(token.PrincipalTypeService), principalID)
	if err != nil {
		return fmt.Errorf("list grader grants: %w", err)
	}
	want := graderScopes(existing, modelScopes)

	var keepID string
	var stale []policyclient.Grant
	for _, gr := range existing {
		if keepID == "" && grantMatchesDesired(gr, want, false) {
			keepID = gr.ID
			continue
		}
		stale = append(stale, gr)
	}
	// Create before deleting, so a refused create leaves grading its previous grant.
	if keepID == "" {
		if _, err = g.grants.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, want, firstBudget(existing), false); err != nil {
			return fmt.Errorf("create grader grant: %w", err)
		}
	}
	for _, gr := range stale {
		if delErr := g.grants.DeleteGrant(ctx, gr.ID); delErr != nil && !errors.Is(delErr, policyclient.ErrGrantNotFound) {
			g.logger.Warn(ctx, "delete stale grader grant failed",
				logging.Field{Key: "grant_id", Value: gr.ID},
				logging.Field{Key: "error", Value: delErr.Error()})
		}
	}
	return nil
}

// Token mints a short-lived SERVICE token for the tenant's grading principal.
func (g *GraderPrincipal) Token(ctx context.Context, tenantID string) (string, error) {
	if tenantID == "" {
		return "", errors.New("grader principal: tenant id is required")
	}
	id := graderPrincipalID(tenantID)
	tok, err := g.minter.MintServiceToken(ctx, tenantID, id, id, graderTokenTTL)
	if err != nil {
		return "", fmt.Errorf("mint grader token: %w", err)
	}
	return tok, nil
}

// graderScopes is inference:invoke plus the union of the requested model scopes and those already granted.
// Any other scope on an existing grant is dropped, so the grant can only ever reach inference.
func graderScopes(existing []policyclient.Grant, modelScopes []string) []string {
	out := []string{graderInvokeScope}
	add := func(s string) {
		if isModelUseScope(s) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, gr := range existing {
		for _, s := range gr.Scopes {
			add(s)
		}
	}
	for _, s := range modelScopes {
		add(s)
	}
	return out
}

// isModelUseScope reports whether s is inference:<family>:<name>:use, the scope the gateway checks per model.
// A wildcard segment is refused, so the grant names each model it can use.
func isModelUseScope(s string) bool {
	parts := strings.Split(s, ":")
	return len(parts) == 4 && parts[0] == "inference" && parts[3] == "use" &&
		parts[1] != "" && parts[2] != "" && !strings.Contains(parts[1]+parts[2], "*")
}
