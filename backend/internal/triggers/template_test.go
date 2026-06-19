package triggers

import "testing"

func TestRenderTemplate(t *testing.T) {
	fields := map[string]string{
		"pr.number":   "42",
		"pr.title":    "Add widget",
		"pr.base_ref": "main",
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"no placeholders", "Just review the PR.", "Just review the PR."},
		{"single", "PR {{ pr.number }}", "PR 42"},
		{"no surrounding spaces", "PR {{pr.number}}", "PR 42"},
		{"multiple", "Review #{{ pr.number }}: {{ pr.title }} -> {{ pr.base_ref }}", "Review #42: Add widget -> main"},
		{"unknown key renders empty", "x={{ pr.author }}y", "x=y"},
		{"repeated key", "{{ pr.number }}/{{ pr.number }}", "42/42"},
		{"empty template", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderTemplate(tt.tmpl, fields); got != tt.want {
				t.Errorf("renderTemplate(%q) = %q, want %q", tt.tmpl, got, tt.want)
			}
		})
	}
}

func TestRenderTemplate_NilFields(t *testing.T) {
	if got := renderTemplate("hi {{ x }}", nil); got != "hi " {
		t.Errorf("renderTemplate with nil fields = %q, want %q", got, "hi ")
	}
}
