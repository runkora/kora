package agents

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestStartingRunAllowances_For(t *testing.T) {
	a := NewStartingRunAllowances(resource.MustParse("180Mi"), resource.MustParse("60Mi"), resource.MustParse("440Mi"))
	for framework, want := range map[AgentFramework]string{"claude-code": "180Mi", "": "180Mi", "codex": "60Mi", "opencode": "440Mi"} {
		got, err := a.For(framework)
		if err != nil || got.Cmp(resource.MustParse(want)) != 0 {
			t.Errorf("For(%q) = %s, %v; want %s", framework, got.String(), err, want)
		}
	}
	if _, err := a.For("gemini"); !errors.Is(err, ErrNoStartingRunAllowance) {
		t.Errorf("For(gemini) error = %v, want ErrNoStartingRunAllowance", err)
	}
}
