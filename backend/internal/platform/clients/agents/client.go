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
	"net/url"
	"time"

	"context"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
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
	// ErrUpstreamUnavailable is returned when orbit-agents itself failed rather
	// than refusing the request: a 5xx, most commonly pod capacity exhaustion.
	//
	// Distinct from ErrUnexpectedStatus so a caller can tell "your request was
	// wrong" from "the service is having a bad time". The two have opposite
	// remedies, and a caller that counts failures against the requester needs to
	// not count this one.
	ErrUpstreamUnavailable = errors.New("orbit-agents unavailable")
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

// AgentType is the subset of an orbit-agents agent type that callers need to
// validate a binding before granting run scopes: its canonical slug, owning
// tenant, and identity model.
type AgentType struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Slug     string `json:"slug"`
	// IdentityMode is the agent's identity model — "autonomous" or "delegated"
	// (docs/adr/2026-07-23-delegated-agent-identity.md). Empty (an older
	// server) reads as autonomous. Triggers refuse to bind delegated agents:
	// only a person may start their runs.
	IdentityMode string `json:"identity_mode"`
	// CanRun reports whether the bearer that read the type may start its runs. Absent reads as false.
	CanRun bool `json:"can_run"`
}

// IdentityModeDelegated is the AgentType.IdentityMode value marking an agent
// that acts on behalf of the person who runs it. Kept as a plain string so the
// client stays decoupled from the orbit-agents feature package.
const IdentityModeDelegated = "delegated"

// Caller is the identity a read is sent as: the caller's platform bearer and,
// for a delegated run, its delegation token.
type Caller struct {
	Bearer     string
	Delegation string
}

// GetAgentType resolves an agent type by id, authenticating as the caller.
// A delegation travels with the bearer so orbit-agents evaluates the delegated run. orbit-agents scopes the read to the token's tenant, so
// a type owned by another tenant (or absent) returns ErrAgentTypeNotFound. The
// Triggers feature uses this to confirm a trigger's target exists in-tenant and
// to derive the canonical slug the run grant must be keyed on.
func (c *Client) GetAgentType(ctx context.Context, caller Caller, id string) (AgentType, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/agent-types/"+url.PathEscape(id), nil)
	if err != nil {
		return AgentType{}, fmt.Errorf("build get agent type request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+caller.Bearer)
	if caller.Delegation != "" {
		httpReq.Header.Set(mw.DelegationTokenHeader, caller.Delegation)
	}

	_, span, resp, err := c.Do(ctx, "agents.Client.GetAgentType", httpReq)
	defer span.End()
	if err != nil {
		return AgentType{}, err
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusOK:
		var out AgentType
		if err := c.DecodeJSON(span, resp, &out); err != nil {
			return AgentType{}, fmt.Errorf("decode agent type response: %w", err)
		}
		return out, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return AgentType{}, c.HandleErrorResponse(span, resp, ErrForbidden)
	case http.StatusNotFound:
		return AgentType{}, c.HandleErrorResponse(span, resp, ErrAgentTypeNotFound)
	default:
		return AgentType{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}

// StartRunRequest is the payload for StartRun. tenant_id and created_by are
// derived server-side from the verified token, so they are intentionally absent.
type StartRunRequest struct {
	AgentTypeID    string `json:"agent_type_id"`
	InitialMessage string `json:"initial_message,omitempty"`

	// TriggerID, TriggerName and DeliveryID attribute the run to the trigger
	// that started it, so a run detail can say "started by trigger X, delivery
	// Y" rather than showing the trigger's SERVICE principal id and nothing
	// else. Omitted for any other caller.
	//
	// orbit-agents accepts them only from a SERVICE principal: a run's record of
	// why it exists is an audit claim, and one any caller could author about
	// itself would be worth nothing. TriggerName is stored as given, a snapshot
	// at start time, so a renamed or deleted trigger still reads as something a
	// person recognizes.
	TriggerID   string `json:"trigger_id,omitempty"`
	TriggerName string `json:"trigger_name,omitempty"`
	DeliveryID  string `json:"delivery_id,omitempty"`
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
		if resp.StatusCode >= http.StatusInternalServerError {
			return StartRunResponse{}, c.HandleErrorResponse(span, resp, ErrUpstreamUnavailable)
		}
		return StartRunResponse{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}
