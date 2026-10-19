package koramcp

import (
	"context"
	"errors"
	"net/url"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runkora/kora/backend/internal/platform/clients/korajson"
)

// registerPATTools wires the Personal Access Token domain: viewing and managing
// PATs. These endpoints are auth-only (no platform scope), scoped by the caller's
// token to their own tenant/principal, so they carry no toolRequires entry.
//
// Deliberately NO create tool: agents must not be able to mint new PATs (a PAT is
// a long-lived standing credential), so the POST /identity/tokens endpoint is not
// exposed through this MCP surface. Agents may view, re-scope, and revoke.
func registerPATTools(srv *sdk.Server, b Backends, jc *korajson.Client) {
	base := b.Identity + "/api/v1/identity/tokens"

	addTool(srv, "pats.list",
		"List the caller's active Personal Access Tokens — each token's id, name, scopes, and expiry "+
			"(the secret is never returned).",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), base, nil))
		})

	addTool(srv, "pats.attribution",
		"Resolve a set of PAT ids to their owning user profiles — who each token belongs to. "+
			"Use this to attribute PAT-authenticated activity to a person.",
		func(ctx context.Context, a patAttributionArgs) (*sdk.CallToolResult, error) {
			if len(a.IDs) == 0 {
				return toolError(errors.New("ids is required")), nil
			}
			q := url.Values{"ids": {strings.Join(a.IDs, ",")}}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/attribution", q))
		})

	addTool(srv, "pats.update_scopes",
		"Update the scopes on a Personal Access Token by id. You can only assign scopes you hold yourself "+
			"(call policy.scopes); the identity service rejects any scope outside your authority.",
		func(ctx context.Context, a updatePATScopesArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" || len(a.Scopes) == 0 {
				return toolError(errors.New("id and scopes are required")), nil
			}
			body := map[string]any{"scopes": a.Scopes}
			return jsonResult(jc.Patch(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.ID)+"/scopes", body))
		})

	addTool(srv, "pats.revoke",
		"Revoke (delete) a Personal Access Token by id. The token stops working immediately.",
		func(ctx context.Context, a patIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.ID)))
		})
}

// ── tool argument types ───────────────────────────────────────────────────────

type patIDArgs struct {
	ID string `json:"id" jsonschema:"the PAT id"`
}

type patAttributionArgs struct {
	IDs []string `json:"ids" jsonschema:"the PAT ids to resolve to owning user profiles"`
}

type updatePATScopesArgs struct {
	ID     string   `json:"id" jsonschema:"the PAT id whose scopes to update"`
	Scopes []string `json:"scopes" jsonschema:"the new scope set — only scopes you hold yourself are allowed (call policy.scopes)"`
}
