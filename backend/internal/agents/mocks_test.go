package agents

import (
	"context"
	"io"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/agents/sandbox/kubeconfig"
	"github.com/runkora/kora/backend/internal/platform/brand"
	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/token"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// usage holds seeded run totals by run id and usageCalls counts RunUsageTotals calls.
	usage      map[string]RunUsage
	usageCalls int
	envs       map[string]*Environment
	events     map[string][]RunEvent
	messages   []RunMessage
	nextSeqNo  int64

	createAgentTypeErr     error
	getAgentTypeErr        error
	listAgentTypesErr      error
	setAgentTypeStateErr   error
	deleteAgentTypeErr     error
	createRunErr           error
	getRunErr              error
	listRunsErr            error
	setRunStateErr         error
	setRunDelegationJTIErr error
	deleteRunErr           error
	activeRunsOverride     map[int]int
	// queued lists the runs QueueRun made claimable, and released counts ReleaseRunClaim calls.
	queued   []string
	released int
	// reclaims counts stale-claim sweeps.
	reclaims            int
	activeRunsErr       error
	appendRunEventErr   error
	listRunEventsErr    error
	appendRunMessageErr error

	outcomesScope RunScope

	// Run stats: the canned result and the arguments the service passed.
	statsResult  RunStats
	statsScope   RunScope
	getBySlugErr error
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		agentTypes: make(map[string]*AgentType),
		slugIndex:  make(map[string]*AgentType),
		runs:       make(map[string]*Run),
		envs:       make(map[string]*Environment),
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
	if m.getBySlugErr != nil {
		return nil, m.getBySlugErr
	}
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

func (m *mockRepository) ListActiveAgentTypes(context.Context) ([]AgentType, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []AgentType
	for _, at := range m.agentTypes {
		if at.State == AgentTypeStateActive {
			out = append(out, *at)
		}
	}
	return out, nil
}

func (m *mockRepository) ListAgentTypesByEnvironment(_ context.Context, tenantID, environmentID string) ([]AgentType, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []AgentType
	for _, at := range m.agentTypes {
		if at.TenantID != tenantID {
			continue
		}
		for _, e := range at.Environments {
			if e.EnvironmentID == environmentID {
				out = append(out, *at)
			}
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
	if to == AgentTypeStateActive {
		at.ActivatedAt = time.Now().UTC()
	}
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
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
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
	cp.IdentityMode = m.runIdentityModeLocked(r)
	// Postgres reads the initial message only when claiming a run.
	cp.InitialMessage = ""
	return &cp, nil
}

// runIdentityModeLocked mirrors the postgres agent_types join: run reads carry
// the agent type's identity mode. Callers must hold m.mu.
func (m *mockRepository) runIdentityModeLocked(r *Run) IdentityMode {
	if at, ok := m.agentTypes[r.AgentTypeID]; ok {
		return at.IdentityMode.OrDefault()
	}
	return r.IdentityMode.OrDefault()
}

// RunUsageTotals returns the seeded totals and counts the calls, so a test can
// assert a page costs one query.
func (m *mockRepository) RunUsageTotals(_ context.Context, _ string, runIDs []string) (map[string]RunUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usageCalls++
	out := map[string]RunUsage{}
	for _, id := range runIDs {
		if u, ok := m.usage[id]; ok {
			out[id] = u
		}
	}
	return out, nil
}

func (m *mockRepository) CountRunOutcomes(_ context.Context, _ string, scope RunScope, _, _ time.Time) (RunOutcomes, error) {
	m.outcomesScope = scope
	return RunOutcomes{}, nil
}

func (m *mockRepository) RunStats(_ context.Context, _ string, scope RunScope, _, _ time.Time) (RunStats, error) {
	m.statsScope = scope
	return m.statsResult, nil
}

// scopeMatches mirrors appendScope's agent-type and own-run clauses.
func scopeMatches(scope RunScope, r *Run) bool {
	if len(scope.OnlyAgentTypeIDs) > 0 && !slices.Contains(scope.OnlyAgentTypeIDs, r.AgentTypeID) {
		return false
	}
	own := scope.Own && scope.Vis.Subject != "" && r.CreatedBy == scope.Vis.Subject
	return own || slices.Contains(scope.AgentTypeIDs, r.AgentTypeID)
}

// ListRuns mirrors the SQL: scope (agent types and delegated visibility),
// states, the keyset cursor, ORDER BY created_at DESC, id DESC, and LIMIT.
func (m *mockRepository) ListRuns(_ context.Context, tenantID string, q RunListQuery) ([]Run, error) {
	if m.listRunsErr != nil {
		return nil, m.listRunsErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	vis := q.Scope.Vis
	var out []Run
	for _, r := range m.runs {
		if r.TenantID != tenantID || !scopeMatches(q.Scope, r) {
			continue
		}
		if len(q.States) > 0 && !slices.Contains(q.States, r.State) {
			continue
		}
		if q.StartedByTrigger && r.StartedBy == nil {
			continue
		}
		if !q.CreatedAfter.IsZero() && r.CreatedAt.Before(q.CreatedAfter) {
			continue
		}
		if !q.CreatedBefore.IsZero() && !r.CreatedAt.Before(q.CreatedBefore) {
			continue
		}
		if a := q.After; a != nil && (r.CreatedAt.After(a.CreatedAt) || (r.CreatedAt.Equal(a.CreatedAt) && r.ID >= a.ID)) {
			continue
		}
		mode := m.runIdentityModeLocked(r)
		if !vis.SeeAll && mode == IdentityModeDelegated && (vis.Subject == "" || r.CreatedBy != vis.Subject) {
			continue
		}
		cp := *r
		cp.IdentityMode = mode
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > q.Limit {
		out = out[:q.Limit]
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

// AssignPod ignores the claim token. TestPostgres_AssignPodFencedByClaim covers the fence.
func (m *mockRepository) AssignPod(_ context.Context, runID string, _ time.Time, podOrdinal int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok || r.State != RunStatePending {
		return ErrConflict
	}
	r.PodOrdinal = podOrdinal
	r.State = RunStateStarting
	return nil
}

// QueueRun records the run as queued.
func (m *mockRepository) QueueRun(_ context.Context, runID string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok || r.State != RunStatePending {
		return ErrConflict
	}
	m.queued = append(m.queued, runID)
	return nil
}

// ReleaseRunClaim returns the run to pending and counts the release.
func (m *mockRepository) ReleaseRunClaim(_ context.Context, id string, _, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok || (r.State != RunStatePending && r.State != RunStateStarting) {
		return ErrConflict
	}
	r.State, r.PodOrdinal = RunStatePending, -1
	m.released++
	return nil
}

// ClaimPendingRuns returns the queued runs that are still pending. It keeps no
// claim, so a run returned once comes back until it leaves pending.
func (m *mockRepository) ClaimPendingRuns(_ context.Context, limit int, _ time.Time) ([]Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Run
	for _, id := range m.queued {
		if r := m.runs[id]; r != nil && r.State == RunStatePending && len(out) < limit {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *mockRepository) ReclaimStaleRunClaims(context.Context, time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reclaims++
	return nil, nil
}

func (m *mockRepository) MarkRunFailed(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runs[id]; ok {
		r.State = RunStateFailed
	}
	return nil
}

// ListEnvironmentsByIDs returns the seeded environments of a tenant in the order of ids.
func (m *mockRepository) ListEnvironmentsByIDs(_ context.Context, tenantID string, ids []string) ([]Environment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Environment{}
	for _, id := range ids {
		if e, ok := m.envs[id]; ok && e.TenantID == tenantID {
			out = append(out, *e)
		}
	}
	return out, nil
}

// GetEnvironment returns a seeded environment within its tenant.
func (m *mockRepository) GetEnvironment(_ context.Context, tenantID, id string) (*Environment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.envs[id]
	if !ok || e.TenantID != tenantID {
		return nil, ErrNotFound
	}
	cp := *e
	return &cp, nil
}

func (m *mockRepository) SetRunEnvironment(_ context.Context, runID string, environmentID *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok {
		return ErrNotFound
	}
	r.EnvironmentID = environmentID
	return nil
}

func (m *mockRepository) SetRunDelegationJTI(_ context.Context, runID, jti string) error {
	if m.setRunDelegationJTIErr != nil {
		return m.setRunDelegationJTIErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mirror postgres: zero rows affected (run gone) is not an error.
	if r, ok := m.runs[runID]; ok {
		r.DelegationJTI = &jti
	}
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

func (m *mockRepository) DeleteRunsByAgentType(_ context.Context, agentTypeID string) ([]DeletedRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var deleted []DeletedRun
	for id, r := range m.runs {
		if r.AgentTypeID == agentTypeID {
			deleted = append(deleted, DeletedRun{ID: id, DelegationJTI: r.DelegationJTI})
			delete(m.runs, id)
		}
	}
	return deleted, nil
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
		// Recent events act as a liveness heartbeat (mirrors the NOT EXISTS clause in
		// PostgresRepository.ReapStaleRuns): an actively-streaming run is not reaped.
		recentEvent := false
		for _, e := range m.events[r.ID] {
			if e.CreatedAt.After(cutoff) {
				recentEvent = true
				break
			}
		}
		if recentEvent {
			continue
		}
		switch r.State {
		case RunStateActive, RunStateStarting, RunStatePending:
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
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
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

// SetRunSummary mirrors the real postgres set-once semantics: the first summary
// wins (WHERE summary IS NULL) and a missing run is a no-op returning nil
// (UPDATE affects 0 rows). Keep this in sync with PostgresRepository.SetRunSummary.
func (m *mockRepository) SetRunSummary(_ context.Context, runID, summary string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.runs {
		if r.ID == runID {
			if m.runs[i].Summary == nil {
				m.runs[i].Summary = &summary
			}
			return nil
		}
	}
	return nil
}

// ── mockK8s ───────────────────────────────────────────────────────────────────

type mockK8s struct {
	mu                      sync.Mutex
	provisionedAgentTypes   []string
	provisionedMissing      []string
	deprovisionedAgentTypes []string
	deletedSandboxes        []string
	rolledOutSlugs          []string
	// noReadyPods makes ReadyOrdinals report no Ready pod.
	noReadyPods bool

	provisionAgentTypeErr   error
	deprovisionAgentTypeErr error
	// readyPods is how many pods ReadyOrdinals reports. Zero means one.
	readyPods             int
	sandboxEndpoint       string
	lookupSandbox         Sandbox
	lookupFound           bool
	lookupErr             error
	provisionSandboxCalls int
	provisionKubeClusters []kubeconfig.Cluster
	provisionSpec         SandboxSpec
	provisionReused       bool
	// podEnv and podLive describe the run's sandbox pod for SandboxEnvironment.
	podEnv  string
	podLive bool
	// sandboxImages overrides the SandboxImages. Unset serves a valid kora-base.
	sandboxImages         map[string]*si.SandboxImage
	imageLookups          int
	provisionSandboxErr   error
	deleteSandboxErr      error
	rolloutStatefulSetErr error
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

// ProvisionMissing records a create-only provision of the agent type.
func (m *mockK8s) ProvisionMissing(_ context.Context, at AgentType, _ string) error {
	if m.provisionAgentTypeErr != nil {
		return m.provisionAgentTypeErr
	}
	m.mu.Lock()
	m.provisionedMissing = append(m.provisionedMissing, at.ID)
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

func (m *mockK8s) ReadyOrdinals(_ context.Context, _, _ string) ([]int, error) {
	if m.noReadyPods {
		return nil, nil
	}
	n := max(m.readyPods, 1)
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out, nil
}

func (m *mockK8s) ProvisionSandbox(_ context.Context, _ Run, _, _ string, spec SandboxSpec) (Sandbox, error) {
	m.provisionKubeClusters = spec.KubeClusters
	m.provisionSpec = spec
	m.provisionSandboxCalls++
	if m.provisionSandboxErr != nil {
		return Sandbox{}, m.provisionSandboxErr
	}
	ep := m.sandboxEndpoint
	if ep == "" {
		ep = "10.0.0.1:50051"
	}
	return Sandbox{Endpoint: ep, ExecToken: "test-exec-token", Created: !m.provisionReused}, nil
}

// validSandboxImage is a SandboxImage the operator accepted.
func validSandboxImage(name, image, digest string) *si.SandboxImage {
	return &si.SandboxImage{
		ObjectMeta: metav1.ObjectMeta{Name: name, Generation: 1},
		Spec:       si.Spec{Image: image},
		Status: si.Status{Image: image, Digest: digest, ObservedGeneration: 1, Conditions: []metav1.Condition{
			{Type: si.ConditionValid, Status: metav1.ConditionTrue, Reason: "Validated"},
		}},
	}
}

func (m *mockK8s) SandboxImage(_ context.Context, _, name string) (*si.SandboxImage, error) {
	m.imageLookups++
	if m.sandboxImages == nil && name == si.BaseName {
		return validSandboxImage(si.BaseName, "ghcr.io/runkora/kora-sandbox-base:1", "sha256:base"), nil
	}
	img, ok := m.sandboxImages[name]
	if !ok {
		return nil, ErrNotFound
	}
	return img, nil
}

func (m *mockK8s) SandboxImages(_ context.Context, _ string) ([]si.SandboxImage, error) {
	out := make([]si.SandboxImage, 0, len(m.sandboxImages))
	for _, img := range m.sandboxImages {
		out = append(out, *img)
	}
	return out, nil
}

func (m *mockK8s) LookupSandbox(_ context.Context, _, _ string) (Sandbox, bool, error) {
	if m.lookupErr != nil {
		return Sandbox{}, false, m.lookupErr
	}
	return m.lookupSandbox, m.lookupFound, nil
}

func (m *mockK8s) SandboxEnvironment(_ context.Context, _, _ string) (string, bool, error) {
	return m.podEnv, m.podLive, nil
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

func (m *mockK8s) GetAgentBudget(_ context.Context, _ AgentType, _ string) (*AgentBudget, error) {
	return nil, nil
}

func (m *mockK8s) EnsureAgentGrant(_ context.Context, _ AgentType, _ string) error {
	return nil
}

func (m *mockK8s) WaitForStatefulSetReady(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockK8s) RolloutStatefulSet(_ context.Context, slug, _ string) error {
	if m.rolloutStatefulSetErr != nil {
		return m.rolloutStatefulSetErr
	}
	m.mu.Lock()
	m.rolledOutSlugs = append(m.rolledOutSlugs, slug)
	m.mu.Unlock()
	return nil
}

// ── mockSMClient ──────────────────────────────────────────────────────────────

type mockSMClient struct {
	mu               sync.Mutex
	sessions         []sessionmanager.CreateSessionRequest
	sentMessages     []string
	sentIdleTimeouts []int
	sentFromPerson   []bool
	closedSessions   []string
	interruptedRuns  []string

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

func (m *mockSMClient) SendMessage(_ context.Context, _, _, content string, idleTimeoutSecs int, fromPerson bool) error {
	if m.sendMessageErr != nil {
		return m.sendMessageErr
	}
	m.mu.Lock()
	m.sentMessages = append(m.sentMessages, content)
	m.sentIdleTimeouts = append(m.sentIdleTimeouts, idleTimeoutSecs)
	m.sentFromPerson = append(m.sentFromPerson, fromPerson)
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
	return newTestServiceWithEval(t, &allowAllEvaluator{})
}

func newTestServiceWithEval(t *testing.T, eval policy.Evaluator) (*testServiceFacade, *mockRepository, *mockK8s, *mockSMClient) {
	t.Helper()
	repo := newMockRepository()
	k8s := &mockK8s{}
	sm := &mockSMClient{}
	logger := logging.NewLoggerWithOutput(io.Discard)
	authz := NewAuthz(eval)
	atSvc := NewAgentTypeService(
		repo, repo, k8s, authz,
		AgentTypeServiceConfig{
			MaxConcurrentRunsPerPod: 5,
			TenantNamespaceFn:       func(tenantID string) string { return "orbit-tenant-" + tenantID },
		},
		logger,
	)
	runSvc, err := NewRunService(
		repo, repo, sm, k8s, NewPubSub(), authz, repo, k8s, k8s, repo,
		RunServiceConfig{
			DefaultEnvironmentSize:  testSizes.Default,
			MaxConcurrentRunsPerPod: 5,
			TenantNamespaceFn:       func(tenantID string) string { return "orbit-tenant-" + tenantID },
			SandboxGuardrails:       SandboxGuardrails{ProvisionAttempts: 2, ProvisionBackoffSecs: 1, ProvisionBudgetSecs: 60},
			Brand:                   brand.MustNew("kora"),
		},
		logger,
	)
	if err != nil {
		t.Fatal(err)
	}
	atSvc.SetEnvironmentLookup(repo)
	atSvc.SetCallerScopes(broadScopes)
	return &testServiceFacade{at: atSvc, run: runSvc}, repo, k8s, sm
}

// testSizes are the environment sizes the test services run with.
var testSizes = EnvironmentSizes{
	Default: EnvironmentSize{CPU: "2", Memory: "4Gi", Disk: "10Gi"},
	Max:     EnvironmentSize{CPU: "8", Memory: "16Gi", Disk: "50Gi"},
}

// withTenantCtx attaches a minimal PlatformClaims to ctx so that
// tenantFromContext returns the given tenant ID. Used by the test facade to
// preserve the pre-auth method shapes (which took tenantID as a parameter)
// without requiring every test to construct PlatformClaims manually.
func withTenantCtx(ctx context.Context, tenantID string) context.Context {
	return token.WithClaims(ctx, &token.PlatformClaims{
		Kora: token.KoraClaims{TenantID: tenantID},
	})
}

// withAgentCtx injects AGENT platform claims mirroring what AuthMiddleware
// stores for an identity-proxy-issued agent token: principal type AGENT, the
// logical tenant, and the agent slug (surfaced both as DisplayName and as the
// final segment of the SPIFFE path). Used by internal-callback handler tests.
func withAgentCtx(ctx context.Context, tenantID, slug string) context.Context {
	return token.WithClaims(ctx, &token.PlatformClaims{
		Kora: token.KoraClaims{
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

func (allowAllEvaluator) CheckBudget(context.Context, string, policy.BudgetCheckRequest) (policy.BudgetResult, error) {
	return policy.BudgetResult{Allow: true}, nil
}

func (allowAllEvaluator) GetTenantBudget(context.Context, string) (*policy.TenantBudget, error) {
	return nil, nil
}

func (allowAllEvaluator) GetBudgetCategories(context.Context, string, string) ([]policy.BudgetCategory, error) {
	return nil, nil
}

func (allowAllEvaluator) GetRoleBudgets(context.Context, string, []string) ([]policy.BudgetCategory, error) {
	return nil, nil
}

func (allowAllEvaluator) ListBudgets(context.Context, string) (map[string]policy.PrincipalBudget, map[string]policy.RoleBudget, error) {
	return map[string]policy.PrincipalBudget{}, map[string]policy.RoleBudget{}, nil
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

// StartAndSchedule is startAndSchedule with the request's tenant in ctx.
func (f *testServiceFacade) StartAndSchedule(ctx context.Context, req StartRunRequest) (*Run, error) {
	return startAndSchedule(withTenantCtx(ctx, req.TenantID), f.run, req)
}

// startAndSchedule starts a run, claims it and makes one scheduler attempt on it,
// which is what a RunScheduler does on its next poll. It returns the run as stored after the attempt.
func startAndSchedule(ctx context.Context, svc *RunService, req StartRunRequest) (*Run, error) {
	run, err := svc.StartRun(ctx, req)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	claimed, err := svc.queue.ClaimPendingRuns(ctx, 100, now)
	if err != nil {
		return nil, err
	}
	for _, c := range claimed {
		svc.startClaimed(ctx, c, now)
	}
	return svc.repo.GetRun(ctx, run.ID)
}

// claimRun returns a seeded run and a claim time, as the scheduler would hold them.
func claimRun(repo *mockRepository, id string) (Run, time.Time) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return *repo.runs[id], time.Now()
}

func (f *testServiceFacade) GetRun(ctx context.Context, id string) (*Run, error) {
	return f.run.GetRun(f.withRunTenant(ctx, id), id)
}

func (f *testServiceFacade) GetRunWithUsage(ctx context.Context, id string) (*Run, error) {
	return f.run.GetRunWithUsage(f.withRunTenant(ctx, id), id)
}

func (f *testServiceFacade) ListRuns(ctx context.Context, tenantID, agentTypeID string) ([]Run, error) {
	var ids []string
	if agentTypeID != "" {
		ids = []string{agentTypeID}
	}
	page, err := f.run.ListRuns(withTenantCtx(ctx, tenantID), ids, RunListQuery{Limit: maxRunsLimit})
	return page.Runs, err
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
	return f.run.SendMessage(f.withRunTenant(ctx, runID), runID, SendMessageRequest{Content: content})
}

func (f *testServiceFacade) InterruptRun(ctx context.Context, runID string) error {
	return f.run.InterruptRun(f.withRunTenant(ctx, runID), runID)
}

func (f *testServiceFacade) ProvisionSandbox(ctx context.Context, runID string) (Sandbox, error) {
	return f.run.ProvisionSandbox(ctx, runID, "", false)
}

func (f *testServiceFacade) PublishEvent(ctx context.Context, runID string, ev PushedEvent) error {
	return f.run.PublishEvent(ctx, runID, ev)
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
