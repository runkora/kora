package identityproxy

import (
	"context"
	"testing"
	"time"
)

const testCollector = "otel-collector.observability.svc:4317"

// The collector gets neither token, and any it was sent is stripped
// (docs/adr/2026-09-18-telemetry-collector-platform-destination.md).
func TestProcessor_CollectorGetsNoTokens(t *testing.T) {
	cache := NewTokenCache()
	cache.Set("bearer-xyz", time.Now().Add(5*time.Minute))
	p := NewProcessor(cache, nil, testCollector, discardLogger())

	resp := p.handleRequestHeaders(context.Background(), reqHeaders(map[string]string{
		":authority":               "OTEL-Collector.observability.svc:4317",
		":path":                    "/opentelemetry.proto.collector.trace.v1.TraceService/Export",
		"x-orbit-delegation-token": "forged",
	}))
	mutation := mutationOf(t, resp)
	if len(mutation.GetSetHeaders()) != 0 {
		t.Errorf("set headers = %v, want none for the collector", mutation.GetSetHeaders())
	}
	if !removesHeader(mutation, authorizationHeader) || !removesHeader(mutation, delegationHeader) {
		t.Errorf("remove headers = %v, want authorization and delegation stripped", mutation.GetRemoveHeaders())
	}
}

func TestProcessor_OtherAuthorityStillGetsToken(t *testing.T) {
	cache := NewTokenCache()
	cache.Set("bearer-xyz", time.Now().Add(5*time.Minute))
	p := NewProcessor(cache, nil, testCollector, discardLogger())

	resp := p.handleRequestHeaders(context.Background(), reqHeaders(map[string]string{
		":authority": "kora-inference-gateway.kora-system.svc.cluster.local",
		":path":      "/v1/messages",
	}))
	if got := setHeaderValue(mutationOf(t, resp), authorizationHeader); got != "Bearer bearer-xyz" {
		t.Errorf("authorization = %q, want the platform token", got)
	}
}
