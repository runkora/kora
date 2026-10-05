package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// errNotifierUnexpectedStatus is the stable sentinel recorded on the notifier
// span when orbit-agents returns a non-2xx response. HandleErrorResponse
// appends the status code and response body to it.
var errNotifierUnexpectedStatus = errors.New("orbit-agents returned unexpected status")

// ErrEnvironmentRejected means orbit-agents refused the environment the agent
// chose. Its message tells the agent why. Retrying does not help.
var ErrEnvironmentRejected = errors.New("environment refused")

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
	// given run and returns how to reach and authenticate to it.
	// environment names one of the agent's environments; empty keeps the
	// current one. replace switches a live sandbox to it.
	ProvisionSandbox(ctx context.Context, runID string, ahead bool, environment string, replace bool) (Sandbox, error)
	// ListEnvironments returns orbit-agents' list of the run's environments as is.
	ListEnvironments(ctx context.Context, runID string) (json.RawMessage, error)
	GetSandbox(ctx context.Context, runID string) (Sandbox, bool, error)
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
func (NoopNotifier) ProvisionSandbox(_ context.Context, _ string, _ bool, _ string, _ bool) (Sandbox, error) {
	return Sandbox{}, nil
}
func (NoopNotifier) ListEnvironments(_ context.Context, _ string) (json.RawMessage, error) {
	return json.RawMessage(`{"environments":[]}`), nil
}
func (NoopNotifier) GetSandbox(_ context.Context, _ string) (Sandbox, bool, error) {
	return Sandbox{}, false, nil
}
func (NoopNotifier) SetSummary(_ context.Context, _ string, _ string) error { return nil }

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
	lifecycleClient *httpclient.Base // longer timeout: durable events and run state callbacks (must not be dropped)
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
	// Durable events take the longer timeout: they are the only record of what
	// the agent did. Delta events are superseded in milliseconds and are better
	// dropped than left to stall the drain loop.
	client := n.client
	if isDurableEvent(ev.Type) {
		client = n.lifecycleClient
	}
	_, span, resp, err := client.Do(ctx, "sessionmanager.Notifier.Notify", req)
	defer span.End()
	if err != nil {
		return fmt.Errorf("post event: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return client.HandleErrorResponse(span, resp, errNotifierUnexpectedStatus)
	}
	return nil
}

// GetSandbox asks orbit-agents what sandbox the run has, without creating one.
// ok is false when the run has none, which orbit-agents reports as 404 and this
// pod must not confuse with a lookup failure.
func (n *HTTPNotifier) GetSandbox(ctx context.Context, runID string) (Sandbox, bool, error) {
	url := fmt.Sprintf("%s/internal/runs/%s/sandbox", n.baseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Sandbox{}, false, fmt.Errorf("build request: %w", err)
	}
	_, span, resp, err := n.sandboxClient.Do(ctx, "sessionmanager.Notifier.GetSandbox", req)
	defer span.End()
	if err != nil {
		return Sandbox{}, false, fmt.Errorf("get sandbox: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// Only the handler's own 404 means "this run has no sandbox". A stale
		// route during a rolling deploy, or a misconfigured base URL, also
		// answers 404, and reading that as no sandbox hands the agent a local
		// shell. Anything without the code is a failure.
		var body struct {
			Code  string `json:"code"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		raw, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return Sandbox{}, false, fmt.Errorf("read sandbox 404 body: %w", readErr)
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return Sandbox{}, false, fmt.Errorf("%w: 404 without a decodable body", errNotifierUnexpectedStatus)
		}
		if body.Code != api.ErrCodeResourceNotFound && body.Error.Code != api.ErrCodeResourceNotFound {
			return Sandbox{}, false, fmt.Errorf("%w: 404 is not %s", errNotifierUnexpectedStatus, api.ErrCodeResourceNotFound)
		}
		return Sandbox{}, false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Sandbox{}, false, n.sandboxClient.HandleErrorResponse(span, resp, errNotifierUnexpectedStatus)
	}
	var result Sandbox
	if err := n.sandboxClient.DecodeJSON(span, resp, &result); err != nil {
		return Sandbox{}, false, fmt.Errorf("decode response: %w", err)
	}
	if result.Endpoint == "" || result.ExecToken == "" {
		return Sandbox{}, false, ErrSandboxIncomplete
	}
	return result, true, nil
}

func (n *HTTPNotifier) ProvisionSandbox(ctx context.Context, runID string, ahead bool, environment string, replace bool) (Sandbox, error) {
	target := fmt.Sprintf("%s/internal/runs/%s/provision-sandbox", n.baseURL, runID)
	if ahead {
		target += "?ahead=true"
	}
	return n.sandboxCall(ctx, "sessionmanager.Notifier.ProvisionSandbox", target, environment, replace)
}

// sandboxCall posts the environment choice to a route that answers with a
// sandbox. A 409 is a refused choice, returned as ErrEnvironmentRejected with
// orbit-agents' message.
func (n *HTTPNotifier) sandboxCall(ctx context.Context, name, target, environment string, replace bool) (Sandbox, error) {
	body, _ := json.Marshal(map[string]any{"environment": environment, "replace": replace})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return Sandbox{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	_, span, resp, err := n.sandboxClient.Do(ctx, name, req)
	defer span.End()
	if err != nil {
		return Sandbox{}, fmt.Errorf("call orbit-agents: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The body is read once: a 409 carries the refusal for the agent, anything else is reported as is.
		body := n.sandboxClient.ReadErrorBody(span, resp)
		var apiErr api.APIError
		if resp.StatusCode == http.StatusConflict && json.Unmarshal([]byte(body), &apiErr) == nil && apiErr.Message != "" {
			return Sandbox{}, fmt.Errorf("%w: %s", ErrEnvironmentRejected, apiErr.Message)
		}
		_ = n.sandboxClient.RecordError(span, errNotifierUnexpectedStatus)
		return Sandbox{}, fmt.Errorf("%w: status %d: %s", errNotifierUnexpectedStatus, resp.StatusCode, body)
	}
	var result Sandbox
	if err := n.sandboxClient.DecodeJSON(span, resp, &result); err != nil {
		return Sandbox{}, fmt.Errorf("decode response: %w", err)
	}
	if result.Endpoint == "" || result.ExecToken == "" {
		return Sandbox{}, ErrSandboxIncomplete
	}
	return result, nil
}

func (n *HTTPNotifier) ListEnvironments(ctx context.Context, runID string) (json.RawMessage, error) {
	target := fmt.Sprintf("%s/internal/runs/%s/environments", n.baseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	_, span, resp, err := n.lifecycleClient.Do(ctx, "sessionmanager.Notifier.ListEnvironments", req)
	defer span.End()
	if err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, n.lifecycleClient.HandleErrorResponse(span, resp, errNotifierUnexpectedStatus)
	}
	var raw json.RawMessage
	if err := n.lifecycleClient.DecodeJSON(span, resp, &raw); err != nil {
		return nil, fmt.Errorf("decode environments: %w", err)
	}
	return raw, nil
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
		return n.lifecycleClient.HandleErrorResponse(span, resp, errNotifierUnexpectedStatus)
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
		return n.lifecycleClient.HandleErrorResponse(span, resp, errNotifierUnexpectedStatus)
	}
	return nil
}
