package connections

import (
	"strings"
	"testing"
)

func TestValidPlaceholderEnvName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"NPM_TOKEN", true},
		{"_A1", true},
		{"npm_token", true},
		{strings.Repeat("A", 128), true},
		{strings.Repeat("A", 129), false},
		{"", false},
		{"1TOKEN", false},
		{"bad name", false},
		{"A-B", false},
		{"PATH", false},
		{"path", false},
		{"HOME", false},
		{"LD_PRELOAD", false},
		{"ld_library_path", false},
		{"DYLD_INSERT_LIBRARIES", false},
		{"KORA_TOKEN", false},
		{"kora_x", false},
		{"KUBECONFIG", false},
		{"NODE_OPTIONS", false},
	}
	for _, tc := range cases {
		if got := ValidPlaceholderEnvName(tc.name); got != tc.want {
			t.Errorf("ValidPlaceholderEnvName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
