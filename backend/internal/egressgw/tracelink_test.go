package egressgw

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/runkora/kora/backend/internal/egressgw/tlsca/testtlsca"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	testTraceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	testTraceID     = "4bf92f3577b34da6a3ce929d0e0e4736"
	testCaller      = "spiffe://runkora.test/tenant/kora-tenant-acme/agent/coder"
)

var (
	recorderOnce sync.Once
	recorder     *tracetest.SpanRecorder
)

// spanRecorder installs one recording tracer provider for the package and clears it.
func spanRecorder() *tracetest.SpanRecorder {
	recorderOnce.Do(func() {
		recorder = tracetest.NewSpanRecorder()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	})
	recorder.Reset()
	return recorder
}

// stubRunTraces answers RunTraceParent from a fixed result and counts calls.
type stubRunTraces struct {
	mu          sync.Mutex
	traceParent string
	err         error
	calls       int
}

func (s *stubRunTraces) RunTraceParent(context.Context, string, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.traceParent, s.err
}

func (s *stubRunTraces) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubRunTraces) set(traceParent string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traceParent, s.err = traceParent, err
}

func newTestLinker(src runTraceParents) *TraceLinker {
	token := func(context.Context) (string, error) { return "tok", nil }
	return NewTraceLinker(src, token, logging.NewLoggerWithOutput(io.Discard))
}

// endedSpan waits for the ended span named name and returns it.
func endedSpan(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range rec.Ended() {
			if s.Name() == name {
				return s
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no ended span named %q", name)
	return nil
}

// assertLinks checks the span is a root of its own trace and links to the run trace only when want.
func assertLinks(t *testing.T, s sdktrace.ReadOnlySpan, want bool) {
	t.Helper()
	if s.Parent().IsValid() {
		t.Errorf("span has parent %v, want a root", s.Parent())
	}
	if s.SpanContext().TraceID().String() == testTraceID {
		t.Error("span joined the run trace, want its own trace")
	}
	links := s.Links()
	switch {
	case want && (len(links) != 1 || links[0].SpanContext.TraceID().String() != testTraceID):
		t.Errorf("links = %v, want one link to trace %s", links, testTraceID)
	case !want && len(links) != 0:
		t.Errorf("links = %v, want none", links)
	}
}

func TestTraceLinker_Links(t *testing.T) {
	ctx := context.Background()

	t.Run("run with a traceparent links to it", func(t *testing.T) {
		links := newTestLinker(&stubRunTraces{traceParent: testTraceParent}).Links(ctx, "run-1")
		if len(links) != 1 || links[0].SpanContext.TraceID().String() != testTraceID {
			t.Fatalf("links = %v, want one link to trace %s", links, testTraceID)
		}
	})

	t.Run("no traceparent, malformed traceparent, lookup failure and nil linker link nothing", func(t *testing.T) {
		for name, l := range map[string]*TraceLinker{
			"none":      newTestLinker(&stubRunTraces{}),
			"malformed": newTestLinker(&stubRunTraces{traceParent: "garbage"}),
			"failure":   newTestLinker(&stubRunTraces{err: errors.New("agents down")}),
			"nil":       nil,
		} {
			if links := l.Links(ctx, "run-1"); len(links) != 0 {
				t.Errorf("%s: links = %v, want none", name, links)
			}
		}
	})

	t.Run("token failure links nothing and skips the lookup", func(t *testing.T) {
		src := &stubRunTraces{traceParent: testTraceParent}
		l := NewTraceLinker(src, func(context.Context) (string, error) { return "", errors.New("no token") },
			logging.NewLoggerWithOutput(io.Discard))
		if links := l.Links(ctx, "run-1"); len(links) != 0 {
			t.Errorf("links = %v, want none", links)
		}
		if src.callCount() != 0 {
			t.Errorf("lookups = %d, want 0 without a token", src.callCount())
		}
	})

	t.Run("cache hit avoids a second lookup", func(t *testing.T) {
		src := &stubRunTraces{traceParent: testTraceParent}
		l := newTestLinker(src)
		l.Links(ctx, "run-1")
		l.Links(ctx, "run-1")
		if src.callCount() != 1 {
			t.Errorf("lookups = %d, want 1", src.callCount())
		}
		l.Links(ctx, "run-2")
		if src.callCount() != 2 {
			t.Errorf("lookups = %d after a second run, want 2", src.callCount())
		}
	})

	t.Run("a failure is held briefly, then looked up again", func(t *testing.T) {
		src := &stubRunTraces{err: errors.New("agents down")}
		l := newTestLinker(src)
		now := time.Now()
		l.now = func() time.Time { return now }
		l.Links(ctx, "run-1")
		now = now.Add(traceLinkFailureTTL / 2)
		l.Links(ctx, "run-1")
		if src.callCount() != 1 {
			t.Fatalf("lookups = %d inside the failure TTL, want 1", src.callCount())
		}
		src.set(testTraceParent, nil)
		now = now.Add(traceLinkFailureTTL)
		if links := l.Links(ctx, "run-1"); len(links) != 1 {
			t.Errorf("links = %v after the failure TTL, want a fresh lookup to link", links)
		}
	})

	t.Run("cache is bounded", func(t *testing.T) {
		l := newTestLinker(&stubRunTraces{traceParent: testTraceParent})
		for i := range traceLinkEntries + 10 {
			l.Links(ctx, "run-"+strconv.Itoa(i))
		}
		if n := len(l.entries); n > traceLinkEntries {
			t.Errorf("entries = %d, want at most %d", n, traceLinkEntries)
		}
	})
}

func TestConnect_RelaySpanLinksToRunTrace(t *testing.T) {
	// A loopback target is refused after the span starts, which keeps the test off the network.
	cases := []struct {
		name string
		src  *stubRunTraces
		want bool
	}{
		{"traceparent", &stubRunTraces{traceParent: testTraceParent}, true},
		{"lookup failure", &stubRunTraces{err: errors.New("agents down")}, false},
		{"no traceparent", &stubRunTraces{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := spanRecorder()
			h := NewConnectHandler(nil, nil, nil, logging.NewLoggerWithOutput(io.Discard)).WithTraceLinker(newTestLinker(c.src))
			req := httptest.NewRequest(http.MethodConnect, "127.0.0.1:5432", nil)
			req.Host = "127.0.0.1:5432"
			w := httptest.NewRecorder()
			authInjector(claimsFor("acme", testCaller, "run-9"), h).ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d regardless of the lookup", w.Code, http.StatusForbidden)
			}
			assertLinks(t, endedSpan(t, rec, "egressgw.ConnectHandler.serveRelay"), c.want)
		})
	}
}

func TestConnect_InterceptedSpanLinksToRunTrace(t *testing.T) {
	cases := []struct {
		name string
		src  *stubRunTraces
		want bool
	}{
		{"traceparent", &stubRunTraces{traceParent: testTraceParent}, true},
		{"lookup failure", &stubRunTraces{err: errors.New("agents down")}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := spanRecorder()
			ca, rootPEM, err := testtlsca.NewCA("acme")
			if err != nil {
				t.Fatalf("testtlsca.NewCA: %v", err)
			}
			inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
			h := NewConnectHandler(&fixedCABundle{tenant: "acme", ca: ca}, inner, nil, logging.NewLoggerWithOutput(io.Discard)).
				WithTraceLinker(newTestLinker(c.src))
			gateway := httptest.NewServer(authInjector(claimsFor("acme", testCaller, "run-9"), h))
			defer gateway.Close()

			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(rootPEM)
			gatewayURL, _ := url.Parse(gateway.URL)
			client := &http.Client{
				Transport: &http.Transport{
					Proxy:           http.ProxyURL(gatewayURL),
					TLSClientConfig: &tls.Config{RootCAs: pool},
					DialContext:     (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				},
				Timeout: 10 * time.Second,
			}
			resp, err := client.Get("https://example.test/")
			if err != nil {
				t.Fatalf("client.Get: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Errorf("status = %d, want %d regardless of the lookup", resp.StatusCode, http.StatusNoContent)
			}
			assertLinks(t, endedSpan(t, rec, http.MethodConnect), c.want)
		})
	}
}
