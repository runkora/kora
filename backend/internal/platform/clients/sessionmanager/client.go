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

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	tracerName    = "orbit.sessionmanager-client"
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

// TokenFunc returns the caller's own platform token. orbit-agents supplies
// identity.Client.SelfTokenSource(), which exchanges the pod's SPIRE SVID for a
// SYSTEM token named "orbit-agents" and caches it until expiry — the same
// mechanism the policy client uses (internal/platform/clients/policy).
type TokenFunc func(ctx context.Context) (string, error)

// Client sends commands to a session-manager pod.
type Client struct {
	*httpclient.Base
	tokenFunc TokenFunc
}

// NewClient creates a Client. The returned client is safe for concurrent use
// and should be shared across calls — one Client per orbit-agents process.
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
func (c *Client) authorize(ctx context.Context, req *http.Request) error {
	tok, err := c.tokenFunc(ctx)
	if err != nil {
		return fmt.Errorf("obtain platform token for session-manager call: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return nil
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
	RunID        string   `json:"run_id"`
	AgentTypeID  string   `json:"agent_type_id"`
	TenantID     string   `json:"tenant_id"`
	Framework    string   `json:"framework,omitempty"`
	Model        string   `json:"model,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	// MCPServers are the distinct MCP servers the agent has tool grants on; the
	// runner provisions one governed endpoint (/v1/mcp/<server>) per entry.
	MCPServers []string `json:"mcp_servers,omitempty"`
}

// CreateSession starts a new agent subprocess on the target pod.
// Returns ErrSessionConflict if a session for the same run ID already exists.
func (c *Client) CreateSession(ctx context.Context, podURL string, req CreateSessionRequest) error {
	body, err := c.EncodeJSON(req)
	if err != nil {
		return fmt.Errorf("encode create session: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, podURL+"/session", body)
	if err != nil {
		return fmt.Errorf("build create session request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if err = c.authorize(ctx, httpReq); err != nil {
		return err
	}

	_, span, resp, err := c.Do(ctx, "sessionmanager.Client.CreateSession", httpReq)
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
		return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}

// SendMessage delivers a user message to an active session on the target pod.
// Returns ErrSessionNotFound if no session exists for the given run ID.
func (c *Client) SendMessage(ctx context.Context, podURL, runID, content string) error {
	body, err := c.EncodeJSON(map[string]string{"content": content})
	if err != nil {
		return fmt.Errorf("encode send message: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, podURL+"/session/"+runID+"/messages", body)
	if err != nil {
		return fmt.Errorf("build send message request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if err = c.authorize(ctx, httpReq); err != nil {
		return err
	}

	_, span, resp, err := c.Do(ctx, "sessionmanager.Client.SendMessage", httpReq)
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

// InterruptSession sends SIGINT to the Claude subprocess on the target pod,
// stopping the current in-progress turn without killing the session.
// Returns ErrSessionNotFound if no session exists for the given run ID.
func (c *Client) InterruptSession(ctx context.Context, podURL, runID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, podURL+"/session/"+runID+"/interrupt", http.NoBody)
	if err != nil {
		return fmt.Errorf("build interrupt session request: %w", err)
	}
	if err = c.authorize(ctx, httpReq); err != nil {
		return err
	}

	_, span, resp, err := c.Do(ctx, "sessionmanager.Client.InterruptSession", httpReq)
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

// CloseSession terminates the session and stops the Claude subprocess on the target pod.
// Returns ErrSessionNotFound if no session exists for the given run ID.
func (c *Client) CloseSession(ctx context.Context, podURL, runID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, podURL+"/session/"+runID, http.NoBody)
	if err != nil {
		return fmt.Errorf("build close session request: %w", err)
	}
	if err = c.authorize(ctx, httpReq); err != nil {
		return err
	}

	_, span, resp, err := c.Do(ctx, "sessionmanager.Client.CloseSession", httpReq)
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
