package agents

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestParseManifestOverrides(t *testing.T) {
	tests := []struct {
		name          string
		environ       []string
		want          ManifestOverrides
		wantMalformed []string
	}{
		{
			name:    "empty environ",
			environ: nil,
			want:    ManifestOverrides{},
		},
		{
			name:    "no matching prefix",
			environ: []string{"OTHER_VAR=value", "ORBIT_AGENTS_URL=http://localhost"},
			want:    ManifestOverrides{},
		},
		{
			name:          "too few segments (no path) — malformed",
			environ:       []string{"ORBIT_MANIFEST__StatefulSet__coder=3"},
			want:          ManifestOverrides{},
			wantMalformed: []string{"ORBIT_MANIFEST__StatefulSet__coder"},
		},
		{
			name:          "only Kind segment — malformed",
			environ:       []string{"ORBIT_MANIFEST__StatefulSet=3"},
			want:          ManifestOverrides{},
			wantMalformed: []string{"ORBIT_MANIFEST__StatefulSet"},
		},
		{
			name:    "wildcard name",
			environ: []string{"ORBIT_MANIFEST__PersistentVolumeClaim__*__spec__storageClassName=premium-ssd"},
			want: ManifestOverrides{
				"PersistentVolumeClaim": {{name: "*", path: "spec.storageClassName", value: "premium-ssd"}},
			},
		},
		{
			name:    "slug-specific name",
			environ: []string{"ORBIT_MANIFEST__StatefulSet__coder__spec__replicas=5"},
			want: ManifestOverrides{
				"StatefulSet": {{name: "coder", path: "spec.replicas", value: "5"}},
			},
		},
		{
			name: "multiple overrides for same kind",
			environ: []string{
				"ORBIT_MANIFEST__StatefulSet__*__spec__replicas=3",
				"ORBIT_MANIFEST__StatefulSet__coder__spec__replicas=5",
			},
			want: ManifestOverrides{
				"StatefulSet": {
					{name: "*", path: "spec.replicas", value: "3"},
					{name: "coder", path: "spec.replicas", value: "5"},
				},
			},
		},
		{
			name: "multiple kinds",
			environ: []string{
				"ORBIT_MANIFEST__StatefulSet__*__spec__replicas=2",
				"ORBIT_MANIFEST__PersistentVolumeClaim__*__spec__storageClassName=fast",
				"ORBIT_MANIFEST__PodDisruptionBudget__*__spec__minAvailable=2",
			},
			want: ManifestOverrides{
				"StatefulSet":           {{name: "*", path: "spec.replicas", value: "2"}},
				"PersistentVolumeClaim": {{name: "*", path: "spec.storageClassName", value: "fast"}},
				"PodDisruptionBudget":   {{name: "*", path: "spec.minAvailable", value: "2"}},
			},
		},
		{
			name:    "deeply nested path",
			environ: []string{"ORBIT_MANIFEST__PersistentVolumeClaim__researcher__spec__resources__requests__storage=50Gi"},
			want: ManifestOverrides{
				"PersistentVolumeClaim": {{name: "researcher", path: "spec.resources.requests.storage", value: "50Gi"}},
			},
		},
		{
			name:    "mixed matching and non-matching entries",
			environ: []string{"UNRELATED=val", "ORBIT_MANIFEST__Service__*__metadata__name=custom", "OTHER=x"},
			want: ManifestOverrides{
				"Service": {{name: "*", path: "metadata.name", value: "custom"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotMalformed := ParseManifestOverrides(tt.environ)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseManifestOverrides() got %d kinds, want %d: %v", len(got), len(tt.want), got)
			}
			for kind, wantOverrides := range tt.want {
				gotOverrides, ok := got[kind]
				if !ok {
					t.Fatalf("ParseManifestOverrides() missing kind %q", kind)
				}
				if len(gotOverrides) != len(wantOverrides) {
					t.Fatalf("kind %q: got %d overrides, want %d", kind, len(gotOverrides), len(wantOverrides))
				}
				for i, wo := range wantOverrides {
					go_ := gotOverrides[i]
					if go_.name != wo.name || go_.path != wo.path || go_.value != wo.value {
						t.Errorf("kind %q override[%d]: got {%q %q %q}, want {%q %q %q}",
							kind, i, go_.name, go_.path, go_.value, wo.name, wo.path, wo.value)
					}
				}
			}
			if len(gotMalformed) != len(tt.wantMalformed) {
				t.Fatalf("malformed keys: got %v, want %v", gotMalformed, tt.wantMalformed)
			}
			for i, wm := range tt.wantMalformed {
				if gotMalformed[i] != wm {
					t.Errorf("malformed[%d]: got %q, want %q", i, gotMalformed[i], wm)
				}
			}
		})
	}
}

func TestOverridesForSlug(t *testing.T) {
	overrides := []resourceOverride{
		{name: "*", path: "spec.replicas", value: "3"},
		{name: "coder", path: "spec.replicas", value: "5"},
		{name: "researcher", path: "spec.replicas", value: "1"},
	}

	tests := []struct {
		slug string
		want []resourceOverride // expected slice in order
	}{
		// Wildcard must come before slug-specific so slug-specific wins (last-write wins).
		{"coder", []resourceOverride{{name: "*", value: "3"}, {name: "coder", value: "5"}}},
		{"researcher", []resourceOverride{{name: "*", value: "3"}, {name: "researcher", value: "1"}}},
		{"other", []resourceOverride{{name: "*", value: "3"}}},
		{"", []resourceOverride{{name: "*", value: "3"}}},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			got := overridesForSlug(overrides, tt.slug)
			if len(got) != len(tt.want) {
				t.Fatalf("overridesForSlug(%q) got %d overrides, want %d", tt.slug, len(got), len(tt.want))
			}
			for i, w := range tt.want {
				if got[i].name != w.name || got[i].value != w.value {
					t.Errorf("override[%d]: got {name:%q value:%q}, want {name:%q value:%q}",
						i, got[i].name, got[i].value, w.name, w.value)
				}
			}
		})
	}
}

func TestApplyOverrides(t *testing.T) {
	t.Run("no overrides returns obj unchanged", func(t *testing.T) {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "workspaces"},
		}
		got, err := applyOverrides(pvc, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "workspaces" {
			t.Errorf("expected name unchanged, got %q", got.Name)
		}
	})

	t.Run("string value sets string field", func(t *testing.T) {
		sc := "premium-ssd"
		pvc := corev1.PersistentVolumeClaim{}
		overrides := []resourceOverride{{name: "*", path: "spec.storageClassName", value: "premium-ssd"}}
		got, err := applyOverrides(pvc, overrides)
		if err != nil {
			t.Fatal(err)
		}
		if got.Spec.StorageClassName == nil || *got.Spec.StorageClassName != sc {
			t.Errorf("expected storageClassName %q, got %v", sc, got.Spec.StorageClassName)
		}
	})

	t.Run("string annotation value set via metadata path", func(t *testing.T) {
		pvc := corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			},
		}
		overrides := []resourceOverride{{name: "*", path: "metadata.annotations.custom\\.io/tag", value: "v2"}}
		got, err := applyOverrides(pvc, overrides)
		if err != nil {
			t.Fatal(err)
		}
		if got.Annotations["custom.io/tag"] != "v2" {
			t.Errorf("expected annotation custom.io/tag=v2, got %v", got.Annotations)
		}
	})

	t.Run("multiple overrides applied in order", func(t *testing.T) {
		pvc := corev1.PersistentVolumeClaim{}
		sc1 := "slow"
		sc2 := "fast"
		overrides := []resourceOverride{
			{name: "*", path: "spec.storageClassName", value: sc1},
			{name: "*", path: "spec.storageClassName", value: sc2},
		}
		got, err := applyOverrides(pvc, overrides)
		if err != nil {
			t.Fatal(err)
		}
		if got.Spec.StorageClassName == nil || *got.Spec.StorageClassName != sc2 {
			t.Errorf("expected last override to win (%q), got %v", sc2, got.Spec.StorageClassName)
		}
	})
}
