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
	"github.com/runkora/kora/backend/internal/platform/observability"
)

const (
	tracerName    = "github.com/runkora/kora/backend/internal/platform/clients/agents"
	clientTimeout = 30 * time.Second
)

var (
	// ErrForbidden is returned when the caller's token lacks the required run scope.
	ErrForbidden = errors.New("forbidden: token lacks required run scope")
	// ErrUnauthorized is returned when kora-agents rejects the caller's token
	// itself, which a fresh token can clear.
	ErrUnauthorized = errors.New("unauthorized: kora-agents rejected the token")
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
// e.g. "http://kora-agents.kora-system.svc". The returned client is safe for
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
	header := http.Header{}
	if caller.Bearer != "" {
		header.Set("Authorization", "Bearer "+caller.Bearer)
	}
	if caller.Delegation != "" {
		header.Set(mw.DelegationTokenHeader, caller.Delegation)
	}

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/api/v1/agent-types/{id}", Params: []string{id}, Header: header,
	})
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
	return c.listAgentTypes(ctx, caller, nil)
}

// ListAgentTypesByIDs returns the listed agent types the caller may read, each with its effective
// connectors, in one call. A type the caller cannot read is left out.
func (c *Client) ListAgentTypesByIDs(ctx context.Context, caller Caller, ids []string) ([]AgentType, error) {
	return c.listAgentTypes(ctx, caller, url.Values{"ids": {strings.Join(ids, ",")}, "include": {"effective_access"}})
}

func (c *Client) listAgentTypes(ctx context.Context, caller Caller, query url.Values) ([]AgentType, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+caller.Bearer)
	if caller.Delegation != "" {
		header.Set(mw.DelegationTokenHeader, caller.Delegation)
	}

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/api/v1/agent-types", Query: query, Header: header,
	})
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

	// TriggerID, TriggerName, TriggerKind and DeliveryID attribute the run to
	// the trigger that started it, so a run detail can say "started by trigger
	// X, delivery Y" rather than showing the trigger's SERVICE principal id and
	// nothing else. Omitted for any other caller.
	//
	// kora-agents accepts them only from a SERVICE principal: a run's record of
	// why it exists is an audit claim, and one any caller could author about
	// itself would be worth nothing. TriggerName is stored as given, a snapshot
	// at start time, so a renamed or deleted trigger still reads as something a
	// person recognizes.
	TriggerID   string `json:"trigger_id,omitempty"`
	TriggerName string `json:"trigger_name,omitempty"`
	TriggerKind string `json:"trigger_kind,omitempty"`
	DeliveryID  string `json:"delivery_id,omitempty"`

	// CausedBy is the event id that led the trigger to fire, and ChainDepth
	// that event's depth plus one. kora-agents keeps them on the run.
	CausedBy   string `json:"caused_by,omitempty"`
	ChainDepth int    `json:"chain_depth,omitempty"`

	// Sources link to what started the run. kora-agents accepts them only from a SERVICE principal.
	Sources []RunSource `json:"sources,omitempty"`
}

// RunSource is a link to what started or joined a run. Kind is one of trigger,
// pull_request, repository or branch. Provider is empty or one of github, slack
// or alertmanager.
type RunSource struct {
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	Label    string `json:"label"`
	Provider string `json:"provider,omitempty"`
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

	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+bearer)

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: c.baseURL, Path: "/api/v1/runs", Header: header, Body: body,
	})
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
	header := http.Header{}
	header.Set("Authorization", "Bearer "+bearer)
	header.Set("Accept", "application/json")
	if delegation != "" {
		header.Set(mw.DelegationTokenHeader, delegation)
	}

	_, span, resp, err := c.Do(observability.WithRun(ctx, runID), httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/api/v1/runs/{runID}", Params: []string{runID}, Header: header,
	})
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

// RunTraceParent returns the run's stored W3C traceparent, empty when it has none. Only the
// egress gateway's SYSTEM token may call it. A missing run returns ErrRunNotFound.
func (c *Client) RunTraceParent(ctx context.Context, bearer, runID string) (string, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+bearer)

	_, span, resp, err := c.Do(observability.WithRun(ctx, runID), httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/internal/runs/{runID}/trace-parent", Params: []string{runID}, Header: header,
	})
	defer span.End()
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var out struct {
			TraceParent string `json:"trace_parent"`
		}
		if err := c.DecodeJSON(span, resp, &out); err != nil {
			return "", fmt.Errorf("decode run trace parent response: %w", err)
		}
		return out.TraceParent, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", c.HandleErrorResponse(span, resp, ErrForbidden)
	case http.StatusNotFound:
		return "", c.HandleErrorResponse(span, resp, ErrRunNotFound)
	default:
		if resp.StatusCode >= http.StatusInternalServerError {
			return "", c.HandleErrorResponse(span, resp, ErrUpstreamUnavailable)
		}
		return "", c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}

// RunChain is the event chain a run is in. It is empty for a run in none.
type RunChain struct {
	CausedBy   string   `json:"caused_by"`
	ChainDepth int      `json:"chain_depth"`
	TriggerIDs []string `json:"trigger_ids"`
}

// RunChain returns the chain of a run of agent in tenant, and an empty chain
// when the run is not that agent's. Only the kora-triggers SYSTEM token may call it.
func (c *Client) RunChain(ctx context.Context, bearer, runID, tenant, agent string) (RunChain, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+bearer)

	_, span, resp, err := c.Do(observability.WithRun(ctx, runID), httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/internal/runs/{runID}/chain", Params: []string{runID},
		Query: url.Values{"tenant": {tenant}, "agent": {agent}}, Header: header,
	})
	defer span.End()
	if err != nil {
		return RunChain{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var out RunChain
		if err := c.DecodeJSON(span, resp, &out); err != nil {
			return RunChain{}, fmt.Errorf("decode run chain response: %w", err)
		}
		return out, nil
	case http.StatusUnauthorized:
		return RunChain{}, c.HandleErrorResponse(span, resp, ErrUnauthorized)
	case http.StatusForbidden:
		return RunChain{}, c.HandleErrorResponse(span, resp, ErrForbidden)
	default:
		if resp.StatusCode >= http.StatusInternalServerError {
			return RunChain{}, c.HandleErrorResponse(span, resp, ErrUpstreamUnavailable)
		}
		return RunChain{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
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

	// CausedBy is the event id that led the trigger to fire, and ChainDepth
	// that event's depth plus one. kora-agents keeps them on the run.
	CausedBy   string `json:"caused_by,omitempty"`
	ChainDepth int    `json:"chain_depth,omitempty"`

	// Sources are appended to the run's sources, de-duplicated by URL.
	Sources []RunSource `json:"sources,omitempty"`
}

// SendRunMessage delivers a message to an active or idle run, authenticating
// with the supplied platform bearer token. A run that cannot take the message
// returns ErrRunNotAcceptingMessages, and one that has not started yet ErrRunStarting.
func (c *Client) SendRunMessage(ctx context.Context, bearer, runID string, req SendRunMessageRequest) error {
	body, err := c.EncodeJSON(req)
	if err != nil {
		return fmt.Errorf("encode send run message: %w", err)
	}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+bearer)

	_, span, resp, err := c.Do(observability.WithRun(ctx, runID), httpclient.Request{
		Method: http.MethodPost, BaseURL: c.baseURL, Path: "/api/v1/runs/{runID}/messages", Params: []string{runID}, Header: header, Body: body,
	})
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
	Sources     []RunSource       `json:"sources,omitempty"`
}

// StartedByTrigger names the trigger and delivery that started a run.
type StartedByTrigger struct {
	TriggerID   string `json:"trigger_id"`
	TriggerName string `json:"trigger_name,omitempty"`
	TriggerKind string `json:"trigger_kind,omitempty"`
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
	header := http.Header{}
	header.Set("Authorization", "Bearer "+bearer)

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/api/v1/runs", Query: params, Header: header,
	})
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
