package egressgw

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// newTestMetrics builds the real instrument set on an SDK meter with a manual
// reader, so these tests assert on what a collector would actually receive
// rather than on a mock of the OTel API.
func newTestMetrics(t *testing.T) (*Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		_ = provider.Shutdown(context.Background())
	})
	return m, reader
}

// collect gathers one collection cycle, keyed by instrument name.
func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

// sumPoints extracts an Int64 sum's data points, keyed by every attribute
// value joined, for easy lookup in assertions.
func sumPoints(t *testing.T, metrics map[string]metricdata.Metrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()
	m, ok := metrics[name]
	if !ok {
		return nil
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("%s is %T, want a sum", name, m.Data)
	}
	return sum.DataPoints
}

func attr(dp metricdata.DataPoint[int64], key string) string {
	v, _ := dp.Attributes.Value(attribute.Key(key))
	return v.AsString()
}

// A nil *Metrics is a supported configuration: instrumentation is injected,
// and a caller that wired no meter must behave exactly as before.
func TestMetrics_NilIsSafe(t *testing.T) {
	var m *Metrics
	ctx := context.Background()
	m.recordDecision(ctx, decisionAllow, "github_app", "acme")
	m.recordMint(ctx, credentials.AdapterGitHubApp, "contents:write", "acme", 0, nil)
	if err := m.Close(); err != nil {
		t.Errorf("Close on nil = %v", err)
	}
}

// mintFailureReason must no longer classify the three resolution/open-time
// sentinels (ErrConnectionNotFound, ErrUnsupportedAdapter, ErrInvalidConfig):
// none of them is ever returned by a real Mint call, so a prior round's
// specific labels for them would be dead code if restored. Only a real
// Mint-time failure (ErrCredentialScopeRejected, or anything else) gets a
// reason here.
func TestMintFailureReason_OnlyRealMintErrorsClassified(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"scope rejected", credentials.ErrCredentialScopeRejected, "scope_rejected"},
		{"connection not found is not a mint reason", credentials.ErrConnectionNotFound, "other"},
		{"unsupported adapter is not a mint reason", credentials.ErrUnsupportedAdapter, "other"},
		{"invalid config is not a mint reason", credentials.ErrInvalidConfig, "other"},
		{"anything else", errors.New("boom"), "other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mintFailureReason(tc.err); got != tc.want {
				t.Errorf("mintFailureReason(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// A host with no registered connection reaches the passthrough adapter. That
// must land on decisions_total{outcome="passthrough"} and nowhere near the
// mint instruments: no credential was minted, so credentials_minted and
// mint_duration must carry zero data points, and the cardinality this guards
// is exactly the one the prior round's "fix" broke (a 0s sample under
// adapter="unknown" that drags mint_duration toward zero).
func TestAuthorize_UnregisteredHost_RecordsPassthroughNotMint(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	m, reader := newTestMetrics(t)
	eval := &stubEvaluator{allow: false} // passthrough must never consult policy
	broker := NewBroker(eval, &memConnRepo{items: map[string]*credentials.Connection{}}, NewAdapterRegistry(), passthrough.New(), logger, m)

	req, _ := http.NewRequest(http.MethodGet, "https://pypi.org/simple/", nil)
	_, err := broker.Authorize(context.Background(),
		claimsFor("acme", "spiffe://kora.test/tenant/kora-tenant-acme/agent/coder", "run-1"),
		FromHTTP(req))
	if err != nil {
		t.Fatalf("Authorize(unregistered host) = %v, want allow", err)
	}

	metrics := collect(t, reader)
	decisions := sumPoints(t, metrics, "kora.egress_gateway.decisions")
	if len(decisions) != 1 {
		t.Fatalf("decisions data points = %d, want 1: %+v", len(decisions), decisions)
	}
	if outcome, adapter := attr(decisions[0], "outcome"), attr(decisions[0], "adapter"); outcome != decisionPassthrough || adapter != string(credentials.AdapterPassthrough) {
		t.Errorf("decision = outcome=%q adapter=%q, want outcome=%q adapter=%q", outcome, adapter, decisionPassthrough, credentials.AdapterPassthrough)
	}
	if tenantID := attr(decisions[0], "tenant_id"); tenantID != "acme" {
		t.Errorf("decision tenant_id = %q, want acme", tenantID)
	}

	if pts := sumPoints(t, metrics, "kora.egress_gateway.credentials_minted"); len(pts) != 0 {
		t.Errorf("credentials_minted data points = %+v, want none: passthrough never mints", pts)
	}
	if _, exported := metrics["kora.egress_gateway.mint_duration"]; exported {
		if hist, ok := metrics["kora.egress_gateway.mint_duration"].Data.(metricdata.Histogram[float64]); ok && len(hist.DataPoints) != 0 {
			t.Errorf("mint_duration data points = %+v, want none: passthrough is not a mint attempt", hist.DataPoints)
		}
	}
}

// A connection naming an adapter type with no registered implementation is a
// genuine configuration problem, discovered before any adapter is opened or
// any Mint call attempted. It must land on decisions_total{outcome="error"}
// and must NOT produce a mint_failures sample — the prior round's "fix"
// recorded this as mint_failures{reason="unsupported_adapter"} even though
// no Mint was ever attempted.
func TestAuthorize_UnsupportedAdapter_RecordsDecisionErrorNotMintFailure(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	m, reader := newTestMetrics(t)
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	if err := repo.Create(context.Background(), &credentials.Connection{
		TenantID: "acme", Name: "public", Adapter: credentials.AdapterPassthrough, BaseHost: "example.com",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	// AdapterPassthrough is never registrable (Register refuses it), so
	// Lookup always misses it here, exactly the ErrUnsupportedAdapter path.
	broker := NewBroker(&stubEvaluator{allow: true}, repo, NewAdapterRegistry(), passthrough.New(), logger, m)

	req, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	_, err := broker.Authorize(context.Background(),
		claimsFor("acme", "spiffe://kora.test/tenant/kora-tenant-acme/agent/coder", "run-1"),
		FromHTTP(req))
	if !errors.Is(err, credentials.ErrUnsupportedAdapter) {
		t.Fatalf("Authorize = %v, want ErrUnsupportedAdapter", err)
	}

	metrics := collect(t, reader)
	decisions := sumPoints(t, metrics, "kora.egress_gateway.decisions")
	if len(decisions) != 1 {
		t.Fatalf("decisions data points = %d, want 1: %+v", len(decisions), decisions)
	}
	if outcome, adapter := attr(decisions[0], "outcome"), attr(decisions[0], "adapter"); outcome != decisionError || adapter != unknownAdapter {
		t.Errorf("decision = outcome=%q adapter=%q, want outcome=%q adapter=%q", outcome, adapter, decisionError, unknownAdapter)
	}

	if pts := sumPoints(t, metrics, "kora.egress_gateway.mint_failures"); len(pts) != 0 {
		t.Errorf("mint_failures data points = %+v, want none: no Mint was attempted", pts)
	}
}

// A connection kora cannot open (bad config) is also a genuine problem found
// before classification and before any Mint call, but with the adapter type
// already resolved. It lands on decisions_total{outcome="error",
// adapter=<real type>} and must not produce a mint_failures sample.
func TestAuthorize_UnreadableConnection_RecordsDecisionErrorNotMintFailure(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	m, reader := newTestMetrics(t)
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	if err := repo.Create(context.Background(), &credentials.Connection{
		TenantID: "acme", Name: "github", Adapter: credentials.AdapterGitHubApp, BaseHost: "github.com",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	registry := NewAdapterRegistry()
	registry.Register(badConfigAdapter{})
	broker := NewBroker(&stubEvaluator{allow: true}, repo, registry, passthrough.New(), logger, m)

	req, _ := http.NewRequest(http.MethodGet, "https://github.com/orbital/kora", nil)
	_, err := broker.Authorize(context.Background(),
		claimsFor("acme", "spiffe://kora.test/tenant/kora-tenant-acme/agent/coder", "run-1"),
		FromHTTP(req))
	if !errors.Is(err, credentials.ErrInvalidConfig) {
		t.Fatalf("Authorize = %v, want wrapped ErrInvalidConfig", err)
	}

	metrics := collect(t, reader)
	decisions := sumPoints(t, metrics, "kora.egress_gateway.decisions")
	if len(decisions) != 1 {
		t.Fatalf("decisions data points = %d, want 1: %+v", len(decisions), decisions)
	}
	if outcome, adapter := attr(decisions[0], "outcome"), attr(decisions[0], "adapter"); outcome != decisionError || adapter != string(credentials.AdapterGitHubApp) {
		t.Errorf("decision = outcome=%q adapter=%q, want outcome=%q adapter=%q", outcome, adapter, decisionError, credentials.AdapterGitHubApp)
	}

	if pts := sumPoints(t, metrics, "kora.egress_gateway.mint_failures"); len(pts) != 0 {
		t.Errorf("mint_failures data points = %+v, want none: no Mint was attempted", pts)
	}
}

// fakeMintAdapter is a minimal registered adapter whose connector's Mint
// either succeeds or fails as configured, so tests can drive the real
// Mint-call metrics without the githubapp adapter's JWT/HTTP machinery.
type fakeMintAdapter struct {
	typ     credentials.AdapterType
	mintErr error
}

func (a fakeMintAdapter) Type() credentials.AdapterType            { return a.typ }
func (a fakeMintAdapter) Actions() []credentials.ActionDeclaration { return nil }
func (a fakeMintAdapter) Open(credentials.Connection) (credentials.Connector, error) {
	return fakeMintConnector{mintErr: a.mintErr}, nil
}

type fakeMintConnector struct{ mintErr error }

func (fakeMintConnector) Classify(*http.Request) (credentials.Classification, error) {
	return credentials.Classification{Action: "use"}, nil
}

func (c fakeMintConnector) Mint(context.Context, credentials.CallerIdentity) (credentials.Credential, error) {
	if c.mintErr != nil {
		return credentials.Credential{}, c.mintErr
	}
	return credentials.Credential{Header: "Authorization", Value: "Bearer minted"}, nil
}

// A registered connection whose real Mint call succeeds must land on both
// decisions_total{outcome="allow"} and credentials_minted/mint_duration:
// this is exactly the "real Mint call" the narrowed instruments exist for.
func TestAuthorize_SuccessfulMint_RecordsAllowAndMint(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	m, reader := newTestMetrics(t)
	const typ credentials.AdapterType = "fake_mint"
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	if err := repo.Create(context.Background(), &credentials.Connection{
		TenantID: "acme", Name: "svc", Adapter: typ, BaseHost: "svc.example.com",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	registry := NewAdapterRegistry()
	registry.Register(fakeMintAdapter{typ: typ})
	broker := NewBroker(&stubEvaluator{allow: true}, repo, registry, passthrough.New(), logger, m)

	req, _ := http.NewRequest(http.MethodGet, "https://svc.example.com/", nil)
	if _, err := broker.Authorize(context.Background(),
		claimsFor("acme", "spiffe://kora.test/tenant/kora-tenant-acme/agent/coder", "run-1"),
		FromHTTP(req)); err != nil {
		t.Fatalf("Authorize = %v, want allow", err)
	}

	metrics := collect(t, reader)
	decisions := sumPoints(t, metrics, "kora.egress_gateway.decisions")
	if len(decisions) != 1 {
		t.Fatalf("decisions data points = %d, want 1: %+v", len(decisions), decisions)
	}
	if outcome, adapter := attr(decisions[0], "outcome"), attr(decisions[0], "adapter"); outcome != decisionAllow || adapter != string(typ) {
		t.Errorf("decision = outcome=%q adapter=%q, want outcome=%q adapter=%q", outcome, adapter, decisionAllow, typ)
	}

	minted := sumPoints(t, metrics, "kora.egress_gateway.credentials_minted")
	if len(minted) != 1 || minted[0].Value != 1 {
		t.Fatalf("credentials_minted data points = %+v, want exactly one sample of 1", minted)
	}
	hist, ok := metrics["kora.egress_gateway.mint_duration"].Data.(metricdata.Histogram[float64])
	if !ok || len(hist.DataPoints) != 1 {
		t.Fatalf("mint_duration = %+v, want exactly one data point", metrics["kora.egress_gateway.mint_duration"].Data)
	}
}

// The upstream refusing the requested credential scope is a real Mint-call
// failure: it must still land on mint_failures{reason="scope_rejected"}, and
// on the decisions counter as a deny (the caller is told no; nothing is
// operationally broken).
func TestAuthorize_MintScopeRejected_RecordsDenyAndMintFailure(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	m, reader := newTestMetrics(t)
	const typ credentials.AdapterType = "fake_mint"
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	if err := repo.Create(context.Background(), &credentials.Connection{
		TenantID: "acme", Name: "svc", Adapter: typ, BaseHost: "svc.example.com",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	registry := NewAdapterRegistry()
	registry.Register(fakeMintAdapter{typ: typ, mintErr: credentials.ErrCredentialScopeRejected})
	broker := NewBroker(&stubEvaluator{allow: true}, repo, registry, passthrough.New(), logger, m)

	req, _ := http.NewRequest(http.MethodGet, "https://svc.example.com/", nil)
	_, err := broker.Authorize(context.Background(),
		claimsFor("acme", "spiffe://kora.test/tenant/kora-tenant-acme/agent/coder", "run-1"),
		FromHTTP(req))
	var d *Denial
	if !errors.As(err, &d) || d.Reason != DenialCredentialRefused {
		t.Fatalf("Authorize = %v, want a DenialCredentialRefused", err)
	}

	metrics := collect(t, reader)
	decisions := sumPoints(t, metrics, "kora.egress_gateway.decisions")
	if len(decisions) != 1 {
		t.Fatalf("decisions data points = %d, want 1: %+v", len(decisions), decisions)
	}
	if outcome := attr(decisions[0], "outcome"); outcome != decisionDeny {
		t.Errorf("decision outcome = %q, want %q", outcome, decisionDeny)
	}

	failures := sumPoints(t, metrics, "kora.egress_gateway.mint_failures")
	if len(failures) != 1 {
		t.Fatalf("mint_failures data points = %d, want 1: %+v", len(failures), failures)
	}
	if reason := attr(failures[0], "reason"); reason != "scope_rejected" {
		t.Errorf("mint_failures reason = %q, want scope_rejected", reason)
	}

	if pts := sumPoints(t, metrics, "kora.egress_gateway.credentials_minted"); len(pts) != 0 {
		t.Errorf("credentials_minted data points = %+v, want none on a failed mint", pts)
	}
}

// A policy denial (the normal "not granted" path, with no anonymous-read
// fallback available) lands on decisions_total{outcome="deny"}.
func TestAuthorize_PolicyDenied_RecordsDenyDecision(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	m, reader := newTestMetrics(t)
	const typ credentials.AdapterType = "fake_mint"
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	if err := repo.Create(context.Background(), &credentials.Connection{
		TenantID: "acme", Name: "svc", Adapter: typ, BaseHost: "svc.example.com",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	registry := NewAdapterRegistry()
	registry.Register(fakeMintAdapter{typ: typ})
	broker := NewBroker(&stubEvaluator{allow: false, reason: "deny:no_matching_grant"}, repo, registry, passthrough.New(), logger, m)

	req, _ := http.NewRequest(http.MethodGet, "https://svc.example.com/", nil)
	_, err := broker.Authorize(context.Background(),
		claimsFor("acme", "spiffe://kora.test/tenant/kora-tenant-acme/agent/coder", "run-1"),
		FromHTTP(req))
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authorize = %v, want ErrForbidden", err)
	}

	metrics := collect(t, reader)
	decisions := sumPoints(t, metrics, "kora.egress_gateway.decisions")
	if len(decisions) != 1 {
		t.Fatalf("decisions data points = %d, want 1: %+v", len(decisions), decisions)
	}
	if outcome, adapter := attr(decisions[0], "outcome"), attr(decisions[0], "adapter"); outcome != decisionDeny || adapter != string(typ) {
		t.Errorf("decision = outcome=%q adapter=%q, want outcome=%q adapter=%q", outcome, adapter, decisionDeny, typ)
	}
	if pts := sumPoints(t, metrics, "kora.egress_gateway.credentials_minted"); len(pts) != 0 {
		t.Errorf("credentials_minted data points = %+v, want none on a denied call", pts)
	}
}
