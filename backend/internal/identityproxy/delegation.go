package identityproxy

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"

	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Per-run delegation token attachment (docs/adr/2026-07-23-delegated-agent-identity.md,
// Decision 4). The identity-proxy — already the platform-token injection point —
// also attaches X-Kora-Delegation-Token, selecting the run by:
//
//   - Path mode (shared agent pod): the /run/{run_id} base-URL path prefix the
//     runners already point every subprocess at.
//   - Sandbox mode (per-run pod): the run-id embedded in the pod's own SVID
//     path (…/agent/{slug}/sandbox/{run-id}); attached on ALL egress.
//
// Retrieval is once per run (cached), re-fetched as the token nears expiry —
// the identity endpoint re-signs from the live issuance record, so the jti
// stays stable while exp is always fresh. Attachment is NOT enforcement:
// revocation bites at the PEP via the JTI blocklist and live evaluation, so
// caching here is safe. A run with no delegation ("autonomous") is negative-
// cached with a modest TTL. Retrieval errors other than not-found attach
// nothing — the fail-closed rego marker turns that into denial downstream —
// and retry with backoff on subsequent requests.

// delegationHeader is the canonical delegation token header. Any inbound value
// is stripped unconditionally: the untrusted subprocess must never inject one.
const delegationHeader = "x-kora-delegation-token"

// runPathPattern extracts the run id from a /run/{run_id}/… request path.
// Mirrors middleware.StripRunPrefix's pattern — the two must stay in lockstep
// or path-mode attachment and gateway stripping diverge (spurious fail-closed
// denials for one traffic class; see the ADR's Consequences).
var runPathPattern = regexp.MustCompile(`^/run/([A-Za-z0-9._-]{1,128})(/.*)$`)

// sandboxSVIDPattern extracts the run id from a per-run sandbox SVID path:
// /tenant/{tenant}/agent/{slug}/sandbox/{run-id}.
var sandboxSVIDPattern = regexp.MustCompile(`^/tenant/[^/]+/agent/[^/]+/sandbox/([A-Za-z0-9._-]{1,128})$`)

// refreshFraction is the remaining-lifetime fraction below which the cached
// token is proactively re-fetched (~20% per the ADR transport phase).
const refreshFraction = 0.2

// defaultNegativeTTL bounds how long "no delegation for this run" (autonomous)
// is cached before re-consulting identity.
const defaultNegativeTTL = 5 * time.Minute

// defaultDelegationFetchTimeout bounds a single background delegation fetch. It
// is decoupled from — and far larger than — Envoy's ~200ms ext_proc budget, so
// the fetch can complete off the request path and populate the cache.
const defaultDelegationFetchTimeout = 5 * time.Second

// delegationFetcher is the subset of the platform identity client used to
// retrieve a run's delegation token. *identityclient.Client satisfies it; the
// call authenticates by mesh-attested SPIFFE identity (XFCC), no bearer.
type delegationFetcher interface {
	GetDelegationForRun(ctx context.Context, runID string) (*identityclient.DelegationToken, error)
}

// delegationEntry is the cached per-run delegation state.
type delegationEntry struct {
	mu sync.Mutex

	token     string
	fetchedAt time.Time
	exp       time.Time

	none      bool // negative cache: run is autonomous (or issuance revoked)
	noneUntil time.Time

	nextRetry time.Time // backoff gate after a retrieval error
	backoff   time.Duration

	fetching bool // a background retrieval is in flight (dedups async fetches)
}

// DelegationManager owns the per-run delegation token cache and the two run
// selection modes. Safe for concurrent use from all ext_proc streams.
type DelegationManager struct {
	fetcher delegationFetcher
	logger  logging.Logger

	negativeTTL    time.Duration
	backoffInitial time.Duration
	backoffMax     time.Duration

	mu      sync.Mutex
	entries map[string]*delegationEntry

	sandboxMu    sync.RWMutex
	sandboxRunID string

	// fetchTimeout bounds a single background delegation fetch. Fetches run OFF
	// the ext_proc request context (which Envoy cancels at the ~200ms
	// message_timeout) — on a context detached from its cancellation — so they can
	// actually complete; fetchTimeout is their only deadline.
	fetchTimeout time.Duration

	now func() time.Time // test seam
}

// NewDelegationManager constructs a manager. Zero durations fall back to
// defaults (negative TTL 5m; backoff from cfg is expected to be non-zero).
func NewDelegationManager(fetcher delegationFetcher, logger logging.Logger, cfg Config) *DelegationManager {
	negTTL := cfg.DelegationNegativeTTL
	if negTTL <= 0 {
		negTTL = defaultNegativeTTL
	}
	backoffInitial := cfg.BackoffInitial
	if backoffInitial <= 0 {
		backoffInitial = 500 * time.Millisecond
	}
	backoffMax := cfg.BackoffMax
	if backoffMax <= 0 {
		backoffMax = 30 * time.Second
	}
	return &DelegationManager{
		fetcher:        fetcher,
		logger:         logger,
		negativeTTL:    negTTL,
		backoffInitial: backoffInitial,
		backoffMax:     backoffMax,
		entries:        make(map[string]*delegationEntry),
		fetchTimeout:   defaultDelegationFetchTimeout,
		now:            time.Now,
	}
}

// TokenForPath resolves the delegation token to attach for a request with the
// given :path. Sandbox mode (a run-id learned from the pod's own SVID) wins
// over the path prefix — a sandbox pod belongs to exactly one run, regardless
// of what the subprocess asserts. Returns ok=false when nothing is attached.
func (m *DelegationManager) TokenForPath(ctx context.Context, path string) (string, bool) {
	runID := m.SandboxRunID()
	if runID == "" {
		matches := runPathPattern.FindStringSubmatch(path)
		if matches == nil {
			return "", false
		}
		runID = matches[1]
	}
	return m.tokenForRun(ctx, runID)
}

// SetSandboxRunID records the run identity attested by this pod's own SVID.
// From then on every outbound request attaches that run's token regardless of
// path (sandbox mode).
func (m *DelegationManager) SetSandboxRunID(ctx context.Context, runID string) {
	m.sandboxMu.Lock()
	changed := m.sandboxRunID != runID
	m.sandboxRunID = runID
	m.sandboxMu.Unlock()
	if changed {
		m.logger.Info(ctx, "sandbox run identity attested from SVID — attaching delegation on all egress",
			logging.Field{Key: "run_id", Value: runID},
		)
		// Warm the delegation cache now, off the request path, so the run's very
		// first egress request already has the token (or a confirmed negative)
		// cached — the ~200ms ext_proc budget is far too small to fetch it inline.
		e := m.entry(runID)
		e.mu.Lock()
		m.startFetchLocked(ctx, runID, e, m.now())
		e.mu.Unlock()
	}
}

// SandboxRunID returns the SVID-attested run identity, or "" in shared pods.
func (m *DelegationManager) SandboxRunID() string {
	m.sandboxMu.RLock()
	defer m.sandboxMu.RUnlock()
	return m.sandboxRunID
}

// entry returns (creating if needed) the cache entry for a run.
func (m *DelegationManager) entry(runID string) *delegationEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[runID]
	if !ok {
		e = &delegationEntry{}
		m.entries[runID] = e
	}
	return e
}

// tokenForRun returns the run's currently-cached delegation token WITHOUT ever
// blocking on the network. ok=false means attach nothing this request: the run
// is autonomous (negative-cached), the token is not fetched yet, or retrieval
// is failing (fail closed downstream). A cache miss or near-expiry token kicks a
// background fetch to populate/refresh the cache for subsequent requests.
//
// It is deliberately non-blocking: this runs inside Envoy's ext_proc header
// phase, whose default message_timeout is ~200ms. A DB+mesh delegation fetch
// routinely overran that, so Envoy failed open (failure_mode_allow) and
// forwarded the request with NO ext_proc mutation — dropping the platform-JWT
// Authorization header too, which the sandbox OPA then fail-closed denied,
// surfacing as intermittent "connection reset by peer" on git clone. The
// Authorization header is attached by handleRequestHeaders regardless of what we
// return here, so a not-yet-cached delegation never costs the platform token.
func (m *DelegationManager) tokenForRun(ctx context.Context, runID string) (string, bool) {
	e := m.entry(runID)
	e.mu.Lock()
	defer e.mu.Unlock()

	now := m.now()

	// Valid cached token: attach it. When it nears expiry, kick a background
	// refresh but keep serving the still-valid token (the endpoint re-signs with
	// a STABLE jti — revocation is enforced at the PEP, not here).
	if e.token != "" && now.Before(e.exp) {
		lifetime := e.exp.Sub(e.fetchedAt)
		remaining := e.exp.Sub(now)
		if lifetime > 0 && float64(remaining) < float64(lifetime)*refreshFraction {
			m.startFetchLocked(ctx, runID, e, now)
		}
		return e.token, true
	}

	// Negative cache: recently confirmed autonomous.
	if e.none && now.Before(e.noneUntil) {
		return "", false
	}

	// No usable cache: kick a background fetch (deduped + backoff-gated) and
	// attach nothing this request.
	m.startFetchLocked(ctx, runID, e, now)
	return "", false
}

// startFetchLocked launches a single background delegation fetch for the run
// unless one is already in flight or the post-error backoff gate is still
// closed. The entry mutex is held by the caller. The fetch keeps the request's
// trace but not its cancellation, which Envoy fires at the ext_proc deadline, so
// it can complete and populate the cache.
func (m *DelegationManager) startFetchLocked(ctx context.Context, runID string, e *delegationEntry, now time.Time) {
	if e.fetching || now.Before(e.nextRetry) {
		return
	}
	e.fetching = true
	go m.fetchAsync(context.WithoutCancel(ctx), runID, e, now)
}

// fetchAsync performs the delegation retrieval off the request path and records
// the outcome. Exactly one runs per entry at a time (guarded by e.fetching).
//
// now is captured at the spawn point (under the entry lock) rather than read
// again here, so the recorded expiry/backoff is deterministic and does not race
// a test clock advanced while the fetch is in flight. Real fetches complete in
// well under a token lifetime, so anchoring to spawn time is immaterial.
func (m *DelegationManager) fetchAsync(ctx context.Context, runID string, e *delegationEntry, now time.Time) {
	ctx, cancel := context.WithTimeout(ctx, m.fetchTimeout)
	defer cancel()
	tok, err := m.fetcher.GetDelegationForRun(ctx, runID)

	e.mu.Lock()
	defer e.mu.Unlock()
	e.fetching = false
	m.recordFetchLocked(runID, e, now, tok, err)
}

// recordFetchLocked applies a completed fetch outcome to the entry. The entry
// mutex is held by the caller.
func (m *DelegationManager) recordFetchLocked(runID string, e *delegationEntry, now time.Time, tok *identityclient.DelegationToken, err error) {
	switch {
	case err == nil:
		e.token = tok.DelegationToken
		e.fetchedAt = now
		e.exp = now.Add(time.Duration(tok.ExpiresIn) * time.Second)
		e.none = false
		e.noneUntil = time.Time{}
		e.nextRetry = time.Time{}
		e.backoff = 0
		m.logger.Info(context.Background(), "delegation token cached for run",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "expires_in_s", Value: tok.ExpiresIn},
		)

	case errors.Is(err, identityclient.ErrDelegationNotFound):
		// Autonomous run (or revoked issuance): cache the absence so the next
		// requests skip the lookup; a token already cached is dropped — a
		// revoked issuance yields nothing on refresh, fail closed.
		e.token = ""
		e.none = true
		e.noneUntil = now.Add(m.negativeTTL)
		e.nextRetry = time.Time{}
		e.backoff = 0

	default:
		// Retrieval error: do NOT attach (fail closed downstream); retry with
		// exponential backoff on subsequent requests.
		if e.backoff <= 0 {
			e.backoff = m.backoffInitial
		} else {
			e.backoff *= 2
			if e.backoff > m.backoffMax {
				e.backoff = m.backoffMax
			}
		}
		e.nextRetry = now.Add(e.backoff)
		m.logger.Warn(context.Background(), "delegation token retrieval failed — attaching nothing (fail closed downstream)",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "retry_in_ms", Value: e.backoff.Milliseconds()},
		)
	}
}

// ── SVID watcher (sandbox mode) ───────────────────────────────────────────────

// SVIDWatcher returns a workloadapi.X509ContextWatcher that learns the pod's
// own run identity from per-run sandbox SVIDs. Register it alongside the
// Refresher's watcher via NewMultiWatcher.
func (m *DelegationManager) SVIDWatcher(ctx context.Context) workloadapi.X509ContextWatcher {
	return &delegationSVIDWatcher{mgr: m, ctx: ctx}
}

type delegationSVIDWatcher struct {
	mgr *DelegationManager
	ctx context.Context //nolint:containedctx // held for goroutine lifetime, mirrors svidWatcher
}

// OnX509ContextUpdate inspects the pod's own SVID path; a sandbox SVID
// (…/agent/{slug}/sandbox/{run-id}) switches the manager into sandbox mode.
func (w *delegationSVIDWatcher) OnX509ContextUpdate(c *workloadapi.X509Context) {
	if c == nil || len(c.SVIDs) == 0 {
		return
	}
	svid := c.DefaultSVID()
	if svid == nil {
		return
	}
	matches := sandboxSVIDPattern.FindStringSubmatch(svid.ID.Path())
	if matches == nil {
		return // shared agent pod — path mode stays in effect
	}
	w.mgr.SetSandboxRunID(w.ctx, matches[1])
}

// OnX509ContextWatchError is handled (logged) by the Refresher's watcher; this
// watcher stays quiet to avoid double logging.
func (w *delegationSVIDWatcher) OnX509ContextWatchError(error) {}

// NewMultiWatcher fans one Workload API stream out to several watchers —
// go-spiffe's WatchX509Context accepts exactly one.
func NewMultiWatcher(watchers ...workloadapi.X509ContextWatcher) workloadapi.X509ContextWatcher {
	return &multiWatcher{watchers: watchers}
}

type multiWatcher struct {
	watchers []workloadapi.X509ContextWatcher
}

func (m *multiWatcher) OnX509ContextUpdate(c *workloadapi.X509Context) {
	for _, w := range m.watchers {
		w.OnX509ContextUpdate(c)
	}
}

func (m *multiWatcher) OnX509ContextWatchError(err error) {
	for _, w := range m.watchers {
		w.OnX509ContextWatchError(err)
	}
}
