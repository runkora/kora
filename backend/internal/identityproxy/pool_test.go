package identityproxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	ext_procv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func x509Context(id string) *workloadapi.X509Context {
	return &workloadapi.X509Context{SVIDs: []*x509svid.SVID{{ID: spiffeid.RequireFromString(id)}}}
}

type countingExchanger struct {
	calls     atomic.Int32
	exchanged chan struct{} // receives once per exchange when set
}

func (c *countingExchanger) Exchange(context.Context) (string, time.Time, error) {
	c.calls.Add(1)
	if c.exchanged != nil {
		c.exchanged <- struct{}{}
	}
	return "agent-token", time.Now().Add(5 * time.Minute), nil
}

const (
	poolID  = "spiffe://runkora.test/tenant/kora-tenant-a/pool/pool-abc12"
	agentID = "spiffe://runkora.test/tenant/kora-tenant-a/agent/reviewer"
	otherID = "spiffe://runkora.test/tenant/kora-tenant-a/agent/other"
)

// A pool pod makes no exchange on its pool SVID and exchanges once the bind
// delivers the agent SVID (docs/adr/2026-11-22-agent-cold-tier.md).
func TestSVIDWatcher_PoolSVIDWaitsForBind(t *testing.T) {
	exc := &countingExchanger{exchanged: make(chan struct{}, 1)}
	r := NewRefresher(NewTokenCache(), exc, discardLogger(), testConfig())
	watcher := r.OnSVIDRotation(t.Context())

	watcher.OnX509ContextUpdate(x509Context(poolID))
	if !r.awaitPool(t.Context(), time.Hour) {
		t.Fatal("awaitPool = false after a pool SVID")
	}
	if closedChan(r.svid.bound) {
		t.Fatal("bound before the agent SVID")
	}
	if n := exc.calls.Load(); n != 0 {
		t.Fatalf("exchanges on a pool SVID = %d, want 0", n)
	}

	watcher.OnX509ContextUpdate(x509Context(agentID))
	if !closedChan(r.svid.bound) {
		t.Fatal("not bound after the agent SVID")
	}
	select {
	case <-exc.exchanged:
	case <-t.Context().Done():
		t.Fatal("no exchange after the agent SVID")
	}
	if !r.awaitPool(t.Context(), time.Hour) {
		t.Error("awaitPool reports the first SVID, a pool SVID")
	}
}

// An agent pod's first SVID is not a pool SVID.
func TestAwaitPool_AgentSVID(t *testing.T) {
	r := NewRefresher(NewTokenCache(), &countingExchanger{}, discardLogger(), testConfig())
	r.svid.observe(x509Context(agentID))
	if r.awaitPool(t.Context(), time.Hour) {
		t.Error("awaitPool = true for an agent SVID")
	}
	if !closedChan(r.svid.bound) {
		t.Error("an agent SVID did not bind the pod")
	}
}

// With no SVID by the timeout, the pod takes the agent path.
func TestAwaitPool_NoSVID(t *testing.T) {
	r := NewRefresher(NewTokenCache(), &countingExchanger{}, discardLogger(), testConfig())
	if r.awaitPool(t.Context(), 0) {
		t.Error("awaitPool = true with no SVID")
	}
}

// A SPIFFE ID other than the bound one drops the token and signals a stop.
// A rotation of the same ID changes nothing (docs/adr/2026-11-22-agent-cold-tier.md).
func TestSVIDWatcher_IdentityChangeDropsTheToken(t *testing.T) {
	for name, next := range map[string]string{"another agent": otherID, "a pool ID": poolID} {
		t.Run(name, func(t *testing.T) {
			cache := NewTokenCache()
			r := NewRefresher(cache, &countingExchanger{exchanged: make(chan struct{}, 2)}, discardLogger(), testConfig())
			watcher := r.OnSVIDRotation(t.Context())
			watcher.OnX509ContextUpdate(x509Context(agentID))
			cache.Set("agent-token", time.Now().Add(5*time.Minute))

			watcher.OnX509ContextUpdate(x509Context(agentID))
			if closedChan(r.svid.changed) {
				t.Fatal("a rotation of the same ID counted as a change")
			}
			watcher.OnX509ContextUpdate(x509Context(next))
			if !closedChan(r.svid.changed) {
				t.Fatal("no change signalled for a new SPIFFE ID")
			}
			if _, _, ok := cache.Get(); ok {
				t.Error("the token survived the identity change")
			}
		})
	}
}

// servePool marks a pool pod SERVING, waits for the bind and opens the fixed
// retry window. An agent pod gets no window.
func TestServePool(t *testing.T) {
	newServer := func() (*Server, *Refresher) {
		cache := NewTokenCache()
		r := NewRefresher(cache, &countingExchanger{}, discardLogger(), testConfig())
		srv := NewServer(cache, nil, &countingExchanger{}, r, discardLogger(), testConfig())
		srv.health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		return srv, r
	}
	status := func(srv *Server) healthpb.HealthCheckResponse_ServingStatus {
		resp, err := srv.health.Check(t.Context(), &healthpb.HealthCheckRequest{})
		if err != nil {
			t.Fatal(err)
		}
		return resp.Status
	}

	srv, r := newServer()
	r.svid.observe(x509Context(poolID))
	r.svid.observe(x509Context(agentID))
	fixedUntil, err := srv.servePool(t.Context())
	if err != nil || fixedUntil.IsZero() {
		t.Fatalf("servePool on a bound pool pod = %v, %v; want a retry window", fixedUntil, err)
	}
	if got := status(srv); got != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("health = %v, want SERVING", got)
	}

	srv, r = newServer()
	r.svid.observe(x509Context(agentID))
	if fixedUntil, err = srv.servePool(t.Context()); err != nil || !fixedUntil.IsZero() {
		t.Errorf("servePool on an agent pod = %v, %v; want no window", fixedUntil, err)
	}
	if got := status(srv); got != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Errorf("agent pod health before its token = %v, want NOT_SERVING", got)
	}
}

func TestHandleTokenz(t *testing.T) {
	cache := NewTokenCache()
	r := NewRefresher(cache, &countingExchanger{}, discardLogger(), testConfig())
	srv := NewServer(cache, nil, &countingExchanger{}, r, discardLogger(), testConfig())
	get := func(path string) int {
		rec := httptest.NewRecorder()
		srv.healthMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}
	if code := get("/tokenz"); code != http.StatusServiceUnavailable {
		t.Errorf("/tokenz with no token = %d, want 503", code)
	}
	if code := get("/healthz"); code != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", code)
	}
	cache.Set("agent-token", time.Now().Add(5*time.Minute))
	if code := get("/tokenz"); code != http.StatusOK {
		t.Errorf("/tokenz with a token = %d, want 200", code)
	}
}

// After a bind the exchange retries at the fixed interval. With a 5s backoff
// the third attempt would miss the 3s deadline.
func TestServer_WaitForInitialToken_FixedRetryAfterBind(t *testing.T) {
	cache := NewTokenCache()
	exc := &stubExchanger{failN: 2, token: "agent-token"}
	cfg := testConfig()
	cfg.BackoffInitial = 5 * time.Second
	srv := NewServer(cache, nil, exc, NewRefresher(cache, exc, discardLogger(), cfg), discardLogger(), cfg)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := srv.waitForInitialToken(ctx, time.Now().Add(boundRetryWindow)); err != nil {
		t.Fatalf("waitForInitialToken: %v", err)
	}
	if n := exc.calls.Load(); n != 3 {
		t.Errorf("exchanges = %d, want 3", n)
	}
}

// An exchange that completes after the SPIFFE ID changed does not put its token back.
func TestRevokedCache_LateExchange(t *testing.T) {
	cache := NewTokenCache()
	exc := &gatedExchanger{entered: make(chan struct{}), release: make(chan struct{}), token: "agent-token"}
	r := NewRefresher(cache, exc, discardLogger(), testConfig())
	r.svid.observe(x509Context(agentID))
	done := make(chan struct{})
	go func() {
		r.exchangeWithBackoff(t.Context())
		close(done)
	}()
	<-exc.entered

	r.OnSVIDRotation(t.Context()).OnX509ContextUpdate(x509Context(otherID))
	close(exc.release)
	<-done
	if _, _, ok := cache.Get(); ok {
		t.Fatal("a token reached the cache after the identity change")
	}
	cache.Set("late-token", time.Now().Add(5*time.Minute))
	if _, _, ok := cache.Get(); ok {
		t.Error("Set filled a revoked cache")
	}
}

// An identity change stops the gRPC server with an ext_proc stream still open.
func TestServeUntilIdentityChange_StopsWithOpenStream(t *testing.T) {
	cache := NewTokenCache()
	r := NewRefresher(cache, &countingExchanger{}, discardLogger(), testConfig())
	srv := NewServer(cache, nil, &countingExchanger{}, r, discardLogger(), testConfig())
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcErr := make(chan error, 1)
	go func() { grpcErr <- srv.grpc.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stream, err := ext_procv3.NewExternalProcessorClient(conn).Process(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&ext_procv3.ProcessingRequest{Request: &ext_procv3.ProcessingRequest_RequestHeaders{RequestHeaders: &ext_procv3.HttpHeaders{}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}

	r.svid.observe(x509Context(agentID))
	r.svid.observe(x509Context(otherID))
	if err := srv.serveUntilIdentityChange(grpcErr); !errors.Is(err, errIdentityChanged) {
		t.Fatalf("serveUntilIdentityChange = %v, want errIdentityChanged", err)
	}
	if _, err := stream.Recv(); err == nil {
		t.Error("the open stream survived the stop")
	}
}
