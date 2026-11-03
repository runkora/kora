package inferencegw

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

func TestAgentSlugFromPrincipal(t *testing.T) {
	t.Parallel()
	const ns = "spiffe://runkora.test/tenant/kora-tenant-example/agent/"
	tests := []struct {
		name, principalType, principalID, want string
	}{
		{"agent", "AGENT", ns + "token-probe", "token-probe"},
		{"sandbox svid records the agent", "AGENT", ns + "token-probe/sandbox/run-1", "token-probe"},
		{"other trust domain", "AGENT", "spiffe://example.com/tenant/kora-tenant-acme/agent/probe", "probe"},
		{"service principal", "SERVICE", "spiffe://runkora.test/tenant/kora-tenant-example/service/probe", ""},
		{"agent type on non-agent path", "AGENT", "spiffe://runkora.test/tenant/kora-tenant-example/service/probe", ""},
		{"human", "HUMAN", "CgNkZXYSCmxvY2FsLWxkYXA", ""},
		{"not spiffe", "AGENT", "agent:probe", ""},
		{"empty slug", "AGENT", ns, ""},
		{"too short", "AGENT", "spiffe://runkora.test/tenant/ns", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := agentSlugFromPrincipal(tc.principalType, tc.principalID); got != tc.want {
				t.Errorf("agentSlugFromPrincipal(%q, %q) = %q, want %q", tc.principalType, tc.principalID, got, tc.want)
			}
		})
	}
}

func TestSetPrincipalAttributes_AgentSlug(t *testing.T) {
	t.Parallel()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	for _, p := range []principal{
		{PrincipalType: "AGENT", PrincipalID: "spiffe://runkora.test/tenant/ns/agent/triage"},
		{PrincipalType: "HUMAN", PrincipalID: "user-1"},
	} {
		_, span := tp.Tracer("t").Start(context.Background(), "chat")
		setPrincipalAttributes(span, p)
		span.End()
	}
	ended := rec.Ended()
	slug := func(i int) string {
		for _, kv := range ended[i].Attributes() {
			if kv.Key == observability.AgentSlugKey {
				return kv.Value.AsString()
			}
		}
		return ""
	}
	if got := slug(0); got != "triage" {
		t.Errorf("agent span slug = %q, want triage", got)
	}
	if got := slug(1); got != "" {
		t.Errorf("human span slug = %q, want none", got)
	}
}
