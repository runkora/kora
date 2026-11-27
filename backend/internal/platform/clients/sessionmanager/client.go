// Package sessionmanager provides an HTTP client for the session-manager API
// that runs inside each agent pod.
//
// Each pod exposes the session-manager on port 8090 behind the agent's headless
// Service. A call goes to the pod IP with the agent Service as Host header (PodAt).
//
// Usage:
//
//	c := sessionmanager.NewClient(logger, identityClient.SelfTokenSource())
//	pod := sessionmanager.PodAt(podIP, at.Slug, namespace)
//	if err := c.CreateSession(ctx, pod, sessionmanager.CreateSessionRequest{...}); err != nil {
//	    return fmt.Errorf("create session: %w", err)
//	}
package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
)

// ErrCodeSandboxUnschedulable marks kora-agents' 503 for a sandbox pod that no node could place.
// The error message is the scheduler's reason.
const ErrCodeSandboxUnschedulable = "sandbox_unschedulable"

const (
	tracerName    = "github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	clientTimeout = 30 * time.Second
	podPort       = 8090
)

var (
	// ErrSessionConflict is returned when attempting to create a session that already exists.
	ErrSessionConflict = errors.New("session already exists on pod")
	// ErrSessionNotFound is returned when the target session does not exist.
	ErrSessionNotFound = errors.New("session not found on pod")
	// ErrUnexpectedStatus is returned when the pod returns an unexpected HTTP status.
	ErrUnexpectedStatus = errors.New("unexpected status from session-manager")
)

// StatusError carries the HTTP status of a response the pod rejected. It wraps
// the sentinel, so errors.Is keeps working.
type StatusError struct {
	StatusCode int
	Err        error
}

func (e *StatusError) Error() string { return e.Err.Error() }
func (e *StatusError) Unwrap() error { return e.Err }

// TokenFunc returns the caller's own platform token. kora-agents supplies
// identity.Client.SelfTokenSource(), which exchanges the pod's SPIRE SVID for a
// SYSTEM token named "kora-agents" and caches it until expiry — the same
// mechanism the policy client uses (internal/platform/clients/policy).
type TokenFunc func(ctx context.Context) (string, error)

// Client sends commands to a session-manager pod.
type Client struct {
	*httpclient.Base
	tokenFunc TokenFunc
}

// NewClient creates a Client. The returned client is safe for concurrent use
// and should be shared across calls — one Client per kora-agents process.
//
// tokenFunc authenticates every session-control call and is required. The
// session-manager authenticates unconditionally, so a client without a token
// source could only ever produce 401s.
func NewClient(logger logging.Logger, tokenFunc TokenFunc) *Client {
	return &Client{
		// No keep-alives: agent pod IPs are reused, and a pooled connection to a
		// pod's old IP would reach whichever pod holds the IP now.
		Base:      httpclient.New(tracerName, logger, clientTimeout, httpclient.WithDisableKeepAlives()),
		tokenFunc: tokenFunc,
	}
}

// authorize attaches the caller's platform token to the outbound request.
func (c *Client) authorize(ctx context.Context, header http.Header) (http.Header, error) {
	tok, err := c.tokenFunc(ctx)
	if err != nil {
		return nil, fmt.Errorf("obtain platform token for session-manager call: %w", err)
	}
	header.Set("Authorization", "Bearer "+tok)
	return header, nil
}

// Pod is the target of a session-manager call: the pod IP URL and the agent
// Service as Host header. The Host header routes the call to the agent Service's
// Envoy cluster, whose DestinationRule pins the agent SPIFFE ID. A call to a bare
// pod IP leaves through PassthroughCluster with no SAN check
// (docs/adr/2026-11-22-agent-cold-tier.md).
type Pod struct {
	URL  string
	Host string
}

// PodAt returns the target for the session-manager on the pod at ip, which
// serves the agent slug in namespace.
func PodAt(ip, slug, namespace string) Pod {
	return Pod{
		URL:  "http://" + net.JoinHostPort(ip, strconv.Itoa(podPort)),
		Host: fmt.Sprintf("%s.%s.svc.cluster.local:%d", slug, namespace, podPort),
	}
}

// CreateSessionRequest is the payload for CreateSession.
//
// Note: there is an independent CreateSessionRequest in internal/agents/sessionmanager/server.go.
// The two structs are kept separate because the client package cannot import the server package
// (the dependency direction would be backwards) and a shared type in internal/platform/ would
// pull session-manager domain concepts into the platform layer.
type CreateSessionRequest struct {
	RunID        string `json:"run_id"`
	AgentTypeID  string `json:"agent_type_id"`
	AgentSlug    string `json:"agent_slug,omitempty"`
	TenantID     string `json:"tenant_id"`
	Framework    string `json:"framework,omitempty"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	// MCPServers are the distinct MCP servers the agent has tool grants on; the
	// runner provisions one governed endpoint (/v1/mcp/<server>) per entry.
	MCPServers []string `json:"mcp_servers,omitempty"`
	// Guardrails are the run's thresholds; a zero field takes the
	// session-manager's default (docs/adr/2026-09-29-agent-run-guardrails.md).
	Guardrails RunGuardrails `json:"guardrails"`
	// IdleTimeoutSecs is how long the run may stay idle before its session
	// closes. Zero takes the session-manager's default.
	IdleTimeoutSecs int `json:"idle_timeout_secs,omitempty"`
	// Triggered marks a run a trigger started.
	Triggered bool `json:"triggered,omitempty"`
}

// RunGuardrails carries the install's sandbox settings and the agent type's
// loop thresholds for one run.
type RunGuardrails struct {
	SandboxProvisionAttempts    int `json:"sandbox_provision_attempts,omitempty"`
	SandboxProvisionBackoffSecs int `json:"sandbox_provision_backoff_secs,omitempty"`
	SandboxProvisionBudgetSecs  int `json:"sandbox_provision_budget_secs,omitempty"`
	RepeatedCallWarn            int `json:"repeated_call_warn,omitempty"`
	RepeatedCallPause           int `json:"repeated_call_pause,omitempty"`
	WaitRearmWarn               int `json:"wait_rearm_warn,omitempty"`
	WaitRearmPause              int `json:"wait_rearm_pause,omitempty"`
}

// CreateSession starts a new agent subprocess on the target pod.
// Returns ErrSessionConflict if a session for the same run ID already exists.
func (c *Client) CreateSession(ctx context.Context, pod Pod, req CreateSessionRequest) error {
	body, err := c.EncodeJSON(req)
	if err != nil {
		return fmt.Errorf("encode create session: %w", err)
	}

	header, err := c.authorize(ctx, http.Header{"Content-Type": {"application/json"}})
	if err != nil {
		return err
	}

	_, span, resp, err := c.Do(observability.WithTenant(observability.WithRun(ctx, req.RunID), req.TenantID), httpclient.Request{
		Method: http.MethodPost, BaseURL: pod.URL, Host: pod.Host, Path: "/session", Header: header, Body: body,
	})
	defer span.End()
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusCreated:
		return nil
	case http.StatusConflict:
		return c.HandleErrorResponse(span, resp, ErrSessionConflict)
	default:
		return &StatusError{StatusCode: resp.StatusCode, Err: c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)}
	}
}

// From is who sent a message. The session manager turns it into the header line
// the agent reads, so the agent can tell senders apart.
// Kind is "person", "agent" or "trigger". A trigger with no ID or Name is unidentified.
type From struct {
	Kind       string `json:"kind"`
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	Email      string `json:"email,omitempty"`
	DeliveryID string `json:"delivery_id,omitempty"`
}

// SendMessage delivers a user message to an active session on the target pod.
// A non-zero idleTimeoutSecs replaces the session's idle timeout from this message on.
// fromPerson marks a message a person sent.
// from names the sender for the agent. Nil sends no header.
// Returns ErrSessionNotFound if no session exists for the given run ID.
func (c *Client) SendMessage(ctx context.Context, pod Pod, runID, content string, idleTimeoutSecs int, fromPerson bool, from *From) error {
	payload := map[string]any{"content": content}
	if idleTimeoutSecs > 0 {
		payload["idle_timeout_secs"] = idleTimeoutSecs
	}
	if fromPerson {
		payload["from_person"] = true
	}
	if from != nil {
		payload["from"] = from
	}
	body, err := c.EncodeJSON(payload)
	if err != nil {
		return fmt.Errorf("encode send message: %w", err)
	}

	header, err := c.authorize(ctx, http.Header{"Content-Type": {"application/json"}})
	if err != nil {
		return err
	}

	_, span, resp, err := c.Do(observability.WithRun(ctx, runID), httpclient.Request{
		Method: http.MethodPost, BaseURL: pod.URL, Host: pod.Host, Path: "/session/{runID}/messages", Params: []string{runID},
		Header: header, Body: body,
	})
	defer span.End()
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil
	case http.StatusNotFound:
		return c.HandleErrorResponse(span, resp, ErrSessionNotFound)
	default:
		return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}

// InterruptSession stops the agent's current in-progress turn on the target
// pod without killing the session.
// Returns ErrSessionNotFound if no session exists for the given run ID.
func (c *Client) InterruptSession(ctx context.Context, pod Pod, runID string) error {
	header, err := c.authorize(ctx, http.Header{})
	if err != nil {
		return err
	}

	_, span, resp, err := c.Do(observability.WithRun(ctx, runID), httpclient.Request{
		Method: http.MethodPost, BaseURL: pod.URL, Host: pod.Host, Path: "/session/{runID}/interrupt", Params: []string{runID}, Header: header,
	})
	defer span.End()
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil
	case http.StatusNotFound:
		return c.HandleErrorResponse(span, resp, ErrSessionNotFound)
	default:
		return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}

// CloseSession terminates the session and stops the agent subprocess on the target pod.
// Returns ErrSessionNotFound if no session exists for the given run ID.
func (c *Client) CloseSession(ctx context.Context, pod Pod, runID string) error {
	header, err := c.authorize(ctx, http.Header{})
	if err != nil {
		return err
	}

	_, span, resp, err := c.Do(observability.WithRun(ctx, runID), httpclient.Request{
		Method: http.MethodDelete, BaseURL: pod.URL, Host: pod.Host, Path: "/session/{runID}", Params: []string{runID}, Header: header,
	})
	defer span.End()
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return c.HandleErrorResponse(span, resp, ErrSessionNotFound)
	default:
		return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}

// Memory is a pod's memory report: its working set (memory.current minus
// inactive_file) and memory.max, zero when the container has no limit.
type Memory struct {
	WorkingSetBytes int64 `json:"working_set_bytes"`
	LimitBytes      int64 `json:"limit_bytes"`
}

// Memory reads the target pod's working set and memory limit.
func (c *Client) Memory(ctx context.Context, pod Pod) (Memory, error) {
	header, err := c.authorize(ctx, http.Header{})
	if err != nil {
		return Memory{}, err
	}

	_, span, resp, err := c.Do(ctx, httpclient.Request{Method: http.MethodGet, BaseURL: pod.URL, Host: pod.Host, Path: "/memory", Header: header})
	defer span.End()
	if err != nil {
		return Memory{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Memory{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
	var m Memory
	if err := c.DecodeJSON(span, resp, &m); err != nil {
		return Memory{}, err
	}
	return m, nil
}

// Bound reports whether the target pod serves its agent: GET /health/bound
// returns 200 once the pod's identity-proxy holds the agent token, and 503
// before. The call reaches the pod only when Envoy presents the agent
// certificate (docs/adr/2026-11-22-agent-cold-tier.md).
func (c *Client) Bound(ctx context.Context, pod Pod) (bool, error) {
	header, err := c.authorize(ctx, http.Header{})
	if err != nil {
		return false, err
	}

	_, span, resp, err := c.Do(ctx, httpclient.Request{Method: http.MethodGet, BaseURL: pod.URL, Host: pod.Host, Path: "/health/bound", Header: header})
	defer span.End()
	if err != nil {
		return false, err
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusServiceUnavailable:
		return false, nil
	default:
		return false, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}
