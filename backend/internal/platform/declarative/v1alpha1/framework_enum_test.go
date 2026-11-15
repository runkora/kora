package v1alpha1

import (
	"os"
	"slices"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/runkora/kora/backend/internal/platform/harnesses"
)

// TestFrameworkEnumMatchesHarnessList fails when the generated Agent CRD's
// spec.framework enum differs from the shared harness list. Fix it by editing
// the kubebuilder Enum marker on AgentSpec.Framework and running `make generate`.
func TestFrameworkEnumMatchesHarnessList(t *testing.T) {
	raw, err := os.ReadFile("../../../../../charts/kora-crds/generated/runkora.io_agents.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var crd struct {
		Spec struct {
			Versions []struct {
				Schema struct {
					OpenAPIV3Schema struct {
						Properties struct {
							Spec struct {
								Properties struct {
									Framework struct {
										Enum []string `json:"enum"`
									} `json:"framework"`
								} `json:"properties"`
							} `json:"spec"`
						} `json:"properties"`
					} `json:"openAPIV3Schema"`
				} `json:"schema"`
			} `json:"versions"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &crd); err != nil {
		t.Fatal(err)
	}
	if len(crd.Spec.Versions) == 0 {
		t.Fatal("CRD has no versions")
	}
	got := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties.Spec.Properties.Framework.Enum
	if !slices.Equal(got, harnesses.IDs()) {
		t.Errorf("CRD spec.framework enum %v differs from harness list %v", got, harnesses.IDs())
	}
}
