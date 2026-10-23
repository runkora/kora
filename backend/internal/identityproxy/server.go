// Package identityproxy implements an Envoy External Processing (ext_proc)
// gRPC server that injects a platform Bearer token into every request's
// Authorization header before it reaches the identity service.
//
// Architecture (docs/adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md):
//
//	Agent → Envoy sidecar ─gRPC ext_proc─► identity-proxy (this package)
//	                                              │
//	                                              ▼
//	                               TokenCache (in-memory, auto-refreshed)
//	                                              │
//	                    ◄──────────────── inject "Authorization: Bearer <token>"
//
// The server itself never validates SVIDs or inspects the XFCC header — that is
// Envoy's job. It only appends the cached platform JWT so downstream services can
// verify it without knowing about SPIRE.
package identityproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	ext_procv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/runkora/kora/backend/internal/platform/api"
	"go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// authorizationHeader is the canonical HTTP header name injected by the proxy.
const authorizationHeader = "authorization"

// Sentinel error descriptions used as static span status strings.
// These are intentionally non-dynamic so that raw network strings, internal
// hostnames, or context messages never appear in telemetry status fields
// (see docs/adr/2026-03-03-database-infrastructure-layer.md). Full error detail is captured via span.RecordError.
var (
	errBuildRequest        = errors.New("failed to build request")
	errEnvoyReadyTimeout   = errors.New("timed out waiting for Envoy to become ready")
	errContextCanceled     = errors.New("context canceled")
	errInitialTokenTimeout = errors.New("timed out waiting for initial token")
)

// istioReadyURL is the Istio pilot-agent health endpoint that returns 200
// only after Envoy has received all xDS configuration (listeners, clusters,
// certificates). We must wait for this before making any outbound calls,
// otherwise Envoy will RST connections because it has no mTLS client cert yet.
// Port 15021 is Istio's fixed health port — it is a platform constant, not an
// operator tunable.
const istioReadyURL = "http://localhost:15021/healthz/ready"

// Processor implements ExternalProcessorServer.
// It injects the cached platform JWT as an Authorization header on every
// request, and — when the request carries per-run identity — the run's
// delegation token as X-Kora-Delegation-Token
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 4). Any inbound
// delegation header is stripped unconditionally: the untrusted subprocess must
// never be able to inject one. The zero-value is not usable — use NewProcessor.
type Processor struct {
	ext_procv3.UnimplementedExternalProcessorServer

	cache       *TokenCache
	delegations *DelegationManager // nil disables delegation attachment (tests); stripping still applies
	collector   string             // telemetry collector authority; requests to it get no token
	logger      logging.Logger
}

// NewProcessor constructs a Processor backed by the given caches. delegations
// may be nil, in which case no delegation token is ever attached (inbound
// delegation headers are still stripped). collector is the telemetry
// collector's host:port, or empty when none is configured.
func NewProcessor(cache *TokenCache, delegations *DelegationManager, collector string, logger logging.Logger) *Processor {
	return &Processor{cache: cache, delegations: delegations, collector: collector, logger: logger}
}

// Process is the bidirectional streaming RPC called by Envoy for every HTTP
// request that passes through the ext_proc filter. We intercept request-header
// messages, inject the Bearer token (and the run's delegation token when
// applicable), and continue.
func (p *Processor) Process(stream ext_procv3.ExternalProcessor_ProcessServer) error {
	ctx := stream.Context()

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "recv error: %v", err)
		}

		switch msg := req.Request.(type) {
		case *ext_procv3.ProcessingRequest_RequestHeaders:
			resp := p.handleRequestHeaders(ctx, msg.RequestHeaders)
			if sendErr := stream.Send(resp); sendErr != nil {
				return status.Errorf(codes.Unknown, "send error: %v", sendErr)
			}

		default:
			// For all other message types (response headers, body, trailers)
			// respond with an empty continuation so Envoy keeps processing.
			if sendErr := stream.Send(&ext_procv3.ProcessingResponse{}); sendErr != nil {
				return status.Errorf(codes.Unknown, "send error: %v", sendErr)
			}
		}
	}
}

// headerValue extracts a header value from an ext_proc HttpHeaders message.
// Envoy populates RawValue; Value is the legacy string field.
func headerValue(headers *ext_procv3.HttpHeaders, key string) string {
	if headers == nil || headers.Headers == nil {
		return ""
	}
	for _, h := range headers.Headers.Headers {
		if h.GetKey() == key {
			if raw := h.GetRawValue(); len(raw) > 0 {
				return string(raw)
			}
			return h.GetValue()
		}
	}
	return ""
}

// handleRequestHeaders reads the cached tokens and returns a ProcessingResponse
// that injects the Authorization header — and, when the request carries run
// identity (the /run/{run_id} path prefix, or this pod's own sandbox SVID) and
// that run holds an active delegation, the X-Kora-Delegation-Token header —
// into the upstream request.
func (p *Processor) handleRequestHeaders(ctx context.Context, headers *ext_procv3.HttpHeaders) *ext_procv3.ProcessingResponse {
	// Telemetry export is platform infrastructure: the collector gets neither
	// the agent's token nor its delegation token.
	if p.collector != "" && strings.EqualFold(headerValue(headers, ":authority"), p.collector) {
		return continueWithout(authorizationHeader, delegationHeader)
	}

	token, _, ok := p.cache.Get()
	if !ok {
		p.logger.Info(ctx, "token cache empty — request will proceed without Authorization header")
		// Return a proper CONTINUE response with the RequestHeaders oneof set.
		// An empty ProcessingResponse{} (no oneof field) causes Envoy to log
		// "Spurious response message 0" and treat the ext_proc stream as
		// errored, returning 503 even when failureModeAllow=true. The exchange
		// endpoint authenticates via mTLS/XFCC — it does not need a Bearer
		// token — so CONTINUE while the cache is being populated on startup or
		// after rotation is correct. The inbound delegation header is still
		// stripped: the subprocess must never smuggle one through this window.
		return continueWithout(delegationHeader)
	}

	// Remove any pre-existing values first to guarantee exactly one value per
	// header. Without this, Envoy's default append behavior would produce two
	// Authorization headers if the upstream request already carries one; and
	// the delegation header is attacker-writable from inside the pod, so it is
	// stripped whether or not we attach our own.
	mutation := &ext_procv3.HeaderMutation{
		RemoveHeaders: []string{authorizationHeader, delegationHeader},
		SetHeaders: []*corev3.HeaderValueOption{
			{
				Header: &corev3.HeaderValue{
					Key:      authorizationHeader,
					RawValue: []byte("Bearer " + token),
				},
				AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			},
		},
	}

	if p.delegations != nil {
		if delegation, found := p.delegations.TokenForPath(ctx, headerValue(headers, ":path")); found {
			mutation.SetHeaders = append(mutation.SetHeaders, &corev3.HeaderValueOption{
				Header: &corev3.HeaderValue{
					Key:      delegationHeader,
					RawValue: []byte(delegation),
				},
				AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			})
		}
	}

	return &ext_procv3.ProcessingResponse{
		Response: &ext_procv3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &ext_procv3.HeadersResponse{
				Response: &ext_procv3.CommonResponse{
					HeaderMutation: mutation,
				},
			},
		},
	}
}

// continueWithout lets the request through with the given headers removed and
// nothing added.
func continueWithout(headers ...string) *ext_procv3.ProcessingResponse {
	return &ext_procv3.ProcessingResponse{
		Response: &ext_procv3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &ext_procv3.HeadersResponse{
				Response: &ext_procv3.CommonResponse{
					Status:         ext_procv3.CommonResponse_CONTINUE,
					HeaderMutation: &ext_procv3.HeaderMutation{RemoveHeaders: headers},
				},
			},
		},
	}
}

// tokenExchanger is the minimal interface consumed by Server and Refresher.
// *Exchanger satisfies it; tests inject a stub.
type tokenExchanger interface {
	Exchange(ctx context.Context) (token string, expiry time.Time, err error)
}

// Server wraps the gRPC server and manages its lifecycle.
type Server struct {
	grpc       *grpc.Server
	health     *health.Server
	cache      *TokenCache
	exchanger  tokenExchanger
	refresher  *Refresher
	logger     logging.Logger
	tracer     trace.Tracer
	cfg        Config
	envoyReady chan struct{} // closed after waitForEnvoy succeeds; gates Refresher exchange calls
}

// NewServer constructs a Server wired with the given components.
//
// It creates an envoyReady channel and injects it into the Refresher so that
// any SVID-rotation callbacks fired before Envoy is ready will block instead
// of racing ahead and hitting connection refused on the exchange endpoint.
//
// delegations may be nil (tests): the processor then never attaches a
// delegation token, but still strips inbound delegation headers.
func NewServer(
	cache *TokenCache,
	delegations *DelegationManager,
	exchanger tokenExchanger,
	refresher *Refresher,
	logger logging.Logger,
	cfg Config,
) *Server {
	processor := NewProcessor(cache, delegations, cfg.CollectorAuthority, logger)

	grpcSrv := grpc.NewServer()
	healthSrv := health.NewServer()

	ext_procv3.RegisterExternalProcessorServer(grpcSrv, processor)
	healthpb.RegisterHealthServer(grpcSrv, healthSrv)

	envoyReady := make(chan struct{})
	refresher.envoyReady = envoyReady // same package — direct field access

	return &Server{
		grpc:       grpcSrv,
		health:     healthSrv,
		cache:      cache,
		exchanger:  exchanger,
		refresher:  refresher,
		logger:     logger,
		tracer:     otel.Tracer("orbit.identity-proxy"),
		cfg:        cfg,
		envoyReady: envoyReady,
	}
}

// Start performs the startup sequence:
//
//  1. Starts the HTTP health server on port 9090 immediately. It always returns
//     200 so Istio's PostStart hook succeeds and iptables intercept is configured
//     before we attempt the exchange call. Istio probes this via
//     /app-health/identity-proxy/readyz → localhost:9090/healthz.
//
//  2. Binds the Unix domain socket and starts the gRPC ext_proc server in
//     the background. The socket must exist before step 3 so the
//     identity-proxy's own outbound exchange call can pass through the
//     EnvoyFilter without "connection refused" (circular startup dependency).
//
//  3. Waits for Envoy to become ready by polling localhost:15021/healthz/ready.
//     Envoy serves this only after it has received xDS configuration including
//     its mTLS client certificate from SPIRE. Without this step, any outbound
//     HTTP call would be RST by Envoy because it has no cert to present.
//
//  4. Obtains the initial platform token (exponential backoff up to ReadyTimeout).
//
//  5. Marks gRPC health SERVING.
//
//  6. Starts the background token-refresh goroutine.
//
// Start blocks until ctx is canceled or a fatal error occurs.
func (s *Server) Start(ctx context.Context) error {
	// Step 1: HTTP health server — must start before everything else so Istio's
	// readiness probe succeeds while the gRPC socket and token are being set up.
	httpSrv := &http.Server{
		Addr:              s.cfg.HealthAddr,
		Handler:           http.HandlerFunc(s.handleHealthz),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Warn(ctx, "health HTTP server error", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	go func() { //nolint:gosec // G118: context.Background intentional — ctx is already canceled here; we need a fresh deadline to drain in-flight health requests.
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()
	s.logger.Info(ctx, "identity-proxy health server listening", logging.Field{Key: "addr", Value: s.cfg.HealthAddr})

	// Step 2: bind the UDS socket and start the gRPC server.
	// The socket must exist before the exchange call (step 3) because the
	// EnvoyFilter on this pod intercepts the identity-proxy's own outbound HTTP
	// via iptables and connects to this socket. Without the socket, Envoy returns
	// "connection refused" which surfaces as a TCP error on the exchange call.
	// Derive the socket directory from cfg.SocketPath so this stays in sync
	// if the configured path ever changes (e.g. in tests or alternate deploys).
	socketDir := filepath.Dir(s.cfg.SocketPath)
	// 0o755: the directory must be world-traversable so that istio-proxy
	// (UID 1337) can reach the socket file inside it. 0o750 would deny
	// access to any process outside the socket owner's group.
	if err := os.MkdirAll(socketDir, 0o755); err != nil { //nolint:gosec // G301: world-traversable required; Envoy (UID 1337) must enter the dir to connect to the socket.
		return fmt.Errorf("create socket directory: %w", err)
	}
	_ = os.Remove(s.cfg.SocketPath)

	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.SocketPath, err)
	}
	// net.Listen applies the process umask which typically strips world-write,
	// leaving the socket at 0o644 or 0o600. Envoy (UID 1337) runs as a
	// different user, so we must chmod the socket to 0o666 explicitly to
	// allow it to connect.
	if err := os.Chmod(s.cfg.SocketPath, 0o666); err != nil {
		return fmt.Errorf("chmod socket %s: %w", s.cfg.SocketPath, err)
	}

	grpcErr := make(chan error, 1)
	go func() {
		s.logger.Info(ctx, "identity-proxy gRPC server listening",
			logging.Field{Key: "socket", Value: s.cfg.SocketPath},
		)
		grpcErr <- s.grpc.Serve(lis)
	}()

	go func() {
		<-ctx.Done()
		s.health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		s.grpc.GracefulStop()
	}()

	// Step 3: wait for Envoy to be fully configured before making any outbound
	// calls. Envoy returns 200 on localhost:15021/healthz/ready only after it
	// has received all xDS configuration (listeners, clusters, and its SPIRE-
	// issued mTLS certificate). Without this, the exchange call to kora-identity
	// is RST by Envoy because it has no client cert to present for mTLS.
	if err := s.waitForEnvoy(ctx); err != nil {
		s.grpc.GracefulStop()
		return fmt.Errorf("waiting for Envoy: %w", err)
	}
	// Signal the Refresher that Envoy is ready. Any SVID-rotation goroutines
	// that fired before this point are now unblocked and will proceed with
	// the exchange call.
	close(s.envoyReady)

	// Step 4: obtain the initial token. The Processor returns an empty CONTINUE
	// for any ext_proc calls arriving before the cache is warm, so the window
	// where requests go without an Authorization header is brief.
	if err := s.waitForInitialToken(ctx); err != nil {
		s.grpc.GracefulStop()
		return fmt.Errorf("initial token acquisition failed: %w", err)
	}

	// Step 5: mark ready.
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	s.logger.Info(ctx, "identity-proxy ready — serving traffic")

	// Step 6: background token refresh.
	go s.refresher.Run(ctx)

	return <-grpcErr
}

// waitForEnvoy polls localhost:15021/healthz/ready until Envoy signals it is
// fully initialized (xDS received, certs loaded) or the context is canceled.
// This prevents the exchange call from being RST by an un-initialized Envoy.
func (s *Server) waitForEnvoy(ctx context.Context) error {
	ctx, span := s.tracer.Start(ctx, "identityproxy.WaitForEnvoy",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(s.cfg.EnvoyReadyTimeout)

	s.logger.Info(ctx, "waiting for Envoy to become ready")
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, istioReadyURL, nil)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(otelcodes.Error, errBuildRequest.Error())
			return fmt.Errorf("build request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				span.SetStatus(otelcodes.Ok, "")
				s.logger.Info(ctx, "Envoy ready")
				return nil
			}
		}

		if time.Now().After(deadline) {
			err = fmt.Errorf("timed out after %s waiting for Envoy to become ready", s.cfg.EnvoyReadyTimeout)
			span.RecordError(err)
			span.SetStatus(otelcodes.Error, errEnvoyReadyTimeout.Error())
			return err
		}

		select {
		case <-ctx.Done():
			span.RecordError(ctx.Err())
			span.SetStatus(otelcodes.Error, errContextCanceled.Error())
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

// handleHealthz serves the HTTP health endpoint on port 9090.
// Always returns 200 — the process is alive and the ext_proc gRPC server is
// listening. Istio's PostStart hook probes this to determine when it is safe
// to configure iptables intercept. If we returned 503 until the first token
// is cached, Envoy would never finish its PostStart hook and the identity-proxy
// could never reach kora-identity to exchange the token (circular deadlock).
// The ext_proc Processor already handles an empty cache gracefully by passing
// requests through without an Authorization header.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	api.JSON(r.Context(), w, s.logger, http.StatusOK, map[string]string{"status": "ok"})
}

// waitForInitialToken blocks until the exchange succeeds or ReadyTimeout is
// exceeded. It uses the same exponential-backoff logic as Refresher so
// transient failures at startup (e.g. identity service not yet ready) are
// handled gracefully.
func (s *Server) waitForInitialToken(ctx context.Context) error {
	ctx, span := s.tracer.Start(ctx, "identityproxy.WaitForInitialToken",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	deadline := time.Now().Add(s.cfg.ReadyTimeout)
	backoff := s.cfg.BackoffInitial

	for {
		token, expiry, err := s.exchanger.Exchange(ctx)
		if err == nil {
			s.cache.Set(token, expiry)
			span.SetStatus(otelcodes.Ok, "")
			s.logger.Info(ctx, "initial token cached")
			return nil
		}

		span.RecordError(err)

		if time.Now().After(deadline) {
			err = fmt.Errorf("timed out after %s waiting for first token: %w", s.cfg.ReadyTimeout, err)
			span.SetStatus(otelcodes.Error, errInitialTokenTimeout.Error())
			return err
		}

		s.logger.Warn(ctx, "initial exchange failed, retrying",
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "backoff_ms", Value: backoff.Milliseconds()},
		)

		select {
		case <-ctx.Done():
			span.SetStatus(otelcodes.Error, errContextCanceled.Error())
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > s.cfg.BackoffMax {
			backoff = s.cfg.BackoffMax
		}
	}
}
