package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	si "github.com/clear-route/orbit/internal/platform/sandboximages/v1alpha1"
)

// seedEnvironments gives the agent "bot" the environments go (default) and node
// and seeds its run "run-1". Each environment has its own valid SandboxImage.
func seedEnvironments(t *testing.T, repo *mockRepository, k8s *mockK8s) {
	t.Helper()
	repo.envs["env-go"] = &Environment{ID: "env-go", TenantID: "t-1", Name: "go", SandboxImage: "acme-go", Size: EnvironmentSize{CPU: "4", Memory: "8Gi", Disk: "20Gi"}}
	repo.envs["env-node"] = &Environment{ID: "env-node", TenantID: "t-1", Name: "node", SandboxImage: "acme-node", Size: testSizes.Default}
	k8s.sandboxImages = map[string]*si.SandboxImage{
		"acme-go":   validSandboxImage("acme-go", "ghcr.io/acme/go:1", "sha256:go"),
		"acme-node": validSandboxImage("acme-node", "ghcr.io/acme/node:1", "sha256:node"),
	}
	// Stored links come back default first.
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", Environments: []AgentEnvironment{
		{EnvironmentID: "env-go", IsDefault: true},
		{EnvironmentID: "env-node"},
	}})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})
}

func TestProvisionSandbox_BuiltinDefault(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, "", false); err != nil {
		t.Fatal(err)
	}
	spec := k8s.provisionSpec
	if spec.Image != "ghcr.io/clear-route/kora-sandbox-base@sha256:base" || spec.Size != testSizes.Default || spec.EnvironmentID != "" {
		t.Errorf("spec = %+v, want kora-base pinned at the default size", spec)
	}
	if repo.runs["run-1"].EnvironmentID != nil {
		t.Errorf("run environment = %v, want nil for the built-in default", *repo.runs["run-1"].EnvironmentID)
	}
	if evs := repo.events["run-1"]; len(evs) != 1 || evs[0].EventType != eventSandboxEnvironment {
		t.Errorf("events = %+v, want one sandbox_environment", evs)
	}
}

func TestProvisionSandbox_ChoosesEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, env, wantImage string
	}{
		{"default when unnamed", "", "ghcr.io/acme/go@sha256:go"},
		{"named", "node", "ghcr.io/acme/node@sha256:node"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, k8s, _ := newTestService(t)
			seedEnvironments(t, repo, k8s)
			if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, tc.env, false); err != nil {
				t.Fatal(err)
			}
			if k8s.provisionSpec.Image != tc.wantImage {
				t.Errorf("image = %s, want %s", k8s.provisionSpec.Image, tc.wantImage)
			}
			var payload map[string]string
			_ = json.Unmarshal(repo.events["run-1"][0].Payload, &payload)
			if payload["action"] != "start" || payload["image"] != tc.wantImage {
				t.Errorf("event = %v, want a start with the image", payload)
			}
		})
	}
}

func TestProvisionSandbox_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(repo *mockRepository, k8s *mockK8s)
		env   string
		want  error
	}{
		{"not attached", func(*mockRepository, *mockK8s) {}, "rust", ErrEnvironmentNotAttached},
		{"switch while alive", func(_ *mockRepository, k8s *mockK8s) { k8s.podEnv, k8s.podLive = "env-go", true }, "node", ErrEnvironmentSwitch},
		{"image rejected", func(_ *mockRepository, k8s *mockK8s) {
			img := k8s.sandboxImages["acme-node"]
			img.Status.Digest = ""
			img.Status.Conditions = []metav1.Condition{{Type: si.ConditionValid, Status: metav1.ConditionFalse, Reason: si.ReasonWrongUser, Message: "runs as root"}}
		}, "node", ErrSandboxImageUnavailable},
		{"image not validated yet", func(_ *mockRepository, k8s *mockK8s) {
			k8s.sandboxImages["acme-node"].Spec.Image = "ghcr.io/acme/node:2"
		}, "node", errSandboxImagePending},
		{"image not registered", func(_ *mockRepository, k8s *mockK8s) { delete(k8s.sandboxImages, "acme-node") }, "node", ErrSandboxImageUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, k8s, _ := newTestService(t)
			seedEnvironments(t, repo, k8s)
			tc.setup(repo, k8s)
			_, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, tc.env, false)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if k8s.provisionSandboxCalls != 0 {
				t.Error("a refused choice still created a pod")
			}
		})
	}
}

// A reused pod the run already records is not an event.
func TestProvisionSandbox_ReusedPodRecordsNothing(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedEnvironments(t, repo, k8s)
	goID := "env-go"
	repo.runs["run-1"].EnvironmentID = &goID
	k8s.podEnv, k8s.podLive = "env-go", true
	k8s.provisionReused = true
	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, "", false); err != nil {
		t.Fatal(err)
	}
	if len(repo.events["run-1"]) != 0 {
		t.Error("a reused sandbox was recorded as a start")
	}
}

// A replacement deletes the sandbox, starts one in the new environment and
// records it. A later unnamed provision keeps that environment.
func TestReplaceEnvironment(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedEnvironments(t, repo, k8s)
	goID := "env-go"
	repo.runs["run-1"].EnvironmentID = &goID
	k8s.podEnv, k8s.podLive = "env-go", true

	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, " ", true); !errors.Is(err, ErrEnvironmentRequired) {
		t.Fatalf("replace without a name = %v, want ErrEnvironmentRequired", err)
	}
	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, "node", true); err != nil {
		t.Fatal(err)
	}
	if len(k8s.deletedSandboxes) != 1 || k8s.provisionSpec.EnvironmentID != "env-node" {
		t.Errorf("deleted %v, provisioned %+v; want the old pod gone and node started", k8s.deletedSandboxes, k8s.provisionSpec)
	}
	if got := repo.runs["run-1"].EnvironmentID; got == nil || *got != "env-node" {
		t.Errorf("run environment = %v, want env-node", got)
	}
	var payload map[string]string
	_ = json.Unmarshal(repo.events["run-1"][0].Payload, &payload)
	if payload["action"] != "replace" || payload["environment"] != "node" {
		t.Errorf("event = %v, want a replace into node", payload)
	}

	// The sandbox died. The next unnamed provision restarts it in node.
	k8s.podEnv, k8s.podLive = "", false
	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, "", false); err != nil {
		t.Fatal(err)
	}
	if k8s.provisionSpec.Environment != "node" {
		t.Errorf("re-provisioned in %q, want node", k8s.provisionSpec.Environment)
	}
}

// A retry after a replacement whose new pod started but was not recorded
// reuses that pod, deletes nothing and records the replacement.
func TestReplaceEnvironment_RetryAfterHalfSuccess(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedEnvironments(t, repo, k8s)
	goID := "env-go"
	repo.runs["run-1"].EnvironmentID = &goID
	k8s.podEnv, k8s.podLive = "env-node", true
	k8s.provisionReused = true

	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, "node", true); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(k8s.deletedSandboxes) != 0 {
		t.Errorf("deleted %v, want the new pod kept", k8s.deletedSandboxes)
	}
	if got := repo.runs["run-1"].EnvironmentID; got == nil || *got != "env-node" {
		t.Errorf("run environment = %v, want env-node recorded", got)
	}
	if len(repo.events["run-1"]) != 1 {
		t.Errorf("events = %+v, want the replacement recorded once", repo.events["run-1"])
	}
}

// A switch whose new pod fails to start leaves the run on the requested environment.
func TestReplaceEnvironment_FailedStartKeepsTarget(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedEnvironments(t, repo, k8s)
	goID := "env-go"
	repo.runs["run-1"].EnvironmentID = &goID
	k8s.podEnv, k8s.podLive = "env-go", true
	k8s.provisionSandboxErr = errors.New("quota exceeded")

	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, "node", true); err == nil {
		t.Fatal("replace succeeded, want the start failure")
	}
	if got := repo.runs["run-1"].EnvironmentID; got == nil || *got != "env-node" {
		t.Errorf("run environment = %v, want env-node", got)
	}
}

func TestListRunEnvironments(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedEnvironments(t, repo, k8s)
	k8s.podEnv, k8s.podLive = "env-node", true

	got, err := svc.run.ListRunEnvironments(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "go" || !got[0].Default || got[0].Current || got[1].Name != "node" || !got[1].Current {
		t.Errorf("environments = %+v, want go default first and node current", got)
	}
	if got[0].Image != "acme-go" {
		t.Errorf("image = %q, want the SandboxImage name", got[0].Image)
	}
}

func TestListRunEnvironments_NoneAttached(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})
	got, err := svc.run.ListRunEnvironments(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != DefaultEnvironmentName || got[0].Image != si.BaseName || !got[0].Default {
		t.Errorf("environments = %+v, want the built-in default on kora-base", got)
	}
}

// A refused choice is a 409 whose message the agent reads.
func TestHandleProvisionSandbox_EnvironmentRefusal(t *testing.T) {
	c, repo, k8s, _ := newTestController(t)
	seedEnvironments(t, repo, k8s)

	w := httptest.NewRecorder()
	c.handleProvisionSandbox(w, callbackReq(http.MethodPost, "/internal/runs/run-1/provision-sandbox", `{"environment":"rust"}`))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "this agent has go, node") {
		t.Errorf("status %d body %s, want 409 naming the attached environments", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	c.handleProvisionSandbox(w, callbackReq(http.MethodPost, "/internal/runs/run-1/provision-sandbox", `{"environment":"node","replace":true}`))
	if w.Code != http.StatusOK {
		t.Errorf("replace status %d body %s, want 200", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	c.handleListRunEnvironments(w, callbackReq(http.MethodGet, "/internal/runs/run-1/environments", ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"go"`) {
		t.Errorf("list status %d body %s", w.Code, w.Body.String())
	}
}

// A provision that timed out while its pod started leaves the run record behind.
// The pod's own environment decides and the next call reuses the pod.
func TestProvisionSandbox_PodEnvironmentWinsOverRecord(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedEnvironments(t, repo, k8s)
	k8s.podEnv, k8s.podLive = "env-node", true // starting pod; run record still empty

	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, "", false); err != nil {
		t.Fatalf("unnamed provision of a starting pod: %v", err)
	}
	if k8s.provisionSpec.EnvironmentID != "env-node" {
		t.Errorf("provisioned %q, want the pod's env-node", k8s.provisionSpec.EnvironmentID)
	}
	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", false, "node", true); err != nil || len(k8s.deletedSandboxes) != 0 {
		t.Errorf("replace into the pod's environment = %v, deleted %v; want the pod reused", err, k8s.deletedSandboxes)
	}
	envs, err := svc.run.ListRunEnvironments(context.Background(), "run-1")
	if err != nil || !envs[1].Current {
		t.Errorf("list = %+v, %v; want node current", envs, err)
	}
}

// A pending image is transient and answers 503. A rejected image answers 409,
// also while the operator cannot reach the registry.
func TestHandleProvisionSandbox_ImageStatus(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(img *si.SandboxImage)
		want  int
	}{
		{"pending", func(img *si.SandboxImage) { img.Spec.Image = "ghcr.io/acme/go:2" }, http.StatusServiceUnavailable},
		{"rejected", func(img *si.SandboxImage) {
			img.Status.Digest = ""
			img.Status.Conditions = []metav1.Condition{{Type: si.ConditionValid, Status: metav1.ConditionFalse, Reason: si.ReasonWrongUser, Message: "runs as root"}}
		}, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, repo, k8s, _ := newTestController(t)
			seedEnvironments(t, repo, k8s)
			tc.setup(k8s.sandboxImages["acme-go"])

			w := httptest.NewRecorder()
			c.handleProvisionSandbox(w, callbackReq(http.MethodPost, "/internal/runs/run-1/provision-sandbox", `{"environment":"go"}`))
			if w.Code != tc.want {
				t.Errorf("status %d body %s, want %d", w.Code, w.Body.String(), tc.want)
			}
		})
	}
}
