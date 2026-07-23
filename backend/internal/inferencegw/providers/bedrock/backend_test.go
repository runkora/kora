package bedrock

import "testing"

// TestInferBackendID_GPT5Models verifies that every GPT-5.x model ID is routed
// to backendMantleGPT5 by both the cold-start heuristic (inferBackendID) and
// the warm-registry path (dispatchBackend).
func TestInferBackendID_GPT5Models(t *testing.T) {
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
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := inferBackendID(tc.id)
			if got != backendMantleGPT5 {
				t.Errorf("inferBackendID(%q) = %v, want backendMantleGPT5 (%v)", tc.id, got, backendMantleGPT5)
			}
		})
	}
}

// TestInferBackendID_MantleOSSModels verifies that openai.gpt-oss-* models are
// still routed to backendMantle (not backendMantleGPT5) so the existing
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

// TestDispatchBackend_GPT5_WarmRegistry verifies that dispatchBackend reads
// backendMantleGPT5 from a pre-populated registry (warm path).
func TestDispatchBackend_GPT5_WarmRegistry(t *testing.T) {
	t.Parallel()
	reg := map[string]backendID{
		"openai.gpt-5.6-terra": backendMantleGPT5,
		"openai.gpt-oss-120b":  backendMantle,
	}
	c := &Connector{}
	c.modelRegistry.Store(&reg)

	cases := []struct {
		id   string
		want backendID
	}{
		{"openai.gpt-5.6-terra", backendMantleGPT5},
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

// TestDispatchBackend_GPT5_ColdStart verifies that dispatchBackend falls back
// to inferBackendID (returning backendMantleGPT5) when the registry is nil.
func TestDispatchBackend_GPT5_ColdStart(t *testing.T) {
	t.Parallel()
	c := &Connector{} // modelRegistry is nil — cold start

	cases := []struct {
		id   string
		want backendID
	}{
		{"openai.gpt-5.6-terra", backendMantleGPT5},
		{"openai.gpt-5.4", backendMantleGPT5},
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

// TestDispatchBackend_GPT5_RegistryMiss_FallsBackToInfer verifies the case
// where the registry exists but does not contain the model (e.g. a new model
// added after the last ListModels sweep).  The fallback to inferBackendID
// must still route GPT-5.x correctly.
func TestDispatchBackend_GPT5_RegistryMiss_FallsBackToInfer(t *testing.T) {
	t.Parallel()
	// Registry is non-nil but empty — every lookup misses and falls through.
	reg := map[string]backendID{}
	c := &Connector{}
	c.modelRegistry.Store(&reg)

	got := c.dispatchBackend("openai.gpt-5.6-luna")
	if got != backendMantleGPT5 {
		t.Errorf("dispatchBackend(\"openai.gpt-5.6-luna\") with empty registry = %v, want backendMantleGPT5", got)
	}
}
