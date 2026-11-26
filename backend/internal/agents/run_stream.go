package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/runkora/kora/backend/internal/platform/api"
	eventsclient "github.com/runkora/kora/backend/internal/platform/clients/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// The run list stream (docs/adr/2026-11-25-runs-list-live-stream.md, Decisions 1 to 4).

// runEventCategory is the only category kora-events serves kora-agents.
const runEventCategory = "agents.run"

// runStreamBuffer bounds the messages queued for one open stream. A stream that
// falls further behind is closed and its client resumes with Last-Event-ID.
const runStreamBuffer = 256

// runResumeLimit bounds the events a resume replays. A client further behind is sent reset
// and reloads the first page, so an old Last-Event-ID cannot make the server replay the log.
const runResumeLimit = 1000

// runStreamLifetime caps a stream, so it never keeps the scope it opened with for longer.
// The client reconnects with Last-Event-ID and a freshly read scope. Tests shorten it.
var runStreamLifetime = 60 * time.Minute

// runStreamHeartbeat keeps a quiet stream open through proxies and paces the
// revocation check. Tests shorten it.
var runStreamHeartbeat = 30 * time.Second

var (
	// errRunCaughtUp ends a resume read once it reaches the feed's position.
	errRunCaughtUp = errors.New("run stream caught up")
	// errRunResumeTooFar ends a resume read that passes runResumeLimit events.
	errRunResumeTooFar = errors.New("run stream resume is too far behind")
	// errRunResumeCut is a resume read kora-events ended before the feed's position.
	errRunResumeCut = errors.New("run stream resume ended early")
	// errRunResumeLoad is a resume whose runs did not load. A reset lets the client past them.
	errRunResumeLoad = errors.New("run stream resume could not load its runs")
	// errGradeOnStream refuses the grade filter: no run event records a new grade.
	errGradeOnStream = errors.New("the run stream takes no grade filter")
)

// runEventStream is the kora-events internal stream.
type runEventStream interface {
	Stream(ctx context.Context, q eventsclient.Query, handle func(eventsclient.Record) error) error
}

// runRowReader loads runs as list rows with their usage.
type runRowReader interface {
	GetRunRows(ctx context.Context, tenantID string, ids []string) ([]Run, error)
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
		// A stream that ended at token expiry reopens after a second. A failed one waits for retry.
		wait := time.Second
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
	rows, err := f.loadRows(ctx, env.Tenant, []string{env.Run})
	if err != nil {
		// Closing the tenant's streams moves the feed on for every other tenant. Their clients resume and are reset.
		f.logger.Error(ctx, "run feed could not load a run", logging.Field{Key: "error", Value: err.Error()})
		f.dropTenant(env.Tenant, rec.Seq)
		return nil
	}
	row := rows[env.Run]
	f.publish(rec.Seq, env.Tenant, env.Type, row.run, row.body)
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

// loadedRun is a run's list row and its JSON.
type loadedRun struct {
	run  *Run
	body json.RawMessage
}

// loadRows loads the tenant's runs among ids as list rows with their usage, in two queries.
// A missing run is left out.
func (f *RunFeed) loadRows(ctx context.Context, tenant string, ids []string) (map[string]loadedRun, error) {
	runs, err := f.rows.GetRunRows(ctx, tenant, ids)
	if err != nil {
		return nil, fmt.Errorf("load run rows: %w", err)
	}
	usage, err := f.rows.RunUsageTotals(ctx, tenant, ids)
	if err != nil {
		return nil, fmt.Errorf("load run usage: %w", err)
	}
	out := make(map[string]loadedRun, len(runs))
	for i := range runs {
		run := &runs[i]
		run.Usage = usage[run.ID]
		body, err := json.Marshal(run)
		if err != nil {
			return nil, fmt.Errorf("encode run row: %w", err)
		}
		out[run.ID] = loadedRun{run: run, body: body}
	}
	return out, nil
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

// dropTenant closes the tenant's streams and moves the feed past seq.
func (f *RunFeed) dropTenant(tenant string, seq int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sub := range f.subs[tenant] {
		f.removeLocked(sub)
	}
	f.after, f.last[tenant] = seq, seq
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

// catchUp sends sub the run changes of its tenant after seq up to the feed position to,
// read from the internal stream with the rows loaded once per run. It returns ErrEventRemoved
// when the log no longer holds seq, errRunResumeTooFar past runResumeLimit events and
// errRunResumeCut when the read ends before to and errRunResumeLoad when the runs do not load.
func (f *RunFeed) catchUp(ctx context.Context, sub *runSubscriber, after, to int64, send func(runMessage)) error {
	type change struct {
		seq       int64
		eventType string
		run       string
	}
	var changes []change
	var ids []string
	q := eventsclient.Query{After: after, Tenant: sub.tenant, Categories: []string{runEventCategory}}
	err := f.events.Stream(ctx, q, func(rec eventsclient.Record) error {
		if rec.Seq > to {
			return errRunCaughtUp
		}
		if len(changes) == runResumeLimit {
			return errRunResumeTooFar
		}
		var env runEnvelope
		if err := json.Unmarshal(rec.Event, &env); err == nil && env.Run != "" {
			changes = append(changes, change{rec.Seq, env.Type, env.Run})
			if !slices.Contains(ids, env.Run) {
				ids = append(ids, env.Run)
			}
		}
		if rec.Seq == to {
			return errRunCaughtUp
		}
		return nil
	})
	switch {
	case err == nil:
		return errRunResumeCut
	case !errors.Is(err, errRunCaughtUp):
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := f.loadRows(ctx, sub.tenant, ids)
	if err != nil {
		return fmt.Errorf("%w: %w", errRunResumeLoad, err)
	}
	for _, c := range changes {
		if row, ok := rows[c.run]; ok && readsRun(sub.tenant, sub.query.Scope, row.run) {
			send(runMessage{Seq: c.seq, Type: c.eventType, Matches: sub.query.matchesRun(row.run), Run: row.body})
		}
	}
	return nil
}

// handleStreamRuns handles GET /api/v1/runs/stream: every run change the caller can read,
// as Server-Sent Events, with matches set by the list filters. It takes no cursor or limit.
func (c *Controller) handleStreamRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, agentTypeIDs, resume, apiErr := parseRunStream(r)
	if apiErr != nil {
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
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
	ctx, stop := streamDeadline(ctx)
	defer stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	// A first body byte, before any resume read, makes browsers expose the open stream without waiting for the first event or heartbeat.
	_, _ = io.WriteString(w, ": open\n\n")
	flusher.Flush()

	floor := position
	if resume >= 0 {
		floor = max(floor, resume)
		if !c.resumeRunStream(ctx, w, sub, resume, position) {
			return
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
			if mw.RevokedSince(ctx, c.blocklist) {
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

// parseRunStream reads the list filters and Last-Event-ID, -1 when absent. The grade filter is refused.
func parseRunStream(r *http.Request) (RunListQuery, []string, int64, *api.APIError) {
	p, agentTypeIDs, apiErr := parseRunFilters(r.URL.Query())
	if apiErr != nil {
		return RunListQuery{}, nil, 0, apiErr
	}
	if p.Grade != nil {
		return RunListQuery{}, nil, 0, api.ValidationError(api.ErrCodeInvalidField, errGradeOnStream.Error(), map[string]any{"field": "grader"})
	}
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		return p, agentTypeIDs, -1, nil
	}
	resume, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || resume < 0 {
		return RunListQuery{}, nil, 0, api.ValidationError(api.ErrCodeInvalidField, "Last-Event-ID must be a seq", nil)
	}
	return p, agentTypeIDs, resume, nil
}

// resumeRunStream sends the changes after resume up to the feed's position, or reset when
// they cannot be replayed. It reports false when the stream should close.
func (c *Controller) resumeRunStream(ctx context.Context, w io.Writer, sub *runSubscriber, resume, position int64) bool {
	// Position 0 is a feed that has seen no event of the tenant since it opened, so the gap has no
	// known end. Resume 0 precedes every event, which is a reload of the first page.
	if position == 0 || resume == 0 {
		writeRunReset(w)
		return true
	}
	if resume >= position {
		return true
	}
	err := c.feed.catchUp(ctx, sub, resume, position, func(m runMessage) { writeRunMessage(w, m) })
	switch {
	case errors.Is(err, errRunResumeLoad):
		c.logger.Error(ctx, "run stream resume could not load its runs", logging.Field{Key: "error", Value: err.Error()})
		writeRunReset(w)
	case errors.Is(err, eventsclient.ErrEventRemoved), errors.Is(err, errRunResumeTooFar), errors.Is(err, errRunResumeCut):
		writeRunReset(w)
	case err != nil:
		if ctx.Err() == nil {
			c.logger.Error(ctx, "run stream resume failed", logging.Field{Key: "error", Value: err.Error()})
		}
		return false
	}
	return true
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

// streamDeadline ends the stream at runStreamLifetime, or sooner when the caller's platform token expires.
func streamDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(runStreamLifetime)
	if claims, ok := token.ClaimsFromContext(ctx); ok && claims != nil && claims.ExpiresAt != nil && claims.ExpiresAt.Before(deadline) {
		deadline = claims.ExpiresAt.Time
	}
	return context.WithDeadline(ctx, deadline)
}
