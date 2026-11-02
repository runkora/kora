package toolgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
)

// TestPublisher_PublishIsSingleWriter pins the invariant the replicas=1
// topology does not cover. The prune is destructive, so scope publication is
// single-writer — and this process now has two callers: the introspection cycle
// when it changed something, and KeepRegistered on its heartbeat. Run under
// -race.
func TestPublisher_PublishIsSingleWriter(t *testing.T) {
	var mu sync.Mutex
	inFlight, overlaps := 0, 0

	policy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(policy.Close)

	ctx := context.Background()
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())
	srv := Server{TenantID: "", Name: "s", ReintrospectIntervalSeconds: 60}
	i.recordResult(ctx, srv, serverKey("", "s"), []Tool{{Name: "echo"}}, nil)

	token := routescopes.TokenSource(func(context.Context) (string, error) { return "t", nil })
	reg := &Registry{servers: []Server{srv}, loaded: true}
	p := NewPublisher(reg, i, routescopes.NewClient(policy.URL, token, testLogger()), testLogger())

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := p.Publish(ctx); err != nil {
				t.Errorf("Publish: %v", err)
			}
		})
	}
	wg.Wait()

	if overlaps > 0 {
		t.Errorf("%d concurrent registrations reached the policy service; the prune is destructive", overlaps)
	}
}
