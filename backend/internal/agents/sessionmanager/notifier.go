package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Notifier delivers events to orbit-agents for live SSE fan-out and run
// lifecycle callbacks.
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
	// given run and returns its gRPC endpoint ("podIP:port").
	ProvisionSandbox(ctx context.Context, runID string) (string, error)
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
func (NoopNotifier) ProvisionSandbox(_ context.Context, _ string) (string, error)    { return "", nil }
func (NoopNotifier) SetSummary(_ context.Context, _ string, _ string) error          { return nil }

// HTTPNotifier POSTs events to orbit-agents via HTTP.
// Each call is fire-and-forget with a short timeout so a slow or absent
// orbit-agents instance does not block the session-manager drain loop.
// ProvisionSandbox uses a long timeout: a sandbox scheduled onto a fresh orbit
// node pays a cold image pull (sandbox-exec is large) plus istio/sidecar
// startup. Must exceed the orbit-agents pod-ready wait (k8s.waitForPodReady).
//
// Each field is a distinct *httpclient.Base (rather than one shared Base) so
// each call site keeps its own timeout and gets its own named OTel tracer scope.
type HTTPNotifier struct {
	baseURL         string
	client          *httpclient.Base // short timeout: event fan-out (best-effort, fire-and-forget)
	lifecycleClient *httpclient.Base // longer timeout: run state callbacks (must not be dropped)
	sandboxClient   *httpclient.Base
}

// NewHTTPNotifier creates a notifier that pushes to the given orbit-agents base URL.
func NewHTTPNotifier(baseURL string, logger logging.Logger) *HTTPNotifier {
	return &HTTPNotifier{
		baseURL:         baseURL,
		client:          httpclient.New("orbit.sessionmanager-notifier", logger, 2*time.Second),
		lifecycleClient: httpclient.New("orbit.sessionmanager-notifier-lifecycle", logger, 15*time.Second),
		sandboxClient:   httpclient.New("orbit.sessionmanager-notifier-sandbox", logger, 300*time.Second),
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
	_, span, resp, err := n.client.Do(ctx, "sessionmanager.Notifier.Notify", req)
	defer span.End()
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

func (n *HTTPNotifier) ProvisionSandbox(ctx context.Context, runID string) (string, error) {
	url := fmt.Sprintf("%s/internal/runs/%s/provision-sandbox", n.baseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	_, span, resp, err := n.sandboxClient.Do(ctx, "sessionmanager.Notifier.ProvisionSandbox", req)
	defer span.End()
	if err != nil {
		return "", fmt.Errorf("post provision-sandbox: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", fmt.Errorf("orbit-agents returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	var result struct {
		Endpoint string `json:"endpoint"`
	}
	if err := n.sandboxClient.DecodeJSON(span, resp, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.Endpoint, nil
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
	_, span, resp, err := n.lifecycleClient.Do(ctx, "sessionmanager.Notifier.SetSummary", req)
	defer span.End()
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
	_, span, resp, err := n.lifecycleClient.Do(ctx, "sessionmanager.Notifier.postRunState", req)
	defer span.End()
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
