// Package agents provides an HTTP client for the orbit-agents control-plane API.
//
// It is used by other platform services (e.g. the Triggers feature hosted in the
// identity binary) to start agent runs non-interactively. The caller supplies a
// platform bearer token per call — for trigger-initiated runs this is a
// short-lived SERVICE JWT minted for the webhook source's mapped identity, so
// orbit-agents attributes and authorizes the run exactly as it would any
// principal. See docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md.
package agents

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"context"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	tracerName    = "orbit.agents-client"
	clientTimeout = 30 * time.Second
)

var (
	// ErrForbidden is returned when the caller's token lacks the required run scope.
	ErrForbidden = errors.New("forbidden: token lacks required run scope")
	// ErrAgentTypeNotFound is returned when the target agent type does not exist.
	ErrAgentTypeNotFound = errors.New("agent type not found")
	// ErrUnexpectedStatus is returned when orbit-agents returns an unexpected status.
	ErrUnexpectedStatus = errors.New("unexpected status from orbit-agents")
)

// Client calls the orbit-agents control-plane API.
type Client struct {
	*httpclient.Base
	baseURL string
}

// NewClient creates a Client targeting the orbit-agents base URL,
// e.g. "http://orbit-agents.orbit-system.svc". The returned client is safe for
// concurrent use and should be shared across calls.
func NewClient(baseURL string, logger logging.Logger) *Client {
	return &Client{
		Base:    httpclient.New(tracerName, logger, clientTimeout),
		baseURL: baseURL,
	}
}

// StartRunRequest is the payload for StartRun. tenant_id and created_by are
// derived server-side from the verified token, so they are intentionally absent.
type StartRunRequest struct {
	AgentTypeID      string `json:"agent_type_id"`
	InitialMessage   string `json:"initial_message,omitempty"`
	ProvisionSandbox bool   `json:"provision_sandbox,omitempty"`
}

// StartRunResponse captures the subset of the created run the caller needs.
type StartRunResponse struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// StartRun creates a run for the given agent type, authenticating with the
// supplied platform bearer token. Returns the created run's id and state.
func (c *Client) StartRun(ctx context.Context, bearer string, req StartRunRequest) (StartRunResponse, error) {
	body, err := c.EncodeJSON(req)
	if err != nil {
		return StartRunResponse{}, fmt.Errorf("encode start run: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/runs", body)
	if err != nil {
		return StartRunResponse{}, fmt.Errorf("build start run request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+bearer)

	_, span, resp, err := c.Do(ctx, "agents.Client.StartRun", httpReq)
	defer span.End()
	if err != nil {
		return StartRunResponse{}, err
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		var out StartRunResponse
		if err := c.DecodeJSON(span, resp, &out); err != nil {
			return StartRunResponse{}, fmt.Errorf("decode start run response: %w", err)
		}
		return out, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return StartRunResponse{}, c.HandleErrorResponse(span, resp, ErrForbidden)
	case http.StatusNotFound:
		return StartRunResponse{}, c.HandleErrorResponse(span, resp, ErrAgentTypeNotFound)
	default:
		return StartRunResponse{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}
