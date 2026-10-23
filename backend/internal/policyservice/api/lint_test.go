package api

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

func TestCoherenceWarnings(t *testing.T) {
	vocab := []*vocabulary.ScopeEntry{
		{Scope: "connection:github:metadata:read"},
		{Scope: "connection:github:issues:write", Requires: []string{"connection:github:metadata:read"}},
		{Scope: "mcp:kora:runs.list:use", Requires: []string{"agents:runs:read"}},
	}

	cases := []struct {
		name   string
		scopes []string
		want   []string
	}{
		{"coherent set", []string{"inference:invoke", "inference:anthropic:use", "mcp:invoke", "mcp:gmail:send:use"}, nil},
		{"model without invoke", []string{"inference:anthropic:sonnet:use"},
			[]string{"inference:anthropic:sonnet:use needs inference:invoke, which is not included"}},
		{"provider without invoke reported once", []string{"inference:anthropic:use", "inference:openai:use"},
			[]string{"inference:anthropic:use needs inference:invoke, which is not included"}},
		{"mcp tool without gate", []string{"mcp:gmail:send:use"},
			[]string{"mcp:gmail:send:use needs mcp:invoke, which is not included"}},
		{"connection category covered by requires", []string{"connection:github:issues:write"}, nil},
		{"qualified connection scope without metadata", []string{"connection:github:issues:kora:write"},
			[]string{"connection:github:issues:kora:write needs connection:github:metadata:read, which is not included"}},
		{"adapter without metadata scope", []string{"connection:k8s:workloads:read"}, nil},
		{"per-agent run without types read", []string{"agents:runs:coder:write"},
			[]string{"agents:runs:coder:write needs agents:types:coder:read, which is not included"}},
		{"per-agent run with tenant types read", []string{"agents:runs:coder:read", "agents:types:read"}, nil},
		{"per-agent run with per-agent types read", []string{"agents:runs:coder:read", "agents:types:coder:read"}, nil},
		{"mode run scope is not per-agent", []string{"agents:runs:delegated:write"}, nil},
		{"types write without read", []string{"agents:types:delegated:write"},
			[]string{"agents:types:delegated:write needs agents:types:read, which is not included"}},
		{"triggers write without read", []string{"triggers:write"},
			[]string{"triggers:write needs triggers:read, which is not included"}},
		{"egress is ignored", []string{"egress:connect"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := coherenceWarnings(tc.scopes, vocab)
			if !slices.Equal(got, tc.want) {
				t.Errorf("coherenceWarnings(%v) = %q, want %q", tc.scopes, got, tc.want)
			}
		})
	}
}

// TestGrantWriteResponseShape locks that warnings sit beside the grant's own
// fields, so existing clients reading the grant keep working.
func TestGrantWriteResponseShape(t *testing.T) {
	body, err := json.Marshal(grantWriteResponse{Grant: &grants.Grant{ID: "g1"}, Warnings: []string{"w"}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "g1" || got["warnings"] == nil {
		t.Errorf("response = %s, want id and warnings at the top level", body)
	}
}
