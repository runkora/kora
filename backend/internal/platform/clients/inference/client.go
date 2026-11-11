// Package inference provides an HTTP client for the inference gateway's
// decision API, POST /v1/systemone (docs/adr/2026-10-10-systemone-decision-models.md),
// and its run budget lookup, GET /v1/budgets/runs/{id} (docs/adr/2026-11-11-run-page-record.md).
//
// The caller supplies a platform bearer token per call. The gateway applies the
// token's inference scopes, the tenant budget and the usage row, so a decision
// call made through this client is attributed and governed like any other.
// kora-triggers uses it to route a delivery (docs/adr/2026-10-19-trigger-routing.md).
// kora-agents uses it to record the ceiling that stopped a run.
package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	tracerName = "github.com/runkora/kora/backend/internal/platform/clients/inference"
	// clientTimeout matches the gateway's per-attempt decision timeout.
	clientTimeout = 15 * time.Second
)

// CodeBudgetNotReached is the gateway's 404 code for a run whose ceilings are all below their limits.
const CodeBudgetNotReached = "budget_not_reached"

// QuestionTypeChoice is a question answered by one option out of its criteria.
const QuestionTypeChoice = "choice"

var (
	// ErrInvalidRequest is returned when the gateway or the decision model
	// rejected the request (400 or 422).
	ErrInvalidRequest = errors.New("invalid decision request")
	// ErrForbidden is returned when the token may not invoke the model or the
	// tenant budget is exhausted (401 or 403).
	ErrForbidden = errors.New("forbidden: token may not invoke the decision model")
	// ErrModelNotFound is returned when the model is not in the catalog or is
	// disabled (404).
	ErrModelNotFound = errors.New("decision model not found")
	// ErrRateLimited is returned when the upstream provider is rate limiting (429).
	ErrRateLimited = errors.New("decision model rate limited")
	// ErrUpstreamUnavailable is returned for a 5xx, a 529 or a transport failure,
	// including the client timeout.
	ErrUpstreamUnavailable = errors.New("inference gateway unavailable")
	// ErrUnexpectedStatus is returned for any other status.
	ErrUnexpectedStatus = errors.New("unexpected status from inference gateway")
	// ErrBudgetNotReached is returned when the gateway answers 404 with CodeBudgetNotReached.
	ErrBudgetNotReached = errors.New("no spending ceiling of the run is reached")
)

// Client calls the inference gateway.
type Client struct {
	*httpclient.Base
	baseURL string
}

// NewClient creates a Client targeting the inference gateway base URL,
// e.g. "http://kora-inference-gateway.kora-system.svc.cluster.local". The
// returned client is safe for concurrent use and should be shared across calls.
func NewClient(baseURL string, logger logging.Logger) *Client {
	return newClient(baseURL, logger, clientTimeout)
}

func newClient(baseURL string, logger logging.Logger, timeout time.Duration) *Client {
	return &Client{
		Base:    httpclient.New(tracerName, logger, timeout),
		baseURL: baseURL,
	}
}

// SystemOneRequest is a System One decision request.
type SystemOneRequest struct {
	Model string `json:"model"`
	// State is the content the questions are asked about, encoded as JSON.
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// Question is one typed question. Criteria maps each option name to its
// description; the model never sees the question id.
type Question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

// SystemOneResponse is the decision model's answer, passed through by the gateway.
type SystemOneResponse struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Answer is the answer to one question. For a choice question, Choice is the
// option with the highest probability and Probabilities covers every option.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

// Usage is the token count the gateway prices the call from.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// SystemOne asks the decision model the request's questions, authenticating
// with the supplied platform bearer token.
func (c *Client) SystemOne(ctx context.Context, bearer string, req SystemOneRequest) (SystemOneResponse, error) {
	body, err := c.EncodeJSON(req)
	if err != nil {
		return SystemOneResponse{}, fmt.Errorf("encode systemone request: %w", err)
	}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+bearer)

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: c.baseURL, Path: "/v1/systemone", Header: header, Body: body,
	})
	defer span.End()
	if errors.Is(err, httpclient.ErrBuildRequest) {
		return SystemOneResponse{}, err
	}
	if err != nil {
		return SystemOneResponse{}, fmt.Errorf("%w: %w", ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch code := resp.StatusCode; {
	case code == http.StatusOK:
		var out SystemOneResponse
		if err := c.DecodeJSON(span, resp, &out); err != nil {
			return SystemOneResponse{}, fmt.Errorf("decode systemone response: %w", err)
		}
		return out, nil
	case code == http.StatusBadRequest || code == http.StatusUnprocessableEntity:
		return SystemOneResponse{}, c.HandleErrorResponse(span, resp, ErrInvalidRequest)
	case code == http.StatusNotFound:
		return SystemOneResponse{}, c.HandleErrorResponse(span, resp, ErrModelNotFound)
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return SystemOneResponse{}, c.HandleErrorResponse(span, resp, ErrForbidden)
	case code == http.StatusTooManyRequests:
		return SystemOneResponse{}, c.HandleErrorResponse(span, resp, ErrRateLimited)
	case code >= http.StatusInternalServerError:
		return SystemOneResponse{}, c.HandleErrorResponse(span, resp, ErrUpstreamUnavailable)
	default:
		return SystemOneResponse{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}

// RunBudget is the spending ceiling that stopped a run. Owner is the id the
// ceiling belongs to and OwnerName its readable name. The gateway leaves
// OwnerName empty for an agent and for a per-run ceiling. ResetsAt is nil for
// a per-run ceiling, which never resets.
type RunBudget struct {
	Scope     string     `json:"scope"`
	Owner     string     `json:"owner"`
	OwnerName string     `json:"owner_name"`
	Window    string     `json:"window"`
	LimitUSD  float64    `json:"limit_usd"`
	ResetsAt  *time.Time `json:"resets_at,omitempty"`
}

// RunBudget returns the ceiling the run's principal has reached. The gateway
// serves it to SYSTEM principals only: bearer is the caller's own service token.
func (c *Client) RunBudget(ctx context.Context, bearer, tenantID, runID string) (RunBudget, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+bearer)
	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/v1/budgets/runs/{runID}", Params: []string{runID},
		Query: url.Values{"tenant_id": {tenantID}}, Header: header,
	})
	defer span.End()
	if errors.Is(err, httpclient.ErrBuildRequest) {
		return RunBudget{}, err
	}
	if err != nil {
		return RunBudget{}, fmt.Errorf("%w: %w", ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch code := resp.StatusCode; {
	case code == http.StatusOK:
		var out RunBudget
		if err := c.DecodeJSON(span, resp, &out); err != nil {
			return RunBudget{}, fmt.Errorf("decode run budget: %w", err)
		}
		return out, nil
	case code == http.StatusNotFound:
		// Only the gateway's own answer means no ceiling is reached. A 404 from a
		// gateway without the route, or for a run with no usage, is unexpected.
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		var body struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(raw, &body) == nil && body.Code == CodeBudgetNotReached {
			return RunBudget{}, ErrBudgetNotReached
		}
		// The error keeps the gateway's message, read once above.
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		return RunBudget{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return RunBudget{}, c.HandleErrorResponse(span, resp, ErrForbidden)
	case code >= http.StatusInternalServerError:
		return RunBudget{}, c.HandleErrorResponse(span, resp, ErrUpstreamUnavailable)
	default:
		return RunBudget{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
}
