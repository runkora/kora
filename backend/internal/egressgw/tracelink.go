package egressgw

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
)

// Cache bounds. Failed lookups are held for traceLinkFailureTTL to cap lookups against an unavailable kora-agents.
const (
	traceLinkTTL        = 5 * time.Minute
	traceLinkFailureTTL = 30 * time.Second
	traceLinkEntries    = 4096
	traceLinkTimeout    = 2 * time.Second
)

// runTraceParents reads a run's stored traceparent. *agents.Client satisfies it.
type runTraceParents interface {
	RunTraceParent(ctx context.Context, bearer, runID string) (string, error)
}

// TraceLinker resolves the span link from a CONNECT span to its run's trace
// (docs/adr/2026-10-31-one-trace-per-run.md, Amendment 2). A nil *TraceLinker links nothing.
type TraceLinker struct {
	src    runTraceParents
	token  func(context.Context) (string, error)
	logger logging.Logger
	now    func() time.Time

	flight  singleflight.Group // one in-flight lookup per run
	mu      sync.Mutex
	entries map[string]traceLinkEntry
}

type traceLinkEntry struct {
	links   []trace.Link
	expires time.Time
}

// NewTraceLinker reads traceparents from src with the bearer from token.
func NewTraceLinker(src runTraceParents, token func(context.Context) (string, error), logger logging.Logger) *TraceLinker {
	return &TraceLinker{src: src, token: token, logger: logger, now: time.Now, entries: map[string]traceLinkEntry{}}
}

// Links returns the links for a CONNECT span of runID. A failed lookup or a run without a
// traceparent returns none and never fails the connection.
func (l *TraceLinker) Links(ctx context.Context, runID string) []trace.Link {
	if l == nil || runID == "" {
		return nil
	}
	l.mu.Lock()
	e, ok := l.entries[runID]
	l.mu.Unlock()
	if ok && l.now().Before(e.expires) {
		return e.links
	}

	v, _, _ := l.flight.Do(runID, func() (any, error) {
		links, ttl := l.lookup(ctx, runID)
		l.put(runID, traceLinkEntry{links: links, expires: l.now().Add(ttl)})
		return links, nil
	})
	return v.([]trace.Link)
}

func (l *TraceLinker) lookup(ctx context.Context, runID string) ([]trace.Link, time.Duration) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), traceLinkTimeout)
	defer cancel()

	bearer, err := l.token(ctx)
	var traceParent string
	if err == nil {
		traceParent, err = l.src.RunTraceParent(ctx, bearer, runID)
	}
	if err != nil {
		l.logger.Warn(ctx, "egress run trace lookup failed", logging.Field{Key: "error", Value: err.Error()})
		return nil, traceLinkFailureTTL
	}
	if link, ok := observability.TraceParentLink(traceParent); ok {
		return []trace.Link{link}, traceLinkTTL
	}
	return nil, traceLinkTTL
}

// put stores an entry, clearing the cache when it is full.
func (l *TraceLinker) put(runID string, e traceLinkEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) >= traceLinkEntries {
		clear(l.entries)
	}
	l.entries[runID] = e
}
