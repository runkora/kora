package operator

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials/adapters/apikey"
)

// apikeyRejectsHeader reports whether the apikey adapter refuses name as a
// credential header.
func apikeyRejectsHeader(t *testing.T, name string) bool {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"headers": []map[string]string{{"name": name}}})
	if err != nil {
		t.Fatal(err)
	}
	return apikey.ValidateConfig(raw) != nil
}

// TestAPIKeyLimitsMatchMCPServer keeps the apikey adapter's header limits and
// reserved header list in step with the MCPServer spec.auth.headers rules.
func TestAPIKeyLimitsMatchMCPServer(t *testing.T) {
	if apikey.MaxHeaders != maxMCPAuthHeaders {
		t.Errorf("apikey.MaxHeaders = %d, maxMCPAuthHeaders = %d", apikey.MaxHeaders, maxMCPAuthHeaders)
	}
	if apikey.MaxHeaderNameLength != maxHeaderNameLength {
		t.Errorf("apikey.MaxHeaderNameLength = %d, maxHeaderNameLength = %d", apikey.MaxHeaderNameLength, maxHeaderNameLength)
	}
	if apikey.MaxPrefixLength != maxPrefixLength {
		t.Errorf("apikey.MaxPrefixLength = %d, maxPrefixLength = %d", apikey.MaxPrefixLength, maxPrefixLength)
	}

	// Every name the operator reserves, including prefix matches, is refused.
	candidates := map[string]bool{}
	for name := range reservedHeaders {
		candidates[name] = true
	}
	for _, p := range reservedHeaderPrefixes {
		candidates[p+"for"] = true
	}
	// Names that are not reserved must stay accepted by both.
	for _, name := range []string{"authorization", "x-api-key", "x-custom-token", "private-token"} {
		candidates[name] = true
	}
	for name := range candidates {
		want := isReservedHeader(name)
		if got := apikeyRejectsHeader(t, name); got != want {
			t.Errorf("header %q: apikey rejects = %v, operator reserves = %v", name, got, want)
		}
		upper := strings.ToUpper(name)
		if got := apikeyRejectsHeader(t, upper); got != want {
			t.Errorf("header %q: apikey rejects = %v, operator reserves = %v", upper, got, want)
		}
	}

	// The limits themselves are enforced at the boundary.
	long := strings.Repeat("x", maxHeaderNameLength+1)
	if !apikeyRejectsHeader(t, long) {
		t.Errorf("apikey accepted a %d character header name", len(long))
	}
	tooMany := make([]map[string]string, 0, maxMCPAuthHeaders+1)
	for i := 0; i <= maxMCPAuthHeaders; i++ {
		tooMany = append(tooMany, map[string]string{"name": fmt.Sprintf("x-key-%d", i)})
	}
	raw, _ := json.Marshal(map[string]any{"headers": tooMany})
	if apikey.ValidateConfig(raw) == nil {
		t.Errorf("apikey accepted %d headers", len(tooMany))
	}
}
