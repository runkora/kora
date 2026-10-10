package inferencegw

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// newModelsCapabilityController wires a Controller whose catalog holds four
// virtual models: chat-only, embedding-only, one spanning both capabilities
// through two routes, and one with no enabled routes (so no capabilities).
func newModelsCapabilityController(t *testing.T) *Controller {
	t.Helper()

	route := func(modelID string, enabled bool) VirtualModelRoute {
		return VirtualModelRoute{ID: modelID, Provider: ProviderBedrock, ModelID: modelID, Priority: 1, Enabled: enabled}
	}
	vms := []VirtualModel{
		{Family: "acme", Name: "chat-only", Strategy: VirtualStrategyPriority, Enabled: true,
			Routes: []VirtualModelRoute{route("chat-1", true)}},
		{Family: "acme", Name: "embed-only", Strategy: VirtualStrategyPriority, Enabled: true,
			Routes: []VirtualModelRoute{route("embed-1", true)}},
		{Family: "acme", Name: "multi", Strategy: VirtualStrategyPriority, Enabled: true,
			Routes: []VirtualModelRoute{route("chat-2", true), route("embed-2", true)}},
		{Family: "acme", Name: "no-routes", Strategy: VirtualStrategyPriority, Enabled: true,
			Routes: []VirtualModelRoute{route("chat-3", false)}},
	}
	conn := &stubConnector{name: ProviderBedrock, models: []ProviderModel{
		{Provider: ProviderBedrock, ModelID: "chat-1", Capabilities: []ModelCapability{CapabilityChat}},
		{Provider: ProviderBedrock, ModelID: "embed-1", Capabilities: []ModelCapability{CapabilityEmbedding}},
		{Provider: ProviderBedrock, ModelID: "chat-2", Capabilities: []ModelCapability{CapabilityChat}},
		{Provider: ProviderBedrock, ModelID: "embed-2", Capabilities: []ModelCapability{CapabilityEmbedding}},
		{Provider: ProviderBedrock, ModelID: "chat-3", Capabilities: []ModelCapability{CapabilityChat}},
	}}

	router := NewRoutingEngine(map[ProviderName]ProviderConnector{ProviderBedrock: conn}, nil, nil)
	router.LoadVirtualCatalog(newTestSnap(vms, nil))

	logger := logging.NewLoggerWithOutput(io.Discard)
	allowed := map[string]bool{}
	for _, vm := range vms {
		allowed[vm.Family+":"+vm.Name] = true
	}
	svc := NewService(&stubEvaluator{allowedCandidateIDs: allowed}, router, NewNopPricingCache(), nil, nil, logger)
	return &Controller{svc: svc, logger: logger}
}

func listModels(t *testing.T, c *Controller, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/models?"+rawQuery, nil)
	req = req.WithContext(token.WithClaims(req.Context(), &token.PlatformClaims{}))
	w := httptest.NewRecorder()
	c.handleListModels(w, req)
	return w
}

func TestHandleListModels_CapabilityFilter(t *testing.T) {
	t.Parallel()
	c := newModelsCapabilityController(t)

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"no parameter returns everything", "", []string{"acme/chat-only", "acme/embed-only", "acme/multi", "acme/no-routes"}},
		{"single value", "capability=chat", []string{"acme/chat-only", "acme/multi"}},
		{"repeated parameter", "capability=chat&capability=embedding", []string{"acme/chat-only", "acme/embed-only", "acme/multi"}},
		{"comma list", "capability=chat,embedding", []string{"acme/chat-only", "acme/embed-only", "acme/multi"}},
		{"whitespace and empty entries", "capability=" + url.QueryEscape(" embedding , ,") + "&capability=", []string{"acme/embed-only", "acme/multi"}},
		{"only empty entries is no filter", "capability=" + url.QueryEscape(" , "), []string{"acme/chat-only", "acme/embed-only", "acme/multi", "acme/no-routes"}},
		{"multi-capability model matches either value", "capability=embedding", []string{"acme/embed-only", "acme/multi"}},
		{"known capability with no models", "capability=rerank", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := listModels(t, c, tc.query)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Data []struct {
					ID           string   `json:"id"`
					Capabilities []string `json:"capabilities"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got := make([]string, 0, len(resp.Data))
			for _, m := range resp.Data {
				got = append(got, m.ID)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("ids = %v, want %v", got, tc.want)
			}
		})
	}
}

// A virtual model with no enabled routes has no capabilities, so it matches no
// filter value, though it still appears (as unavailable) without a filter.
func TestHandleListModels_NoRoutesMatchesNoFilter(t *testing.T) {
	t.Parallel()
	c := newModelsCapabilityController(t)
	for _, capability := range KnownCapabilities() {
		w := listModels(t, c, "capability="+string(capability))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; body: %s", capability, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "acme/no-routes") {
			t.Errorf("capability=%s returned the route-less model", capability)
		}
	}
}

func TestHandleListModels_UnknownCapability(t *testing.T) {
	t.Parallel()
	c := newModelsCapabilityController(t)

	for _, query := range []string{"capability=bogus", "capability=chat,bogus", "capability=chat&capability=bogus"} {
		w := listModels(t, c, query)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body: %s", query, w.Code, w.Body.String())
		}
		var resp struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Code != "invalid_request" {
			t.Errorf("%s: code = %q, want invalid_request", query, resp.Code)
		}
		if !strings.Contains(resp.Message, `"bogus"`) {
			t.Errorf("%s: message %q does not name the bad value", query, resp.Message)
		}
		for _, k := range KnownCapabilities() {
			if !strings.Contains(resp.Message, string(k)) {
				t.Errorf("%s: message %q does not list %q", query, resp.Message, k)
			}
		}
	}
}

func TestParseCapabilityFilter_UnknownIsSentinel(t *testing.T) {
	t.Parallel()
	_, err := parseCapabilityFilter([]string{"nope"})
	if !errors.Is(err, ErrUnknownCapability) {
		t.Errorf("err = %v, want ErrUnknownCapability", err)
	}
}
