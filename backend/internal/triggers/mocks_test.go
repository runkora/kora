package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
)

// Compile-time interface checks.
var (
	_ Repository  = (*mockRepository)(nil)
	_ TokenMinter = (*fakeMinter)(nil)
)

// testCipherKey is a fixed 32-byte AES key for deterministic tests.
var testCipherKey = []byte("0123456789abcdef0123456789abcdef")

func silentLogger() logging.Logger { return logging.NewLoggerWithOutput(io.Discard) }

// ── mockRepository ──────────────────────────────────────────────────────────

type mockRepository struct {
	// derived counts DeriveDeliveryStatus calls per delivery.
	derived map[string]int

	// refuseAdmission makes EnqueueWithinCeiling turn a fire away, so a test can
	// assert what the service does with a refusal.
	refuseAdmission bool
	// admissionErr makes EnqueueWithinCeiling fail with an infrastructure error,
	// so a test can tell a refusal apart from a database fault.
	admissionErr error
	// orphaned is what DrainOrphanedPending hands back.
	orphaned []OrphanedPending

	mu           sync.Mutex
	sources      map[string]*WebhookSource
	triggers     map[string]*Trigger
	deliveries   []*Delivery
	deliveryRuns []*DeliveryRun
	pending      []*pendingRow
	retention    map[string]int          // tenant_id → payload_days
	seenDeliv    map[string]bool         // source_id|provider_delivery_id → seen (dedup)
	replayKeys   map[string]replayKeyRow // source_id|body_hash → what was seen

	createSourceErr   error
	createTriggerErr  error
	insertDeliveryErr error
	listTriggersErr   error
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		sources:    map[string]*WebhookSource{},
		triggers:   map[string]*Trigger{},
		retention:  map[string]int{},
		seenDeliv:  map[string]bool{},
		replayKeys: map[string]replayKeyRow{},
	}
}

func (m *mockRepository) CreateSource(_ context.Context, s *WebhookSource) error {
	if m.createSourceErr != nil {
		return m.createSourceErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	// Mirrors the SQL: the secret a source starts with was issued now, and the
	// timestamps are the database's, not the caller's.
	now := time.Now()
	cp.SecretRotatedAt = &now
	cp.CreatedAt = now
	cp.UpdatedAt = now
	m.sources[s.ID] = &cp
	return nil
}

func (m *mockRepository) GetSource(_ context.Context, tenantID, id string) (*WebhookSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sources[id]
	if !ok || s.TenantID != tenantID {
		return nil, ErrSourceNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *mockRepository) GetSourceByID(_ context.Context, id string) (*WebhookSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sources[id]
	if !ok {
		return nil, ErrSourceNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *mockRepository) ListSources(_ context.Context, tenantID string) ([]WebhookSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []WebhookSource
	for _, s := range m.sources {
		if s.TenantID == tenantID {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (m *mockRepository) UpdateSource(_ context.Context, s *WebhookSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev, ok := m.sources[s.ID]
	if !ok {
		return ErrSourceNotFound
	}
	cp := *s
	// Mirrors the SQL: the date moves only when the stored secret changes, so
	// an ordinary enable/disable cannot invalidate every older replay.
	cp.SecretRotatedAt = prev.SecretRotatedAt
	if prev.SecretCiphertext != s.SecretCiphertext {
		now := time.Now()
		cp.SecretRotatedAt = &now
	}
	m.sources[s.ID] = &cp
	return nil
}

func (m *mockRepository) DeleteSource(_ context.Context, tenantID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sources[id]
	if !ok || s.TenantID != tenantID {
		return ErrSourceNotFound
	}
	delete(m.sources, id)
	return nil
}

func (m *mockRepository) CreateTrigger(_ context.Context, t *Trigger) error {
	if m.createTriggerErr != nil {
		return m.createTriggerErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *t
	// The database stamps these; the mock must too, or code that reasons about
	// a trigger's age (grant propagation) sees every trigger as ancient.
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
		cp.UpdatedAt = cp.CreatedAt
	}
	m.triggers[t.ID] = &cp
	return nil
}

func (m *mockRepository) GetTrigger(_ context.Context, tenantID, id string) (*Trigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.triggers[id]
	if !ok || t.TenantID != tenantID {
		return nil, ErrTriggerNotFound
	}
	cp := *t
	cp.PendingCount = m.pendingCountLocked(t.ID)
	return &cp, nil
}

// pendingCountLocked mirrors the correlated subquery in triggerColumns: fires
// waiting on this trigger, unclaimed only. Callers must hold m.mu.
func (m *mockRepository) pendingCountLocked(triggerID string) int {
	n := 0
	for _, p := range m.pending {
		if p.TriggerID == triggerID && !p.claimed {
			n++
		}
	}
	return n
}

func (m *mockRepository) ListTriggers(_ context.Context, tenantID, agentTypeID string) ([]Trigger, error) {
	if m.listTriggersErr != nil {
		return nil, m.listTriggersErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Trigger
	for _, t := range m.triggers {
		if t.TenantID == tenantID && (agentTypeID == "" || t.AgentTypeID == agentTypeID) {
			cp := *t
			cp.PendingCount = m.pendingCountLocked(t.ID)
			out = append(out, cp)
		}
	}
	return out, nil
}

// ListMatchableTriggersForSource mirrors the SQL: enabled, plus auto-paused, so
// a paused trigger reaches admission and records 'paused' instead of vanishing.
func (m *mockRepository) ListMatchableTriggersForSource(_ context.Context, sourceID string) ([]Trigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Trigger
	for _, t := range m.triggers {
		if t.SourceID == sourceID && matchable(t) {
			cp := *t
			cp.PendingCount = m.pendingCountLocked(t.ID)
			out = append(out, cp)
		}
	}
	return out, nil
}

func (m *mockRepository) ListMatchableScheduledTriggers(_ context.Context) ([]Trigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Trigger
	for _, t := range m.triggers {
		if t.Kind == KindScheduled && matchable(t) {
			cp := *t
			cp.PendingCount = m.pendingCountLocked(t.ID)
			out = append(out, cp)
		}
	}
	return out, nil
}

// matchable mirrors the SQL predicate `enabled = true OR auto_paused_at IS NOT NULL`.
func matchable(t *Trigger) bool { return t.Enabled || t.AutoPausedAt != nil }

func (m *mockRepository) UpdateTrigger(_ context.Context, t *Trigger, writeEnablement bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.triggers[t.ID]
	if !ok {
		return ErrTriggerNotFound
	}
	cp := *t
	if !writeEnablement {
		// Mirror the SQL: an edit that did not touch enablement leaves the
		// stored enabled/auto_paused_at alone rather than writing back the
		// snapshot the caller read at the start.
		cp.Enabled = stored.Enabled
		cp.AutoPausedAt = stored.AutoPausedAt
	}
	m.triggers[t.ID] = &cp
	return nil
}

func (m *mockRepository) DeleteTrigger(_ context.Context, tenantID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.triggers[id]
	if !ok || t.TenantID != tenantID {
		return ErrTriggerNotFound
	}
	delete(m.triggers, id)
	return nil
}

func (m *mockRepository) InsertDelivery(_ context.Context, d *Delivery) (bool, error) {
	if m.insertDeliveryErr != nil {
		return false, m.insertDeliveryErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mirrors the partial unique index: only ORIGINAL arrivals are deduped. A row
	// naming the delivery it descends from is lineage, and recording it is the
	// entire point of writing it.
	if d.ProviderDeliveryID != "" && d.RedeliveryOf == nil {
		key := d.SourceID + "|" + d.ProviderDeliveryID
		if m.seenDeliv[key] {
			return false, nil
		}
		m.seenDeliv[key] = true
	}
	cp := *d
	if cp.ReceivedAt.IsZero() {
		cp.ReceivedAt = time.Now()
	}
	m.deliveries = append(m.deliveries, &cp)
	return true, nil
}

func (m *mockRepository) UpdateDeliveryOutcome(_ context.Context, id, status string, triggerID, runID, errMsg *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.deliveries {
		if d.ID == id {
			// Mirrors the SQL: arrival-level statuses survive a later run
			// outcome. What a duplicate IS does not change because a trigger
			// with dedupe off acted on it.
			if d.Status != StatusDuplicate && d.Status != StatusMissed {
				d.Status = status
			}
			d.TriggerID = triggerID
			d.RunID = runID
			d.Error = errMsg
			return nil
		}
	}
	return nil
}

func (m *mockRepository) ListDeliveries(_ context.Context, tenantID string, filter DeliveryFilter) ([]Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Delivery
	for _, d := range m.deliveries {
		if d.TenantID == tenantID && (filter.SourceID == "" || d.SourceID == filter.SourceID) {
			out = append(out, *d)
		}
	}
	return out, nil
}

func (m *mockRepository) GetDelivery(_ context.Context, tenantID, id string) (*Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.deliveries {
		if d.ID == id && d.TenantID == tenantID {
			cp := *d
			return &cp, nil
		}
	}
	return nil, ErrDeliveryNotFound
}

// GetDeliveryByProviderID mirrors the SQL: lineage rows are excluded, so a
// source that has already been replayed still resolves to the one original.
func (m *mockRepository) GetDeliveryByProviderID(_ context.Context, sourceID, providerDeliveryID string) (*Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.deliveries {
		if d.SourceID == sourceID && d.ProviderDeliveryID == providerDeliveryID && d.RedeliveryOf == nil {
			cp := *d
			return &cp, nil
		}
	}
	return nil, ErrDeliveryNotFound
}

func (m *mockRepository) InsertDeliveryRun(_ context.Context, r *DeliveryRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *r
	m.deliveryRuns = append(m.deliveryRuns, &cp)
	return nil
}

func (m *mockRepository) ListDeliveryRunsForDeliveries(_ context.Context, tenantID string, deliveryIDs []string) (map[string][]DeliveryRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[string]bool{}
	for _, id := range deliveryIDs {
		want[id] = true
	}
	out := map[string][]DeliveryRun{}
	for _, r := range m.deliveryRuns {
		if r.TenantID == tenantID && want[r.DeliveryID] {
			out[r.DeliveryID] = append(out[r.DeliveryID], *r)
		}
	}
	return out, nil
}

// deliveryRunsFor returns the recorded run rows for a delivery, for assertions.
// outcomeStatuses is what a delivery's fan-out recorded, which is the service's
// output. The delivery's own status is derived from these by SQL, so a service
// test asserts what was recorded and leaves the derivation to
// TestPostgres_DeriveDeliveryStatus.
func (m *mockRepository) outcomeStatuses(deliveryID string) []string {
	var out []string
	for _, r := range m.deliveryRunsFor(deliveryID) {
		out = append(out, r.Status)
	}
	return out
}

func (m *mockRepository) deliveryRunsFor(deliveryID string) []DeliveryRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []DeliveryRun
	for _, r := range m.deliveryRuns {
		if r.DeliveryID == deliveryID {
			out = append(out, *r)
		}
	}
	return out
}

// deliveryByID returns the recorded delivery for assertions.
func (m *mockRepository) deliveryByID(id string) *Delivery {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.deliveries {
		if d.ID == id {
			cp := *d
			return &cp
		}
	}
	return nil
}

func (m *mockRepository) lastDelivery() *Delivery {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.deliveries) == 0 {
		return nil
	}
	cp := *m.deliveries[len(m.deliveries)-1]
	return &cp
}

// ── fakeMinter ──────────────────────────────────────────────────────────────

type fakeMinter struct {
	token string
	err   error

	mu    sync.Mutex
	calls []mintCall
}

type mintCall struct {
	tenantID    string
	principalID string
}

func (f *fakeMinter) MintServiceToken(_ context.Context, tenantID, principalID, _ string, _ time.Duration) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, mintCall{tenantID: tenantID, principalID: principalID})
	f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	return f.token, nil
}

// reset clears recorded calls so a test can assert on mints made after setup.
func (f *fakeMinter) reset() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

// ── policy stub server ────────────────────────────────────────────────────────

type policyStub struct {
	server *httptest.Server

	mu         sync.Mutex
	created    map[string]string // grantID → principalID
	lastScopes []string          // scopes of the most recent create
	deleted    []string
	counter    int
	failCreate bool
}

func newPolicyStub() *policyStub {
	ps := &policyStub{created: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/policy/grants", func(w http.ResponseWriter, r *http.Request) {
		ps.mu.Lock()
		defer ps.mu.Unlock()
		if ps.failCreate {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			PrincipalID string   `json:"principal_id"`
			Scopes      []string `json:"scopes"`
		}
		_ = json.Unmarshal(body, &req)
		ps.counter++
		id := fmt.Sprintf("grant-%d", ps.counter)
		ps.created[id] = req.PrincipalID
		ps.lastScopes = req.Scopes
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"id":%q}`, id)
	})
	mux.HandleFunc("GET /api/v1/policy/grants", func(w http.ResponseWriter, r *http.Request) {
		ps.mu.Lock()
		defer ps.mu.Unlock()
		pid := r.URL.Query().Get("principal_id")
		grants := []string{}
		for id, p := range ps.created {
			if p == pid {
				grants = append(grants, fmt.Sprintf(`{"id":%q,"principal_id":%q}`, id, p))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"grants":[%s]}`, strings.Join(grants, ","))
	})
	mux.HandleFunc("DELETE /api/v1/policy/grants/{id}", func(w http.ResponseWriter, r *http.Request) {
		ps.mu.Lock()
		ps.deleted = append(ps.deleted, r.PathValue("id"))
		delete(ps.created, r.PathValue("id"))
		ps.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	ps.server = httptest.NewServer(mux)
	return ps
}

func (ps *policyStub) deletedCount() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return len(ps.deleted)
}

// ── agents stub server ──────────────────────────────────────────────────────

type startCall struct {
	bearer         string
	agentTypeID    string
	initialMessage string
	// The trigger→run backlink as it went over the wire, so tests assert what
	// orbit-agents actually receives rather than what the caller intended.
	triggerID   string
	triggerName string
	deliveryID  string
}

type agentsStub struct {
	server *httptest.Server

	mu         sync.Mutex
	calls      []startCall
	status     int    // run-start response status; 0 → 201
	runErrBody string // run-start error body written when status >= 300
	// statusOnce, when set, applies to the next run-start only and is then
	// cleared: a refusal that resolves on retry, as a young grant does.
	statusOnce int

	// agent-type resolution (GET /api/v1/agent-types/{id}).
	atSlug         string // slug to return; "" → "code-reviewer"
	atTenant       string // tenant to return; "" → testTenant
	atIdentityMode string // identity_mode to return; "" → autonomous-by-default
	atStatus       int    // response status; 0 → 200
}

func newAgentsStub() *agentsStub {
	as := &agentsStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agent-types/{id}", func(w http.ResponseWriter, r *http.Request) {
		as.mu.Lock()
		status, slug, tenant, identityMode := as.atStatus, as.atSlug, as.atTenant, as.atIdentityMode
		as.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		if slug == "" {
			slug = "code-reviewer"
		}
		if tenant == "" {
			tenant = testTenant
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status < 300 {
			fmt.Fprintf(w, `{"id":%q,"tenant_id":%q,"slug":%q,"identity_mode":%q}`, r.PathValue("id"), tenant, slug, identityMode)
		}
	})
	mux.HandleFunc("POST /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			AgentTypeID    string `json:"agent_type_id"`
			InitialMessage string `json:"initial_message"`
			TriggerID      string `json:"trigger_id"`
			TriggerName    string `json:"trigger_name"`
			DeliveryID     string `json:"delivery_id"`
		}
		_ = json.Unmarshal(body, &req)
		as.mu.Lock()
		as.calls = append(as.calls, startCall{
			bearer:         r.Header.Get("Authorization"),
			agentTypeID:    req.AgentTypeID,
			initialMessage: req.InitialMessage,
			triggerID:      req.TriggerID,
			triggerName:    req.TriggerName,
			deliveryID:     req.DeliveryID,
		})
		status, errBody := as.status, as.runErrBody
		if as.statusOnce != 0 {
			status, as.statusOnce = as.statusOnce, 0
		}
		as.mu.Unlock()
		if status == 0 {
			status = http.StatusCreated
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status < 300 {
			_, _ = w.Write([]byte(`{"id":"run-1","state":"active"}`))
		} else if errBody != "" {
			_, _ = w.Write([]byte(errBody))
		}
	})
	as.server = httptest.NewServer(mux)
	return as
}

func (as *agentsStub) lastCall() (startCall, bool) {
	as.mu.Lock()
	defer as.mu.Unlock()
	if len(as.calls) == 0 {
		return startCall{}, false
	}
	return as.calls[len(as.calls)-1], true
}

func (as *agentsStub) callCount() int {
	as.mu.Lock()
	defer as.mu.Unlock()
	return len(as.calls)
}

// ── test wiring ─────────────────────────────────────────────────────────────

type testDeps struct {
	repo       *mockRepository
	policy     *policyStub
	agents     *agentsStub
	minter     *fakeMinter
	policyClnt *policyclient.Client
	// The services a test may need alongside the pipeline, sharing this
	// harness's stubs.
	sources  *SourceService
	triggers *TriggerService
}

// newTestSourceService builds the sources half against the same stubs. It takes
// a sourceStore, so a test that only exercises sources can pass any six-method
// fake rather than the whole mockRepository.
func newTestSourceService(t *testing.T, repo sourceStore, policy *policyclient.Client) *SourceService {
	t.Helper()
	cipher, err := newSecretCipher(testCipherKey)
	if err != nil {
		t.Fatalf("newSecretCipher: %v", err)
	}
	return NewSourceService(repo, NewRegistry(), cipher, policy, "/api/v1/webhooks/", silentLogger())
}

func newTestService(t *testing.T) (*Service, *testDeps) {
	t.Helper()
	repo := newMockRepository()
	cipher, err := newSecretCipher(testCipherKey)
	if err != nil {
		t.Fatalf("newSecretCipher: %v", err)
	}
	ps := newPolicyStub()
	as := newAgentsStub()
	t.Cleanup(func() {
		ps.server.Close()
		as.server.Close()
	})
	pc := policyclient.NewClient(ps.server.URL, func(context.Context) (string, error) { return "system-tok", nil }, silentLogger())
	ac := agentsclient.NewClient(as.server.URL, silentLogger())
	minter := &fakeMinter{token: "minted-token"}
	// nil metrics: instrumentation is injected, and its absence must leave
	// behavior identical. Metrics have their own test.
	admission := NewAdmission(repo, silentLogger())
	svc := NewService(repo, NewRegistry(), cipher, pc, ac, minter, admission, silentLogger(), nil)
	deps := &testDeps{repo: repo, policy: ps, agents: as, minter: minter, policyClnt: pc}
	deps.sources = newTestSourceService(t, repo, pc)
	deps.triggers = NewTriggerService(repo, pc, ac, minter, admission, silentLogger())
	return svc, deps
}

// InsertScheduledDelivery mirrors the Postgres unique (trigger_id, scheduled_at)
// index, which is the scheduler's concurrency control. It is on the Repository
// interface precisely so this can be faked: the service used to reach the
// concrete Postgres type for it, which made the behaviour untestable.
func (m *mockRepository) InsertScheduledDelivery(_ context.Context, d *Delivery) (bool, error) {
	if m.insertDeliveryErr != nil {
		return false, m.insertDeliveryErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.TriggerID != nil && d.ScheduledAt != nil {
		key := "sched|" + *d.TriggerID + "|" + d.ScheduledAt.UTC().Format(time.RFC3339)
		if m.seenDeliv[key] {
			return false, nil
		}
		m.seenDeliv[key] = true
	}
	cp := *d
	if cp.ReceivedAt.IsZero() {
		cp.ReceivedAt = time.Now()
	}
	m.deliveries = append(m.deliveries, &cp)
	return true, nil
}

// DeriveDeliveryStatus records that the service asked for a re-derive.
//
// It does not compute the status. Which outcome wins is a CASE expression, and
// TestPostgres_DeriveDeliveryStatus asserts it against the database; repeating
// the precedence here would only show this file agrees with itself. Counting the
// calls is what a service test needs, because a derive is the signal that an
// asynchronous fan-out has finished.
func (m *mockRepository) DeriveDeliveryStatus(_ context.Context, deliveryID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.derived == nil {
		m.derived = map[string]int{}
	}
	m.derived[deliveryID]++
	return nil
}

// derivedCount is how many times the service re-derived this delivery.
func (m *mockRepository) derivedCount(deliveryID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.derived[deliveryID]
}

// anyDerived reports whether any delivery has been re-derived, for tests that
// wait on an asynchronous fan-out without knowing the id up front.
func (m *mockRepository) anyDerived() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.derived) > 0
}

func (m *mockRepository) RecordSourceRejection(_ context.Context, sourceID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if src, ok := m.sources[sourceID]; ok {
		src.RejectionCount++
		now := time.Now()
		src.LastRejectionAt = &now
		src.LastRejectionReason = reason
	}
	return nil
}

func (m *mockRepository) RecordAccepted(_ context.Context, sourceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if src, ok := m.sources[sourceID]; ok {
		src.AcceptedCount++
	}
	return nil
}

// replayKeyRow is one remembered body fingerprint.
type replayKeyRow struct {
	providerDeliveryID string
	seenAt             time.Time
}

// ClaimReplayKey mirrors the SQL: within the window, the same body is fresh only
// under the delivery id it first arrived with (an ordinary provider retry).
func (m *mockRepository) ClaimReplayKey(_ context.Context, sourceID string, bodyHash []byte, providerDeliveryID string, window time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := sourceID + "|" + string(bodyHash)
	seen, ok := m.replayKeys[key]
	if ok && time.Since(seen.seenAt) < window && seen.providerDeliveryID != providerDeliveryID {
		return false, nil
	}
	m.replayKeys[key] = replayKeyRow{providerDeliveryID: providerDeliveryID, seenAt: time.Now()}
	return true, nil
}

func (m *mockRepository) PruneReplayKeys(_ context.Context, window time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for k, seen := range m.replayKeys {
		if time.Since(seen.seenAt) >= window {
			delete(m.replayKeys, k)
			n++
		}
	}
	return n, nil
}

func (m *mockRepository) CountRedeliveries(_ context.Context, originalID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, d := range m.deliveries {
		if d.RedeliveryOf != nil && *d.RedeliveryOf == originalID {
			n++
		}
	}
	return n, nil
}

// allDeliveries returns every persisted delivery. Used to assert that an
// unauthenticated request creates no rows at all.
func (m *mockRepository) allDeliveries() []*Delivery {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Delivery, len(m.deliveries))
	copy(out, m.deliveries)
	return out
}

// mockEvaluator stands in for the policy evaluator in controller tests.
//
// It embeds the interface so only Check is implemented: anything else the
// triggers package starts calling will panic here rather than silently return a
// zero value, which for an authorization component is the safer failure.
//
// It records the scope it was asked about, because the exact literal is the part
// most likely to be got wrong.
type mockEvaluator struct {
	policy.Evaluator

	allow bool
	err   error

	mu       sync.Mutex
	lastArgs policy.CheckInput
}

func (m *mockEvaluator) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	m.mu.Lock()
	m.lastArgs = in
	m.mu.Unlock()
	if m.err != nil {
		return policy.Result{}, m.err
	}
	return policy.Result{Allow: m.allow}, nil
}

// scopeAsked rebuilds the scope literal from the structured check input.
func (m *mockEvaluator) scopeAsked() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	parts := append([]string{m.lastArgs.ResourceType}, m.lastArgs.PathSegments...)
	return strings.Join(append(parts, m.lastArgs.Action), ":")
}

// DueScheduledMinutesWithoutDelivery mirrors the SQL: every minute in the window
// that has no delivery for this trigger.
func (m *mockRepository) DueScheduledMinutesWithoutDelivery(_ context.Context, triggerID string, since time.Time) ([]time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	seen := map[time.Time]bool{}
	for _, d := range m.deliveries {
		if d.TriggerID != nil && *d.TriggerID == triggerID && d.ScheduledAt != nil {
			seen[d.ScheduledAt.UTC().Truncate(time.Minute)] = true
		}
	}

	var out []time.Time
	for t := since.UTC().Truncate(time.Minute); t.Before(time.Now().UTC().Truncate(time.Minute)); t = t.Add(time.Minute) {
		if !seen[t] {
			out = append(out, t)
		}
	}
	return out, nil
}

// ─── Admission control ───────────────────────────────────────────────────────

// pendingRow is the mock's stand-in for a row in trigger_pending_deliveries,
// with the claimed flag the real table keeps as a nullable timestamp.
type pendingRow struct {
	PendingDelivery
	claimed bool
	// claimedAt mirrors the real table's nullable timestamp, which is what the
	// stale-claim reaper acts on: "claimed" alone cannot express "claimed by a
	// worker that died twenty minutes ago".
	claimedAt time.Time
}

func (m *mockRepository) EnqueuePending(_ context.Context, p *PendingDelivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	if cp.EnqueuedAt.IsZero() {
		cp.EnqueuedAt = time.Now()
	}
	m.pending = append(m.pending, &pendingRow{PendingDelivery: cp})
	return nil
}

// EnqueueWithinCeiling admits unless the test has said the store refuses.
//
// A stub rather than a reimplementation of the occupancy count: what a service
// test has to show is that a refusal is recorded as a dropped outcome, and for
// that the refusal only needs to happen. Whether the real statement refuses at
// the right moment is TestPostgres_EnqueueWithinCeilingRefusesAtTheCeiling's.
func (m *mockRepository) EnqueueWithinCeiling(_ context.Context, p *PendingDelivery, _ int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.admissionErr != nil {
		return false, m.admissionErr
	}
	if m.refuseAdmission {
		return false, nil
	}
	cp := *p
	if cp.EnqueuedAt.IsZero() {
		cp.EnqueuedAt = time.Now()
	}
	m.pending = append(m.pending, &pendingRow{PendingDelivery: cp})
	return true, nil
}

// ClaimPending hands back the oldest waiting rows, up to limit.
//
// It does not model the ceiling or the enabled check the real statement applies:
// which rows are claimable is decided by SQL, and asserting it here would only
// confirm this file agrees with itself. TestPostgres_StrandedClaimReleasesItsSlot
// and its neighbours cover that against the database.
func (m *mockRepository) ClaimPending(_ context.Context, limit int) ([]PendingDelivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []PendingDelivery
	for _, p := range m.pending {
		if len(out) >= limit {
			break
		}
		if p.claimed {
			continue
		}
		p.claimed = true
		p.claimedAt = time.Now()
		out = append(out, p.PendingDelivery)
	}
	return out, nil
}

// ReclaimStalePending mirrors the SQL: un-claim rows whose claim is older than
// the window, returning what was taken back.
func (m *mockRepository) ReclaimStalePending(_ context.Context, olderThan time.Duration) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-olderThan)
	var out []string
	for _, p := range m.pending {
		if p.claimed && !p.claimedAt.IsZero() && p.claimedAt.Before(cutoff) {
			p.claimed = false
			p.claimedAt = time.Time{}
			out = append(out, p.ID)
		}
	}
	return out, nil
}

func (m *mockRepository) ReleasePending(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = slices.DeleteFunc(m.pending, func(p *pendingRow) bool { return p.ID == id })
	return nil
}

// SupersedeAndEnqueue displaces the waiting rows and enqueues the replacement
// under one lock. The mock does both under its own mutex, which is enough for a
// service test to see the newest fire as the only one left; the transactional
// and concurrency guarantees are TestPostgres_SupersedeAndEnqueue's.
func (m *mockRepository) SupersedeAndEnqueue(_ context.Context, p *PendingDelivery) ([]PendingDelivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	superseded := m.removePendingLocked(func(row *pendingRow) bool {
		return row.TriggerID == p.TriggerID && !row.claimed
	})
	cp := *p
	if cp.EnqueuedAt.IsZero() {
		cp.EnqueuedAt = time.Now()
	}
	m.pending = append(m.pending, &pendingRow{PendingDelivery: cp})
	return superseded, nil
}

// DrainPending mirrors the SQL: only WAITING rows go. A claimed row belongs to a
// live dispatch that will record its own outcome.
func (m *mockRepository) DrainPending(_ context.Context, triggerID string) ([]PendingDelivery, error) {
	return m.removePending(func(p *pendingRow) bool { return p.TriggerID == triggerID && !p.claimed })
}

// DrainOrphanedPending returns whatever the test has staged as orphaned.
//
// Which waiting rows count as orphaned is a query over triggers that no longer
// exist or are switched off, and TestPostgres_DrainOrphanedPending covers it.
// The worker's job, which is what a service test is about, is to record what it
// is handed.
func (m *mockRepository) DrainOrphanedPending(_ context.Context, limit int) ([]OrphanedPending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.orphaned) > limit {
		out := m.orphaned[:limit]
		m.orphaned = m.orphaned[limit:]
		return out, nil
	}
	out := m.orphaned
	m.orphaned = nil
	return out, nil
}

func (m *mockRepository) TouchPendingClaim(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pending {
		if p.ID == id && p.claimed {
			p.claimedAt = time.Now()
		}
	}
	return nil
}

// TrimPending evicts the oldest rows beyond depth, matching the SQL's OFFSET on
// a newest-first ordering. depth counts the fire about to be enqueued, so the
// offset is depth-1.
func (m *mockRepository) TrimPending(_ context.Context, triggerID string, depth int) ([]PendingDelivery, error) {
	offset := max(depth-1, 0)
	m.mu.Lock()
	var waiting []*pendingRow
	for _, p := range m.pending {
		if p.TriggerID == triggerID && !p.claimed {
			waiting = append(waiting, p)
		}
	}
	slices.SortFunc(waiting, func(a, b *pendingRow) int { return b.EnqueuedAt.Compare(a.EnqueuedAt) })
	evict := map[string]bool{}
	for i, p := range waiting {
		if i >= offset {
			evict[p.ID] = true
		}
	}
	m.mu.Unlock()

	return m.removePending(func(p *pendingRow) bool { return evict[p.ID] })
}

func (m *mockRepository) removePending(match func(*pendingRow) bool) ([]PendingDelivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removePendingLocked(match), nil
}

// removePendingLocked is removePending's body, for callers that already hold
// the mutex (SupersedeAndEnqueue does its delete and insert under one lock).
func (m *mockRepository) removePendingLocked(match func(*pendingRow) bool) []PendingDelivery {
	var removed []PendingDelivery
	kept := m.pending[:0]
	for _, p := range m.pending {
		if match(p) {
			removed = append(removed, p.PendingDelivery)
			continue
		}
		kept = append(kept, p)
	}
	m.pending = kept
	return removed
}

// backdateClaim ages a row's claim, standing in for a worker that claimed it and
// then died: the row the reaper exists to take back.
func (m *mockRepository) backdateClaim(pendingID string, by time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pending {
		if p.ID == pendingID {
			p.claimedAt = time.Now().Add(-by)
		}
	}
}

func (m *mockRepository) pendingFor(triggerID string) []PendingDelivery {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []PendingDelivery
	for _, p := range m.pending {
		if p.TriggerID == triggerID {
			out = append(out, p.PendingDelivery)
		}
	}
	return out
}

// ─── Delivery safety ─────────────────────────────────────────────────────────

func (m *mockRepository) BumpConsecutiveFailures(_ context.Context, triggerID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.triggers[triggerID]
	if !ok {
		return 0, nil
	}
	t.ConsecutiveFailures++
	return t.ConsecutiveFailures, nil
}

func (m *mockRepository) ResetConsecutiveFailures(_ context.Context, triggerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.triggers[triggerID]; ok {
		t.ConsecutiveFailures = 0
	}
	return nil
}

func (m *mockRepository) AutoPause(_ context.Context, triggerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.triggers[triggerID]; ok {
		t.Enabled = false
		now := time.Now()
		t.AutoPausedAt = &now
	}
	return nil
}

func (m *mockRepository) GetRetention(_ context.Context, tenantID string) (int, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	days, ok := m.retention[tenantID]
	return days, ok, nil
}

func (m *mockRepository) SetRetention(_ context.Context, tenantID string, days int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retention[tenantID] = days
	return nil
}

// ObservedEventTypes mirrors the SQL: distinct non-empty event types this source
// has actually delivered, sorted.
func (m *mockRepository) ObservedEventTypes(_ context.Context, sourceID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	for _, d := range m.deliveries {
		if d.SourceID == sourceID && d.EventType != "" {
			seen[d.EventType] = true
		}
	}
	out := make([]string, 0, len(seen))
	for et := range seen {
		out = append(out, et)
	}
	slices.Sort(out)
	return out, nil
}

func (m *mockRepository) PendingStats(_ context.Context) (PendingStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var stats PendingStats
	oldest := time.Time{}
	for _, p := range m.pending {
		if p.claimed {
			stats.Claimed++
			continue
		}
		stats.Waiting++
		if oldest.IsZero() || p.EnqueuedAt.Before(oldest) {
			oldest = p.EnqueuedAt
		}
	}
	if !oldest.IsZero() {
		stats.OldestUnclaimedAge = time.Since(oldest)
	}
	return stats, nil
}

func (m *mockRepository) PurgePayloads(_ context.Context, defaultDays int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().AddDate(0, 0, -defaultDays)
	var n int64
	for _, d := range m.deliveries {
		if d.Payload != nil && d.ReceivedAt.Before(cutoff) {
			d.Payload = nil
			d.PayloadPurged = true
			n++
		}
	}
	return n, nil
}

// drainPending runs one worker tick synchronously.
//
// Dispatch is asynchronous by design — every fire is queued so it survives a
// restart — so a test that wants to observe the run must drain the queue. This
// keeps those tests end-to-end rather than stopping at "it was queued".
func drainPending(t *testing.T, svc *Service, repo *mockRepository) {
	t.Helper()
	claimed, err := repo.ClaimPending(context.Background(), 64)
	if err != nil {
		t.Fatalf("claim pending: %v", err)
	}
	for _, p := range claimed {
		svc.DispatchPending(context.Background(), p)
		if err := repo.ReleasePending(context.Background(), p.ID); err != nil {
			t.Fatalf("release pending: %v", err)
		}
	}
}
