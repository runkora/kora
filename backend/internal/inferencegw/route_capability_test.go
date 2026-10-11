package inferencegw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	openai "github.com/openai/openai-go/v3"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// discovered stores models as a discovery sweep would.
func discovered(r *RoutingEngine, models ...ProviderModel) {
	r.modelCatalog.Store(&models)
}

func described(provider ProviderName, id string, caps ...ModelCapability) ProviderModel {
	return ProviderModel{Provider: provider, ModelID: id, Capabilities: caps}
}

func TestRefuseByCapability(t *testing.T) {
	t.Parallel()
	r := NewRoutingEngine(nil, nil, nil)
	discovered(r,
		described("mixed", "chat-a", CapabilityChat),
		described("mixed", "jev", CapabilityDecision),
		described("mixed", "embed-a", CapabilityEmbedding),
	)
	tests := []struct {
		model   string
		route   ModelCapability
		refused bool
	}{
		{"chat-a", CapabilityDecision, true},
		{"embed-a", CapabilityDecision, true},
		{"jev", CapabilityDecision, false},
		{"jev", CapabilityChat, true},
		{"jev", CapabilityEmbedding, true},
		{"chat-a", CapabilityChat, false},
		{"unknown", CapabilityDecision, false},
		{"unknown", CapabilityChat, false},
	}
	for _, tc := range tests {
		err := r.refuseByCapability(RouteTarget{Provider: "mixed", ModelID: tc.model}, tc.route)
		if (err != nil) != tc.refused {
			t.Errorf("%s on %s: err = %v, want refused %v", tc.model, tc.route, err, tc.refused)
			continue
		}
		var ce *ConnectorError
		if err != nil && (!errors.As(err, &ce) || ce.Code != "MODEL_NOT_SUPPORTED" || !ce.Retryable) {
			t.Errorf("%s on %s: err = %v, want a retryable MODEL_NOT_SUPPORTED", tc.model, tc.route, err)
		}
	}
}

func TestSystemOneRefusesAChatModelBeforeTheUpstream(t *testing.T) {
	t.Parallel()
	stub := &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody)}}
	env := newSystemOneEnv(t, allow(), map[ProviderName]ProviderConnector{"mixed": stub},
		decisionModel(VirtualModelRoute{Provider: "mixed", ModelID: "chat-a"}), nil)
	discovered(env.router, described("mixed", "chat-a", CapabilityChat))

	rec := env.post(systemOneBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var body struct{ Code, Message string }
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != "invalid_request" || !strings.Contains(body.Message, "does not serve the systemone API") {
		t.Errorf("body = %+v", body)
	}
	if len(stub.calls) != 0 {
		t.Errorf("the upstream was called %d times", len(stub.calls))
	}
}

func TestSystemOneFailsOverPastAChatModel(t *testing.T) {
	t.Parallel()
	chat := &systemOneStub{err: errors.New("forwarded to a chat model")}
	decide := &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody)}}
	env := newSystemOneEnv(t, allow(), map[ProviderName]ProviderConnector{"mixed": chat, "typesafe": decide},
		decisionModel(VirtualModelRoute{Provider: "mixed", ModelID: "chat-a"}, VirtualModelRoute{Provider: "typesafe", ModelID: "jev"}), nil)
	discovered(env.router, described("mixed", "chat-a", CapabilityChat), described("typesafe", "jev", CapabilityDecision))

	if rec := env.post(systemOneBody); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if len(chat.calls) != 0 || len(decide.calls) != 1 {
		t.Errorf("calls: chat %d, decision %d", len(chat.calls), len(decide.calls))
	}
}

// chatStub answers chat completions and counts them.
type chatStub struct {
	stubConnector
	mu    sync.Mutex
	calls int
}

func (s *chatStub) ChatCompletion(context.Context, openai.ChatCompletionNewParams) (*OpenAIResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return &OpenAIResult{Completion: &openai.ChatCompletion{}}, nil
}

func TestChatCompletionsRefuseADecisionModelBeforeTheUpstream(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		caps       ModelCapability
		wantStatus int
		wantCalls  int
	}{
		{"decision model is refused", CapabilityDecision, http.StatusBadRequest, 0},
		{"chat model is served", CapabilityChat, http.StatusOK, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub := &chatStub{}
			router := NewRoutingEngine(map[ProviderName]ProviderConnector{"mixed": stub}, nil, nil)
			router.LoadVirtualCatalog(newTestSnap(decisionModel(VirtualModelRoute{Provider: "mixed", ModelID: "m"}), nil))
			discovered(router, described("mixed", "m", tc.caps))
			logger := logging.NewLoggerWithOutput(io.Discard)
			svc := NewService(allow(), router, NewStaticPricingCache(nil), newMockUsageRepo(), &mockSpendCounter{}, logger)
			c := &Controller{svc: svc, logger: logger}

			req := httptest.NewRequest(http.MethodPost, "/chat/completions",
				strings.NewReader(`{"model":"decide/jev","messages":[{"role":"user","content":"hi"}]}`))
			req = req.WithContext(token.WithClaims(req.Context(), testClaims("tenant-1")))
			rec := httptest.NewRecorder()
			c.handleChatCompletions(rec, req)

			if rec.Code != tc.wantStatus || stub.calls != tc.wantCalls {
				t.Fatalf("status = %d, calls = %d, body %s", rec.Code, stub.calls, rec.Body)
			}
			if tc.wantStatus == http.StatusBadRequest {
				var body struct{ Code, Message string }
				_ = json.Unmarshal(rec.Body.Bytes(), &body)
				if body.Code != "model_not_supported" || !strings.Contains(body.Message, "POST /v1/systemone") {
					t.Errorf("body = %+v", body)
				}
			}
		})
	}
}

// A refusal made by the gateway must not hide a real upstream error from an
// earlier route.
func TestSystemOneKeepsTheUpstreamErrorOverALaterRefusal(t *testing.T) {
	t.Parallel()
	decide := &systemOneStub{err: upstreamErr("INVALID_REQUEST", false, http.StatusUnprocessableEntity, "questions.q1.instructions: Required")}
	env := newSystemOneEnv(t, allow(), map[ProviderName]ProviderConnector{"typesafe": decide, "mixed": &systemOneStub{}},
		decisionModel(VirtualModelRoute{Provider: "typesafe", ModelID: "jev"}, VirtualModelRoute{Provider: "mixed", ModelID: "chat-a"}), nil)
	discovered(env.router, described("typesafe", "jev", CapabilityDecision), described("mixed", "chat-a", CapabilityChat))

	rec := env.post(systemOneBody)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "instructions: Required") {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
}
