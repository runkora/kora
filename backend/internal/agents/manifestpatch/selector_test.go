package manifestpatch

import "testing"

func TestPatchMatches(t *testing.T) {
	tests := []struct {
		name     string
		patch    Patch
		kind     string
		resource string
		labels   map[string]string
		annots   map[string]string
		want     bool
		wantErr  bool
	}{
		{
			name:     "kind mismatch",
			patch:    Patch{Kind: "Pod"},
			kind:     "StatefulSet",
			resource: "coder",
			want:     false,
		},
		{
			name:     "kind match, empty name wildcard",
			patch:    Patch{Kind: "StatefulSet"},
			kind:     "StatefulSet",
			resource: "coder",
			want:     true,
		},
		{
			name:     "name exact match",
			patch:    Patch{Kind: "StatefulSet", Name: "coder"},
			kind:     "StatefulSet",
			resource: "coder",
			want:     true,
		},
		{
			name:     "name mismatch",
			patch:    Patch{Kind: "StatefulSet", Name: "researcher"},
			kind:     "StatefulSet",
			resource: "coder",
			want:     false,
		},
		{
			name:     "label selector match",
			patch:    Patch{Kind: "Pod", LabelSelector: "orbit.io/role=sandbox"},
			kind:     "Pod",
			resource: "sandbox-run-1",
			labels:   map[string]string{"orbit.io/role": "sandbox"},
			want:     true,
		},
		{
			name:     "label selector mismatch",
			patch:    Patch{Kind: "Pod", LabelSelector: "orbit.io/role=sandbox"},
			kind:     "Pod",
			resource: "sandbox-run-1",
			labels:   map[string]string{"orbit.io/role": "session-manager"},
			want:     false,
		},
		{
			name:     "annotation selector match",
			patch:    Patch{Kind: "Pod", AnnotationSelector: "sidecar.istio.io/inject=true"},
			kind:     "Pod",
			resource: "coder",
			annots:   map[string]string{"sidecar.istio.io/inject": "true"},
			want:     true,
		},
		{
			name:     "annotation selector mismatch",
			patch:    Patch{Kind: "Pod", AnnotationSelector: "sidecar.istio.io/inject=true"},
			kind:     "Pod",
			resource: "coder",
			annots:   map[string]string{},
			want:     false,
		},
		{
			name:     "invalid label selector errors",
			patch:    Patch{Kind: "Pod", LabelSelector: "===bad==="},
			kind:     "Pod",
			resource: "x",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.patch.Matches(tt.kind, tt.resource, tt.labels, tt.annots)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Errorf("Matches = %v, want %v", got, tt.want)
			}
		})
	}
}
