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

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	smclient "github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
)

// errNotifierUnexpectedStatus is the stable sentinel recorded on the notifier
// span when kora-agents returns a non-2xx response. HandleErrorResponse
// appends the status code and response body to it.
var errNotifierUnexpectedStatus = errors.New("kora-agents returned unexpected status")

// ErrEnvironmentRejected means kora-agents refused the environment the agent
// chose. Its message tells the agent why. Retrying does not help.
var ErrEnvironmentRejected = errors.New("environment refused")

// ErrSandboxUnschedulable means no node could place the sandbox pod. Its message carries the scheduler's reason.
var ErrSandboxUnschedulable = errors.New("no node can run the sandbox")

// unschedulableError carries the scheduler's reason as a field, so callers read it without parsing error text.
type unschedulableError struct{ reason string }

func (e *unschedulableError) Error() string { return ErrSandboxUnschedulable.Error() + ": " + e.reason }

func (e *unschedulableError) Is(target error) bool { return target == ErrSandboxUnschedulable }

// Notifier delivers events to kora-agents for live SSE fan-out and run
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
	// ProvisionSandbox requests kora-agents to provision a sandbox pod for the
	// given run and returns how to reach and authenticate to it.
	// environment names one of the agent's environments. Empty keeps the
	// current one. replace switches a live sandbox to it.
	ProvisionSandbox(ctx context.Context, runID string, environment string, replace bool) (Sandbox, error)
	// ListEnvironments returns kora-agents' list of the run's environments unchanged.
	ListEnvironments(ctx context.Context, runID string) (json.RawMessage, error)
	GetSandbox(ctx context.Context, runID string) (Sandbox, bool, error)
	// SetSummary reports the run's generated task summary to kora-agents, which
	// persists it set-once. Best-effort.
	SetSummary(ctx context.Context, runID, summary string) error
}

// NoopNotifier silently discards all events.
// Used when KORA_AGENTS_URL is not configured (local dev without kora-agents).
type NoopNotifier struct{}

func (NoopNotifier) Notify(_ context.Context, _ string, _ Event) error               { return nil }
func (NoopNotifier) IdleRun(_ context.Context, _ string) error                       { return nil }
func (NoopNotifier) ActivateRun(_ context.Context, _ string) error                   { return nil }
func (NoopNotifier) CompleteRun(_ context.Context, _ string, _ string, _ bool) error { return nil }
func (NoopNotifier) ExhaustRun(_ context.Context, _ string, _ string) error          { return nil }
func (NoopNotifier) ProvisionSandbox(_ context.Context, _ string, _ string, _ bool) (Sandbox, error) {
	return Sandbox{}, nil
}
func (NoopNotifier) ListEnvironments(_ context.Context, _ string) (json.RawMessage, error) {
	return json.RawMessage(`{"environments":[]}`), nil
}
func (NoopNotifier) GetSandbox(_ context.Context, _ string) (Sandbox, bool, error) {
	return Sandbox{}, false, nil
}
func (NoopNotifier) SetSummary(_ context.Context, _ string, _ string) error { return nil }

// HTTPNotifier POSTs events to kora-agents via HTTP.
// Each call is fire-and-forget with a short timeout so a slow or absent
// kora-agents instance does not block the session-manager drain loop.
// ProvisionSandbox uses a long timeout: a sandbox scheduled onto a fresh kora
// node pays a cold image pull (sandbox-exec is large) plus istio/sidecar
// startup. Must exceed the kora-agents pod-ready wait (k8s.waitForPodReady).
//
// Each field is a distinct *httpclient.Base (rather than one shared Base) so
// each call site keeps its own timeout and gets its own named OTel tracer scope.
type HTTPNotifier struct {
	baseURL         string
	client          *httpclient.Base // short timeout: event fan-out (best-effort, fire-and-forget)
	lifecycleClient *httpclient.Base // longer timeout: durable events and run state callbacks (must not be dropped)
	sandboxClient   *httpclient.Base
}

// NewHTTPNotifier creates a notifier that pushes to the given kora-agents base URL.
func NewHTTPNotifier(baseURL string, logger logging.Logger) *HTTPNotifier {
	return &HTTPNotifier{
		baseURL:         baseURL,
		client:          httpclient.New(tracerName, logger, 2*time.Second),
		lifecycleClient: httpclient.New(tracerName, logger, 15*time.Second),
		sandboxClient:   httpclient.New(tracerName, logger, 720*time.Second),
	}
}

func (n *HTTPNotifier) Notify(ctx context.Context, runID string, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	// Durable events take the longer timeout: they are the only record of what
	// the agent did. Delta events are superseded in milliseconds and are better
	// dropped than left to stall the drain loop.
	client := n.client
	if isDurableEvent(ev.Type) {
		client = n.lifecycleClient
	}
	_, span, resp, err := client.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: n.baseURL, Path: "/internal/runs/{runID}/events", Params: []string{runID}, Header: header, Body: bytes.NewReader(body),
	})
	defer span.End()
	span.SetAttributes(observability.RunIDKey.String(runID))
	if err != nil {
		return fmt.Errorf("post event: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return client.HandleErrorResponse(span, resp, errNotifierUnexpectedStatus)
	}
	return nil
}

// GetSandbox asks kora-agents what sandbox the run has, without creating one.
// ok is false when the run has none, which kora-agents reports as 404 and this
// pod must not confuse with a lookup failure.
func (n *HTTPNotifier) GetSandbox(ctx context.Context, runID string) (Sandbox, bool, error) {
	_, span, resp, err := n.sandboxClient.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: n.baseURL, Path: "/internal/runs/{runID}/sandbox", Params: []string{runID},
	})
	defer span.End()
	span.SetAttributes(observability.RunIDKey.String(runID))
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

func (n *HTTPNotifier) ProvisionSandbox(ctx context.Context, runID string, environment string, replace bool) (Sandbox, error) {
	body, _ := json.Marshal(map[string]any{"environment": environment, "replace": replace})
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	_, span, resp, err := n.sandboxClient.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: n.baseURL, Path: "/internal/runs/{runID}/provision-sandbox", Params: []string{runID}, Header: header, Body: bytes.NewReader(body),
	})
	defer span.End()
	span.SetAttributes(observability.RunIDKey.String(runID))
	if err != nil {
		return Sandbox{}, fmt.Errorf("call kora-agents: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The body is read once. A 409 is a refused environment choice whose message the agent reads.
		body := n.sandboxClient.ReadErrorBody(span, resp)
		var apiErr api.APIError
		if json.Unmarshal([]byte(body), &apiErr) == nil && apiErr.Message != "" {
			switch {
			case resp.StatusCode == http.StatusConflict:
				return Sandbox{}, fmt.Errorf("%w: %s", ErrEnvironmentRejected, apiErr.Message)
			case apiErr.Code == smclient.ErrCodeSandboxUnschedulable:
				return Sandbox{}, &unschedulableError{reason: apiErr.Message}
			}
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
	_, span, resp, err := n.lifecycleClient.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: n.baseURL, Path: "/internal/runs/{runID}/environments", Params: []string{runID},
	})
	defer span.End()
	span.SetAttributes(observability.RunIDKey.String(runID))
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
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	_, span, resp, err := n.lifecycleClient.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: n.baseURL, Path: "/internal/runs/{runID}/summary", Params: []string{runID}, Header: header, Body: bytes.NewReader(body),
	})
	defer span.End()
	span.SetAttributes(observability.RunIDKey.String(runID))
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
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	_, span, resp, err := n.lifecycleClient.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: n.baseURL, Path: "/internal/runs/{runID}/state", Params: []string{runID}, Header: header, Body: bytes.NewReader(body),
	})
	defer span.End()
	span.SetAttributes(observability.RunIDKey.String(runID))
	if err != nil {
		return fmt.Errorf("post state: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return n.lifecycleClient.HandleErrorResponse(span, resp, errNotifierUnexpectedStatus)
	}
	return nil
}
