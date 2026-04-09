package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ErrPolicyGatewayUnexpectedStatus is returned when the policy gateway responds
// with a non-200 status code.
var ErrPolicyGatewayUnexpectedStatus = errors.New("policy gateway returned unexpected status")

// PolicyGatewayEvaluator implements PolicyEvaluator by calling the policy
// gateway's POST /v1/policy/check HTTP API (Layer 2 enforcement).
// Replace AllowAllPolicyEvaluator with this once the policy gateway is deployed.
type PolicyGatewayEvaluator struct {
	*httpclient.Base
	checkURL string
}

// NewPolicyGatewayEvaluator creates a PolicyGatewayEvaluator.
// checkURL is the full URL of the policy gateway check endpoint,
// e.g. http://policy-gateway.orbit-system.svc/v1/policy/check
func NewPolicyGatewayEvaluator(checkURL string, logger logging.Logger) *PolicyGatewayEvaluator {
	return &PolicyGatewayEvaluator{
		Base:     httpclient.New("orbit.policy-gateway-client", logger, 5*time.Second),
		checkURL: checkURL,
	}
}

type checkRequest struct {
	Token    string        `json:"token"`
	Resource checkResource `json:"resource"`
	Action   string        `json:"action"`
}

type checkResource struct {
	Type         string            `json:"type"`
	PathSegments []string          `json:"path,omitempty"`
	Attributes   map[string]string `json:"attributes,omitempty"`
}

type checkResponse struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason"`
}

// EvaluateInferenceRequest calls the policy gateway check API to determine
// whether the principal may invoke the given provider/model.
// The raw platform token must be embedded in the InferencePolicyRequest
// via the Token field (set by the inference gateway service layer).
func (e *PolicyGatewayEvaluator) EvaluateInferenceRequest(ctx context.Context, req *InferencePolicyRequest) (*PolicyDecision, error) {
	if req.RawToken == "" {
		// No token available — deny by default (fail-closed).
		return &PolicyDecision{Allowed: false, Reason: "no token provided for policy evaluation"}, nil
	}

	body := checkRequest{
		Token: req.RawToken,
		Resource: checkResource{
			Type:         "inference",
			PathSegments: []string{string(req.Provider), req.ModelID},
			Attributes: map[string]string{
				"provider": string(req.Provider),
				"model_id": req.ModelID,
			},
		},
		Action: "invoke",
	}

	bodyReader, err := e.EncodeJSON(body)
	if err != nil {
		return nil, fmt.Errorf("encode check request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.checkURL, bodyReader) //nolint:gosec // G704: checkURL is a configured service endpoint set at construction time, not user input
	if err != nil {
		return nil, fmt.Errorf("create check request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	_, span, resp, err := e.Do(ctx, "policy-gateway.Client.Check", httpReq)
	defer span.End()
	if err != nil {
		// Fail-closed: policy gateway unavailable → deny.
		return &PolicyDecision{Allowed: false, Reason: "policy gateway unavailable"}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &PolicyDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("policy gateway returned %d", resp.StatusCode),
		}, e.HandleErrorResponse(span, resp, ErrPolicyGatewayUnexpectedStatus)
	}

	var checkResp checkResponse
	if err := e.DecodeJSON(span, resp, &checkResp); err != nil {
		return nil, fmt.Errorf("decode check response: %w", err)
	}

	return &PolicyDecision{
		Allowed: checkResp.Allow,
		Reason:  checkResp.Reason,
	}, nil
}
