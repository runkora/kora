package bundle_test

import (
	"fmt"
	"testing"

	"github.com/runkora/kora/backend/internal/policyservice/grants"
)

// BenchmarkLayer2Decision measures one Layer 2 decision at a range of tenant grant counts.
func BenchmarkLayer2Decision(b *testing.B) {
	for _, n := range []int{10, 100, 1000, 5000} {
		b.Run(fmt.Sprintf("grants=%d", n), func(b *testing.B) {
			gs := []*grants.Grant{humanGrant("user-1", "trigger:fire")}
			for i := range n {
				gs = append(gs, agentGrant(fmt.Sprintf("agent-%d", i), "agents:read", fmt.Sprintf("connection:c-%d:use", i)))
			}
			pq := compileAndPrepare(b, bundleOpts{grants: gs})
			claims := humanClaims("user-1")
			if allow, reason := evalLayer2(b, pq, claims, nil, "trigger", []string{"trg-1"}, "fire"); !allow {
				b.Fatalf("decision = deny (%s), want allow", reason)
			}
			b.ResetTimer()
			for range b.N {
				evalLayer2(b, pq, claims, nil, "trigger", []string{"trg-1"}, "fire")
			}
		})
	}
}
