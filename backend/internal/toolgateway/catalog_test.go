package toolgateway

import "testing"

func TestCatalogToolsFromSnapshots(t *testing.T) {
	snaps := []ServerSnapshot{
		{
			Server: Server{TenantID: "", Name: "orbit"},
			State:  StateOK,
			Tools: []Tool{
				{Name: "agents.create", Description: "create an agent"},
			},
		},
		{
			Server: Server{TenantID: "acme", Name: "github"},
			State:  StateOK,
			Tools: []Tool{
				{Name: "create_issue"},
			},
		},
	}

	rows := CatalogToolsFromSnapshots(snaps)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}

	byScope := map[string]CatalogTool{}
	for _, r := range rows {
		byScope[r.Scope] = r
	}

	orbit, ok := byScope["mcp:orbit:agents:create:use"]
	if !ok {
		t.Fatalf("missing orbit tool scope; rows=%+v", rows)
	}
	if orbit.TenantID != "" || orbit.ServerSlug != "orbit" || orbit.ToolName != "agents.create" || orbit.Description != "create an agent" {
		t.Errorf("orbit row wrong: %+v", orbit)
	}

	gh, ok := byScope["mcp:github:create_issue:use"]
	if !ok {
		t.Fatalf("missing github tool scope; rows=%+v", rows)
	}
	if gh.TenantID != "acme" || gh.ServerSlug != "github" {
		t.Errorf("github row wrong: %+v", gh)
	}
}
