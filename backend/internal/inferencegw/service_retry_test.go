package inferencegw

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func retryTestService() *Service {
	connectors := map[ProviderName]ProviderConnector{
		ProviderBedrock:  &stubConnector{name: ProviderBedrock},
		ProviderVertexAI: &stubConnector{name: ProviderVertexAI},
	}
	return NewService(&stubEvaluator{}, NewRoutingEngine(connectors, nil, nil), NewNopPricingCache(), nil, nil, logging.NewLoggerWithOutput(io.Discard), nil)
}

func TestRetryTargets_RequestRejectedStopsFailover(t *testing.T) {
	t.Parallel()
	targets := []RouteTarget{{Provider: ProviderBedrock}, {Provider: ProviderVertexAI}, {Provider: ProviderBedrock}}
	var calls int
	_, used, _, err := retryTargets(context.Background(), retryTestService(), targets, CapabilityChat, "retry",
		func(ProviderConnector, RouteTarget) (struct{}, error) {
			calls++
			return struct{}{}, &ConnectorError{Code: "INVALID_REQUEST", HTTPStatus: 400, RequestRejected: true}
		})
	if calls != 1 {
		t.Errorf("upstream calls = %d, want 1", calls)
	}
	var ce *ConnectorError
	if !errors.As(err, &ce) || ce.Code != "INVALID_REQUEST" {
		t.Errorf("err = %v, want INVALID_REQUEST", err)
	}
	if used != nil {
		t.Errorf("usedTarget = %v, want nil", used)
	}
}

func TestRetryTargets_TargetSpecificNonRetryableStillFailsOver(t *testing.T) {
	t.Parallel()
	targets := []RouteTarget{{Provider: ProviderBedrock}, {Provider: ProviderVertexAI}}
	var calls int
	_, used, _, err := retryTargets(context.Background(), retryTestService(), targets, CapabilityChat, "retry",
		func(_ ProviderConnector, tg RouteTarget) (string, error) {
			calls++
			if tg.Provider == ProviderBedrock {
				return "", &ConnectorError{Code: "MODEL_NOT_FOUND", HTTPStatus: 404}
			}
			return "ok", nil
		})
	if err != nil || calls != 2 || used == nil || used.Provider != ProviderVertexAI {
		t.Errorf("err=%v calls=%d used=%v, want failover to vertex", err, calls, used)
	}
}
