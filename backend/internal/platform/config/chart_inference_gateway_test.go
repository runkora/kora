package config_test

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"
)

// gatewayDrain holds the rendered inference gateway settings that must agree
// for in-flight streams to finish on shutdown.
type gatewayDrain struct {
	shutdownTimeoutLine string
	graceSecs           int64
	proxyConfig         string
	pdbRendered         bool
}

func renderGatewayDrain(t *testing.T, values string) gatewayDrain {
	t.Helper()
	var got gatewayDrain
	for doc := range strings.SplitSeq(renderChart(t, values), "\n---\n") {
		var obj struct {
			Kind     string                `json:"kind"`
			Metadata struct{ Name string } `json:"metadata"`
			Data     map[string]string     `json:"data"`
			Spec     appsv1.DeploymentSpec `json:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatalf("parse rendered manifest: %v", err)
		}
		switch {
		case obj.Kind == "ConfigMap" && obj.Metadata.Name == "orbit-inference-gateway-config":
			for _, data := range obj.Data {
				for line := range strings.SplitSeq(data, "\n") {
					if strings.HasPrefix(strings.TrimSpace(line), "shutdownTimeoutSecs:") {
						got.shutdownTimeoutLine = strings.TrimSpace(line)
					}
				}
			}
		case obj.Kind == "Deployment" && obj.Metadata.Name == "orbit-inference-gateway":
			if g := obj.Spec.Template.Spec.TerminationGracePeriodSeconds; g != nil {
				got.graceSecs = *g
			}
			got.proxyConfig = obj.Spec.Template.Annotations["proxy.istio.io/config"]
		case obj.Kind == "PodDisruptionBudget" && obj.Metadata.Name == "orbit-inference-gateway":
			got.pdbRendered = true
		}
	}
	return got
}

// The gateway drains streams for shutdownTimeoutSecs, the pod outlives that by
// 30s and the Istio sidecar keeps serving for the same period. The
// PodDisruptionBudget exists only with more than one replica.
func TestChartInferenceGatewayDrain(t *testing.T) {
	tests := []struct {
		name      string
		values    string
		wantPDB   bool
		wantGrace int64
		wantTime  string
	}{
		{name: "one replica", values: "inferenceGateway:\n  replicas: 1\n", wantPDB: false, wantGrace: 150, wantTime: "120"},
		{name: "two replicas", values: "inferenceGateway:\n  replicas: 2\n", wantPDB: true, wantGrace: 150, wantTime: "120"},
		{name: "overridden timeout", values: "inferenceGateway:\n  shutdownTimeoutSecs: 60\n", wantPDB: false, wantGrace: 90, wantTime: "60"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderGatewayDrain(t, tt.values)
			if want := "shutdownTimeoutSecs: " + tt.wantTime; got.shutdownTimeoutLine != want {
				t.Errorf("config line = %q, want %q", got.shutdownTimeoutLine, want)
			}
			if got.graceSecs != tt.wantGrace {
				t.Errorf("terminationGracePeriodSeconds = %d, want %d", got.graceSecs, tt.wantGrace)
			}
			if want := "terminationDrainDuration: " + tt.wantTime + "s"; !strings.Contains(got.proxyConfig, want) {
				t.Errorf("proxy.istio.io/config = %q, want it to contain %q", got.proxyConfig, want)
			}
			if got.pdbRendered != tt.wantPDB {
				t.Errorf("PodDisruptionBudget rendered = %v, want %v", got.pdbRendered, tt.wantPDB)
			}
		})
	}
}
