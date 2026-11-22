package identityproxy

import (
	"context"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// Refresher manages proactive token renewal.
//
// It runs two concurrent strategies:
//  1. A background poll goroutine that wakes every Config.PollInterval and
//     re-exchanges if the token is within Config.RefreshThreshold of expiry.
//  2. A go-spiffe X509Context callback that fires on every SVID rotation,
//     triggering a re-exchange that atomically replaces the cached token.
//
// Both strategies write to the shared TokenCache via the tokenExchanger.
//
// Neither strategy will make any exchange calls until envoyReady is closed.
// The Server sets this field before calling Start, and closes it after
// waitForEnvoy succeeds. This prevents exchange attempts before Envoy has
// received its mTLS configuration from SPIRE (which would result in
// connection refused on the outbound exchange call).
type Refresher struct {
	cache      *TokenCache
	exchanger  tokenExchanger
	logger     logging.Logger
	tracer     trace.Tracer
	cfg        Config
	envoyReady <-chan struct{} // closed by Server once Envoy is ready
	svid       svidState
}

// svidState records the pod's SVIDs. A pool pod holds a pool SVID until
// kora-agents binds it to an agent (docs/adr/2026-11-22-agent-cold-tier.md).
type svidState struct {
	once      sync.Once
	boundOnce sync.Once
	seen      chan struct{} // closed on the first SVID
	bound     chan struct{} // closed on the first SVID that is not a pool SVID
	mu        sync.Mutex
	pool      bool // the first SVID was a pool SVID
}

// observe records an SVID and reports whether it is a pool SVID.
func (s *svidState) observe(c *workloadapi.X509Context) bool {
	pool := false
	if c != nil && len(c.SVIDs) > 0 {
		_, pool = token.PoolNamespace(c.DefaultSVID().ID.String())
	}
	s.once.Do(func() {
		s.mu.Lock()
		s.pool = pool
		s.mu.Unlock()
		close(s.seen)
	})
	if !pool {
		s.boundOnce.Do(func() { close(s.bound) })
	}
	return pool
}

// NewRefresher constructs a Refresher. The envoyReady gate is injected by
// NewServer via the package-internal envoyReady field; tests that do not need
// the gate can leave it nil (exchangeWithBackoff checks for nil before blocking).
func NewRefresher(cache *TokenCache, exchanger tokenExchanger, logger logging.Logger, cfg Config) *Refresher {
	return &Refresher{
		cache:     cache,
		exchanger: exchanger,
		logger:    logger,
		tracer:    otel.Tracer("github.com/runkora/kora/backend/internal/identityproxy"),
		cfg:       cfg,
		svid:      svidState{seen: make(chan struct{}), bound: make(chan struct{})},
	}
}

// awaitPool waits up to timeout for the first SVID and reports whether it is a
// pool SVID. With no SVID by then it reports false, the agent pod's path.
func (r *Refresher) awaitPool(ctx context.Context, timeout time.Duration) bool {
	select {
	case <-r.svid.seen:
	case <-time.After(timeout):
		return false
	case <-ctx.Done():
		return false
	}
	r.svid.mu.Lock()
	defer r.svid.mu.Unlock()
	return r.svid.pool
}

// awaitBind waits for the first SVID that is not a pool SVID.
func (r *Refresher) awaitBind(ctx context.Context) error {
	select {
	case <-r.svid.bound:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run starts the background poll goroutine. It blocks until ctx is canceled.
// Call this in its own goroutine after the initial token has been cached.
func (r *Refresher) Run(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info(ctx, "refresh goroutine stopping")
			return
		case <-ticker.C:
			if r.cache.IsExpiringSoon(r.cfg.RefreshThreshold) {
				r.logger.Info(ctx, "token expiring soon, proactively re-exchanging")
				r.exchangeWithBackoff(ctx)
			}
		}
	}
}

// OnSVIDRotation returns a go-spiffe X509ContextWatcher that triggers a
// re-exchange whenever the SPIRE Agent delivers a new SVID. The watcher's
// callbacks must return promptly — heavy work is done in a new goroutine so the
// go-spiffe callback goroutine is not blocked.
func (r *Refresher) OnSVIDRotation(ctx context.Context) workloadapi.X509ContextWatcher {
	return &svidWatcher{refresher: r, ctx: ctx}
}

// exchangeWithBackoff performs an exchange call with exponential backoff.
//
// If envoyReady is non-nil (set by Server.NewServer), it blocks until that
// channel is closed before making the first exchange call. This prevents
// SVID-rotation callbacks that fire during Envoy's startup sequence from
// racing ahead of waitForEnvoy and hitting connection refused.
//
// It stops retrying only when the call succeeds or ctx is canceled.
func (r *Refresher) exchangeWithBackoff(ctx context.Context) {
	ctx, span := r.tracer.Start(ctx, "identityproxy.Refresh",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	// Wait for Envoy to be ready before making any outbound calls.
	// The channel is closed by Server after waitForEnvoy() returns nil.
	// Tests leave envoyReady nil, so the select is skipped entirely.
	if r.envoyReady != nil {
		select {
		case <-r.envoyReady:
			// Envoy is ready — proceed.
		case <-ctx.Done():
			observability.RecordFailure(span, errSpanContextCanceledWaitingForEnvoy, errSpanContextCanceledWaitingForEnvoy)
			return
		}
	}

	attempt := 0
	backoff := r.cfg.BackoffInitial
	for {
		attempt++
		token, expiry, err := r.exchanger.Exchange(ctx)
		if err == nil {
			r.cache.Set(token, expiry)
			span.SetAttributes(observability.TokenRefreshAttemptsKey.Int(attempt))
			span.SetStatus(codes.Ok, "")
			r.logger.Info(ctx, "token cache refreshed via background goroutine")
			return
		}

		span.RecordError(err)
		r.logger.Warn(ctx, "exchange failed, retrying",
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "backoff_ms", Value: backoff.Milliseconds()},
		)

		select {
		case <-ctx.Done():
			span.SetAttributes(observability.TokenRefreshAttemptsKey.Int(attempt))
			observability.RecordFailure(span, errSpanContextCanceledDuringRetry, errSpanContextCanceledDuringRetry)
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > r.cfg.BackoffMax {
			backoff = r.cfg.BackoffMax
		}
	}
}

// svidWatcher implements workloadapi.X509ContextWatcher so we can register it
// with the go-spiffe WatchX509Context call at startup.
type svidWatcher struct {
	refresher *Refresher
	ctx       context.Context //nolint:containedctx // held for goroutine lifetime
}

// OnX509ContextUpdate is called by go-spiffe whenever a new SVID arrives.
// We re-exchange in the background so the callback returns promptly; the new
// token atomically replaces the cached one via cache.Set when it arrives.
//
// We deliberately do NOT invalidate the cache first. SVID rotation is periodic
// key rotation of the same workload identity — not an identity change or
// revocation — and the platform JWT carries its own independent expiry, so the
// currently cached token stays valid across a rotation (and if it has in fact
// expired, cache.Get already reports the cache empty). Clearing it eagerly opened
// a brief window where the cache was empty; handleRequestHeaders then forwarded
// requests with NO Authorization header (ext_proc is failure_mode_allow), OPA
// fail-closed denied them for lack of an egress scope, and the sandbox Envoy
// reset the connection — surfacing to agents as intermittent "Connection reset
// by peer" on git clone and other egress. Keep the valid token until the swap.
//
// A pool SVID gets no exchange: kora-identity refuses the pool ID, and a pool
// pod holds no token until the bind.
func (w *svidWatcher) OnX509ContextUpdate(c *workloadapi.X509Context) {
	if w.refresher.svid.observe(c) {
		w.refresher.logger.Info(w.ctx, "pool SVID received, no token until the pod is bound to an agent")
		return
	}
	w.refresher.logger.Info(w.ctx, "SVID rotated — re-exchanging platform token (serving current token until replaced)")
	go w.refresher.exchangeWithBackoff(w.ctx)
}

// OnX509ContextWatchError is called by go-spiffe when the Workload API watch
// stream encounters an error (e.g. temporary disconnect from the SPIRE Agent).
// We log and continue — go-spiffe will reconnect automatically.
func (w *svidWatcher) OnX509ContextWatchError(err error) {
	w.refresher.logger.Warn(w.ctx, "SVID watch error",
		logging.Field{Key: "error", Value: err.Error()},
	)
}
