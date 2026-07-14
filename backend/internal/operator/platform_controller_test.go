package operator

import (
	"strings"
	"testing"
)

func newTestOrbitPlatform(mutate func(*OrbitPlatform)) *OrbitPlatform {
	p := &OrbitPlatform{
		Spec: OrbitPlatformSpec{
			TrustDomain: "orbit.test",
		},
	}
	p.Spec.SPIRE.Enabled = true
	if mutate != nil {
		mutate(p)
	}
	return p
}

// TestBuildIstiodValues_EnableNativeSidecarsPinnedUnderFlatEnvKey guards
// pinned decision 8: ENABLE_NATIVE_SIDECARS must be set under the chart's
// flat top-level `env:` key, never `pilot.env`, which istiod's own
// Deployment template never reads.
func TestBuildIstiodValues_EnableNativeSidecarsPinnedUnderFlatEnvKey(t *testing.T) {
	p := newTestOrbitPlatform(nil)
	values, err := buildIstiodValues(p, "opa-image", "info")
	if err != nil {
		t.Fatalf("buildIstiodValues: %v", err)
	}

	env, ok := values["env"].(map[string]any)
	if !ok {
		t.Fatalf(`values["env"] missing or wrong type: %#v`, values["env"])
	}
	if env["ENABLE_NATIVE_SIDECARS"] != "false" {
		t.Errorf(`env["ENABLE_NATIVE_SIDECARS"] = %v, want "false"`, env["ENABLE_NATIVE_SIDECARS"])
	}

	if _, ok := values["pilot"]; ok {
		t.Errorf(`values["pilot"] must not be set — istiod's Deployment never reads pilot.env, only the flat env: key`)
	}
}

// TestBuildIstiodValues_DiscoverySelectorsExcludeDevenvOnHostOnly guards
// pinned decision 10: the host istiod excludes devenv namespaces from mesh
// discovery, but an in-vcluster istiod (SPIRE.Enabled=false) must not carry
// this selector at all — it has no devenv namespaces of its own to exclude.
func TestBuildIstiodValues_DiscoverySelectorsExcludeDevenvOnHostOnly(t *testing.T) {
	host := newTestOrbitPlatform(func(p *OrbitPlatform) { p.Spec.SPIRE.Enabled = true })
	hostValues, err := buildIstiodValues(host, "", "")
	if err != nil {
		t.Fatalf("buildIstiodValues: %v", err)
	}
	meshConfig, ok := hostValues["meshConfig"].(map[string]any)
	if !ok {
		t.Fatalf(`values["meshConfig"] missing or wrong type`)
	}
	selectors, ok := meshConfig["discoverySelectors"].([]map[string]any)
	if !ok || len(selectors) != 1 {
		t.Fatalf("host install: meshConfig.discoverySelectors = %#v, want exactly one selector", meshConfig["discoverySelectors"])
	}

	inVCluster := newTestOrbitPlatform(func(p *OrbitPlatform) { p.Spec.SPIRE.Enabled = false })
	vclusterValues, err := buildIstiodValues(inVCluster, "", "")
	if err != nil {
		t.Fatalf("buildIstiodValues: %v", err)
	}
	vcMeshConfig, ok := vclusterValues["meshConfig"].(map[string]any)
	if !ok {
		t.Fatalf(`values["meshConfig"] missing or wrong type`)
	}
	if _, ok := vcMeshConfig["discoverySelectors"]; ok {
		t.Errorf("in-vcluster install: meshConfig.discoverySelectors must be unset, got %#v", vcMeshConfig["discoverySelectors"])
	}
}

// TestBuildIstiodValues_MeshJWTValidationSetsEscapedJWTRuleWithAudiences
// guards pinned decision 9: JWT_RULE must be YAML-escaped for the chart's
// unescaped env template, and audiences must always be present and
// non-empty (an empty audiences list rejects every token).
func TestBuildIstiodValues_MeshJWTValidationSetsEscapedJWTRuleWithAudiences(t *testing.T) {
	p := newTestOrbitPlatform(func(p *OrbitPlatform) {
		p.Spec.MeshJWTValidation = MeshJWTValidationSpec{
			Enabled: true,
			Issuer:  `https://kubernetes.default.svc.cluster.local`,
			JWKS:    `{"keys":[]}`,
		}
	})
	values, err := buildIstiodValues(p, "", "")
	if err != nil {
		t.Fatalf("buildIstiodValues: %v", err)
	}
	env, ok := values["env"].(map[string]any)
	if !ok {
		t.Fatalf(`values["env"] missing or wrong type`)
	}
	jwtRule, ok := env["JWT_RULE"].(string)
	if !ok || jwtRule == "" {
		t.Fatalf(`env["JWT_RULE"] = %#v, want a non-empty string`, env["JWT_RULE"])
	}
	// The raw (unescaped) JSON must carry a non-empty audiences list — an
	// absent/empty one makes JWT_RULE reject every token.
	unescaped := unescapeHelmDoubleQuotedScalar(jwtRule)
	if !containsAudiences(unescaped) {
		t.Errorf("JWT_RULE JSON does not carry a non-empty audiences list: %s", unescaped)
	}

	if _, ok := values["volumes"]; !ok {
		t.Error(`values["volumes"] must be set for the mesh-jwks ConfigMap mount when MeshJWTValidation is enabled`)
	}
	if _, ok := values["initContainers"]; !ok {
		t.Error(`values["initContainers"] must be set for the jwks-mirror sidecar when MeshJWTValidation is enabled`)
	}
}

// TestBuildIstiodValues_MeshJWTValidationDisabledOmitsJWTRule confirms the
// default (host, non-devenv) shape carries none of the JWT_RULE plumbing.
func TestBuildIstiodValues_MeshJWTValidationDisabledOmitsJWTRule(t *testing.T) {
	p := newTestOrbitPlatform(nil)
	values, err := buildIstiodValues(p, "", "")
	if err != nil {
		t.Fatalf("buildIstiodValues: %v", err)
	}
	env, ok := values["env"].(map[string]any)
	if !ok {
		t.Fatalf(`values["env"] missing or wrong type`)
	}
	if _, ok := env["JWT_RULE"]; ok {
		t.Errorf(`env["JWT_RULE"] must be unset when MeshJWTValidation is disabled, got %#v`, env["JWT_RULE"])
	}
	if _, ok := values["volumes"]; ok {
		t.Error(`values["volumes"] must be unset when MeshJWTValidation is disabled`)
	}
	if _, ok := values["initContainers"]; ok {
		t.Error(`values["initContainers"] must be unset when MeshJWTValidation is disabled`)
	}
}

// TestHelmDoubleQuotedScalarEscape_RoundTrips guards pinned decision 9's
// YAML-escaping requirement directly: escaping then embedding in a
// double-quoted YAML scalar must decode back to the original string.
func TestHelmDoubleQuotedScalarEscape_RoundTrips(t *testing.T) {
	tests := []string{
		`{"issuer":"https://x","jwks_uri":"http://127.0.0.1:8899/jwks","audiences":["istio-ca"]}`,
		`no special characters`,
		`back\slash`,
		`"quoted"`,
	}
	for _, in := range tests {
		escaped := helmDoubleQuotedScalarEscape(in)
		got := unescapeHelmDoubleQuotedScalar(escaped)
		if got != in {
			t.Errorf("round trip failed: in=%q escaped=%q got=%q", in, escaped, got)
		}
	}
}

// unescapeHelmDoubleQuotedScalar reverses helmDoubleQuotedScalarEscape,
// for test assertions only.
func unescapeHelmDoubleQuotedScalar(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '\\' || s[i+1] == '"') {
			out = append(out, s[i+1])
			i++
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// containsAudiences is a light substring check (avoids importing
// encoding/json into the test just for this) that the marshaled JWT_RULE
// carries a non-empty audiences array.
func containsAudiences(jsonStr string) bool {
	return strings.Contains(jsonStr, `"audiences":["istio-ca"]`)
}
