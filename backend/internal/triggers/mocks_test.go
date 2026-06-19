package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
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
	mu         sync.Mutex
	sources    map[string]*WebhookSource
	triggers   map[string]*Trigger
	deliveries []*Delivery
	seenDeliv  map[string]bool // source_id|provider_delivery_id → seen (dedup)

	createSourceErr   error
	createTriggerErr  error
	insertDeliveryErr error
	listTriggersErr   error
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		sources:   map[string]*WebhookSource{},
		triggers:  map[string]*Trigger{},
		seenDeliv: map[string]bool{},
	}
}

func (m *mockRepository) CreateSource(_ context.Context, s *WebhookSource) error {
	if m.createSourceErr != nil {
		return m.createSourceErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
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
	if _, ok := m.sources[s.ID]; !ok {
		return ErrSourceNotFound
	}
	cp := *s
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
	return &cp, nil
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
			out = append(out, *t)
		}
	}
	return out, nil
}

func (m *mockRepository) ListEnabledTriggersForSource(_ context.Context, sourceID string) ([]Trigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Trigger
	for _, t := range m.triggers {
		if t.SourceID == sourceID && t.Enabled {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (m *mockRepository) UpdateTrigger(_ context.Context, t *Trigger) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.triggers[t.ID]; !ok {
		return ErrTriggerNotFound
	}
	cp := *t
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
	if d.ProviderDeliveryID != "" {
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
			d.Status = status
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
}

type agentsStub struct {
	server *httptest.Server

	mu     sync.Mutex
	calls  []startCall
	status int // run-start response status; 0 → 201

	// agent-type resolution (GET /api/v1/agent-types/{id}).
	atSlug   string // slug to return; "" → "code-reviewer"
	atTenant string // tenant to return; "" → testTenant
	atStatus int    // response status; 0 → 200
}

func newAgentsStub() *agentsStub {
	as := &agentsStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agent-types/{id}", func(w http.ResponseWriter, r *http.Request) {
		as.mu.Lock()
		status, slug, tenant := as.atStatus, as.atSlug, as.atTenant
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
			fmt.Fprintf(w, `{"id":%q,"tenant_id":%q,"slug":%q}`, r.PathValue("id"), tenant, slug)
		}
	})
	mux.HandleFunc("POST /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			AgentTypeID    string `json:"agent_type_id"`
			InitialMessage string `json:"initial_message"`
		}
		_ = json.Unmarshal(body, &req)
		as.mu.Lock()
		as.calls = append(as.calls, startCall{
			bearer:         r.Header.Get("Authorization"),
			agentTypeID:    req.AgentTypeID,
			initialMessage: req.InitialMessage,
		})
		status := as.status
		as.mu.Unlock()
		if status == 0 {
			status = http.StatusCreated
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status < 300 {
			_, _ = w.Write([]byte(`{"id":"run-1","state":"active"}`))
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
	repo   *mockRepository
	policy *policyStub
	agents *agentsStub
	minter *fakeMinter
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
	svc := NewService(repo, NewRegistry(), cipher, pc, ac, minter, "/api/v1/webhooks/", silentLogger())
	return svc, &testDeps{repo: repo, policy: ps, agents: as, minter: minter}
}
