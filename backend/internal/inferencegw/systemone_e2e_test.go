package inferencegw_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/compatible"
	"github.com/clear-route/orbit/internal/platform/api"
	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders/v1alpha1"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// allowAll is a policy evaluator that allows every check; the methods this
// path never calls are left to the nil embedded interface.
type allowAll struct{ policy.Evaluator }

func (allowAll) Check(context.Context, policy.CheckInput) (policy.Result, error) {
	return policy.Result{Allow: true}, nil
}

// usageSink records usage rows in memory.
type usageSink struct {
	inferencegw.InferenceUsageRepository
	mu   sync.Mutex
	rows []inferencegw.InferenceUsageRecord
}

func (u *usageSink) Create(_ context.Context, rec inferencegw.InferenceUsageRecord) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.rows = append(u.rows, rec)
	return nil
}

// TestSystemOneEndToEnd sends a TypeSafe-shaped request through the real
// route, service, routing engine and compatible connector to an httptest
// System One stub, and checks the response and the usage row. Only the
// network peer and the database are faked.
func TestSystemOneEndToEnd(t *testing.T) {
	const upstreamResponse = `{"model":"jev-latest","answers":{"urgent":{"yes":0.91,"no":0.09},` +
		`"team":{"billing":0.7,"support":0.3},"severity":{"1":0.1,"2":0.2,"3":0.7}},` +
		`"usage":{"input_tokens":392,"output_tokens":65}}`
	// The request as the TypeSafe SDK sends it: state, model, and typed questions.
	const request = `{"state":{"subject":"Refund not received","body":"I was charged twice."},` +
		`"model":"decide/jev","questions":{` +
		`"urgent":{"type":"noul","instructions":"Is this urgent?"},` +
		`"team":{"type":"choice","instructions":"Which team?","options":["billing","support"]},` +
		`"severity":{"type":"score","instructions":"How severe?","levels":3}}}`

	var gotPath, gotAuth, gotBody string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPath, gotAuth, gotBody = r.URL.Path, r.Header.Get("Authorization"), string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamResponse)
	}))
	t.Cleanup(stub.Close)

	logger := logging.NewLoggerWithOutput(io.Discard)
	conn, err := compatible.New(compatible.Config{
		BaseURL: stub.URL + "/v1", APIKey: "ts-secret", APIs: []ip.API{ip.APISystemOne},
	}, "compatible:typesafe", logger)
	if err != nil {
		t.Fatal(err)
	}

	router := inferencegw.NewRoutingEngine(map[inferencegw.ProviderName]inferencegw.ProviderConnector{
		"compatible:typesafe": conn,
	}, nil, nil)
	inferencegw.LoadTestCatalog(router, []inferencegw.VirtualModel{{
		Family: "decide", Name: "jev", Enabled: true, Strategy: inferencegw.VirtualStrategyPriority,
		Routes: []inferencegw.VirtualModelRoute{{Provider: "compatible:typesafe", ModelID: "jev-latest", Priority: 1, Enabled: true}},
	}})

	// Jev: $0.042 per million input tokens, output free.
	pricing := inferencegw.NewStaticPricingCache([]inferencegw.ModelPricing{{
		Provider: "compatible:typesafe", ModelID: "jev-latest", InputCostPerMillion: 0.042,
	}})
	usage := &usageSink{}
	svc := inferencegw.NewService(allowAll{}, router, pricing, usage, nil, logger)

	mux := api.NewMux(context.Background(), logger)
	mux.SetBasePath("/v1")
	inferencegw.NewController(context.Background(), svc, nil, nil, "/v1", nil, logger).Boot(mux)

	claims := &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"},
		Orbit:            token.OrbitClaims{TenantID: "tenant-1", PrincipalType: token.PrincipalTypeHuman},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(request))
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if rec.Body.String() != upstreamResponse {
		t.Errorf("response changed:\n got %s\nwant %s", rec.Body, upstreamResponse)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	// The stub saw the provider's key and the target's model id; the caller's
	// token was not forwarded and nothing else in the body changed.
	if gotPath != "/v1/systemone" || gotAuth != "Bearer ts-secret" {
		t.Errorf("upstream saw path %q auth %q", gotPath, gotAuth)
	}
	if want := strings.Replace(request, `"model":"decide/jev"`, `"model":"jev-latest"`, 1); gotBody != want {
		t.Errorf("upstream body:\n got %s\nwant %s", gotBody, want)
	}

	if len(usage.rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(usage.rows))
	}
	row := usage.rows[0]
	if row.Status != inferencegw.UsageStatusSuccess || row.APIDialect != inferencegw.DialectOpenAI {
		t.Errorf("status/dialect = %s/%s", row.Status, row.APIDialect)
	}
	if row.TenantID != "tenant-1" || row.PrincipalID != "user-1" || row.ModelID != "jev-latest" {
		t.Errorf("attribution = %s/%s/%s", row.TenantID, row.PrincipalID, row.ModelID)
	}
	if row.PromptTokens != 392 || row.CompletionTokens != 65 || row.TotalTokens != 457 {
		t.Errorf("tokens = %d/%d/%d", row.PromptTokens, row.CompletionTokens, row.TotalTokens)
	}
	if want := 392 * 0.042 / 1e6; row.EstimatedCostUSD < want-1e-12 || row.EstimatedCostUSD > want+1e-12 {
		t.Errorf("cost = %v, want %v (input only, output free)", row.EstimatedCostUSD, want)
	}
	if row.PriceUnknown {
		t.Error("price_unknown set for a priced model")
	}
}
