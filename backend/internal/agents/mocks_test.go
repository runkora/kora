package agents

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/sessionmanager"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Compile-time interface checks.
var _ AgentTypeRepository = (*mockRepository)(nil)
var _ RunRepository = (*mockRepository)(nil)
var _ k8sProvisioner = (*mockK8s)(nil)
var _ smClient = (*mockSMClient)(nil)

// ── mockRepository ────────────────────────────────────────────────────────────

type mockRepository struct {
	mu         sync.Mutex
	agentTypes map[string]*AgentType
	slugIndex  map[string]*AgentType // tenantID+":"+slug → AgentType
	runs       map[string]*Run
	events     map[string][]RunEvent
	messages   []RunMessage
	nextSeqNo  int64

	createAgentTypeErr   error
	getAgentTypeErr      error
	listAgentTypesErr    error
	setAgentTypeStateErr error
	deleteAgentTypeErr   error
	createRunErr         error
	getRunErr            error
	listRunsErr          error
	setRunStateErr       error
	assignPodErr         error
	deleteRunErr         error
	activeRunsOverride   map[int]int
	activeRunsErr        error
	appendRunEventErr    error
	listRunEventsErr     error
	appendRunMessageErr  error
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		agentTypes: make(map[string]*AgentType),
		slugIndex:  make(map[string]*AgentType),
		runs:       make(map[string]*Run),
		events:     make(map[string][]RunEvent),
	}
}

func (m *mockRepository) CreateAgentType(_ context.Context, at AgentType) error {
	if m.createAgentTypeErr != nil {
		return m.createAgentTypeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := at.TenantID + ":" + at.Slug
	if _, ok := m.slugIndex[key]; ok {
		return ErrSlugTaken
	}
	cp := at
	m.agentTypes[at.ID] = &cp
	m.slugIndex[key] = &cp
	return nil
}

func (m *mockRepository) GetAgentType(_ context.Context, id string) (*AgentType, error) {
	if m.getAgentTypeErr != nil {
		return nil, m.getAgentTypeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	at, ok := m.agentTypes[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *at
	return &cp, nil
}

func (m *mockRepository) GetAgentTypeBySlug(_ context.Context, tenantID, slug string) (*AgentType, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	at, ok := m.slugIndex[tenantID+":"+slug]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *at
	return &cp, nil
}

func (m *mockRepository) ListAgentTypes(_ context.Context, tenantID string) ([]AgentType, error) {
	if m.listAgentTypesErr != nil {
		return nil, m.listAgentTypesErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []AgentType
	for _, at := range m.agentTypes {
		if at.TenantID == tenantID {
			out = append(out, *at)
		}
	}
	return out, nil
}

func (m *mockRepository) ListAllAgentTypeSlugs(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, at := range m.agentTypes {
		if !seen[at.Slug] {
			seen[at.Slug] = true
			out = append(out, at.Slug)
		}
	}
	return out, nil
}

func (m *mockRepository) UpdateAgentType(_ context.Context, at AgentType) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agentTypes[at.ID]; !ok {
		return ErrNotFound
	}
	cp := at
	m.agentTypes[at.ID] = &cp
	return nil
}

func (m *mockRepository) SetAgentTypeState(_ context.Context, id string, from, to AgentTypeState) error {
	if m.setAgentTypeStateErr != nil {
		return m.setAgentTypeStateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	at, ok := m.agentTypes[id]
	if !ok {
		return ErrNotFound
	}
	if at.State != from {
		return ErrConflict
	}
	at.State = to
	return nil
}

func (m *mockRepository) DeleteAgentType(_ context.Context, id string) error {
	if m.deleteAgentTypeErr != nil {
		return m.deleteAgentTypeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	at, ok := m.agentTypes[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.slugIndex, at.TenantID+":"+at.Slug)
	delete(m.agentTypes, id)
	return nil
}

func (m *mockRepository) CreateRun(_ context.Context, r Run) error {
	if m.createRunErr != nil {
		return m.createRunErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := r
	m.runs[r.ID] = &cp
	return nil
}

func (m *mockRepository) GetRun(_ context.Context, id string) (*Run, error) {
	if m.getRunErr != nil {
		return nil, m.getRunErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (m *mockRepository) ListRuns(_ context.Context, tenantID, agentTypeID string) ([]Run, error) {
	if m.listRunsErr != nil {
		return nil, m.listRunsErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Run
	for _, r := range m.runs {
		if r.TenantID == tenantID && (agentTypeID == "" || r.AgentTypeID == agentTypeID) {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *mockRepository) SetRunState(_ context.Context, id string, from, to RunState) error {
	if m.setRunStateErr != nil {
		return m.setRunStateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return ErrNotFound
	}
	if r.State != from {
		return ErrConflict
	}
	r.State = to
	return nil
}

func (m *mockRepository) AssignPod(_ context.Context, runID string, podOrdinal int) error {
	if m.assignPodErr != nil {
		return m.assignPodErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok {
		return ErrNotFound
	}
	r.PodOrdinal = podOrdinal
	r.State = RunStateStarting
	return nil
}

func (m *mockRepository) DeleteRun(_ context.Context, id string) error {
	if m.deleteRunErr != nil {
		return m.deleteRunErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[id]; !ok {
		return ErrNotFound
	}
	delete(m.runs, id)
	return nil
}

func (m *mockRepository) DeleteRunsByAgentType(_ context.Context, agentTypeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.runs {
		if r.AgentTypeID == agentTypeID {
			delete(m.runs, id)
		}
	}
	return nil
}

func (m *mockRepository) ActiveRunsPerPod(_ context.Context, agentTypeID string) (map[int]int, error) {
	if m.activeRunsErr != nil {
		return nil, m.activeRunsErr
	}
	if m.activeRunsOverride != nil {
		return m.activeRunsOverride, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := map[int]int{}
	for _, r := range m.runs {
		if r.AgentTypeID == agentTypeID && (r.State == RunStateActive || r.State == RunStateStarting) {
			counts[r.PodOrdinal]++
		}
	}
	return counts, nil
}

func (m *mockRepository) ReapStaleRuns(_ context.Context, staleDuration time.Duration) (failed, completed []string, err error) {
	cutoff := time.Now().Add(-staleDuration)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.UpdatedAt.IsZero() || r.UpdatedAt.After(cutoff) {
			continue
		}
		switch r.State {
		case RunStateActive, RunStateStarting:
			r.State = RunStateFailed
			failed = append(failed, r.ID)
		case RunStateIdle:
			r.State = RunStateCompleted
			completed = append(completed, r.ID)
		}
	}
	return failed, completed, nil
}

func (m *mockRepository) AppendRunEvent(_ context.Context, e RunEvent) (int64, error) {
	if m.appendRunEventErr != nil {
		return 0, m.appendRunEventErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextSeqNo++
	e.SequenceNo = m.nextSeqNo
	e.CreatedAt = time.Now()
	m.events[e.RunID] = append(m.events[e.RunID], e)
	return e.SequenceNo, nil
}

func (m *mockRepository) ListRunEvents(_ context.Context, runID string, afterSeqNo int64) ([]RunEvent, error) {
	if m.listRunEventsErr != nil {
		return nil, m.listRunEventsErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []RunEvent
	for _, e := range m.events[runID] {
		if e.SequenceNo > afterSeqNo {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *mockRepository) AppendRunMessage(_ context.Context, msg RunMessage) error {
	if m.appendRunMessageErr != nil {
		return m.appendRunMessageErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

// ── mockK8s ───────────────────────────────────────────────────────────────────

type mockK8s struct {
	mu                      sync.Mutex
	provisionedAgentTypes   []string
	deprovisionedAgentTypes []string
	deletedSandboxes        []string

	provisionAgentTypeErr   error
	deprovisionAgentTypeErr error
	replicaCount            int
	replicaCountErr         error
	sandboxEndpoint         string
	provisionSandboxErr     error
	deleteSandboxErr        error
}

func (m *mockK8s) ProvisionAgentType(_ context.Context, at AgentType, _ string) error {
	if m.provisionAgentTypeErr != nil {
		return m.provisionAgentTypeErr
	}
	m.mu.Lock()
	m.provisionedAgentTypes = append(m.provisionedAgentTypes, at.ID)
	m.mu.Unlock()
	return nil
}

func (m *mockK8s) DeprovisionAgentType(_ context.Context, at AgentType, _ string) error {
	if m.deprovisionAgentTypeErr != nil {
		return m.deprovisionAgentTypeErr
	}
	m.mu.Lock()
	m.deprovisionedAgentTypes = append(m.deprovisionedAgentTypes, at.ID)
	m.mu.Unlock()
	return nil
}

func (m *mockK8s) ReplicaCount(_ context.Context, _, _ string) (int, error) {
	if m.replicaCountErr != nil {
		return 0, m.replicaCountErr
	}
	if m.replicaCount == 0 {
		return 1, nil
	}
	return m.replicaCount, nil
}

func (m *mockK8s) ProvisionSandbox(_ context.Context, _ Run, _, _ string) (string, error) {
	if m.provisionSandboxErr != nil {
		return "", m.provisionSandboxErr
	}
	ep := m.sandboxEndpoint
	if ep == "" {
		ep = "10.0.0.1:50051"
	}
	return ep, nil
}

func (m *mockK8s) DeleteSandbox(_ context.Context, runID, _ string) error {
	if m.deleteSandboxErr != nil {
		return m.deleteSandboxErr
	}
	m.mu.Lock()
	m.deletedSandboxes = append(m.deletedSandboxes, runID)
	m.mu.Unlock()
	return nil
}

func (m *mockK8s) EnsureAgentGrant(_ context.Context, _ AgentType, _ string) error {
	return nil
}

func (m *mockK8s) WaitForStatefulSetReady(_ context.Context, _, _ string) error {
	return nil
}

// ── mockSMClient ──────────────────────────────────────────────────────────────

type mockSMClient struct {
	mu              sync.Mutex
	sessions        []sessionmanager.CreateSessionRequest
	sentMessages    []string
	closedSessions  []string
	interruptedRuns []string

	createSessionErr    error
	sendMessageErr      error
	interruptSessionErr error
	closeSessionErr     error
}

func (m *mockSMClient) CreateSession(_ context.Context, _ string, req sessionmanager.CreateSessionRequest) error {
	if m.createSessionErr != nil {
		return m.createSessionErr
	}
	m.mu.Lock()
	m.sessions = append(m.sessions, req)
	m.mu.Unlock()
	return nil
}

func (m *mockSMClient) SendMessage(_ context.Context, _, _, content string) error {
	if m.sendMessageErr != nil {
		return m.sendMessageErr
	}
	m.mu.Lock()
	m.sentMessages = append(m.sentMessages, content)
	m.mu.Unlock()
	return nil
}

func (m *mockSMClient) InterruptSession(_ context.Context, _, runID string) error {
	if m.interruptSessionErr != nil {
		return m.interruptSessionErr
	}
	m.mu.Lock()
	m.interruptedRuns = append(m.interruptedRuns, runID)
	m.mu.Unlock()
	return nil
}

func (m *mockSMClient) CloseSession(_ context.Context, _, runID string) error {
	if m.closeSessionErr != nil {
		return m.closeSessionErr
	}
	m.mu.Lock()
	m.closedSessions = append(m.closedSessions, runID)
	m.mu.Unlock()
	return nil
}

// ── test wiring helpers ───────────────────────────────────────────────────────

func newTestService(t *testing.T) (*testServiceFacade, *mockRepository, *mockK8s, *mockSMClient) {
	t.Helper()
	repo := newMockRepository()
	k8s := &mockK8s{}
	sm := &mockSMClient{}
	logger := logging.NewLoggerWithOutput(io.Discard)
	authz := NewAuthz(&allowAllEvaluator{})
	atSvc := NewAgentTypeService(
		repo, repo, k8s, authz,
		AgentTypeServiceConfig{
			MaxConcurrentRunsPerPod: 5,
			TenantNamespaceFn:       func(tenantID string) string { return "orbit-tenant-" + tenantID },
		},
		logger,
	)
	runSvc := NewRunService(
		repo, repo, sm, k8s, NewPubSub(), authz,
		RunServiceConfig{
			MaxConcurrentRunsPerPod: 5,
			TenantNamespaceFn:       func(tenantID string) string { return "orbit-tenant-" + tenantID },
		},
		logger,
	)
	return &testServiceFacade{at: atSvc, run: runSvc}, repo, k8s, sm
}

// withTenantCtx attaches a minimal PlatformClaims to ctx so that
// tenantFromContext returns the given tenant ID. Used by the test facade to
// preserve the pre-auth method shapes (which took tenantID as a parameter)
// without requiring every test to construct PlatformClaims manually.
func withTenantCtx(ctx context.Context, tenantID string) context.Context {
	return token.WithClaims(ctx, &token.PlatformClaims{
		Orbit: token.OrbitClaims{TenantID: tenantID},
	})
}

// withAgentCtx injects AGENT platform claims mirroring what AuthMiddleware
// stores for an identity-proxy-issued agent token: principal type AGENT, the
// logical tenant, and the agent slug (surfaced both as DisplayName and as the
// final segment of the SPIFFE path). Used by internal-callback handler tests.
func withAgentCtx(ctx context.Context, tenantID, slug string) context.Context {
	return token.WithClaims(ctx, &token.PlatformClaims{
		Orbit: token.OrbitClaims{
			PrincipalType: token.PrincipalTypeAgent,
			TenantID:      tenantID,
			DisplayName:   slug,
			Claims:        map[string]string{"spiffe_path": "/tenant/orbit-tenant-" + tenantID + "/agent/" + slug},
		},
	})
}

// allowAllEvaluator is a permissive stub policy.Evaluator used in tests that
// are not exercising authorization logic. Service-level authz tests should use
// stubEvaluator (defined in authz_test.go) which lets each test configure
// allow/deny per scope.
type allowAllEvaluator struct{}

func (allowAllEvaluator) Check(context.Context, policy.CheckInput) (policy.Result, error) {
	return policy.Result{Allow: true, Reason: "allow-all"}, nil
}

func (allowAllEvaluator) Query(_ context.Context, in policy.QueryInput) ([]string, error) {
	out := make([]string, 0, len(in.Candidates))
	for _, c := range in.Candidates {
		out = append(out, c.ID)
	}
	return out, nil
}

func (allowAllEvaluator) Scopes(context.Context, policy.ScopesInput) ([]string, error) {
	return nil, nil
}

func (allowAllEvaluator) ScopesExplain(context.Context, policy.ScopesInput) (*policy.ScopesExplainResult, error) {
	return &policy.ScopesExplainResult{}, nil
}

func (allowAllEvaluator) Start(context.Context) {}

// testServiceFacade wraps AgentTypeService + RunService to expose the same method set
// as the former god-object Service, so existing service_test.go tests need no changes.
type testServiceFacade struct {
	at  *AgentTypeService
	run *RunService
}

// ── AgentType delegation ──────────────────────────────────────────────────────
//
// Facade methods auto-inject claims into ctx so existing tests do not need to
// construct PlatformClaims manually. For methods taking a request struct with
// TenantID, the request value is used; for methods taking a resource ID, the
// resource's stored tenant is read from the repository and injected.

func (f *testServiceFacade) SetAgentTypeHook(fn func()) { f.at.SetAgentTypeHook(fn) }

func (f *testServiceFacade) CreateAgentType(ctx context.Context, req CreateAgentTypeRequest) (*AgentType, error) {
	return f.at.CreateAgentType(withTenantCtx(ctx, req.TenantID), req)
}

func (f *testServiceFacade) GetAgentType(ctx context.Context, id string) (*AgentType, error) {
	return f.at.GetAgentType(f.withAgentTypeTenant(ctx, id), id)
}

func (f *testServiceFacade) ListAgentTypes(ctx context.Context, tenantID string) ([]AgentType, error) {
	return f.at.ListAgentTypes(withTenantCtx(ctx, tenantID))
}

func (f *testServiceFacade) UpdateAgentType(ctx context.Context, req UpdateAgentTypeRequest) (*AgentType, error) {
	return f.at.UpdateAgentType(f.withAgentTypeTenant(ctx, req.ID), req)
}

func (f *testServiceFacade) DeleteAgentType(ctx context.Context, id string) error {
	return f.at.DeleteAgentType(f.withAgentTypeTenant(ctx, id), id)
}

// ── Run delegation ────────────────────────────────────────────────────────────

func (f *testServiceFacade) StartRun(ctx context.Context, req StartRunRequest) (*Run, error) {
	return f.run.StartRun(withTenantCtx(ctx, req.TenantID), req)
}

func (f *testServiceFacade) GetRun(ctx context.Context, id string) (*Run, error) {
	return f.run.GetRun(f.withRunTenant(ctx, id), id)
}

func (f *testServiceFacade) ListRuns(ctx context.Context, tenantID, agentTypeID string) ([]Run, error) {
	return f.run.ListRuns(withTenantCtx(ctx, tenantID), agentTypeID)
}

func (f *testServiceFacade) TerminateRun(ctx context.Context, id string) error {
	return f.run.TerminateRun(f.withRunTenant(ctx, id), id)
}

// withAgentTypeTenant returns ctx with claims for the agent type's stored
// tenant. Falls through with no claims if the agent type is missing — the
// service-layer call will then return ErrNotFound as expected by existing tests.
func (f *testServiceFacade) withAgentTypeTenant(ctx context.Context, id string) context.Context {
	at, err := f.at.repo.GetAgentType(ctx, id)
	if err != nil || at == nil {
		return ctx
	}
	return withTenantCtx(ctx, at.TenantID)
}

func (f *testServiceFacade) withRunTenant(ctx context.Context, id string) context.Context {
	run, err := f.run.repo.GetRun(ctx, id)
	if err != nil || run == nil {
		return ctx
	}
	return withTenantCtx(ctx, run.TenantID)
}

func (f *testServiceFacade) TransitionRunState(ctx context.Context, runID string, from, to RunState) error {
	return f.run.TransitionRunState(ctx, runID, from, to)
}

func (f *testServiceFacade) SendMessage(ctx context.Context, runID, content string) error {
	return f.run.SendMessage(f.withRunTenant(ctx, runID), runID, content)
}

func (f *testServiceFacade) InterruptRun(ctx context.Context, runID string) error {
	return f.run.InterruptRun(f.withRunTenant(ctx, runID), runID)
}

func (f *testServiceFacade) ProvisionSandbox(ctx context.Context, runID string) (string, error) {
	return f.run.ProvisionSandbox(ctx, runID)
}

func (f *testServiceFacade) PublishEvent(ctx context.Context, runID string, ev PushedEvent) {
	f.run.PublishEvent(ctx, runID, ev)
}

func (f *testServiceFacade) ListRunEvents(ctx context.Context, runID string, afterSeqNo int64) ([]RunEvent, error) {
	return f.run.ListRunEvents(ctx, runID, afterSeqNo)
}

func (f *testServiceFacade) SubscribeRun(runID string) (<-chan PushedEvent, func()) {
	return f.run.SubscribeRun(runID)
}

func (f *testServiceFacade) StartReaper(ctx context.Context) {
	f.run.StartReaper(ctx)
}

// seedAgentType inserts an active AgentType directly into the mock repo.
func seedAgentType(_ *testing.T, repo *mockRepository, at AgentType) AgentType {
	at.State = AgentTypeStateActive
	cp := at
	repo.agentTypes[at.ID] = &cp
	repo.slugIndex[at.TenantID+":"+at.Slug] = &cp
	return at
}

// seedRun inserts a Run directly into the mock repo. If the run's AgentTypeID
// does not yet have a corresponding AgentType in the repo, a minimal stub is
// auto-seeded so the authz layer's slug lookup succeeds. Tests that need a
// specific AgentType configuration should call seedAgentType first.
func seedRun(t *testing.T, repo *mockRepository, r Run) Run {
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now()
	}
	if _, ok := repo.agentTypes[r.AgentTypeID]; !ok && r.AgentTypeID != "" {
		seedAgentType(t, repo, AgentType{
			ID:       r.AgentTypeID,
			TenantID: r.TenantID,
			Slug:     "stub-" + r.AgentTypeID,
		})
	}
	cp := r
	repo.runs[r.ID] = &cp
	return r
}
