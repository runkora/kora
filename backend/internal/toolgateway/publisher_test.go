package toolgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
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
	var bad, empty, overlaps, inFlight int
	policy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > 1 {
			overlaps++
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			inFlight--
			mu.Unlock()
		}()

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
			mu.Lock()
			if !seenB {
				bad++
			}
			if len(body.Scopes) == 0 {
				empty++
			}
			mu.Unlock()
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
			runtime.Gosched() // never starve the publishes on one CPU
		}
	})
	// The registry mounts and unmounts underneath them too. An empty server
	// list reads as "nothing to keep", so pairing one with a loaded registry
	// would prune the whole vocabulary.
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			reg.mu.Lock()
			reg.servers, reg.loaded = nil, false
			reg.mu.Unlock()
			reg.mu.Lock()
			reg.servers, reg.loaded = []Server{a, b}, true
			reg.mu.Unlock()
			runtime.Gosched()
		}
	})
	// Concurrent, because the heartbeat and the cycle both call Publish. That
	// also exercises the serialization the destructive prune needs: replicas=1
	// rules out a second process, not a second goroutine.
	var pubs sync.WaitGroup
	for range 8 {
		pubs.Go(func() {
			for range 25 {
				if err := p.Publish(ctx); err != nil {
					t.Errorf("Publish: %v", err)
					return
				}
			}
		})
	}
	pubs.Wait()
	close(stop)
	wg.Wait()

	if bad > 0 {
		t.Errorf("%d publishes asked to prune with b missing from the keep-list", bad)
	}
	if empty > 0 {
		t.Errorf("%d publishes asked to prune with an empty keep-list; that strips every mcp scope", empty)
	}
	if overlaps > 0 {
		t.Errorf("%d registrations overlapped at the policy service; the prune is destructive", overlaps)
	}
}
