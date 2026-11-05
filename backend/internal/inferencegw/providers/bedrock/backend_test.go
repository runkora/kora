package bedrock

import "testing"

// TestInferBackendID_MantleOpenAIV1Models verifies that every openai.gpt-* model ID except gpt-oss, and every xAI Grok model ID,
// is routed to backendMantleOpenAIV1 by both the cold-start heuristic (inferBackendID) and
// the warm-registry path (dispatchBackend).
func TestInferBackendID_MantleOpenAIV1Models(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id string
	}{
		{"openai.gpt-5.4"},
		{"openai.gpt-5.4-2026-03-05"},
		{"openai.gpt-5.5"},
		{"openai.gpt-5.5-2026-04-23"},
		{"openai.gpt-5.6-luna"},
		{"openai.gpt-5.6-sol"},
		{"openai.gpt-5.6-terra"},
		{"us.openai.gpt-5.6-terra"}, // prefixed form — stripRegionPrefix must normalise
		{"xai.grok-4.3"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := inferBackendID(tc.id)
			if got != backendMantleOpenAIV1 {
				t.Errorf("inferBackendID(%q) = %v, want backendMantleOpenAIV1 (%v)", tc.id, got, backendMantleOpenAIV1)
			}
		})
	}
}

// TestInferBackendID_MantleOSSModels verifies that openai.gpt-oss-* models are
// still routed to backendMantle (not backendMantleOpenAIV1) so the existing
// chat/completions path is not regressed.
func TestInferBackendID_MantleOSSModels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id string
	}{
		{"openai.gpt-oss-120b"},
		{"openai.gpt-oss-20b"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := inferBackendID(tc.id)
			if got != backendMantle {
				t.Errorf("inferBackendID(%q) = %v, want backendMantle (%v)", tc.id, got, backendMantle)
			}
		})
	}
}

// TestDispatchBackend_MantleOpenAIV1_WarmRegistry verifies that dispatchBackend reads
// backendMantleOpenAIV1 from a pre-populated registry (warm path).
func TestDispatchBackend_MantleOpenAIV1_WarmRegistry(t *testing.T) {
	t.Parallel()
	reg := map[string]backendID{
		"openai.gpt-5.6-terra": backendMantleOpenAIV1,
		"openai.gpt-oss-120b":  backendMantle,
	}
	c := &Connector{}
	c.modelRegistry.Store(&reg)

	cases := []struct {
		id   string
		want backendID
	}{
		{"openai.gpt-5.6-terra", backendMantleOpenAIV1},
		{"openai.gpt-oss-120b", backendMantle},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := c.dispatchBackend(tc.id)
			if got != tc.want {
				t.Errorf("dispatchBackend(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

// TestDispatchBackend_MantleOpenAIV1_ColdStart verifies that dispatchBackend falls back
// to inferBackendID (returning backendMantleOpenAIV1) when the registry is nil.
func TestDispatchBackend_MantleOpenAIV1_ColdStart(t *testing.T) {
	t.Parallel()
	c := &Connector{} // modelRegistry is nil — cold start

	cases := []struct {
		id   string
		want backendID
	}{
		{"openai.gpt-5.6-terra", backendMantleOpenAIV1},
		{"openai.gpt-5.4", backendMantleOpenAIV1},
		{"openai.gpt-oss-120b", backendMantle}, // must not regress
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := c.dispatchBackend(tc.id)
			if got != tc.want {
				t.Errorf("dispatchBackend(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

// TestDispatchBackend_MantleOpenAIV1_RegistryMiss_FallsBackToInfer verifies the case
// where the registry exists but does not contain the model (e.g. a new model
// added after the last ListModels sweep).  The fallback to inferBackendID
// must still route openai.gpt-* (except gpt-oss) and xAI Grok correctly.
func TestDispatchBackend_MantleOpenAIV1_RegistryMiss_FallsBackToInfer(t *testing.T) {
	t.Parallel()
	// Registry is non-nil but empty — every lookup misses and falls through.
	reg := map[string]backendID{}
	c := &Connector{}
	c.modelRegistry.Store(&reg)

	got := c.dispatchBackend("openai.gpt-5.6-luna")
	if got != backendMantleOpenAIV1 {
		t.Errorf("dispatchBackend(\"openai.gpt-5.6-luna\") with empty registry = %v, want backendMantleOpenAIV1", got)
	}
}

func TestMantleModelID(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"us.openai.gpt-6-sol":       "openai.gpt-6-sol",
		"global.openai.gpt-5.6-sol": "openai.gpt-5.6-sol",
		"openai.gpt-6.1-terra":      "openai.gpt-6.1-terra",
		"openai.gpt-oss-20b-1:0":    "openai.gpt-oss-20b",
		"openai.gpt-oss-120b":       "openai.gpt-oss-120b",
		"xai.grok-4.3":              "xai.grok-4.3",
	}
	for in, want := range cases {
		if got := mantleModelID(in); got != want {
			t.Errorf("mantleModelID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInferBackendID_MantleRules(t *testing.T) {
	t.Parallel()
	cases := map[string]backendID{
		"us.openai.gpt-6-sol":       backendMantleOpenAIV1,
		"global.openai.gpt-5.6-sol": backendMantleOpenAIV1,
		"openai.gpt-6.1-terra":      backendMantleOpenAIV1,
		"openai.gpt-oss-20b-1:0":    backendMantle,
	}
	for in, want := range cases {
		if got := inferBackendID(in); got != want {
			t.Errorf("inferBackendID(%q) = %v, want %v", in, got, want)
		}
	}
}
