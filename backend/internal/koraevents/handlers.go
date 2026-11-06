package koraevents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

const (
	eventsReadScope = "events:read"
	defaultLimit    = 50
	maxLimit        = 200
	// fromHub starts a stream at the hub's position, with no replay.
	fromHub = -1
)

// heartbeat keeps a quiet stream open through proxy idle timeouts and paces
// the revocation check of an open stream. Tests shorten it.
var heartbeat = 30 * time.Second

// errInvalidSeq reports a seq, Last-Event-ID or cursor that does not parse.
var errInvalidSeq = errors.New("invalid seq")

// readStore is the store surface the read API uses.
type readStore interface {
	List(ctx context.Context, q ListQuery) ([]Record, error)
	Get(ctx context.Context, tenant, id string) (Record, error)
	After(ctx context.Context, f Filter, seq int64) ([]Record, error)
}

// subscriber is the hub surface the streams use.
type subscriber interface {
	Subscribe() (<-chan []Record, int64, func())
}

// Routes binds every public route to events:read.
func Routes() []routescopes.RouteScope {
	return []routescopes.RouteScope{{PathPrefix: "/v1/events", Method: http.MethodGet, RequiredScope: eventsReadScope}}
}

// Scopes declares the scope vocabulary kora-events owns.
func Scopes() []routescopes.ScopeDeclaration {
	return []routescopes.ScopeDeclaration{{
		Scope:        eventsReadScope,
		ResourceType: "events",
		PathSegments: []string{},
		Action:       "read",
		Description:  "Read the tenant's platform events",
	}}
}

// Controller serves the public read API and the tenant stream.
type Controller struct {
	store      readStore
	hub        subscriber
	revoked    mw.RevocationChecker
	logger     logging.Logger
	middleware mw.MiddlewareHandler
}

// NewController protects every route with auth. The caller's chain must end
// with scope enforcement over Routes.
// revoked closes an open stream whose token or delegation is revoked.
func NewController(store readStore, hub subscriber, auth mw.MiddlewareHandler, revoked mw.RevocationChecker, logger logging.Logger) *Controller {
	return &Controller{store: store, hub: hub, revoked: revoked, logger: logger, middleware: auth}
}

// GroupedBy leaves paths explicit in Boot.
func (c *Controller) GroupedBy() string { return "" }

// Middleware returns the auth and scope chain.
func (c *Controller) Middleware() mw.MiddlewareHandler { return c.middleware }

// Boot registers the public routes.
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("GET /v1/events", c.handleList)
	mux.Handle("GET /v1/events/stream", c.handleStream)
	mux.Handle("GET /v1/events/{id}", c.handleGet)
}

// TriggersComponent is the SYSTEM principal name of kora-triggers, from its
// SPIFFE ID /system/kora-triggers.
const TriggersComponent = "kora-triggers"

// InternalController serves every tenant's stream to kora-triggers. The mesh
// AuthorizationPolicy and the controller both admit only kora-triggers.
type InternalController struct {
	store   readStore
	hub     subscriber
	auth    mw.Middleware
	revoked mw.RevocationChecker
	logger  logging.Logger
}

// NewInternalController verifies the platform token with auth.
func NewInternalController(store readStore, hub subscriber, auth mw.Middleware, revoked mw.RevocationChecker, logger logging.Logger) *InternalController {
	return &InternalController{store: store, hub: hub, auth: auth, revoked: revoked, logger: logger}
}

// GroupedBy leaves paths explicit in Boot.
func (c *InternalController) GroupedBy() string { return "" }

// Middleware verifies the platform token and admits only kora-triggers.
func (c *InternalController) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(c.auth, mw.NewMiddleware(c.triggersOnly))
}

// triggersOnly refuses every principal but kora-triggers. Any in-mesh
// component can hold a SYSTEM token, so the type alone is not enough.
func (c *InternalController) triggersOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := token.ClaimsFromContext(r.Context())
		if !ok || claims == nil || claims.Kora.PrincipalType != token.PrincipalTypeSystem || claims.Kora.DisplayName != TriggersComponent {
			api.ErrorResponse(r.Context(), w, c.logger, api.ForbiddenError(api.ErrCodeForbidden, "Only kora-triggers may read the internal event stream.", nil), nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Boot registers the internal stream.
func (c *InternalController) Boot(mux *api.Mux) {
	mux.Handle("GET /internal/v1/events/stream", c.handleStream)
}

func (c *InternalController) handleStream(w http.ResponseWriter, r *http.Request) {
	after, err := parseSeq(r.URL.Query().Get("after"))
	if err != nil {
		invalid(w, r, c.logger, "after must be a seq.")
		return
	}
	r, cancel := untilTokenExpiry(r)
	defer cancel()
	stream(w, r, c.store, c.hub, c.revoked, c.logger, Filter{}, after)
}

func (c *Controller) handleList(w http.ResponseWriter, r *http.Request) {
	filter, ok := c.filter(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	q := ListQuery{Filter: filter, Q: strings.TrimSpace(query.Get("q")), Limit: defaultLimit}
	var err error
	if q.Before, err = parseBound(query.Get("before")); err != nil {
		invalid(w, r, c.logger, "before must be a seq or an RFC 3339 time.")
		return
	}
	if q.After, err = parseBound(query.Get("after")); err != nil {
		invalid(w, r, c.logger, "after must be a seq or an RFC 3339 time.")
		return
	}
	if raw := query.Get("cursor"); raw != "" {
		if q.Before.Seq, err = decodeCursor(raw); err != nil {
			invalid(w, r, c.logger, "cursor is not valid.")
			return
		}
	}
	if raw := query.Get("limit"); raw != "" {
		if q.Limit, err = strconv.Atoi(raw); err != nil || q.Limit < 1 || q.Limit > maxLimit {
			invalid(w, r, c.logger, fmt.Sprintf("limit must be from 1 to %d.", maxLimit))
			return
		}
	}

	limit := q.Limit
	q.Limit++
	records, err := c.store.List(r.Context(), q)
	if err != nil {
		api.ErrorResponse(r.Context(), w, c.logger, api.InternalError(api.ErrCodeInternalError, "Failed to list events.", nil), err)
		return
	}
	page := Page{Events: records}
	if len(records) > limit {
		page.Events = records[:limit]
		next := encodeCursor(page.Events[limit-1].Seq)
		page.NextCursor = &next
	}
	if page.Events == nil {
		page.Events = []Record{}
	}
	api.JSON(r.Context(), w, c.logger, http.StatusOK, page)
}

func (c *Controller) handleGet(w http.ResponseWriter, r *http.Request) {
	tenant, ok := c.tenant(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	rec, err := Record{}, ErrNotFound
	if _, parseErr := uuid.Parse(id); parseErr == nil {
		rec, err = c.store.Get(r.Context(), tenant, id)
	}
	switch {
	case errors.Is(err, ErrNotFound):
		api.ErrorResponse(r.Context(), w, c.logger, api.NotFoundError(api.ErrCodeResourceNotFound, "Event not found.", nil), nil)
	case err != nil:
		api.ErrorResponse(r.Context(), w, c.logger, api.InternalError(api.ErrCodeInternalError, "Failed to read the event.", nil), err)
	default:
		api.JSON(r.Context(), w, c.logger, http.StatusOK, rec)
	}
}

func (c *Controller) handleStream(w http.ResponseWriter, r *http.Request) {
	filter, ok := c.filter(w, r)
	if !ok {
		return
	}
	after := int64(fromHub)
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		var err error
		if after, err = parseSeq(raw); err != nil {
			invalid(w, r, c.logger, "Last-Event-ID must be a seq.")
			return
		}
	}
	r, cancel := untilTokenExpiry(r)
	defer cancel()
	stream(w, r, c.store, c.hub, c.revoked, c.logger, filter, after)
}

// filter reads the caller's tenant and the filter query parameters.
func (c *Controller) filter(w http.ResponseWriter, r *http.Request) (Filter, bool) {
	tenant, ok := c.tenant(w, r)
	if !ok {
		return Filter{}, false
	}
	query := r.URL.Query()
	f := Filter{
		Tenant:     tenant,
		Category:   query.Get("category"),
		Types:      values(query["type"]),
		Agents:     values(query["agent"]),
		Principals: values(query["principal"]),
		RunID:      query.Get("run_id"),
	}
	if _, err := uuid.Parse(f.RunID); f.RunID != "" && err != nil {
		invalid(w, r, c.logger, "run_id must be a UUID.")
		return Filter{}, false
	}
	return f, true
}

// tenant returns the tenant of the verified platform token.
func (c *Controller) tenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	claims, ok := token.ClaimsFromContext(r.Context())
	if !ok || claims == nil || claims.Kora.TenantID == "" {
		api.ErrorResponse(r.Context(), w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "A tenant-scoped platform token is required.", nil), nil)
		return "", false
	}
	return claims.Kora.TenantID, true
}

// stream writes the events after seq that match f, then live events, until
// the client goes away. A stream that falls behind the hub catches up from
// the log.
func stream(w http.ResponseWriter, r *http.Request, store readStore, hub subscriber, revoked mw.RevocationChecker, logger logging.Logger, f Filter, after int64) {
	ctx := r.Context()
	flusher, ok := w.(http.Flusher)
	if !ok {
		api.ErrorResponse(ctx, w, logger, api.InternalError(api.ErrCodeInternalError, "Streaming is not supported.", nil), nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	live, position, cancel := hub.Subscribe()
	defer func() { cancel() }()
	s := &sseStream{w: w, flusher: flusher, store: store, filter: f, cursor: after}
	if s.cursor == fromHub {
		s.cursor = position
	}
	if err := s.catchUp(ctx); err != nil {
		logStreamEnd(ctx, logger, err)
		return
	}

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if revokedSince(ctx, revoked) {
				return
			}
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		case batch, open := <-live:
			if open {
				s.writeLive(batch)
				continue
			}
			live, _, cancel = hub.Subscribe()
			if err := s.catchUp(ctx); err != nil {
				logStreamEnd(ctx, logger, err)
				return
			}
		}
	}
}

// sseStream writes one client's events and tracks the last seq it covered.
type sseStream struct {
	w       io.Writer
	flusher http.Flusher
	store   readStore
	filter  Filter
	cursor  int64
}

// catchUp writes the matching events after the cursor from the log.
func (s *sseStream) catchUp(ctx context.Context) error {
	for {
		batch, err := s.store.After(ctx, s.filter, s.cursor)
		if err != nil {
			return err
		}
		for _, rec := range batch {
			writeEvent(s.w, rec)
			s.cursor = rec.Seq
		}
		if len(batch) < readBatchSize {
			s.flusher.Flush()
			return nil
		}
	}
}

// writeLive writes the matching events of a hub batch that are past the cursor.
func (s *sseStream) writeLive(batch []Record) {
	for _, rec := range batch {
		if rec.Seq > s.cursor && s.filter.matches(rec) {
			writeEvent(s.w, rec)
		}
		s.cursor = max(s.cursor, rec.Seq)
	}
	s.flusher.Flush()
}

// untilTokenExpiry ends the request when the caller's platform token expires.
// The client reconnects with a fresh token and Last-Event-ID.
func untilTokenExpiry(r *http.Request) (*http.Request, context.CancelFunc) {
	claims, ok := token.ClaimsFromContext(r.Context())
	if !ok || claims == nil || claims.ExpiresAt == nil {
		return r, func() {}
	}
	ctx, cancel := context.WithDeadline(r.Context(), claims.ExpiresAt.Time)
	return r.WithContext(ctx), cancel
}

// revokedSince reports whether the caller's token or delegation was revoked
// after the stream opened. A blocklist error keeps the stream open, as the
// auth middleware does.
func revokedSince(ctx context.Context, revoked mw.RevocationChecker) bool {
	var jtis []string
	if claims, ok := token.ClaimsFromContext(ctx); ok && claims != nil && claims.ID != "" {
		jtis = append(jtis, claims.ID)
	}
	if d := policy.DelegationFromContext(ctx); d != nil && d.JTI != "" {
		jtis = append(jtis, d.JTI)
	}
	for _, jti := range jtis {
		if blocked, err := revoked.IsBlocked(ctx, jti); err == nil && blocked {
			return true
		}
	}
	return false
}

func writeEvent(w io.Writer, rec Record) {
	body, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", rec.Seq, body)
}

func logStreamEnd(ctx context.Context, logger logging.Logger, err error) {
	if ctx.Err() == nil {
		logger.Error(ctx, "event stream read failed", logging.Field{Key: "error", Value: err.Error()})
	}
}

func invalid(w http.ResponseWriter, r *http.Request, logger logging.Logger, message string) {
	api.ErrorResponse(r.Context(), w, logger, api.ValidationError(api.ErrCodeInvalidField, message, nil), nil)
}

// values drops the empty values of a repeated query parameter.
func values(raw []string) []string {
	var out []string
	for _, v := range raw {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// parseSeq reads an optional non-negative seq.
func parseSeq(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	seq, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seq < 0 {
		return 0, errInvalidSeq
	}
	return seq, nil
}

// parseBound reads a seq or an RFC 3339 time.
func parseBound(raw string) (Bound, error) {
	if raw == "" {
		return Bound{}, nil
	}
	if seq, err := parseSeq(raw); err == nil {
		return Bound{Seq: seq}, nil
	}
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return Bound{}, err
	}
	return Bound{Time: at}, nil
}

func encodeCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(seq, 10)))
}

func decodeCursor(raw string) (int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, err
	}
	seq, err := parseSeq(string(b))
	if err != nil || seq == 0 {
		return 0, errInvalidSeq
	}
	return seq, nil
}
