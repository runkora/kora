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
	"strconv"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const tracerName = "github.com/runkora/kora/backend/internal/platform/clients/inference"

// DefaultTimeout bounds a call made through NewClient. Routing a trigger
// delivery needs a quick answer.
const DefaultTimeout = 15 * time.Second

// CodeBudgetNotReached is the gateway's 404 code for a run whose ceilings are all below their limits.
const CodeBudgetNotReached = "budget_not_reached"

// System One question types.
const (
	// QuestionTypeNoul is a yes or no question.
	QuestionTypeNoul = "noul"
	// QuestionTypeChoice is a question answered by one option out of its criteria.
	QuestionTypeChoice = "choice"
	// QuestionTypeScore is a question answered by one of its ordered levels.
	QuestionTypeScore = "score"
)

// Headers of a System One call. The gateway reads headerDecisionTimeout and
// sets the other two on its answer.
const (
	headerDecisionTimeout = "X-Kora-Decision-Timeout"
	headerUsageID         = "X-Kora-Usage-Id"
	headerCostUSD         = "X-Kora-Cost-USD"
)

// errBothCriteria is returned when a question sets both Criteria and Levels.
var errBothCriteria = errors.New("a question sets criteria or levels, not both")

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
// e.g. "http://kora-inference-gateway.kora-system.svc.cluster.local", with
// DefaultTimeout. The returned client is safe for concurrent use and should be
// shared across calls.
func NewClient(baseURL string, logger logging.Logger) *Client {
	return NewClientWithTimeout(baseURL, logger, DefaultTimeout)
}

// NewClientWithTimeout is NewClient with its own bound on each call. A call
// over a large state needs longer than DefaultTimeout.
func NewClientWithTimeout(baseURL string, logger logging.Logger, timeout time.Duration) *Client {
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
	// DecisionTimeout asks the gateway for a longer timeout on each upstream
	// attempt, rounded up to whole seconds. The gateway caps it at its
	// configured maximum. Zero keeps the gateway's 15 second default. The
	// gateway retries up to four attempts, so a client timeout at or below
	// DecisionTimeout leaves no time for a retry.
	DecisionTimeout time.Duration `json:"-"`
}

// Question is one typed question. The model never sees the question id.
type Question struct {
	Type         string
	Instructions string
	// Criteria maps each option name of a choice question to its description.
	Criteria map[string]string
	// Levels are the descriptions of a score question's levels, lowest first.
	Levels []string
}

// MarshalJSON writes Criteria or Levels as criteria. System One reads a map
// for a choice question and an ordered list for a score question.
func (q Question) MarshalJSON() ([]byte, error) {
	if len(q.Criteria) > 0 && len(q.Levels) > 0 {
		return nil, errBothCriteria
	}
	// An empty map or list is omitted, as the map field was before.
	var criteria any
	switch {
	case len(q.Criteria) > 0:
		criteria = q.Criteria
	case len(q.Levels) > 0:
		criteria = q.Levels
	}
	return json.Marshal(struct {
		Type         string `json:"type"`
		Instructions string `json:"instructions"`
		Criteria     any    `json:"criteria,omitempty"`
	}{q.Type, q.Instructions, criteria})
}

// SystemOneResponse is the decision model's answer, passed through by the
// gateway. UsageID and CostUSD come from the gateway's response headers.
// CostUSD is nil when the gateway sent no cost, which means the model has no price.
// UsageID names the inference_usage row and is empty when the gateway could
// not write it.
type SystemOneResponse struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
	UsageID string            `json:"-"`
	CostUSD *float64          `json:"-"`
}

// Answer is the answer to one question.
//
// A noul answer sets only Noul, the probability of yes. A choice answer sets
// Choice, the most probable option, and Probabilities keyed by option name. A
// score answer sets Score, the answered level index from 0, and Probabilities
// and Legend keyed by level index. Score is fractional when the probability is
// spread over levels.
type Answer struct {
	Type          string             `json:"type"`
	Noul          float64            `json:"noul"`
	Choice        string             `json:"choice"`
	Score         float64            `json:"score"`
	Legend        map[string]string  `json:"legend"`
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
	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: c.baseURL, Path: "/v1/systemone", Header: systemOneHeader(bearer, req.DecisionTimeout), Body: body,
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
		c.readUsageHeaders(ctx, resp, &out)
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

// systemOneHeader is the request header of a System One call. A positive
// timeout is sent in whole seconds, rounded up.
func systemOneHeader(bearer string, timeout time.Duration) http.Header {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+bearer)
	if timeout > 0 {
		secs := int64((timeout + time.Second - 1) / time.Second)
		header.Set(headerDecisionTimeout, strconv.FormatInt(secs, 10))
	}
	return header
}

// readUsageHeaders copies the usage row's id and cost from the gateway's headers.
func (c *Client) readUsageHeaders(ctx context.Context, resp *http.Response, out *SystemOneResponse) {
	out.UsageID = resp.Header.Get(headerUsageID)
	cost := resp.Header.Get(headerCostUSD)
	if cost == "" {
		return
	}
	v, err := strconv.ParseFloat(cost, 64)
	if err != nil {
		// The gateway has billed the call, so the answer is kept and the cost is unknown.
		c.Logger().Warn(ctx, "unparsable System One cost header",
			logging.Field{Key: "value", Value: cost}, logging.Field{Key: "error", Value: err.Error()})
		return
	}
	out.CostUSD = &v
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
