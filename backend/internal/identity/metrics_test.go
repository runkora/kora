package identity

import (
	"context"
	"io"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// newTestMetrics builds the real instrument set on an SDK meter with a manual
// reader, so tests assert on what a collector would actually receive rather
// than on a mock of the OTel API.
func newTestMetrics(t *testing.T, repo activeSessionsReader) (*Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m, err := NewMetrics(provider.Meter("test"), repo, logging.NewLoggerWithOutput(io.Discard))
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

func sumInt64(t *testing.T, m metricdata.Metrics) int64 {
	t.Helper()
	switch d := m.Data.(type) {
	case metricdata.Sum[int64]:
		var total int64
		for _, dp := range d.DataPoints {
			total += dp.Value
		}
		return total
	case metricdata.Gauge[int64]:
		var total int64
		for _, dp := range d.DataPoints {
			total += dp.Value
		}
		return total
	default:
		t.Fatalf("%s is not an int64 sum or gauge: %T", m.Name, m.Data)
		return 0
	}
}

// A nil *Metrics is a supported configuration: instrumentation is injected,
// and a caller that wired no meter must behave exactly as before.
func TestMetrics_NilIsSafe(t *testing.T) {
	var m *Metrics
	ctx := context.Background()
	m.recordDelegationIssued(ctx)
	m.recordDelegationRevoked(ctx)
	m.recordAuthFailure(ctx, authFailureReasonInvalidToken)
	if err := m.Close(); err != nil {
		t.Errorf("Close on nil = %v", err)
	}
}

// The sessions-active gauge is observed at collection time from one
// repository query, so it reports live state rather than a counter the code
// remembered to keep updated.
func TestMetrics_SessionsActiveGaugeObservesLiveState(t *testing.T) {
	repo := newMockSessionRepo()
	_, reader := newTestMetrics(t, repo)
	ctx := context.Background()

	now := time.Now()
	active := &Session{ID: "s-active", JTI: "j-active", ExpiresAt: now.Add(time.Hour)}
	if err := repo.CreateSession(ctx, active); err != nil {
		t.Fatalf("create active session: %v", err)
	}
	expired := &Session{ID: "s-expired", JTI: "j-expired", ExpiresAt: now.Add(-time.Hour)}
	if err := repo.CreateSession(ctx, expired); err != nil {
		t.Fatalf("create expired session: %v", err)
	}
	revokedAt := now
	revoked := &Session{ID: "s-revoked", JTI: "j-revoked", ExpiresAt: now.Add(time.Hour), RevokedAt: &revokedAt}
	if err := repo.CreateSession(ctx, revoked); err != nil {
		t.Fatalf("create revoked session: %v", err)
	}

	got := sumInt64(t, collect(t, reader)["orbit.identity.sessions.active"])
	if got != 1 {
		t.Errorf("sessions active = %d, want 1 (expired and revoked sessions excluded)", got)
	}
}

// Minting and revoking a delegation issuance has to increment the counters
// where those operations actually succeed, not merely declare them.
func TestMetrics_DelegationIssuedAndRevokedCounters(t *testing.T) {
	svc, _, _ := newDelegationTestService(t)
	m, reader := newTestMetrics(t, newMockSessionRepo())
	svc.metrics = m

	claims := humanClaims("t1", "user-1")
	issuance := mintTestDelegation(t, svc, claims, "run-1")

	if got := sumInt64(t, collect(t, reader)["orbit.identity.delegations_issued"]); got != 1 {
		t.Errorf("delegations issued = %d, want 1", got)
	}

	if err := svc.RevokeDelegation(context.Background(), claims, issuance.JTI); err != nil {
		t.Fatalf("RevokeDelegation: %v", err)
	}

	if got := sumInt64(t, collect(t, reader)["orbit.identity.delegations_revoked"]); got != 1 {
		t.Errorf("delegations revoked = %d, want 1", got)
	}
}

// The auth-failures counter carries a fixed reason label drawn from the
// sentinel error, never an arbitrary string.
func TestMetrics_AuthFailureCounterCarriesReason(t *testing.T) {
	svc := newTestService(t, newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(), newMockPATRepo())
	m, reader := newTestMetrics(t, newMockSessionRepo())
	svc.metrics = m

	_, err := svc.RefreshSession(context.Background(), SessionRequest{RefreshToken: "unknown-token"})
	if err == nil {
		t.Fatal("RefreshSession with an unknown token should fail")
	}

	got, ok := collect(t, reader)["orbit.identity.auth_failures"]
	if !ok {
		t.Fatal("auth-failures counter not exported")
	}
	sum, ok := got.Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("auth-failures data = %+v, want one data point", got.Data)
	}
	reason, present := sum.DataPoints[0].Attributes.Value("reason")
	if !present || reason.AsString() != authFailureReasonRefreshTokenNotFound {
		t.Errorf("reason = %v, want %q", reason, authFailureReasonRefreshTokenNotFound)
	}
}
