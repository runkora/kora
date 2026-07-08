package bundle

import (
	"strings"
	"testing"
)

func TestExpandRequires(t *testing.T) {
	reqMap := map[string][]string{
		"mcp:orbit:agents:list:use":   {"agents:types:read"},
		"mcp:orbit:agents:create:use": {"agents:types:write"},
	}
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"no requires", []string{"inference:invoke"}, []string{"inference:invoke"}},
		{"one tool confers its scope", []string{"mcp:orbit:agents:list:use", "mcp:invoke"}, []string{"mcp:orbit:agents:list:use", "mcp:invoke", "agents:types:read"}},
		{"dedups a shared required scope is kept once", []string{"mcp:orbit:agents:list:use", "agents:types:read"}, []string{"mcp:orbit:agents:list:use", "agents:types:read"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := expandRequires(tc.in, reqMap)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("expandRequires(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestExpandRequires_PrefixAware verifies a family/parent scope confers the
// requires of every leaf it covers (not only an exact-key match) — so a role or
// grant holding mcp:orbit:use passes the gateway PEP AND holds each tool's
// upstream platform scope.
func TestExpandRequires_PrefixAware(t *testing.T) {
	reqMap := map[string][]string{
		"mcp:orbit:agents:list:use": {"agents:types:read"},
		"mcp:orbit:models:list:use": {"inference:list"},
	}

	// The server-level family confers every covered leaf's requires.
	all := strings.Join(expandRequires([]string{"mcp:orbit:use"}, reqMap), ",")
	for _, want := range []string{"agents:types:read", "inference:list"} {
		if !strings.Contains(all, want) {
			t.Errorf("mcp:orbit:use should confer %q; got %s", want, all)
		}
	}

	// A narrower family confers only the leaves under it.
	agents := strings.Join(expandRequires([]string{"mcp:orbit:agents:use"}, reqMap), ",")
	if !strings.Contains(agents, "agents:types:read") {
		t.Errorf("mcp:orbit:agents:use should confer agents:types:read; got %s", agents)
	}
	if strings.Contains(agents, "inference:list") {
		t.Errorf("mcp:orbit:agents:use must not confer the models leaf's inference:list; got %s", agents)
	}
}

func TestExpandRequires_CycleSafe(t *testing.T) {
	reqMap := map[string][]string{"a": {"b"}, "b": {"a"}}
	got := expandRequires([]string{"a"}, reqMap)
	if strings.Join(got, ",") != "a,b" {
		t.Errorf("cycle: got %v, want [a b]", got)
	}
}
