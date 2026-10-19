package koramcp

import (
	"context"
	"errors"
	"net/url"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runkora/kora/backend/internal/platform/clients/korajson"
)

// registerPolicyTools wires the policy domain: the caller's authorization view plus
// grant and role administration. Writes forward the caller's token to the policy
// service, which enforces the anti-escalation ceiling (a caller may only assign
// scopes it holds itself — 2026-06-30 amendment to the authorisation model), so a
// caller can never escalate beyond its own scopes through these tools.
func registerPolicyTools(srv *sdk.Server, b Backends, jc *korajson.Client) {
	base := b.Policy + "/api/v1/policy"

	addTool(srv, "policy.scopes",
		"List the calling principal's full effective scope set (grants + roles). These are also the scopes "+
			"you are allowed to assign in policy.grants.create / policy.roles.upsert — you cannot grant what you do not hold.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/effective-scopes", nil))
		})

	// ── grants ──────────────────────────────────────────────────────────────────

	addTool(srv, "policy.grants.list",
		"List policy grants in the caller's tenant, optionally filtered by principal_type and/or principal_id.",
		func(ctx context.Context, a listGrantsArgs) (*sdk.CallToolResult, error) {
			q := url.Values{}
			if a.PrincipalType != "" {
				q.Set("principal_type", a.PrincipalType)
			}
			if a.PrincipalID != "" {
				q.Set("principal_id", a.PrincipalID)
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/grants", q))
		})

	addTool(srv, "policy.grants.get",
		"Get a single grant by id — its principal, scopes, and budget.",
		func(ctx context.Context, a grantIDArgs) (*sdk.CallToolResult, error) {
			if a.GrantID == "" {
				return toolError(errors.New("grant_id is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/grants/"+url.PathEscape(a.GrantID), nil))
		})

	addEnumTool(srv, "policy.grants.create",
		"Grant a set of scopes to a principal. You can only assign scopes you hold yourself (call policy.scopes "+
			"to see them); the policy service rejects any scope outside your own authority.",
		map[string][]any{"principal_type": principalTypes},
		func(ctx context.Context, a createGrantArgs) (*sdk.CallToolResult, error) {
			if a.PrincipalType == "" || a.PrincipalID == "" || len(a.Scopes) == 0 {
				return toolError(errors.New("principal_type, principal_id, and scopes are required")), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), base+"/grants", a))
		})

	addTool(srv, "policy.grants.set_budget",
		"Set or remove the spend budget on a grant. Scopes on a grant are immutable; only the budget is mutable. "+
			"Omit budget to remove it.",
		func(ctx context.Context, a setBudgetArgs) (*sdk.CallToolResult, error) {
			if a.GrantID == "" {
				return toolError(errors.New("grant_id is required")), nil
			}
			body := map[string]any{"budget": a.Budget}
			return jsonResult(jc.Patch(ctx, callerToken(ctx), base+"/grants/"+url.PathEscape(a.GrantID), body))
		})

	addTool(srv, "policy.grants.delete",
		"Delete (revoke) a grant by id.",
		func(ctx context.Context, a grantIDArgs) (*sdk.CallToolResult, error) {
			if a.GrantID == "" {
				return toolError(errors.New("grant_id is required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx), base+"/grants/"+url.PathEscape(a.GrantID)))
		})

	// ── roles ───────────────────────────────────────────────────────────────────

	addTool(srv, "policy.roles.list",
		"List the roles defined in the caller's tenant (a role bundles a set of scopes).",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/roles", nil))
		})

	addTool(srv, "policy.roles.get",
		"Get a single role by name, including the scopes it bundles.",
		func(ctx context.Context, a roleNameArgs) (*sdk.CallToolResult, error) {
			if a.Role == "" {
				return toolError(errors.New("role is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/roles/"+url.PathEscape(a.Role), nil))
		})

	addTool(srv, "policy.roles.upsert",
		"Create or update a role bundling a set of scopes. Like grants, you can only bundle scopes you hold "+
			"yourself (call policy.scopes).",
		func(ctx context.Context, a upsertRoleArgs) (*sdk.CallToolResult, error) {
			if a.Role == "" || len(a.Scopes) == 0 {
				return toolError(errors.New("role and scopes are required")), nil
			}
			return jsonResult(jc.Put(ctx, callerToken(ctx), base+"/roles", a))
		})

	addTool(srv, "policy.roles.delete",
		"Delete a role by name.",
		func(ctx context.Context, a roleNameArgs) (*sdk.CallToolResult, error) {
			if a.Role == "" {
				return toolError(errors.New("role is required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx), base+"/roles/"+url.PathEscape(a.Role)))
		})

	// ── vocabulary + routes (read the authorization model itself) ────────────────

	addTool(srv, "policy.vocabulary.list",
		"List the scope vocabulary — every scope the platform knows about, its resource type, action, and "+
			"description. This is the catalog of what can be granted.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/vocabulary", nil))
		})

	addTool(srv, "policy.vocabulary.get",
		"Get a single scope-vocabulary entry by scope string (e.g. platform:grants:read) — its resource type, "+
			"action, and description.",
		func(ctx context.Context, a scopeArgs) (*sdk.CallToolResult, error) {
			if a.Scope == "" {
				return toolError(errors.New("scope is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/vocabulary/"+url.PathEscape(a.Scope), nil))
		})

	addTool(srv, "policy.routes.list",
		"List every registered route→scope mapping — which API route requires which scope. Use this to "+
			"understand what a scope actually authorizes across the platform.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/routes", nil))
		})
}

// scopeArgs identifies a single scope-vocabulary entry.
type scopeArgs struct {
	Scope string `json:"scope" jsonschema:"the scope string (e.g. platform:grants:read)"`
}

// principalTypes is the closed set of grant principal types, surfaced as a
// JSON-Schema enum. Mirrors internal/policyservice/grants.PrincipalType.
var principalTypes = []any{"AGENT", "HUMAN", "SERVICE", "PAT"}

// ── tool argument types (JSON schema is generated from these) ─────────────────

// budgetArg is an optional spend budget on a grant or role.
type budgetArg struct {
	LimitUSD       float64 `json:"limit_usd,omitempty" jsonschema:"per-window spend ceiling in USD (0 = none)"`
	ResetCadence   string  `json:"reset_cadence,omitempty" jsonschema:"budget window: weekly or monthly (default monthly)"`
	Pooled         bool    `json:"pooled,omitempty" jsonschema:"whether the budget is pooled across principals holding the role"`
	PerRunLimitUSD float64 `json:"per_run_limit_usd,omitempty" jsonschema:"windowless per-run ceiling in USD (grants only; 0 = none)"`
}

type listGrantsArgs struct {
	PrincipalType string `json:"principal_type,omitempty" jsonschema:"filter by principal type (AGENT, HUMAN, SERVICE, PAT)"`
	PrincipalID   string `json:"principal_id,omitempty" jsonschema:"filter by principal id"`
}

type grantIDArgs struct {
	GrantID string `json:"grant_id" jsonschema:"the grant id"`
}

type createGrantArgs struct {
	PrincipalType string     `json:"principal_type" jsonschema:"the principal type to grant to"`
	PrincipalID   string     `json:"principal_id" jsonschema:"the principal id (an agent's SPIFFE id, a user id, a service or PAT id)"`
	Scopes        []string   `json:"scopes" jsonschema:"the scopes to grant — only scopes you hold yourself are allowed; call the policy.scopes tool to list them"`
	Budget        *budgetArg `json:"budget,omitempty" jsonschema:"optional spend budget for the grant"`
}

type setBudgetArgs struct {
	GrantID string     `json:"grant_id" jsonschema:"the grant id whose budget to set"`
	Budget  *budgetArg `json:"budget,omitempty" jsonschema:"the new budget; omit to remove the budget"`
}

type roleNameArgs struct {
	Role string `json:"role" jsonschema:"the role name"`
}

type upsertRoleArgs struct {
	Role   string     `json:"role" jsonschema:"the role name"`
	Scopes []string   `json:"scopes" jsonschema:"the scopes the role bundles — only scopes you hold yourself are allowed; call the policy.scopes tool"`
	Budget *budgetArg `json:"budget,omitempty" jsonschema:"optional budget for the role"`
}
