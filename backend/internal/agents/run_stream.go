package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/runkora/kora/backend/internal/platform/api"
	eventsclient "github.com/runkora/kora/backend/internal/platform/clients/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// The run list stream (docs/adr/2026-11-25-runs-list-live-stream.md, Decisions 1 to 4).

// runEventCategory is the only category kora-events serves kora-agents.
const runEventCategory = "agents.run"

// runStreamBuffer bounds the messages queued for one open stream. A stream that
// falls further behind is closed and its client resumes with Last-Event-ID.
const runStreamBuffer = 256

// runStreamHeartbeat keeps a quiet stream open through proxies and paces the
// revocation check. Tests shorten it.
var runStreamHeartbeat = 30 * time.Second

var (
	// errRunCaughtUp ends a resume read once it reaches the feed's position.
	errRunCaughtUp = errors.New("run stream caught up")
	// errGradeOnStream refuses the grade filter: no run event records a new grade.
	errGradeOnStream = errors.New("the run stream takes no grade filter")
)

// runEventStream is the kora-events internal stream.
type runEventStream interface {
	Stream(ctx context.Context, q eventsclient.Query, handle func(eventsclient.Record) error) error
}

// runRowReader loads a run as a list row with its usage.
type runRowReader interface {
	GetRunRow(ctx context.Context, tenantID, id string) (*Run, error)
	RunUsageTotals(ctx context.Context, tenantID string, runIDs []string) (map[string]RunUsage, error)
}

// runMessage is one run change for one open stream.
type runMessage struct {
	Seq     int64           `json:"-"`
	Type    string          `json:"type"`
	Matches bool            `json:"matches"`
	Run     json.RawMessage `json:"run"`
}

// runSubscriber is one open run stream. query carries its scope and filters.
type runSubscriber struct {
	tenant string
	query  RunListQuery
	ch     chan runMessage
}

// RunFeed reads every tenant's run events from kora-events, once per replica,
// and fans each change out to the run streams open on this replica.
type RunFeed struct {
	events runEventStream
	rows   runRowReader
	logger logging.Logger
	retry  time.Duration

	mu   sync.Mutex
	subs map[string]map[*runSubscriber]struct{}
	// after is the last seq handled, the point a dropped feed reopens from. 0 opens at the hub.
	after int64
	// last is the last seq handled per tenant, where a resuming stream's read stops.
	last map[string]int64
}

// NewRunFeed returns a feed that has not started.
func NewRunFeed(events runEventStream, rows runRowReader, logger logging.Logger) *RunFeed {
	return &RunFeed{events: events, rows: rows, logger: logger, retry: 5 * time.Second,
		subs: map[string]map[*runSubscriber]struct{}{}, last: map[string]int64{}}
}

// Start reads the feed until ctx ends, reopening a dropped stream from the last seq handled.
func (f *RunFeed) Start(ctx context.Context) {
	for ctx.Err() == nil {
		after := f.position()
		q := eventsclient.Query{After: after, Live: after == 0, Categories: []string{runEventCategory}}
		// A stream that ended at token expiry reopens at once. A failed one waits.
		wait := time.Duration(0)
		if err := f.events.Stream(ctx, q, func(rec eventsclient.Record) error { return f.handle(ctx, rec) }); err != nil && ctx.Err() == nil {
			f.logger.Error(ctx, "run feed stream failed", logging.Field{Key: "error", Value: err.Error()})
			wait = f.retry
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

func (f *RunFeed) position() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.after
}

// runEnvelope is the part of an event envelope the feed reads.
type runEnvelope struct {
	Type   string `json:"type"`
	Tenant string `json:"koratenant"`
	Run    string `json:"korarun"`
}

// handle fans one event out. An error reopens the stream from the last seq handled.
func (f *RunFeed) handle(ctx context.Context, rec eventsclient.Record) error {
	var env runEnvelope
	if err := json.Unmarshal(rec.Event, &env); err != nil || env.Run == "" {
		f.publish(rec.Seq, "", "", nil, nil)
		return nil
	}
	if !f.watched(env.Tenant, rec.Seq) {
		return nil
	}
	run, body, err := f.row(ctx, env.Tenant, env.Run)
	if err != nil {
		return err
	}
	f.publish(rec.Seq, env.Tenant, env.Type, run, body)
	return nil
}

// watched reports whether a stream on this replica is in tenant. When none is,
// the event is handled here, under the lock a new stream registers with.
func (f *RunFeed) watched(tenant string, seq int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.subs[tenant]) > 0 {
		return true
	}
	f.after, f.last[tenant] = seq, seq
	return false
}

// row loads the run as a list row and its JSON. A missing run is nil.
func (f *RunFeed) row(ctx context.Context, tenant, id string) (*Run, json.RawMessage, error) {
	run, err := f.rows.GetRunRow(ctx, tenant, id)
	if errors.Is(err, ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load run row: %w", err)
	}
	usage, err := f.rows.RunUsageTotals(ctx, tenant, []string{id})
	if err != nil {
		return nil, nil, fmt.Errorf("load run usage: %w", err)
	}
	run.Usage = usage[id]
	body, err := json.Marshal(run)
	if err != nil {
		return nil, nil, fmt.Errorf("encode run row: %w", err)
	}
	return run, body, nil
}

// publish sends the run to each stream in its tenant whose scope reads it. A stream
// whose buffer is full is closed rather than block the feed for every other stream.
func (f *RunFeed) publish(seq int64, tenant, eventType string, run *Run, body json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.after = seq
	if tenant == "" {
		return
	}
	f.last[tenant] = seq
	if run == nil {
		return
	}
	for sub := range f.subs[tenant] {
		if !readsRun(sub.tenant, sub.query.Scope, run) {
			continue
		}
		select {
		case sub.ch <- runMessage{Seq: seq, Type: eventType, Matches: sub.query.matchesRun(run), Run: body}:
		default:
			f.removeLocked(sub)
		}
	}
}

// subscribe registers a stream and returns the tenant's position: the feed sends it
// every run change after that seq. 0 means the feed has handled no event of the tenant.
func (f *RunFeed) subscribe(tenant string, q RunListQuery) (*runSubscriber, int64, func()) {
	sub := &runSubscriber{tenant: tenant, query: q, ch: make(chan runMessage, runStreamBuffer)}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subs[tenant] == nil {
		f.subs[tenant] = map[*runSubscriber]struct{}{}
	}
	f.subs[tenant][sub] = struct{}{}
	return sub, f.last[tenant], func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.removeLocked(sub)
	}
}

func (f *RunFeed) removeLocked(sub *runSubscriber) {
	if _, ok := f.subs[sub.tenant][sub]; !ok {
		return
	}
	delete(f.subs[sub.tenant], sub)
	if len(f.subs[sub.tenant]) == 0 {
		delete(f.subs, sub.tenant)
	}
	close(sub.ch)
}

// catchUp sends sub the run changes of its tenant after seq up to the feed position
// to, from the internal stream. It returns ErrEventRemoved when the log no longer holds seq.
func (f *RunFeed) catchUp(ctx context.Context, sub *runSubscriber, after, to int64, send func(runMessage)) error {
	q := eventsclient.Query{After: after, Tenant: sub.tenant, Categories: []string{runEventCategory}}
	err := f.events.Stream(ctx, q, func(rec eventsclient.Record) error {
		if rec.Seq > to {
			return errRunCaughtUp
		}
		var env runEnvelope
		if err := json.Unmarshal(rec.Event, &env); err == nil && env.Run != "" {
			run, body, err := f.row(ctx, sub.tenant, env.Run)
			if err != nil {
				return err
			}
			if run != nil && readsRun(sub.tenant, sub.query.Scope, run) {
				send(runMessage{Seq: rec.Seq, Type: env.Type, Matches: sub.query.matchesRun(run), Run: body})
			}
		}
		if rec.Seq == to {
			return errRunCaughtUp
		}
		return nil
	})
	if errors.Is(err, errRunCaughtUp) {
		return nil
	}
	return err
}

// WithRunFeed sets the feed GET /api/v1/runs/stream serves from.
func (c *Controller) WithRunFeed(feed *RunFeed) *Controller {
	c.feed = feed
	return c
}

// handleStreamRuns handles GET /api/v1/runs/stream: every run change the caller can read,
// as Server-Sent Events, with matches set by the list filters. It takes no cursor or limit.
func (c *Controller) handleStreamRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, agentTypeIDs, apiErr := parseRunFilters(r.URL.Query())
	if apiErr == nil && p.Grade != nil {
		apiErr = api.ValidationError(api.ErrCodeInvalidField, errGradeOnStream.Error(), map[string]any{"field": "grader"})
	}
	if apiErr != nil {
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}
	resume := int64(-1)
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			api.ErrorResponse(ctx, w, c.logger, api.ValidationError(api.ErrCodeInvalidField, "Last-Event-ID must be a seq", nil), nil)
			return
		}
		resume = n
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "streaming not supported", nil), nil)
		return
	}
	tenant := tenantFromContext(ctx)
	scope, err := c.runs.readableRunScope(ctx, tenant)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to read the run scope", nil), err)
		return
	}
	scope.OnlyAgentTypeIDs = agentTypeIDs
	p.Scope = scope

	// Registered before the headers go out, so no change is lost between opening the stream and fetching the first page.
	sub, position, cancel := c.feed.subscribe(tenant, p)
	defer cancel()
	ctx, stop := untilTokenExpiry(ctx)
	defer stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	floor := position
	if resume >= 0 {
		floor = max(floor, resume)
		// Position 0 is a feed that has seen no event of the tenant since it opened, so the gap has no known end.
		if position == 0 {
			writeRunReset(w)
		} else if resume < position {
			err := c.feed.catchUp(ctx, sub, resume, position, func(m runMessage) { writeRunMessage(w, m) })
			switch {
			case errors.Is(err, eventsclient.ErrEventRemoved):
				writeRunReset(w)
			case err != nil:
				if ctx.Err() == nil {
					c.logger.Error(ctx, "run stream resume failed", logging.Field{Key: "error", Value: err.Error()})
				}
				return
			}
		}
		flusher.Flush()
	}

	ticker := time.NewTicker(runStreamHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if revokedSince(ctx, c.blocklist) {
				return
			}
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		case m, open := <-sub.ch:
			// A closed channel is a stream the feed dropped for falling behind.
			if !open {
				return
			}
			if m.Seq > floor {
				writeRunMessage(w, m)
				flusher.Flush()
			}
		}
	}
}

func writeRunMessage(w io.Writer, m runMessage) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "id: %d\nevent: run\ndata: %s\n\n", m.Seq, data)
}

// writeRunReset tells the client its resume point is lost and to reload the first page.
func writeRunReset(w io.Writer) {
	_, _ = io.WriteString(w, "event: reset\ndata: {}\n\n")
}

// untilTokenExpiry ends the stream when the caller's platform token expires, as kora-events does.
func untilTokenExpiry(ctx context.Context) (context.Context, context.CancelFunc) {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil || claims.ExpiresAt == nil {
		return ctx, func() {}
	}
	return context.WithDeadline(ctx, claims.ExpiresAt.Time)
}

// revokedSince reports whether the caller's token or delegation was revoked after the
// stream opened. A blocklist error keeps the stream open, as the auth middleware does.
func revokedSince(ctx context.Context, blocklist mw.JTIBlocklist) bool {
	if blocklist == nil {
		return false
	}
	var jtis []string
	if claims, ok := token.ClaimsFromContext(ctx); ok && claims != nil && claims.ID != "" {
		jtis = append(jtis, claims.ID)
	}
	if d := policy.DelegationFromContext(ctx); d != nil && d.JTI != "" {
		jtis = append(jtis, d.JTI)
	}
	for _, jti := range jtis {
		if blocked, err := blocklist.IsBlocked(ctx, jti); err == nil && blocked {
			return true
		}
	}
	return false
}
