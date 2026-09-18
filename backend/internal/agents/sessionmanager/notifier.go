package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Notifier delivers events to orbit-agents, which records the durable ones and
// fans out to live SSE subscribers, plus the run lifecycle callbacks.
// Implementations are best-effort — errors are logged but never fatal.
type Notifier interface {
	Notify(ctx context.Context, runID string, ev Event) error
	// IdleRun transitions a run from active → idle. Called when the agent
	// completes a turn and is waiting for the next user message.
	IdleRun(ctx context.Context, runID string) error
	// ActivateRun transitions a run from idle → active. Called when a new
	// user message arrives and the agent begins processing.
	ActivateRun(ctx context.Context, runID string) error
	// CompleteRun transitions a run from fromState to completed or failed.
	// fromState is "active" if the subprocess exited mid-turn, "idle" if it
	// exited cleanly after a completed turn (e.g. idle timeout).
	CompleteRun(ctx context.Context, runID string, fromState string, failed bool) error
	// ExhaustRun transitions a run from fromState to the terminal
	// budget_exhausted state — the gateway rejected its LLM calls with
	// BUDGET_EXCEEDED; the run did nothing wrong, its budget ended.
	ExhaustRun(ctx context.Context, runID string, fromState string) error
	// ProvisionSandbox requests orbit-agents to provision a sandbox pod for the
	// given run and returns how to reach and authenticate to it.
	ProvisionSandbox(ctx context.Context, runID string) (Sandbox, error)
	// SetSummary reports the run's generated task summary to orbit-agents, which
	// persists it set-once. Best-effort.
	SetSummary(ctx context.Context, runID, summary string) error
}

// NoopNotifier silently discards all events.
// Used when ORBIT_AGENTS_URL is not configured (local dev without orbit-agents).
type NoopNotifier struct{}

func (NoopNotifier) Notify(_ context.Context, _ string, _ Event) error               { return nil }
func (NoopNotifier) IdleRun(_ context.Context, _ string) error                       { return nil }
func (NoopNotifier) ActivateRun(_ context.Context, _ string) error                   { return nil }
func (NoopNotifier) CompleteRun(_ context.Context, _ string, _ string, _ bool) error { return nil }
func (NoopNotifier) ExhaustRun(_ context.Context, _ string, _ string) error          { return nil }
func (NoopNotifier) ProvisionSandbox(_ context.Context, _ string) (Sandbox, error) {
	return Sandbox{}, nil
}
func (NoopNotifier) SetSummary(_ context.Context, _ string, _ string) error { return nil }

// HTTPNotifier POSTs events to orbit-agents via HTTP.
// Streaming events are fire-and-forget with a short timeout so a slow or absent
// orbit-agents instance does not block the session-manager drain loop.
// ProvisionSandbox uses a long timeout: a sandbox scheduled onto a fresh orbit
// node pays a cold image pull (sandbox-exec is large) plus istio/sidecar
// startup. Must exceed the orbit-agents pod-ready wait (k8s.waitForPodReady).
type HTTPNotifier struct {
	baseURL         string
	client          *http.Client // short timeout: event fan-out (best-effort, fire-and-forget)
	lifecycleClient *http.Client // longer timeout: durable events and run state callbacks (must not be dropped)
	sandboxClient   *http.Client
}

// NewHTTPNotifier creates a notifier that pushes to the given orbit-agents base URL.
func NewHTTPNotifier(baseURL string) *HTTPNotifier {
	return &HTTPNotifier{
		baseURL:         baseURL,
		client:          &http.Client{Timeout: 2 * time.Second},
		lifecycleClient: &http.Client{Timeout: 15 * time.Second},
		sandboxClient:   &http.Client{Timeout: 300 * time.Second},
	}
}

func (n *HTTPNotifier) Notify(ctx context.Context, runID string, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	url := fmt.Sprintf("%s/internal/runs/%s/events", n.baseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// A durable event is the only record of what the agent did, so it gets the
	// longer timeout. Delta events are superseded within milliseconds and are
	// better dropped than allowed to stall the drain loop.
	client := n.client
	if isDurableEvent(ev.Type) {
		client = n.lifecycleClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post event: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("orbit-agents returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}

func (n *HTTPNotifier) ProvisionSandbox(ctx context.Context, runID string) (Sandbox, error) {
	url := fmt.Sprintf("%s/internal/runs/%s/provision-sandbox", n.baseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return Sandbox{}, fmt.Errorf("build request: %w", err)
	}
	resp, err := n.sandboxClient.Do(req)
	if err != nil {
		return Sandbox{}, fmt.Errorf("post provision-sandbox: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return Sandbox{}, fmt.Errorf("orbit-agents returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	var result Sandbox
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Sandbox{}, fmt.Errorf("decode response: %w", err)
	}
	if result.Endpoint == "" || result.ExecToken == "" {
		return Sandbox{}, ErrSandboxIncomplete
	}
	return result, nil
}

func (n *HTTPNotifier) SetSummary(ctx context.Context, runID, summary string) error {
	body, err := json.Marshal(map[string]string{"summary": summary})
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	url := fmt.Sprintf("%s/internal/runs/%s/summary", n.baseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.lifecycleClient.Do(req)
	if err != nil {
		return fmt.Errorf("post summary: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("orbit-agents returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}

func (n *HTTPNotifier) IdleRun(ctx context.Context, runID string) error {
	return n.postRunState(ctx, runID, "active", "idle")
}

func (n *HTTPNotifier) ActivateRun(ctx context.Context, runID string) error {
	return n.postRunState(ctx, runID, "idle", "active")
}

func (n *HTTPNotifier) CompleteRun(ctx context.Context, runID string, fromState string, failed bool) error {
	toState := "completed"
	if failed {
		toState = "failed"
	}
	return n.postRunState(ctx, runID, fromState, toState)
}

func (n *HTTPNotifier) ExhaustRun(ctx context.Context, runID string, fromState string) error {
	return n.postRunState(ctx, runID, fromState, "budget_exhausted")
}

func (n *HTTPNotifier) postRunState(ctx context.Context, runID, from, to string) error {
	body, err := json.Marshal(map[string]string{"from": from, "to": to})
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	url := fmt.Sprintf("%s/internal/runs/%s/state", n.baseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.lifecycleClient.Do(req)
	if err != nil {
		return fmt.Errorf("post state: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("orbit-agents returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}
