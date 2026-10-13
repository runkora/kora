package inferencegw

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// systemOneStub is a connector whose SystemOne is scripted; every other
// method comes from stubConnector.
type systemOneStub struct {
	stubConnector
	mu     sync.Mutex
	result *SystemOneResult
	err    error
	calls  []SystemOneRequest
}

func (s *systemOneStub) SystemOne(_ context.Context, req SystemOneRequest) (*SystemOneResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	return s.result, s.err
}

// budgetDenied allows the model policy check and refuses the budget check.
type budgetDenied struct{ *stubEvaluator }

func (budgetDenied) CheckBudget(context.Context, string, policy.BudgetCheckRequest) (policy.BudgetResult, error) {
	return policy.BudgetResult{Allow: false, HasLimit: true}, nil
}

const systemOneBody = `{"model":"decide/jev","state":"A  <b>user</b> & more","questions":{"q1":{"type":"noul","instructions":"x"}}}`

// upstream is the response with spacing and an HTML character, to prove it is not re-encoded.
const upstreamBody = `{"model":"jev-latest",  "answers":{"q1":{"yes":0.9}}, "note":"a<b","usage":{"input_tokens":1000000,"output_tokens":500000}}`

func refusal(provider ProviderName) error {
	return NewAPINotServedError(provider, "systemone")
}

func upstreamErr(code string, retryable bool, status int, msg string) error {
	return &ConnectorError{Provider: "typesafe", Code: code, Retryable: retryable, HTTPStatus: status,
		Underlying: errors.New(msg), ProviderMessage: msg}
}

func decisionModel(routes ...VirtualModelRoute) []VirtualModel {
	for i := range routes {
		routes[i].Enabled = true
		routes[i].Priority = i + 1
	}
	return []VirtualModel{{Family: "decide", Name: "jev", Enabled: true, Strategy: VirtualStrategyPriority, Routes: routes}}
}

type systemOneEnv struct {
	handler http.HandlerFunc
	usage   *mockInferenceUsageRepository
	router  *RoutingEngine
}

func newSystemOneEnv(t *testing.T, eval policy.Evaluator, conns map[ProviderName]ProviderConnector, vms []VirtualModel, prices []ModelPricing) systemOneEnv {
	t.Helper()
	router := NewRoutingEngine(conns, nil, nil)
	router.LoadVirtualCatalog(newTestSnap(vms, nil))
	repo := newMockUsageRepo()
	logger := logging.NewLoggerWithOutput(io.Discard)
	svc := NewService(eval, router, NewStaticPricingCache(prices), repo, &mockSpendCounter{}, logger)
	c := &Controller{svc: svc, logger: logger}
	return systemOneEnv{handler: c.handleSystemOne, usage: repo, router: router}
}

func (e systemOneEnv) post(body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/systemone", strings.NewReader(body))
	req = req.WithContext(token.WithClaims(req.Context(), testClaims("tenant-1")))
	rec := httptest.NewRecorder()
	e.handler(rec, req)
	return rec
}

func allow() *stubEvaluator {
	yes := true
	return &stubEvaluator{checkAllow: &yes}
}

func TestHandleSystemOne(t *testing.T) {
	t.Parallel()
	jev := ModelPricing{Provider: "typesafe", ModelID: "jev-latest", InputCostPerMillion: 0.042}

	tests := []struct {
		name        string
		stub        *systemOneStub
		prices      []ModelPricing
		body        string
		wantStatus  int
		wantCode    string
		wantMsg     string
		wantUsage   UsageStatus
		wantErrCode string
		wantCost    float64
		wantUnknown bool
	}{
		{
			name:       "success returns the upstream body unchanged, priced from the catalog",
			stub:       &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody), InputTokens: 1_000_000, OutputTokens: 500_000}},
			prices:     []ModelPricing{jev},
			body:       systemOneBody,
			wantStatus: 200, wantUsage: UsageStatusSuccess, wantCost: 0.042,
		},
		{
			name: "non-zero output price is charged",
			stub: &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody), InputTokens: 1_000_000, OutputTokens: 500_000}},
			prices: []ModelPricing{{Provider: "typesafe", ModelID: "jev-latest",
				InputCostPerMillion: 0.042, OutputCostPerMillion: 2}},
			body:       systemOneBody,
			wantStatus: 200, wantUsage: UsageStatusSuccess, wantCost: 1.042,
		},
		{
			name:       "no price writes price_unknown and zero cost",
			stub:       &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody), InputTokens: 1_000_000, OutputTokens: 500_000}},
			body:       systemOneBody,
			wantStatus: 200, wantUsage: UsageStatusSuccess, wantUnknown: true,
		},
		{
			name:       "upstream 422 is 422 with the upstream message",
			stub:       &systemOneStub{err: upstreamErr("INVALID_REQUEST", false, 422, "questions.q1: choice needs options")},
			body:       systemOneBody,
			wantStatus: 422, wantCode: "invalid_request", wantMsg: "questions.q1: choice needs options",
			wantUsage: UsageStatusProviderError, wantErrCode: "INVALID_REQUEST",
		},
		{
			name:       "upstream 400 is 400",
			stub:       &systemOneStub{err: upstreamErr("INVALID_REQUEST", false, 400, "bad state")},
			body:       systemOneBody,
			wantStatus: 400, wantCode: "invalid_request", wantMsg: "bad state",
			wantUsage: UsageStatusProviderError, wantErrCode: "INVALID_REQUEST",
		},
		{
			name:       "final 429 is 429 rate_limited",
			stub:       &systemOneStub{err: upstreamErr("THROTTLED", true, 429, "slow down")},
			body:       systemOneBody,
			wantStatus: 429, wantCode: "rate_limited",
			wantUsage: UsageStatusProviderError, wantErrCode: "THROTTLED",
		},
		{
			name:       "final 529 is 503 provider_error",
			stub:       &systemOneStub{err: upstreamErr("PROVIDER_ERROR", true, 529, "overloaded")},
			body:       systemOneBody,
			wantStatus: 503, wantCode: "provider_error",
			wantUsage: UsageStatusProviderError, wantErrCode: "PROVIDER_ERROR",
		},
		{
			name:       "final 500 is 503 provider_error",
			stub:       &systemOneStub{err: upstreamErr("PROVIDER_ERROR", true, 500, "boom")},
			body:       systemOneBody,
			wantStatus: 503, wantCode: "provider_error",
			wantUsage: UsageStatusProviderError, wantErrCode: "PROVIDER_ERROR",
		},
		{
			name:       "upstream 401 is 503 provider_error, never 401",
			stub:       &systemOneStub{err: upstreamErr("PROVIDER_ERROR", false, 401, "bad key")},
			body:       systemOneBody,
			wantStatus: 503, wantCode: "provider_error",
			wantUsage: UsageStatusProviderError, wantErrCode: "PROVIDER_ERROR",
		},
		{
			name:       "upstream 403 is 503 provider_error",
			stub:       &systemOneStub{err: upstreamErr("PROVIDER_ERROR", false, 403, "forbidden")},
			body:       systemOneBody,
			wantStatus: 503, wantCode: "provider_error",
			wantUsage: UsageStatusProviderError, wantErrCode: "PROVIDER_ERROR",
		},
		{
			name:       "no route serves systemone names the API",
			stub:       &systemOneStub{err: refusal("typesafe")},
			body:       systemOneBody,
			wantStatus: 400, wantCode: "invalid_request",
			wantMsg:   "provider typesafe does not serve the systemone API",
			wantUsage: UsageStatusProviderError, wantErrCode: "MODEL_NOT_SUPPORTED",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conns := map[ProviderName]ProviderConnector{"typesafe": tt.stub}
			env := newSystemOneEnv(t, allow(), conns, decisionModel(VirtualModelRoute{Provider: "typesafe", ModelID: "jev-latest"}), tt.prices)
			rec := env.post(tt.body)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
			}
			if tt.wantStatus == 200 {
				if rec.Body.String() != upstreamBody {
					t.Errorf("body changed:\n got %s\nwant %s", rec.Body, upstreamBody)
				}
				if got := tt.stub.calls[0]; got.Model != "jev-latest" || string(got.Body) != systemOneBody {
					t.Errorf("connector got model %q body %s", got.Model, got.Body)
				}
			} else {
				body := rec.Body.String()
				if !strings.Contains(body, `"`+tt.wantCode+`"`) {
					t.Errorf("body %s lacks code %q", body, tt.wantCode)
				}
				if tt.wantMsg != "" && !strings.Contains(body, tt.wantMsg) {
					t.Errorf("body %s lacks message %q", body, tt.wantMsg)
				}
				if rec.Header().Get("Retry-After") != "" {
					t.Error("Retry-After must not be forwarded")
				}
			}

			if len(env.usage.records) != 1 {
				t.Fatalf("usage rows = %d, want 1", len(env.usage.records))
			}
			row := env.usage.records[0]
			if row.Status != tt.wantUsage || row.APIDialect != DialectOpenAI {
				t.Errorf("row status/dialect = %s/%s", row.Status, row.APIDialect)
			}
			if row.TenantID != "tenant-1" || row.PrincipalID != "user-1" {
				t.Errorf("row attribution = %s/%s", row.TenantID, row.PrincipalID)
			}
			if tt.wantUsage == UsageStatusSuccess {
				if row.PromptTokens != 1_000_000 || row.CompletionTokens != 500_000 || row.TotalTokens != 1_500_000 {
					t.Errorf("tokens = %d/%d/%d", row.PromptTokens, row.CompletionTokens, row.TotalTokens)
				}
				if diff := row.EstimatedCostUSD - tt.wantCost; diff > 1e-9 || diff < -1e-9 {
					t.Errorf("cost = %v, want %v", row.EstimatedCostUSD, tt.wantCost)
				}
				if row.PriceUnknown != tt.wantUnknown {
					t.Errorf("price_unknown = %v, want %v", row.PriceUnknown, tt.wantUnknown)
				}
			} else if row.ErrorCode == nil || *row.ErrorCode != tt.wantErrCode {
				t.Errorf("error code = %v, want %s", row.ErrorCode, tt.wantErrCode)
			}
		})
	}
}

func TestHandleSystemOne_BudgetRefusedBeforeUpstream(t *testing.T) {
	t.Parallel()
	stub := &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody)}}
	env := newSystemOneEnv(t, budgetDenied{allow()},
		map[ProviderName]ProviderConnector{"typesafe": stub},
		decisionModel(VirtualModelRoute{Provider: "typesafe", ModelID: "jev-latest"}), nil)

	rec := env.post(systemOneBody)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "BUDGET_EXCEEDED") {
		t.Fatalf("got %d %s, want 403 BUDGET_EXCEEDED", rec.Code, rec.Body)
	}
	if len(stub.calls) != 0 {
		t.Errorf("upstream called %d times, want 0", len(stub.calls))
	}
	if len(env.usage.records) != 1 || env.usage.records[0].Status != UsageStatusBudgetExceeded {
		t.Errorf("usage rows = %+v, want one budget_exceeded row", env.usage.records)
	}
}

func TestHandleSystemOne_FailoverFromRefusingRoute(t *testing.T) {
	t.Parallel()
	refusing := &systemOneStub{err: refusal("bedrock")}
	serving := &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody), InputTokens: 10, OutputTokens: 1}}
	env := newSystemOneEnv(t, allow(),
		map[ProviderName]ProviderConnector{"bedrock": refusing, "typesafe": serving},
		decisionModel(
			VirtualModelRoute{Provider: "bedrock", ModelID: "chat-model"},
			VirtualModelRoute{Provider: "typesafe", ModelID: "jev-latest"},
		), nil)

	rec := env.post(systemOneBody)
	if rec.Code != 200 || rec.Body.String() != upstreamBody {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if len(serving.calls) != 1 || serving.calls[0].Model != "jev-latest" {
		t.Errorf("serving route calls = %+v", serving.calls)
	}
	row := env.usage.records[0]
	if row.Provider == nil || *row.Provider != "typesafe" || row.RetryCount == 0 {
		t.Errorf("row provider/retries = %v/%d, want typesafe after a retry", row.Provider, row.RetryCount)
	}
}

func TestHandleSystemOne_Envelope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"not an object", `["decide/jev"]`},
		{"string", `"decide/jev"`},
		{"null", `null`},
		{"not json", `{`},
		{"empty body", ``},
		{"missing model", `{"state":"x","questions":{}}`},
		{"empty model", `{"model":""}`},
		{"non-string model", `{"model":7}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody)}}
			env := newSystemOneEnv(t, allow(), map[ProviderName]ProviderConnector{"typesafe": stub},
				decisionModel(VirtualModelRoute{Provider: "typesafe", ModelID: "jev-latest"}), nil)
			rec := env.post(tt.body)
			if rec.Code != 400 {
				t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			if len(stub.calls) != 0 || len(env.usage.records) != 0 {
				t.Error("an invalid envelope must not reach the upstream or write usage")
			}
		})
	}
}

func TestHandleSystemOne_QuestionsAreNotValidated(t *testing.T) {
	t.Parallel()
	stub := &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody)}}
	env := newSystemOneEnv(t, allow(), map[ProviderName]ProviderConnector{"typesafe": stub},
		decisionModel(VirtualModelRoute{Provider: "typesafe", ModelID: "jev-latest"}), nil)
	body := `{"model":"decide/jev","state":42,"questions":"nonsense"}`
	if rec := env.post(body); rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if string(stub.calls[0].Body) != body {
		t.Errorf("body forwarded as %s", stub.calls[0].Body)
	}
}

func TestHandleSystemOne_BodyCap(t *testing.T) {
	t.Parallel()
	stub := &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody)}}
	env := newSystemOneEnv(t, allow(), map[ProviderName]ProviderConnector{"typesafe": stub},
		decisionModel(VirtualModelRoute{Provider: "typesafe", ModelID: "jev-latest"}), nil)

	pad := func(n int) string {
		return `{"model":"decide/jev","state":"` + strings.Repeat("a", n) + `"}`
	}
	over := env.post(pad(10 << 20))
	if over.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("over the cap: status = %d, want 413", over.Code)
	}
	if len(stub.calls) != 0 {
		t.Error("an oversized body must not reach the upstream")
	}
	if under := env.post(pad(10<<20 - 64)); under.Code != 200 {
		t.Errorf("under the cap: status = %d, want 200", under.Code)
	}
}
