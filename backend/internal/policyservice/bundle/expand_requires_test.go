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

func TestExpandRequires_CycleSafe(t *testing.T) {
	reqMap := map[string][]string{"a": {"b"}, "b": {"a"}}
	got := expandRequires([]string{"a"}, reqMap)
	if strings.Join(got, ",") != "a,b" {
		t.Errorf("cycle: got %v, want [a b]", got)
	}
}
