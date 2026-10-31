package koramcp

import (
	"context"
	"net/url"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runkora/kora/backend/internal/platform/clients/korajson"
)

// registerBudgetTools wires the budgets domain: the tenant-level rollup cap (on the
// policy service) plus per-principal budget status (on the inference gateway). Grant
// and role budgets are set through policy.grants.set_budget and policy.roles.upsert.
// Reads require platform:budgets:read, writes platform:budgets:write.
func registerBudgetTools(srv *sdk.Server, b Backends, jc *korajson.Client) {
	policy := b.Policy + "/api/v1/policy"

	addTool(srv, "budgets.tenant.get",
		"Get the tenant's rollup spending cap (the ceiling across all grants and roles). "+
			"Defaults to the caller's tenant.",
		func(ctx context.Context, a tenantBudgetIDArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, caller(ctx), policy, "/tenant-budget", tenantQuery(a.TenantID)))
		})

	addTool(srv, "budgets.tenant.set",
		"Set the tenant's rollup spending cap. limit_usd is the ceiling; reset_cadence is weekly or monthly "+
			"(default monthly). Defaults to the caller's tenant.",
		func(ctx context.Context, a setTenantBudgetArgs) (*sdk.CallToolResult, error) {
			body := map[string]any{"limit_usd": a.LimitUSD}
			if a.TenantID != "" {
				body["tenant_id"] = a.TenantID
			}
			if a.ResetCadence != "" {
				body["reset_cadence"] = a.ResetCadence
			}
			return jsonResult(jc.Put(ctx, caller(ctx), policy, "/tenant-budget", body))
		})

	addTool(srv, "budgets.tenant.delete",
		"Remove the tenant's rollup spending cap. Defaults to the caller's tenant.",
		func(ctx context.Context, a tenantBudgetIDArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Delete(ctx, caller(ctx), policy, "/tenant-budget", tenantQuery(a.TenantID)))
		})

	addTool(srv, "budgets.principals.status",
		"Read per-principal budget status across the tenant — each principal's spend against its budget. "+
			"Use this to find who is near or over budget.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, caller(ctx), b.Inference, "/v1/budgets/principals", nil))
		})
}

// tenantQuery builds the optional tenant_id query (empty → caller's own tenant).
func tenantQuery(tenantID string) url.Values {
	if tenantID == "" {
		return nil
	}
	return url.Values{"tenant_id": {tenantID}}
}

// ── tool argument types ───────────────────────────────────────────────────────

type tenantBudgetIDArgs struct {
	TenantID string `json:"tenant_id,omitempty" jsonschema:"the tenant id (default: the caller's own tenant)"`
}

type setTenantBudgetArgs struct {
	TenantID     string  `json:"tenant_id,omitempty" jsonschema:"the tenant id (default: the caller's own tenant)"`
	LimitUSD     float64 `json:"limit_usd" jsonschema:"the rollup spend ceiling in USD"`
	ResetCadence string  `json:"reset_cadence,omitempty" jsonschema:"budget window: weekly or monthly (default monthly)"`
}
