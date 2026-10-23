// Package agents provides an HTTP client for the kora-agents control-plane API.
//
// It is used by other platform services (e.g. the Triggers feature hosted in the
// identity binary) to start agent runs non-interactively. The caller supplies a
// platform bearer token per call — for trigger-initiated runs this is a
// short-lived SERVICE JWT minted for the webhook source's mapped identity, so
// kora-agents attributes and authorizes the run exactly as it would any
// principal. See docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md.
package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	// ErrRunNotFound is returned when the run does not exist in the caller's tenant.
	ErrRunNotFound = errors.New("run not found")
	// ErrUnexpectedStatus is returned when kora-agents returns an unexpected status.
	ErrUnexpectedStatus = errors.New("unexpected status from kora-agents")
	// ErrUpstreamUnavailable is returned when kora-agents itself failed rather
	// than refusing the request: a 5xx, most commonly pod capacity exhaustion.
	//
	// Distinct from ErrUnexpectedStatus so a caller can tell "your request was
	// wrong" from "the service is having a bad time". The two have opposite
	// remedies, and a caller that counts failures against the requester needs to
	// not count this one.
	ErrUpstreamUnavailable = errors.New("kora-agents unavailable")
	// ErrRunNotAcceptingMessages is returned when the run has left active and
	// idle, so it cannot take a message.
	ErrRunNotAcceptingMessages = errors.New("run is not accepting messages")
	// ErrRunStarting is returned when the run has not started yet and will take
	// messages once it is active.
	ErrRunStarting = errors.New("run is starting")
)

// Client calls the kora-agents control-plane API.
type Client struct {
	*httpclient.Base
	baseURL string
}

// NewClient creates a Client targeting the kora-agents base URL,
// e.g. "http://orbit-agents.orbit-system.svc". The returned client is safe for
// concurrent use and should be shared across calls.
func NewClient(baseURL string, logger logging.Logger) *Client {
	return &Client{
		Base:    httpclient.New(tracerName, logger, clientTimeout),
		baseURL: baseURL,
	}
}

// AgentType is the subset of a kora-agents agent type that callers need to
// validate a binding before granting run scopes: its canonical slug, owning
// tenant, and identity model. Name and Description are the text trigger
// routing asks the decision model about.
type AgentType struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// IdentityMode is the agent's identity model — "autonomous" or "delegated"
	// (docs/adr/2026-07-23-delegated-agent-identity.md). Empty (an older
	// server) reads as autonomous. Triggers refuse to bind delegated agents:
	// only a person may start their runs.
	IdentityMode string `json:"identity_mode"`
	// CanRun reports whether the bearer that read the type may start its runs. Absent reads as false.
	CanRun bool `json:"can_run"`
	// MCPTools are the agent's governed tool grants, as mcp:<server>:<tool>:use scopes.
	MCPTools []string `json:"mcp_tools"`
	// EffectiveConnectors are the agent's own connectors merged with its environments', by connection.
	// Only ListAgentTypesByIDs fills it.
	EffectiveConnectors []Connector `json:"effective_connectors"`
}

// Connector is a connection an agent holds and the actions it may take through it.
type Connector struct {
	Connection string   `json:"connection"`
	Actions    []string `json:"actions"`
}

// IdentityModeDelegated is the AgentType.IdentityMode value marking an agent
// that acts on behalf of the person who runs it. Kept as a plain string so the
// client stays decoupled from the kora-agents feature package.
const IdentityModeDelegated = "delegated"

// Caller is the identity a read is sent as: the caller's platform bearer and,
// for a delegated run, its delegation token.
type Caller struct {
	Bearer     string
	Delegation string
}

// GetAgentType resolves an agent type by id, authenticating as the caller.
// A delegation travels with the bearer so kora-agents evaluates the delegated run. kora-agents scopes the read to the token's tenant, so
// a type owned by another tenant (or absent) returns ErrAgentTypeNotFound. The
// Triggers feature uses this to confirm a trigger's target exists in-tenant and
// to derive the canonical slug the run grant must be keyed on.
func (c *Client) GetAgentType(ctx context.Context, caller Caller, id string) (AgentType, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/agent-types/"+url.PathEscape(id), nil)
	if err != nil {
		return AgentType{}, fmt.Errorf("build get agent type request: %w", err)
	}
	if caller.Bearer != "" {
		httpReq.Header.Set("Authorization", "Bearer "+caller.Bearer)
	}
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

// ListAgentTypes returns the agent types the caller may read in its tenant,
// each with the caller's can_run. A delegation travels with the bearer.
func (c *Client) ListAgentTypes(ctx context.Context, caller Caller) ([]AgentType, error) {
	return c.listAgentTypes(ctx, caller, "agents.Client.ListAgentTypes", "")
}

// ListAgentTypesByIDs returns the listed agent types the caller may read, each with its effective
// connectors, in one call. A type the caller cannot read is left out.
func (c *Client) ListAgentTypesByIDs(ctx context.Context, caller Caller, ids []string) ([]AgentType, error) {
	query := url.Values{"ids": {strings.Join(ids, ",")}, "include": {"effective_access"}}
	return c.listAgentTypes(ctx, caller, "agents.Client.ListAgentTypesByIDs", "?"+query.Encode())
}

func (c *Client) listAgentTypes(ctx context.Context, caller Caller, op, query string) ([]AgentType, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/agent-types"+query, nil)
	if err != nil {
		return nil, fmt.Errorf("build list agent types request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+caller.Bearer)
	if caller.Delegation != "" {
		httpReq.Header.Set(mw.DelegationTokenHeader, caller.Delegation)
	}

	_, span, resp, err := c.Do(ctx, op, httpReq)
	defer span.End()
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var out struct {
			AgentTypes []AgentType `json:"agent_types"`
		}
		if err := c.DecodeJSON(span, resp, &out); err != nil {
			return nil, fmt.Errorf("decode list agent types response: %w", err)
		}
		return out.AgentTypes, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, c.HandleErrorResponse(span, resp, ErrForbidden)
	default:
		if resp.StatusCode >= http.StatusInternalServerError {
			return nil, c.HandleErrorResponse(span, resp, ErrUpstreamUnavailable)
		}
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
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
	// kora-agents accepts them only from a SERVICE principal: a run's record of
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

// CanReadRun reports whether the bearer may read the run, by reading it from
// kora-agents, which owns the run visibility rules. It returns nil when the
// read succeeds, ErrForbidden when it is refused and ErrRunNotFound when the
// run is not in the caller's tenant. A delegated caller's delegation token is
// forwarded so kora-agents applies the delegated rules.
func (c *Client) CanReadRun(ctx context.Context, bearer, delegation, runID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/runs/"+url.PathEscape(runID), nil)
	if err != nil {
		return fmt.Errorf("build get run request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+bearer)
	httpReq.Header.Set("Accept", "application/json")
	if delegation != "" {
		httpReq.Header.Set(mw.DelegationTokenHeader, delegation)
	}

	_, span, resp, err := c.Do(ctx, "agents.Client.CanReadRun", httpReq)
	defer span.End()
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return c.HandleErrorResponse(span, resp, ErrForbidden)
	case http.StatusNotFound:
		return c.HandleErrorResponse(span, resp, ErrRunNotFound)
	default:
		if resp.StatusCode >= http.StatusInternalServerError {
			return c.HandleErrorResponse(span, resp, ErrUpstreamUnavailable)
		}
		return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}

// SendRunMessageRequest is the payload for SendRunMessage. TriggerID,
// TriggerName and DeliveryID name the trigger and delivery the message comes
// from, under the same rules as StartRunRequest's attribution.
type SendRunMessageRequest struct {
	Content     string `json:"content"`
	TriggerID   string `json:"trigger_id,omitempty"`
	TriggerName string `json:"trigger_name,omitempty"`
	DeliveryID  string `json:"delivery_id,omitempty"`
}

// SendRunMessage delivers a message to an active or idle run, authenticating
// with the supplied platform bearer token. A run that cannot take the message
// returns ErrRunNotAcceptingMessages, and one that has not started yet ErrRunStarting.
func (c *Client) SendRunMessage(ctx context.Context, bearer, runID string, req SendRunMessageRequest) error {
	body, err := c.EncodeJSON(req)
	if err != nil {
		return fmt.Errorf("encode send run message: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/runs/"+url.PathEscape(runID)+"/messages", body)
	if err != nil {
		return fmt.Errorf("build send run message request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+bearer)

	_, span, resp, err := c.Do(ctx, "agents.Client.SendRunMessage", httpReq)
	defer span.End()
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusAccepted, http.StatusOK, http.StatusNoContent:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return c.HandleErrorResponse(span, resp, ErrForbidden)
	case http.StatusNotFound:
		return c.HandleErrorResponse(span, resp, ErrRunNotFound)
	case http.StatusConflict:
		return c.HandleErrorResponse(span, resp, conflictReason(resp))
	default:
		if resp.StatusCode >= http.StatusInternalServerError {
			return c.HandleErrorResponse(span, resp, ErrUpstreamUnavailable)
		}
		return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}

// conflictReason reads the reason detail of a 409 from POST
// /api/v1/runs/{id}/messages, leaving the body for the error that wraps it.
func conflictReason(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body = io.NopCloser(bytes.NewReader(body))
	var detail struct {
		Details struct {
			Reason string `json:"reason"`
		} `json:"details"`
	}
	if json.Unmarshal(body, &detail) == nil && detail.Details.Reason == "run_starting" {
		return ErrRunStarting
	}
	return ErrRunNotAcceptingMessages
}

// ListRunsQuery filters ListRuns. Empty fields do not filter, and a zero Limit
// takes the server's default.
type ListRunsQuery struct {
	AgentTypeIDs []string
	States       []string
	// StartedByTrigger keeps only runs a trigger started.
	StartedByTrigger bool
	Limit            int
}

// Run is the subset of a kora-agents run that callers need to pick a run to message.
type Run struct {
	ID          string            `json:"id"`
	AgentTypeID string            `json:"agent_type_id"`
	State       string            `json:"state"`
	Summary     *string           `json:"summary"`
	StartedAt   *time.Time        `json:"started_at"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	StartedBy   *StartedByTrigger `json:"started_by,omitempty"`
}

// StartedByTrigger names the trigger and delivery that started a run.
type StartedByTrigger struct {
	TriggerID   string `json:"trigger_id"`
	TriggerName string `json:"trigger_name,omitempty"`
	DeliveryID  string `json:"delivery_id,omitempty"`
}

// ListRuns returns the first page of runs the bearer may read, newest first.
// kora-agents scopes the list to the token's tenant and run grants.
func (c *Client) ListRuns(ctx context.Context, bearer string, q ListRunsQuery) ([]Run, error) {
	params := url.Values{}
	for _, id := range q.AgentTypeIDs {
		params.Add("agent_type_id", id)
	}
	for _, st := range q.States {
		params.Add("state", st)
	}
	if q.StartedByTrigger {
		params.Set("started_by", "trigger")
	}
	if q.Limit > 0 {
		params.Set("limit", strconv.Itoa(q.Limit))
	}
	target := c.baseURL + "/api/v1/runs"
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build list runs request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+bearer)

	_, span, resp, err := c.Do(ctx, "agents.Client.ListRuns", httpReq)
	defer span.End()
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var out struct {
			Runs []Run `json:"runs"`
		}
		if err := c.DecodeJSON(span, resp, &out); err != nil {
			return nil, fmt.Errorf("decode list runs response: %w", err)
		}
		return out.Runs, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, c.HandleErrorResponse(span, resp, ErrForbidden)
	default:
		if resp.StatusCode >= http.StatusInternalServerError {
			return nil, c.HandleErrorResponse(span, resp, ErrUpstreamUnavailable)
		}
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}
