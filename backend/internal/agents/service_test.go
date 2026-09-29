package agents

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/sessionmanager"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// waitFor polls check every 5 ms until it returns true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

// ── CreateAgentType ───────────────────────────────────────────────────────────

func TestCreateAgentType_HappyPath(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	ctx := context.Background()

	at, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{
		TenantID: "t-1",
		Name:     "My Bot",
		Slug:     "my-bot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// CreateAgentType returns immediately with provisioning state.
	if at.State != AgentTypeStateProvisioning {
		t.Errorf("returned state = %q, want provisioning", at.State)
	}
	if at.ID == "" {
		t.Error("ID must be set")
	}

	// Background goroutine transitions to active.
	waitFor(t, time.Second, func() bool {
		stored, _ := repo.GetAgentType(ctx, at.ID)
		return stored != nil && stored.State == AgentTypeStateActive
	})
	k8s.mu.Lock()
	provisioned := len(k8s.provisionedAgentTypes)
	k8s.mu.Unlock()
	if provisioned != 1 {
		t.Errorf("expected 1 k8s provision call, got %d", provisioned)
	}
}

func TestDeriveInferenceScopes(t *testing.T) {
	cases := map[string][]string{
		"anthropic/claude-sonnet-4-6": {"inference:invoke", "inference:anthropic:claude-sonnet-4-6:use"},
		"":                            {"inference:invoke"},
		"bare":                        {"inference:invoke"},
		"too/many/parts":              {"inference:invoke"},
	}
	for model, want := range cases {
		if got := deriveInferenceScopes(model); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("deriveInferenceScopes(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestReconcileInferenceScopes_SwapsModelScope(t *testing.T) {
	old := "anthropic/claude-sonnet-4-6"
	prior := append([]string{"connection:github:repo:read"}, deriveInferenceScopes(old)...)
	next := "google/gemini-flash"
	got := reconcileInferenceScopes(prior, &next)
	want := []string{"connection:github:repo:read", "inference:invoke", "inference:google:gemini-flash:use"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("reconcileInferenceScopes swapped wrong: got %v, want %v", got, want)
	}
}

func TestMCPToolScopes(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"none", nil, nil},
		{"one tool derives the coarse gate", []string{"mcp:orbit:agents:list:use"}, []string{"mcp:orbit:agents:list:use", "mcp:invoke"}},
		{"many tools, one gate", []string{"mcp:orbit:agents:list:use", "mcp:orbit:models:list:use"}, []string{"mcp:orbit:agents:list:use", "mcp:orbit:models:list:use", "mcp:invoke"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AgentType{MCPTools: tc.in}.MCPToolScopes()
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("MCPToolScopes(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCreateAgentType_DerivesInferenceScopesFromModel(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	model := "anthropic/claude-sonnet-4-6"
	at, err := svc.CreateAgentType(context.Background(), CreateAgentTypeRequest{
		TenantID: "t-1", Name: "Bot", Slug: "bot", ModelID: &model,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "inference:invoke,inference:anthropic:claude-sonnet-4-6:use"
	if strings.Join(at.GrantScopes, ",") != want {
		t.Errorf("derived grant_scopes = %v, want [%s]", at.GrantScopes, want)
	}
}

func TestCreateAgentType_ExplicitInferenceScopeSuppressesDerivation(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	model := "anthropic/claude-sonnet-4-6"
	at, err := svc.CreateAgentType(context.Background(), CreateAgentTypeRequest{
		TenantID: "t-1", Name: "Bot", Slug: "bot", ModelID: &model,
		GrantScopes: []string{"inference:anthropic:use"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(at.GrantScopes, ",") != "inference:anthropic:use" {
		t.Errorf("explicit inference scope should suppress derivation; got %v", at.GrantScopes)
	}
}

func TestCreateAgentType_DefaultsFrameworkAndMaxRuns(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	at, err := svc.CreateAgentType(context.Background(), CreateAgentTypeRequest{
		TenantID: "t-1",
		Name:     "Bot",
		Slug:     "bot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if at.Framework != AgentFrameworkClaudeCode {
		t.Errorf("framework = %q, want claude-code", at.Framework)
	}
	if at.MaxConcurrentRunsPerPod != 5 {
		t.Errorf("MaxConcurrentRunsPerPod = %d, want 5 (from ServiceConfig)", at.MaxConcurrentRunsPerPod)
	}
}

func TestCreateAgentType_PreservesExplicitFrameworkAndMaxRuns(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	at, err := svc.CreateAgentType(context.Background(), CreateAgentTypeRequest{
		TenantID:                "t-1",
		Name:                    "Codex Bot",
		Slug:                    "codex-bot",
		Framework:               AgentFrameworkCodex,
		MaxConcurrentRunsPerPod: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if at.Framework != AgentFrameworkCodex {
		t.Errorf("framework = %q, want codex", at.Framework)
	}
	if at.MaxConcurrentRunsPerPod != 2 {
		t.Errorf("MaxConcurrentRunsPerPod = %d, want 2", at.MaxConcurrentRunsPerPod)
	}
}

func TestCreateAgentType_SlugTaken(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()

	req := CreateAgentTypeRequest{TenantID: "t-1", Name: "Bot", Slug: "dup"}
	if _, err := svc.CreateAgentType(ctx, req); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := svc.CreateAgentType(ctx, req)
	if !errors.Is(err, ErrSlugTaken) {
		t.Errorf("expected ErrSlugTaken, got %v", err)
	}
}

func TestCreateAgentType_K8sFailure(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	k8s.provisionAgentTypeErr = errors.New("k8s unavailable")

	ctx := context.Background()
	at, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{
		TenantID: "t-1", Name: "Bot", Slug: "bot",
	})
	// Provisioning is async: CreateAgentType now succeeds immediately.
	if err != nil {
		t.Fatalf("unexpected error from CreateAgentType: %v", err)
	}
	// Background goroutine should delete the record on k8s failure.
	waitFor(t, time.Second, func() bool {
		_, err := repo.GetAgentType(ctx, at.ID)
		return errors.Is(err, ErrNotFound)
	})
}

// TestCreateAgentType_K8sFailure_DeprovisionCalled asserts that an async
// provisioning failure triggers DeprovisionAgentType so partial k8s state
// (StatefulSet, Service, HPA, …) is cleaned up, not just the DB row. This
// prevents the orphan-resource bug we hit with sales-agent in dev.
func TestCreateAgentType_K8sFailure_DeprovisionCalled(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	k8s.provisionAgentTypeErr = errors.New("late-step failure (e.g. agent grant)")

	ctx := context.Background()
	at, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{
		TenantID: "t-1", Name: "Bot", Slug: "bot",
	})
	if err != nil {
		t.Fatalf("unexpected error from CreateAgentType: %v", err)
	}

	// Wait for the background goroutine to run the rollback path.
	waitFor(t, time.Second, func() bool {
		_, err := repo.GetAgentType(ctx, at.ID)
		return errors.Is(err, ErrNotFound)
	})

	// DeprovisionAgentType must have been invoked on the same agent type.
	k8s.mu.Lock()
	deprovisioned := append([]string(nil), k8s.deprovisionedAgentTypes...)
	k8s.mu.Unlock()
	if len(deprovisioned) != 1 || deprovisioned[0] != at.ID {
		t.Errorf("expected DeprovisionAgentType to be called once for %s, got %v", at.ID, deprovisioned)
	}
}

func TestCreateAgentType_FiresHook(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	var mu sync.Mutex
	called := 0
	svc.SetAgentTypeHook(func() {
		mu.Lock()
		called++
		mu.Unlock()
	})

	if _, err := svc.CreateAgentType(context.Background(), CreateAgentTypeRequest{
		TenantID: "t-1", Name: "Bot", Slug: "bot",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Hook fires in the background goroutine.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return called == 1
	})
}

// ── DeleteAgentType ───────────────────────────────────────────────────────────

func TestDeleteAgentType_HappyPath(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)

	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})

	called := 0
	svc.SetAgentTypeHook(func() { called++ })

	if err := svc.DeleteAgentType(context.Background(), at.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(k8s.deprovisionedAgentTypes) != 1 {
		t.Errorf("expected 1 k8s deprovision call, got %d", len(k8s.deprovisionedAgentTypes))
	}
	if _, err := repo.GetAgentType(context.Background(), at.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("agent type should be gone, got err=%v", err)
	}
	if called != 1 {
		t.Errorf("hook called %d times, want 1", called)
	}
}

func TestDeleteAgentType_AlreadyDeleting_SkipsStateTransition(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)

	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	// Force state to deleting directly so SetAgentTypeState would conflict.
	repo.agentTypes[at.ID].State = AgentTypeStateDeleting
	repo.slugIndex["t-1:bot"].State = AgentTypeStateDeleting

	if err := svc.DeleteAgentType(context.Background(), at.ID); err != nil {
		t.Fatalf("retry of stuck deletion should succeed: %v", err)
	}
	if len(k8s.deprovisionedAgentTypes) != 1 {
		t.Error("k8s should still be deprovisioned")
	}
}

func TestDeleteAgentType_NotFound(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	err := svc.DeleteAgentType(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing agent type")
	}
}

// ── GetRun / ListRuns ─────────────────────────────────────────────────────────

func TestGetRun_Found(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	got, err := svc.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != run.ID {
		t.Errorf("ID = %q, want %q", got.ID, run.ID)
	}
}

func TestGetRun_NotFound(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	_, err := svc.GetRun(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListRuns_FiltersByTenant(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})
	seedRun(t, repo, Run{ID: "run-2", TenantID: "t-2", AgentTypeID: "at-1", State: RunStateActive})

	runs, err := svc.ListRuns(context.Background(), "t-1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run-1" {
		t.Errorf("expected only t-1's run, got %+v", runs)
	}
}

// ── PublishEvent / SubscribeRun ───────────────────────────────────────────────

func TestPublishEvent_DeliveredToSubscriber(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	ch, cancel := svc.SubscribeRun("run-1")
	defer cancel()

	svc.PublishEvent(context.Background(), "run-1", PushedEvent{Type: "text_delta"})

	select {
	case ev := <-ch:
		if ev.Type != "text_delta" {
			t.Errorf("event type = %q, want text_delta", ev.Type)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestPublishEvent_WrongRunID_NotDelivered(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	ch, cancel := svc.SubscribeRun("run-A")
	defer cancel()

	svc.PublishEvent(context.Background(), "run-B", PushedEvent{Type: "text_delta"})

	select {
	case <-ch:
		t.Error("event for run-B should not reach run-A subscriber")
	default:
	}
}

// ── GetAgentType / ListAgentTypes ─────────────────────────────────────────────

func TestGetAgentType_Found(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})

	got, err := svc.GetAgentType(context.Background(), at.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != at.ID {
		t.Errorf("ID = %q, want %q", got.ID, at.ID)
	}
}

func TestGetAgentType_NotFound(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	_, err := svc.GetAgentType(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListAgentTypes_FiltersByTenant(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot-a"})
	seedAgentType(t, repo, AgentType{ID: "at-2", TenantID: "t-2", Slug: "bot-b"})

	types, err := svc.ListAgentTypes(context.Background(), "t-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(types) != 1 || types[0].ID != "at-1" {
		t.Errorf("expected only t-1's agent type, got %+v", types)
	}
}

// ── StartRun ──────────────────────────────────────────────────────────────────

func TestStartRun_HappyPath(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{
		ID: "at-1", TenantID: "t-1", Slug: "bot",
		Framework:               AgentFrameworkClaudeCode,
		SystemPrompt:            "be helpful",
		MaxConcurrentRunsPerPod: 5,
	})

	run, err := svc.StartRun(context.Background(), StartRunRequest{
		TenantID:    "t-1",
		AgentTypeID: at.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.State != RunStateActive {
		t.Errorf("run state = %q, want active", run.State)
	}
	if run.AgentTypeID != at.ID {
		t.Errorf("AgentTypeID = %q, want %q", run.AgentTypeID, at.ID)
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if len(sm.sessions) != 1 {
		t.Fatalf("expected 1 session-manager call, got %d", len(sm.sessions))
	}
	if sm.sessions[0].SystemPrompt != "be helpful" {
		t.Errorf("SystemPrompt not forwarded: %q", sm.sessions[0].SystemPrompt)
	}
	if sm.sessions[0].Framework != string(AgentFrameworkClaudeCode) {
		t.Errorf("Framework not forwarded: got %q, want %q", sm.sessions[0].Framework, AgentFrameworkClaudeCode)
	}
}

// Pre-provisioning is forwarded to the session-manager, the only component that
// writes the sandbox descriptor. A pod warmed without one sits idle.
func TestStartRun_ForwardsProvisionSandbox(t *testing.T) {
	for _, requested := range []bool{true, false} {
		t.Run(strconv.FormatBool(requested), func(t *testing.T) {
			svc, repo, _, sm := newTestService(t)
			at := seedAgentType(t, repo, AgentType{
				ID: "at-1", TenantID: "t-1", Slug: "bot",
				Framework:               AgentFrameworkClaudeCode,
				MaxConcurrentRunsPerPod: 5,
			})

			if _, err := svc.StartRun(context.Background(), StartRunRequest{
				TenantID:         "t-1",
				AgentTypeID:      at.ID,
				ProvisionSandbox: requested,
			}); err != nil {
				t.Fatalf("StartRun: %v", err)
			}

			sm.mu.Lock()
			defer sm.mu.Unlock()
			if len(sm.sessions) != 1 {
				t.Fatalf("expected 1 session-manager call, got %d", len(sm.sessions))
			}
			if got := sm.sessions[0].ProvisionSandbox; got != requested {
				t.Errorf("ProvisionSandbox forwarded as %v, want %v", got, requested)
			}
		})
	}
}

func TestStartRun_FailedStartMarksRunFailed(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{
		ID: "at-1", TenantID: "t-1", Slug: "bot",
		Framework: AgentFrameworkClaudeCode, MaxConcurrentRunsPerPod: 5,
	})
	sm.createSessionErr = errors.New("session-manager unreachable")

	if _, err := svc.StartRun(context.Background(), StartRunRequest{TenantID: "t-1", AgentTypeID: at.ID}); err == nil {
		t.Fatal("expected StartRun to fail when the session cannot be created")
	}

	// The created run must be marked failed, not left stuck in starting.
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.runs) != 1 {
		t.Fatalf("expected 1 run row, got %d", len(repo.runs))
	}
	for _, r := range repo.runs {
		if r.State != RunStateFailed {
			t.Errorf("run left in %q, want failed", r.State)
		}
	}
}

func TestStartRun_TenantMismatch(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})

	_, err := svc.StartRun(context.Background(), StartRunRequest{
		TenantID:    "t-other",
		AgentTypeID: at.ID,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for tenant mismatch, got %v", err)
	}
}

func TestStartRun_AllPodsAtCapacity(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{
		ID: "at-1", TenantID: "t-1", Slug: "bot",
		MaxConcurrentRunsPerPod: 2,
	})
	k8s.replicaCount = 1
	// Override active runs so pod 0 is at capacity.
	repo.activeRunsOverride = map[int]int{0: 2}

	_, err := svc.StartRun(context.Background(), StartRunRequest{
		TenantID:    "t-1",
		AgentTypeID: at.ID,
	})
	if err == nil {
		t.Fatal("expected error when all pods at capacity")
	}
}

func TestStartRun_SelectsLeastLoadedPod(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{
		ID: "at-1", TenantID: "t-1", Slug: "bot",
		MaxConcurrentRunsPerPod: 5,
	})
	k8s.replicaCount = 3
	// Pod 1 has the fewest runs.
	repo.activeRunsOverride = map[int]int{0: 3, 1: 1, 2: 2}

	run, err := svc.StartRun(context.Background(), StartRunRequest{
		TenantID:    "t-1",
		AgentTypeID: at.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.PodOrdinal != 1 {
		t.Errorf("expected pod ordinal 1 (least loaded), got %d", run.PodOrdinal)
	}
}

func TestStartRun_AgentTypeNotFound(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	_, err := svc.StartRun(context.Background(), StartRunRequest{
		TenantID:    "t-1",
		AgentTypeID: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for missing agent type")
	}
}

// ── TerminateRun ──────────────────────────────────────────────────────────────

func TestTerminateRun_ActiveRun(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	if err := svc.TerminateRun(context.Background(), run.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sm.mu.Lock()
	if len(sm.closedSessions) != 1 {
		t.Errorf("expected 1 close-session call, got %d", len(sm.closedSessions))
	}
	sm.mu.Unlock()
	if _, err := repo.GetRun(context.Background(), run.ID); !errors.Is(err, ErrNotFound) {
		t.Error("run should be deleted from repo")
	}
}

func TestTerminateRun_InactiveRun_SkipsSessionClose(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateCompleted})

	if err := svc.TerminateRun(context.Background(), run.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sm.mu.Lock()
	if len(sm.closedSessions) != 0 {
		t.Error("should not call CloseSession for a completed run")
	}
	sm.mu.Unlock()
}

func TestTerminateRun_SessionNotFoundContinues(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	sm.closeSessionErr = sessionmanager.ErrSessionNotFound

	if err := svc.TerminateRun(context.Background(), run.ID); err != nil {
		t.Fatalf("ErrSessionNotFound should be tolerated, got: %v", err)
	}
	if _, err := repo.GetRun(context.Background(), run.ID); !errors.Is(err, ErrNotFound) {
		t.Error("run should still be deleted despite session-manager returning not-found")
	}
}

func TestTerminateRun_SMNetworkError_ContinuesToDBCleanup(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	// A general network error (not ErrSessionNotFound) should still be tolerated;
	// the service logs and proceeds to delete the run from the DB.
	sm.closeSessionErr = errors.New("connection refused")

	if err := svc.TerminateRun(context.Background(), run.ID); err != nil {
		t.Fatalf("network error from SM should be tolerated, got: %v", err)
	}
	if _, err := repo.GetRun(context.Background(), run.ID); !errors.Is(err, ErrNotFound) {
		t.Error("run should be deleted from DB despite SM network error")
	}
}

func TestTerminateRun_NotFound(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	err := svc.TerminateRun(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing run")
	}
}

// ── TransitionRunState ────────────────────────────────────────────────────────

func TestTransitionRunState_UpdatesStateAndPublishesEvent(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateStarting})

	ch, cancel := svc.SubscribeRun("run-1")
	defer cancel()

	if err := svc.TransitionRunState(context.Background(), "run-1", RunStateStarting, RunStateActive); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.Type != "run_state" {
			t.Errorf("event type = %q, want run_state", ev.Type)
		}
	default:
		t.Error("expected event to be published to pubsub")
	}

	evs, _ := repo.ListRunEvents(context.Background(), "run-1", 0)
	if len(evs) != 1 || evs[0].EventType != "run_state" {
		t.Errorf("expected run_state persisted event, got %+v", evs)
	}
}

func TestTransitionRunState_DeprovisionsSandboxOnComplete(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateCompleting})

	if err := svc.TransitionRunState(context.Background(), "run-1", RunStateCompleting, RunStateCompleted); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	k8s.mu.Lock()
	sandboxDeleted := len(k8s.deletedSandboxes) == 1 && k8s.deletedSandboxes[0] == "run-1"
	k8s.mu.Unlock()
	if !sandboxDeleted {
		t.Error("expected sandbox to be deprovisioned on completion")
	}
}

func TestTransitionRunState_DeprovisionsSandboxOnFailed(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	if err := svc.TransitionRunState(context.Background(), "run-1", RunStateActive, RunStateFailed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	k8s.mu.Lock()
	sandboxDeleted := len(k8s.deletedSandboxes) == 1
	k8s.mu.Unlock()
	if !sandboxDeleted {
		t.Error("expected sandbox to be deprovisioned on failure")
	}
}

func TestTransitionRunState_Conflict(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	// Wrong `from` state — CAS mismatch.
	err := svc.TransitionRunState(context.Background(), "run-1", RunStatePending, RunStateActive)
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

// ── SendMessage ───────────────────────────────────────────────────────────────

func TestSendMessage_HappyPath(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	if err := svc.SendMessage(context.Background(), "run-1", "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repo.mu.Lock()
	if len(repo.messages) != 1 || repo.messages[0].Content != "hello" {
		t.Errorf("message not persisted: %+v", repo.messages)
	}
	repo.mu.Unlock()

	sm.mu.Lock()
	if len(sm.sentMessages) != 1 || sm.sentMessages[0] != "hello" {
		t.Errorf("message not forwarded to session-manager: %+v", sm.sentMessages)
	}
	sm.mu.Unlock()
}

func TestSendMessage_NonActiveRun(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateCompleted})

	err := svc.SendMessage(context.Background(), "run-1", "hello")
	if err == nil {
		t.Fatal("expected error when sending to a non-active run")
	}
}

func TestSendMessage_RunNotFound(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	err := svc.SendMessage(context.Background(), "nonexistent", "hello")
	if err == nil {
		t.Fatal("expected error for missing run")
	}
}

// ── ProvisionSandbox ──────────────────────────────────────────────────────────

func TestProvisionSandbox_HappyPath(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	k8s.sandboxEndpoint = "10.0.0.5:50051"

	sandbox, err := svc.ProvisionSandbox(context.Background(), "run-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sandbox.Endpoint != "10.0.0.5:50051" {
		t.Errorf("endpoint = %q, want 10.0.0.5:50051", sandbox.Endpoint)
	}
	if sandbox.ExecToken == "" {
		t.Error("expected an exec token alongside the endpoint")
	}
}

func TestProvisionSandbox_RunNotFound(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	_, err := svc.ProvisionSandbox(context.Background(), "nonexistent", false)
	if err == nil {
		t.Fatal("expected error for missing run")
	}
}

// ── UpdateAgentType ───────────────────────────────────────────────────────────

func TestUpdateAgentType_PartialFields(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{
		ID:           "at-1",
		TenantID:     "t-1",
		Slug:         "bot",
		Name:         "Original Name",
		Description:  "Original Desc",
		SystemPrompt: "Original Prompt",
	})

	newName := "Updated Name"
	updated, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{
		ID:   "at-1",
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "Updated Name" {
		t.Errorf("name = %q, want Updated Name", updated.Name)
	}
	// Unset fields must be preserved.
	if updated.Description != "Original Desc" {
		t.Errorf("description changed unexpectedly: %q", updated.Description)
	}
	if updated.SystemPrompt != "Original Prompt" {
		t.Errorf("system_prompt changed unexpectedly: %q", updated.SystemPrompt)
	}
}

func TestUpdateAgentType_AllFields(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})

	newName := "New Name"
	newDesc := "New Desc"
	newPrompt := "New Prompt"
	newMax := 10
	updated, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{
		ID:                      "at-1",
		Name:                    &newName,
		Description:             &newDesc,
		SystemPrompt:            &newPrompt,
		MaxConcurrentRunsPerPod: &newMax,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != newName || updated.Description != newDesc ||
		updated.SystemPrompt != newPrompt || updated.MaxConcurrentRunsPerPod != newMax {
		t.Errorf("unexpected update result: %+v", updated)
	}
}

func TestUpdateAgentType_NotFound(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	newName := "x"
	err := func() error {
		_, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{ID: "missing", Name: &newName})
		return err
	}()
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateAgentType_FrameworkChange_RollsStatefulSet(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{
		ID: "at-1", TenantID: "t-1", Slug: "bot",
		Framework: AgentFrameworkClaudeCode,
	})

	newFramework := AgentFrameworkCodex
	if _, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{
		ID:        "at-1",
		Framework: &newFramework,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	waitFor(t, 500*time.Millisecond, func() bool {
		k8s.mu.Lock()
		defer k8s.mu.Unlock()
		return len(k8s.rolledOutSlugs) > 0
	})
	k8s.mu.Lock()
	defer k8s.mu.Unlock()
	if len(k8s.rolledOutSlugs) != 1 || k8s.rolledOutSlugs[0] != "bot" {
		t.Errorf("StatefulSet rollout not triggered: rolledOutSlugs = %v", k8s.rolledOutSlugs)
	}
}

func TestUpdateAgentType_ModelIDChange_RollsStatefulSet(t *testing.T) {
	oldModel := "claude-sonnet-4-5"
	svc, repo, k8s, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{
		ID: "at-1", TenantID: "t-1", Slug: "bot",
		Framework: AgentFrameworkClaudeCode,
		ModelID:   &oldModel,
	})

	newModel := "claude-opus-4-8"
	if _, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{
		ID:      "at-1",
		ModelID: &newModel,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	waitFor(t, 500*time.Millisecond, func() bool {
		k8s.mu.Lock()
		defer k8s.mu.Unlock()
		return len(k8s.rolledOutSlugs) > 0
	})
	k8s.mu.Lock()
	defer k8s.mu.Unlock()
	if len(k8s.rolledOutSlugs) != 1 || k8s.rolledOutSlugs[0] != "bot" {
		t.Errorf("StatefulSet rollout not triggered: rolledOutSlugs = %v", k8s.rolledOutSlugs)
	}
}

func TestUpdateAgentType_NameChange_DoesNotRollStatefulSet(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{
		ID: "at-1", TenantID: "t-1", Slug: "bot",
		Framework: AgentFrameworkClaudeCode,
	})

	newName := "New Name"
	if _, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{
		ID:   "at-1",
		Name: &newName,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A brief wait ensures a spurious goroutine would have had time to fire.
	time.Sleep(50 * time.Millisecond)
	k8s.mu.Lock()
	defer k8s.mu.Unlock()
	if len(k8s.rolledOutSlugs) != 0 {
		t.Errorf("name-only update must not roll StatefulSet, got: %v", k8s.rolledOutSlugs)
	}
}

// ── SendMessage (idle state) ──────────────────────────────────────────────────

func TestSendMessage_IdleRun(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateIdle})

	if err := svc.SendMessage(context.Background(), "run-1", "follow up"); err != nil {
		t.Fatalf("idle run must accept messages, got: %v", err)
	}

	sm.mu.Lock()
	forwarded := len(sm.sentMessages) == 1 && sm.sentMessages[0] == "follow up"
	sm.mu.Unlock()
	if !forwarded {
		t.Error("message not forwarded to session-manager for idle run")
	}
}

func TestSendMessage_RejectsTerminalStates(t *testing.T) {
	for _, state := range []RunState{RunStateCompleted, RunStateFailed, RunStatePending} {
		t.Run(string(state), func(t *testing.T) {
			svc, repo, _, _ := newTestService(t)
			at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: state})

			err := svc.SendMessage(context.Background(), "run-1", "hello")
			if err == nil {
				t.Fatalf("state %s: expected rejection, got nil", state)
			}
			if !errors.Is(err, ErrRunNotAcceptingMessages) {
				t.Errorf("state %s: want ErrRunNotAcceptingMessages, got %v", state, err)
			}
		})
	}
}

// ── InterruptRun ──────────────────────────────────────────────────────────────

func TestInterruptRun_HappyPath(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	if err := svc.InterruptRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sm.mu.Lock()
	interrupted := len(sm.interruptedRuns) == 1 && sm.interruptedRuns[0] == "run-1"
	sm.mu.Unlock()
	if !interrupted {
		t.Error("interrupt not forwarded to session-manager")
	}
}

func TestInterruptRun_RejectsNonActive(t *testing.T) {
	for _, state := range []RunState{RunStateIdle, RunStateCompleted, RunStateFailed, RunStatePending} {
		t.Run(string(state), func(t *testing.T) {
			svc, repo, _, _ := newTestService(t)
			at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: state})

			err := svc.InterruptRun(context.Background(), "run-1")
			if err == nil {
				t.Fatalf("state %s: expected rejection, got nil", state)
			}
		})
	}
}

func TestInterruptRun_RunNotFound(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	err := svc.InterruptRun(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing run")
	}
}

// ── ListRunEvents ─────────────────────────────────────────────────────────────

func TestListRunEvents_ReturnsEventsAfterSeqNo(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	ctx := context.Background()
	repo.AppendRunEvent(ctx, RunEvent{RunID: "run-1", EventType: "text_delta"})   //nolint:errcheck
	repo.AppendRunEvent(ctx, RunEvent{RunID: "run-1", EventType: "tool_use"})     //nolint:errcheck
	repo.AppendRunEvent(ctx, RunEvent{RunID: "run-1", EventType: "message_stop"}) //nolint:errcheck

	evs, err := svc.ListRunEvents(ctx, "run-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evs) != 3 {
		t.Fatalf("expected 3 events, got %d", len(evs))
	}

	evs2, err := svc.ListRunEvents(ctx, "run-1", evs[1].SequenceNo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evs2) != 1 || evs2[0].EventType != "message_stop" {
		t.Errorf("expected 1 event after seqNo %d, got %+v", evs[1].SequenceNo, evs2)
	}
}

func TestListRunEvents_EmptyResult(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	evs, err := svc.ListRunEvents(context.Background(), "run-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evs) != 0 {
		t.Errorf("expected no events, got %d", len(evs))
	}
}

// ── StartReaper ───────────────────────────────────────────────────────────────

func newTestServiceWithReaper(t *testing.T) (*testServiceFacade, *mockRepository, *mockK8s, *mockSMClient) {
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
			StaleRunTimeout:         time.Minute, // shorter than UpdatedAt back-dating in tests
			ReaperInterval:          20 * time.Millisecond,
		},
		logger,
	)
	return &testServiceFacade{at: atSvc, run: runSvc}, repo, k8s, sm
}

func TestStartReaper_MarksStaleActiveRunFailed(t *testing.T) {
	svc, repo, k8s, _ := newTestServiceWithReaper(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	r := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	// Back-date updated_at so the run appears stale.
	repo.mu.Lock()
	repo.runs[r.ID].UpdatedAt = time.Now().Add(-2 * time.Hour)
	repo.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go svc.StartReaper(ctx)

	// Wait for the sandbox deprovision — the reaper's last step, after the state
	// transition and event publish — so the assertion does not race the reaper.
	waitFor(t, time.Second, func() bool {
		k8s.mu.Lock()
		defer k8s.mu.Unlock()
		return len(k8s.deletedSandboxes) == 1 && k8s.deletedSandboxes[0] == r.ID
	})

	repo.mu.Lock()
	state := repo.runs[r.ID].State
	repo.mu.Unlock()
	if state != RunStateFailed {
		t.Errorf("run state = %q, want failed", state)
	}
}

func TestStartReaper_MarksStaleIdleRunCompleted(t *testing.T) {
	svc, repo, k8s, _ := newTestServiceWithReaper(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	r := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateIdle})

	repo.mu.Lock()
	repo.runs[r.ID].UpdatedAt = time.Now().Add(-2 * time.Hour)
	repo.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go svc.StartReaper(ctx)

	// Wait for the sandbox deprovision — the reaper's last step, after the state
	// transition and event publish — so the assertion does not race the reaper.
	waitFor(t, time.Second, func() bool {
		k8s.mu.Lock()
		defer k8s.mu.Unlock()
		return len(k8s.deletedSandboxes) == 1 && k8s.deletedSandboxes[0] == r.ID
	})

	repo.mu.Lock()
	state := repo.runs[r.ID].State
	repo.mu.Unlock()
	if state != RunStateCompleted {
		t.Errorf("run state = %q, want completed", state)
	}
}

func TestStartReaper_DoesNotReapFreshRuns(t *testing.T) {
	svc, repo, k8s, _ := newTestServiceWithReaper(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	r := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	// updated_at is time.Now() (fresh) — must not be reaped.
	_ = r

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go svc.StartReaper(ctx)
	<-ctx.Done()

	repo.mu.Lock()
	state := repo.runs["run-1"].State
	repo.mu.Unlock()
	if state != RunStateActive {
		t.Errorf("fresh run must not be reaped, state = %s", state)
	}

	k8s.mu.Lock()
	cleaned := len(k8s.deletedSandboxes)
	k8s.mu.Unlock()
	if cleaned != 0 {
		t.Errorf("no sandbox should be deleted for fresh run, got %d deletions", cleaned)
	}
}

func TestStartReaper_PublishesPubSubEventOnReap(t *testing.T) {
	svc, repo, _, _ := newTestServiceWithReaper(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	r := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	repo.mu.Lock()
	repo.runs[r.ID].UpdatedAt = time.Now().Add(-2 * time.Hour)
	repo.mu.Unlock()

	ch, cancel := svc.SubscribeRun("run-1")
	defer cancel()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCtx()
	go svc.StartReaper(ctx)

	select {
	case ev := <-ch:
		if ev.Type != "run_state" {
			t.Errorf("event type = %q, want run_state", ev.Type)
		}
		if !strings.Contains(string(ev.Payload), `"failed"`) {
			t.Errorf("event payload = %q, want state failed", string(ev.Payload))
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for run_state event from reaper")
	}
}

func TestStartReaper_DoesNotReapRunWithRecentEvents(t *testing.T) {
	svc, repo, k8s, _ := newTestServiceWithReaper(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	r := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	// Stale updated_at (no state transition for a long active turn)...
	repo.mu.Lock()
	repo.runs[r.ID].UpdatedAt = time.Now().Add(-2 * time.Hour)
	repo.mu.Unlock()
	// ...but the run is still streaming events, so it must NOT be reaped.
	if _, err := repo.AppendRunEvent(context.Background(), RunEvent{RunID: r.ID, EventType: "text_delta", Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go svc.StartReaper(ctx)
	<-ctx.Done()

	repo.mu.Lock()
	state := repo.runs[r.ID].State
	repo.mu.Unlock()
	if state != RunStateActive {
		t.Errorf("active run with recent events must not be reaped, state = %s", state)
	}
	k8s.mu.Lock()
	cleaned := len(k8s.deletedSandboxes)
	k8s.mu.Unlock()
	if cleaned != 0 {
		t.Errorf("no sandbox should be deleted for live run, got %d deletions", cleaned)
	}
}

func TestAgentType_MCPServers(t *testing.T) {
	cases := []struct {
		name  string
		tools []string
		want  []string
	}{
		{"empty", nil, []string{}},
		{"single", []string{"mcp:orbit:agents:list:use"}, []string{"orbit"}},
		{
			"multi distinct, first-seen order + dedup",
			[]string{"mcp:orbit:agents:list:use", "mcp:github:issues:create:use", "mcp:orbit:agents:create:use"},
			[]string{"orbit", "github"},
		},
		{"malformed skipped", []string{"not-a-scope", "mcp:", "mcp:orbit:x:use"}, []string{"orbit"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := AgentType{MCPTools: tc.tools}
			got := at.MCPServers()
			if len(got) != len(tc.want) {
				t.Fatalf("MCPServers() = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("MCPServers()[%d] = %q, want %q (%v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

// PublishEvent is where a pushed event becomes a run_events row. The
// session-manager holds no database connection, so it cannot notice a lost one.
func TestPublishEvent_DurableEventIsRecorded(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	ch, cancel := svc.run.SubscribeRun("run-1")
	defer cancel()

	if err := svc.run.PublishEvent(context.Background(), "run-1", PushedEvent{
		Type:    "tool_use",
		Payload: []byte(`{"name":"Bash"}`),
	}); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	events, err := repo.ListRunEvents(context.Background(), "run-1", 0)
	if err != nil {
		t.Fatalf("ListRunEvents: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "tool_use" {
		t.Fatalf("expected one recorded tool_use, got %+v", events)
	}

	select {
	case got := <-ch:
		if got.SequenceNo != events[0].SequenceNo {
			t.Errorf("published sequence %d, recorded %d", got.SequenceNo, events[0].SequenceNo)
		}
	default:
		t.Error("expected the event to reach subscribers")
	}
}

// A replay-only type is recorded but withheld from subscribers, who already have
// the same content from the delta events.
func TestPublishEvent_ReplayOnlyEventIsNotPublished(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	ch, cancel := svc.run.SubscribeRun("run-1")
	defer cancel()

	if err := svc.run.PublishEvent(context.Background(), "run-1", PushedEvent{
		Type:    "text_block",
		Payload: []byte(`{"content":"hi"}`),
	}); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	events, err := repo.ListRunEvents(context.Background(), "run-1", 0)
	if err != nil {
		t.Fatalf("ListRunEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected the event to be recorded, got %+v", events)
	}
	select {
	case got := <-ch:
		t.Errorf("expected no fan-out for a replay-only event, got %+v", got)
	default:
	}
}

// A failed write still fans out, as an ephemeral event, and reports the failure:
// a subscriber should keep seeing the run while the database is unavailable, and
// the pushing session-manager should learn the row is missing.
func TestPublishEvent_WriteFailureStillPublishesAndReports(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})
	repo.appendRunEventErr = errors.New("database is down")

	ch, cancel := svc.run.SubscribeRun("run-1")
	defer cancel()

	err := svc.run.PublishEvent(context.Background(), "run-1", PushedEvent{
		Type:    "tool_use",
		Payload: []byte(`{"name":"Bash"}`),
	})
	if err == nil {
		t.Fatal("expected the write failure to be reported")
	}

	select {
	case got := <-ch:
		if got.SequenceNo != 0 {
			t.Errorf("expected an unsequenced event after a failed write, got %d", got.SequenceNo)
		}
	default:
		t.Error("expected the event to reach subscribers despite the failed write")
	}
}
