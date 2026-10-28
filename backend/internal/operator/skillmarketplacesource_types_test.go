package operator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewScheme_RegistersSkillMarketplaceSource(t *testing.T) {
	s := NewScheme()
	for _, kind := range []string{"SkillMarketplaceSource", "SkillMarketplaceSourceList"} {
		if !s.Recognizes(GroupVersion.WithKind(kind)) {
			t.Errorf("scheme does not recognise %s", kind)
		}
	}
}

func TestSkillMarketplaceSourceDeepCopy_CopiesStatus(t *testing.T) {
	now := metav1.Now()
	in := &SkillMarketplaceSource{Status: SkillMarketplaceSourceStatus{
		LastReconciledAt: &now,
		Conditions:       []metav1.Condition{{Type: conditionAggregated, Reason: MarketplaceReasonNotAllowlisted}},
	}}
	out := in.DeepCopy()
	out.Status.Conditions[0].Reason = MarketplaceReasonAggregated

	if in.Status.Conditions[0].Reason != MarketplaceReasonNotAllowlisted {
		t.Error("Conditions shares its array with the copy")
	}
	if out.Status.LastReconciledAt == in.Status.LastReconciledAt {
		t.Error("LastReconciledAt pointer is shared with the copy")
	}
}

func TestSkillMarketplaceSourceGitHub(t *testing.T) {
	g := SkillMarketplaceSourceGitHub{Owner: "Acme-Corp", Repo: "Internal-Skills"}
	if got := g.repoKey(); got != "acme-corp/internal-skills" {
		t.Errorf("repoKey() = %q, want %q", got, "acme-corp/internal-skills")
	}
}
