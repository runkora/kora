package toolgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
)

// TestPublisher_NeverPrunesAnIncompleteKeepList pins the invariant the prune
// guard exists for, against the interleaving the KeepRegistered heartbeat
// introduced: Publish captures the snapshots, Sync keeps running (it does not
// take Publisher.mu), and a prune decision read from live state could pair an
// incomplete keep-list with a state that has since become complete. The prune
// is destructive, and a later publish restores the vocabulary, not the grants
// and roles it was stripped from.
//
// The assertion is one-sided, so it cannot flake: every request that asks to
// prune must carry a complete keep-list. Run under -race.
func TestPublisher_NeverPrunesAnIncompleteKeepList(t *testing.T) {
	a := Server{TenantID: "", Name: "a", ReintrospectIntervalSeconds: 60}
	b := Server{TenantID: "", Name: "b", ReintrospectIntervalSeconds: 60}

	var mu sync.Mutex
	var bad int
	policy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Scopes    []routescopes.ScopeDeclaration `json:"scopes"`
			SkipPrune bool                           `json:"skip_prune"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.SkipPrune {
			var seenB bool
			for _, s := range body.Scopes {
				if strings.Contains(s.Scope, ":b:") {
					seenB = true
				}
			}
			if !seenB {
				mu.Lock()
				bad++
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(policy.Close)

	ctx := context.Background()
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())
	i.recordResult(ctx, a, serverKey("", "a"), []Tool{{Name: "t"}}, nil)

	token := routescopes.TokenSource(func(context.Context) (string, error) { return "t", nil })
	reg := &Registry{servers: []Server{a, b}, loaded: true}
	p := NewPublisher(reg, i, routescopes.NewClient(policy.URL, token, testLogger()), testLogger())

	// b flips between unreached and reached while publishes run, so a publish
	// that read live state would eventually see it complete after capturing a
	// keep-list that omits it.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			i.recordResult(ctx, b, serverKey("", "b"), []Tool{{Name: "t"}}, nil)
			i.mu.Lock()
			delete(i.snapshots, serverKey("", "b"))
			i.mu.Unlock()
		}
	})
	for range 200 {
		if err := p.Publish(ctx); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	close(stop)
	wg.Wait()

	if bad > 0 {
		t.Errorf("%d publishes asked to prune with b missing from the keep-list", bad)
	}
}
