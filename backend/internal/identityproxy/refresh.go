package identityproxy

import (
	"context"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Refresher manages proactive token renewal.
//
// It runs two concurrent strategies:
//  1. A background poll goroutine that wakes every Config.PollInterval and
//     re-exchanges if the token is within Config.RefreshThreshold of expiry.
//  2. A go-spiffe X509Context callback that fires on every SVID rotation,
//     immediately invalidating the cache and triggering a re-exchange.
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
}

// NewRefresher constructs a Refresher. The envoyReady gate is injected by
// NewServer via the package-internal envoyReady field; tests that do not need
// the gate can leave it nil (exchangeWithBackoff checks for nil before blocking).
func NewRefresher(cache *TokenCache, exchanger tokenExchanger, logger logging.Logger, cfg Config) *Refresher {
	return &Refresher{
		cache:     cache,
		exchanger: exchanger,
		logger:    logger,
		tracer:    otel.Tracer("orbit.identity-proxy"),
		cfg:       cfg,
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

// OnSVIDRotation returns a go-spiffe X509ContextWatcher that invalidates the
// cache and triggers a re-exchange whenever the SPIRE Agent delivers a new SVID.
// The watcher's callbacks must return promptly — heavy work is done in a new
// goroutine so the go-spiffe callback goroutine is not blocked.
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
			span.SetStatus(codes.Error, "context canceled waiting for Envoy")
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
			span.SetAttributes(attribute.Int("refresh.attempts", attempt))
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
			span.SetAttributes(attribute.Int("refresh.attempts", attempt))
			span.SetStatus(codes.Error, "context canceled during retry")
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
// We invalidate the cache immediately so no stale token is served, then
// re-exchange in the background so the callback returns promptly.
func (w *svidWatcher) OnX509ContextUpdate(_ *workloadapi.X509Context) {
	w.refresher.logger.Info(w.ctx, "SVID rotated — invalidating token cache and re-exchanging")
	w.refresher.cache.Invalidate()
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
