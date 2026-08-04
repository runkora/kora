package operator

import (
	"strings"
	"testing"
)

// ── buildIstiodValues ─────────────────────────────────────────────────────
//
// docs/adr/2026-08-04-devenv-mode-platform-support.md, F-2/F-3/F-4.

func TestBuildIstiodValues_DiscoverySelectorsOnlyOnHostInstalls(t *testing.T) {
	host := &OrbitPlatform{Spec: OrbitPlatformSpec{TrustDomain: "orbit.test", SPIRE: SPIRESpec{Enabled: true}}}
	values, err := buildIstiodValues(host, "opa-image", "info")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	meshConfig := values["meshConfig"].(map[string]any)
	if _, ok := meshConfig["discoverySelectors"]; !ok {
		t.Error("host install (SPIRE.Enabled=true): expected discoverySelectors to be set")
	}

	devenv := &OrbitPlatform{Spec: OrbitPlatformSpec{TrustDomain: "orbit.test", SPIRE: SPIRESpec{Enabled: false}}}
	values, err = buildIstiodValues(devenv, "opa-image", "info")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	meshConfig = values["meshConfig"].(map[string]any)
	if _, ok := meshConfig["discoverySelectors"]; ok {
		t.Error("in-vcluster install (SPIRE.Enabled=false): expected discoverySelectors to be absent")
	}
}

func TestBuildIstiodValues_NativeSidecarsPinnedUnderFlatEnv(t *testing.T) {
	p := &OrbitPlatform{Spec: OrbitPlatformSpec{TrustDomain: "orbit.test", SPIRE: SPIRESpec{Enabled: true}}}
	values, err := buildIstiodValues(p, "opa-image", "info")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env, ok := values["env"].(map[string]any)
	if !ok {
		t.Fatal("expected top-level env key")
	}
	if env["ENABLE_NATIVE_SIDECARS"] != "false" {
		t.Errorf("env[ENABLE_NATIVE_SIDECARS] = %v, want %q", env["ENABLE_NATIVE_SIDECARS"], "false")
	}

	if pilot, ok := values["pilot"]; ok {
		if pilotMap, ok := pilot.(map[string]any); ok {
			if _, ok := pilotMap["env"]; ok {
				t.Error("ENABLE_NATIVE_SIDECARS must never be set under pilot.env — istiod's Deployment never reads it there")
			}
		}
	}
}

func TestBuildIstiodValues_MeshJWTValidationDisabledByDefault(t *testing.T) {
	p := &OrbitPlatform{Spec: OrbitPlatformSpec{TrustDomain: "orbit.test", SPIRE: SPIRESpec{Enabled: true}}}
	values, err := buildIstiodValues(p, "opa-image", "info")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env := values["env"].(map[string]any)
	if _, ok := env["JWT_RULE"]; ok {
		t.Error("JWT_RULE must not be set when MeshJWTValidation.Enabled is false")
	}
	if _, ok := values["volumes"]; ok {
		t.Error("mesh-jwks volume must not be set when MeshJWTValidation.Enabled is false")
	}
}

func TestBuildIstiodValues_MeshJWTValidationEnabledSetsNonEmptyAudiences(t *testing.T) {
	p := &OrbitPlatform{
		Spec: OrbitPlatformSpec{
			TrustDomain: "orbit.test",
			SPIRE:       SPIRESpec{Enabled: false},
			MeshJWTValidation: MeshJWTValidationSpec{
				Enabled: true,
				Issuer:  "https://host.example.com",
				JWKS:    `{"keys":[]}`,
			},
		},
	}
	values, err := buildIstiodValues(p, "opa-image", "info")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env := values["env"].(map[string]any)
	jwtRule, ok := env["JWT_RULE"].(string)
	if !ok || jwtRule == "" {
		t.Fatal("expected non-empty JWT_RULE")
	}
	if !strings.Contains(jwtRule, `\"audiences\":[\"istio-ca\"]`) {
		t.Errorf("JWT_RULE = %q, want a non-empty audiences list", jwtRule)
	}
	if _, ok := values["volumes"]; !ok {
		t.Error("expected mesh-jwks volume to be set when MeshJWTValidation.Enabled is true")
	}
	if _, ok := values["initContainers"]; !ok {
		t.Error("expected jwks-mirror initContainer to be set when MeshJWTValidation.Enabled is true")
	}
}

func TestBuildIstiodValues_MeshJWTValidationEnabledRejectsMissingIssuerOrJWKS(t *testing.T) {
	tests := []struct {
		name   string
		issuer string
		jwks   string
	}{
		{"missing issuer", "", `{"keys":[]}`},
		{"missing jwks", "https://host.example.com", ""},
		{"missing both", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &OrbitPlatform{
				Spec: OrbitPlatformSpec{
					TrustDomain: "orbit.test",
					MeshJWTValidation: MeshJWTValidationSpec{
						Enabled: true,
						Issuer:  tt.issuer,
						JWKS:    tt.jwks,
					},
				},
			}
			if _, err := buildIstiodValues(p, "opa-image", "info"); err == nil {
				t.Error("expected an error for an enabled rule with a missing issuer/jwks, not a silently unauthenticatable JWT_RULE")
			}
		})
	}
}

// ── buildGatewayValues ────────────────────────────────────────────────────
//
// docs/adr/2026-08-04-devenv-mode-platform-support.md, F-6.

func TestBuildGatewayValues_SpireTemplateAlwaysPresent(t *testing.T) {
	values := buildGatewayValues("LoadBalancer")
	annotations, ok := values["podAnnotations"].(map[string]any)
	if !ok {
		t.Fatal("expected podAnnotations")
	}
	templates, _ := annotations["inject.istio.io/templates"].(string)
	if !strings.Contains(templates, "spire") {
		t.Errorf("inject.istio.io/templates = %q, want it to include \"spire\" unconditionally", templates)
	}
	if values["service"].(map[string]any)["type"] != "LoadBalancer" {
		t.Errorf("service.type = %v, want %q", values["service"], "LoadBalancer")
	}
}

// ── buildOrbitPlatformSpiffeValues ────────────────────────────────────────
//
// docs/adr/2026-08-04-devenv-mode-platform-support.md, F-1 (values distinction).

func TestBuildOrbitPlatformSpiffeValues(t *testing.T) {
	tests := []struct {
		name             string
		spireEnabled     bool
		devEnvironmentID string
		wantEnabled      bool
		wantSpireEnabled bool
		wantDevEnvID     string
	}{
		{
			name:             "host install",
			spireEnabled:     true,
			devEnvironmentID: "",
			wantEnabled:      true,
			wantSpireEnabled: true,
			wantDevEnvID:     "",
		},
		{
			name:             "devenv install, SPIRE disabled",
			spireEnabled:     false,
			devEnvironmentID: "spike-1",
			wantEnabled:      true,
			wantSpireEnabled: false,
			wantDevEnvID:     "spike-1",
		},
		{
			name:             "neither SPIRE nor devenv",
			spireEnabled:     false,
			devEnvironmentID: "",
			wantEnabled:      false,
			wantSpireEnabled: false,
			wantDevEnvID:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &OrbitPlatform{
				Spec: OrbitPlatformSpec{
					SPIRE:            SPIRESpec{Enabled: tt.spireEnabled},
					DevEnvironmentID: tt.devEnvironmentID,
				},
			}
			values := buildOrbitPlatformSpiffeValues(p)
			if values["enabled"] != tt.wantEnabled {
				t.Errorf("enabled = %v, want %v", values["enabled"], tt.wantEnabled)
			}
			if values["spireEnabled"] != tt.wantSpireEnabled {
				t.Errorf("spireEnabled = %v, want %v", values["spireEnabled"], tt.wantSpireEnabled)
			}
			if values["devEnvID"] != tt.wantDevEnvID {
				t.Errorf("devEnvID = %v, want %q", values["devEnvID"], tt.wantDevEnvID)
			}
		})
	}
}

func TestBuildOrbitPlatformSpiffeValues_EnabledIsNotSpireEnabled(t *testing.T) {
	// The distinction this test pins: a devenv install's "enabled" (pin
	// custom SANs at all) and "spireEnabled" (SPIRE itself issues the
	// certs) must diverge — checking only the SPIRE-style path
	// unconditionally would leave the devenv-mode identity shape
	// unmatchable (see orbit.workloadPrincipals, charts/orbit/templates/_helpers.tpl).
	p := &OrbitPlatform{
		Spec: OrbitPlatformSpec{
			SPIRE:            SPIRESpec{Enabled: false},
			DevEnvironmentID: "spike-1",
		},
	}
	values := buildOrbitPlatformSpiffeValues(p)
	if values["enabled"] == values["spireEnabled"] {
		t.Errorf("enabled (%v) must differ from spireEnabled (%v) on a SPIRE-disabled devenv install", values["enabled"], values["spireEnabled"])
	}
}
