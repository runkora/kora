package koramcp

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runkora/kora/backend/internal/platform/clients/korajson"
)

// registerIdentityTools wires the identity domain: read-only user/profile lookups
// for access-audit workflows (who is in the tenant, who owns what). Auth-only
// (no platform scope), tenant-scoped by the caller's token — no toolRequires.
func registerIdentityTools(srv *sdk.Server, b Backends, jc *korajson.Client) {
	base := b.Identity + "/api/v1/identity/users"

	addTool(srv, "identity.users.list",
		"List the user profiles in the caller's tenant — id, display name, email. Optionally filter by a search "+
			"term and paginate. Use this to audit who has access.",
		func(ctx context.Context, a listUsersArgs) (*sdk.CallToolResult, error) {
			q := url.Values{}
			if a.Search != "" {
				q.Set("search", a.Search)
			}
			if a.Page > 0 {
				q.Set("page", strconv.Itoa(a.Page))
			}
			if a.Limit > 0 {
				q.Set("limit", strconv.Itoa(a.Limit))
			}
			return jsonResult(jc.Get(ctx, caller(ctx), base, q))
		})

	addTool(srv, "identity.users.get",
		"Get a single user profile by id — display name, email, and profile metadata.",
		func(ctx context.Context, a userIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Get(ctx, caller(ctx), base+"/"+url.PathEscape(a.ID), nil))
		})
}

// ── tool argument types ───────────────────────────────────────────────────────

type userIDArgs struct {
	ID string `json:"id" jsonschema:"the user id"`
}

type listUsersArgs struct {
	Search string `json:"search,omitempty" jsonschema:"filter by name/email substring"`
	Page   int    `json:"page,omitempty" jsonschema:"page number (1-based)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size"`
}
