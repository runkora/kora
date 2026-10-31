// Package sessionmanager provides an HTTP client for the session-manager API
// that runs inside each agent StatefulSet pod.
//
// Each pod exposes the session-manager on port 8090 via a headless Service.
// Pods are addressable by ordinal through the StatefulSet DNS pattern:
//
//	http://<slug>-<ordinal>.<slug>.<namespace>.svc.cluster.local:8090
//
// Use PodURL to construct the target URL, then pass it to each method call.
//
// Usage:
//
//	c := sessionmanager.NewClient(logger, identityClient.SelfTokenSource())
//	url := sessionmanager.PodURL(at.Slug, podOrdinal, namespace)
//	if err := c.CreateSession(ctx, url, sessionmanager.CreateSessionRequest{...}); err != nil {
//	    return fmt.Errorf("create session: %w", err)
//	}
package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
		Base:      httpclient.New(tracerName, logger, clientTimeout),
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

// PodURL returns the base URL for a specific session-manager pod.
// slug is the agent type slug, ordinal is the StatefulSet pod index (0-based),
// and namespace is the tenant's Kubernetes namespace.
func PodURL(slug string, ordinal int, namespace string) string {
	return fmt.Sprintf("http://%s-%d.%s.%s.svc.cluster.local:%d", slug, ordinal, slug, namespace, podPort)
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
func (c *Client) CreateSession(ctx context.Context, podURL string, req CreateSessionRequest) error {
	body, err := c.EncodeJSON(req)
	if err != nil {
		return fmt.Errorf("encode create session: %w", err)
	}

	header, err := c.authorize(ctx, http.Header{"Content-Type": {"application/json"}})
	if err != nil {
		return err
	}

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: podURL, Path: "/session", Header: header, Body: body,
	})
	defer span.End()
	span.SetAttributes(observability.RunIDKey.String(req.RunID), observability.TenantIDKey.String(req.TenantID))
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

// SendMessage delivers a user message to an active session on the target pod.
// A non-zero idleTimeoutSecs replaces the session's idle timeout from this message on.
// fromPerson marks a message a person sent.
// Returns ErrSessionNotFound if no session exists for the given run ID.
func (c *Client) SendMessage(ctx context.Context, podURL, runID, content string, idleTimeoutSecs int, fromPerson bool) error {
	payload := map[string]any{"content": content}
	if idleTimeoutSecs > 0 {
		payload["idle_timeout_secs"] = idleTimeoutSecs
	}
	if fromPerson {
		payload["from_person"] = true
	}
	body, err := c.EncodeJSON(payload)
	if err != nil {
		return fmt.Errorf("encode send message: %w", err)
	}

	header, err := c.authorize(ctx, http.Header{"Content-Type": {"application/json"}})
	if err != nil {
		return err
	}

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: podURL, Path: "/session/{runID}/messages", Params: []string{runID},
		Header: header, Body: body,
	})
	defer span.End()
	span.SetAttributes(observability.RunIDKey.String(runID))
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

// InterruptSession sends SIGINT to the Claude subprocess on the target pod,
// stopping the current in-progress turn without killing the session.
// Returns ErrSessionNotFound if no session exists for the given run ID.
func (c *Client) InterruptSession(ctx context.Context, podURL, runID string) error {
	header, err := c.authorize(ctx, http.Header{})
	if err != nil {
		return err
	}

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: podURL, Path: "/session/{runID}/interrupt", Params: []string{runID}, Header: header,
	})
	defer span.End()
	span.SetAttributes(observability.RunIDKey.String(runID))
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

// CloseSession terminates the session and stops the Claude subprocess on the target pod.
// Returns ErrSessionNotFound if no session exists for the given run ID.
func (c *Client) CloseSession(ctx context.Context, podURL, runID string) error {
	header, err := c.authorize(ctx, http.Header{})
	if err != nil {
		return err
	}

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodDelete, BaseURL: podURL, Path: "/session/{runID}", Params: []string{runID}, Header: header,
	})
	defer span.End()
	span.SetAttributes(observability.RunIDKey.String(runID))
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
