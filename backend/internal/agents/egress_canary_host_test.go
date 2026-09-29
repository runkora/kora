package agents

import (
	"context"
	"testing"

	platformconns "github.com/clear-route/orbit/internal/platform/connections"
)

// The canary probes a host the agent may reach, so an agent without
// api.github.com egress never waits out a probe that cannot pass.
func TestEgressCanaryHost(t *testing.T) {
	github := map[string]struct{ adapter, baseHost string }{
		"github": {platformconns.AdapterGitHubApp, "github.com"},
	}
	tests := []struct {
		name  string
		at    AgentType
		conns connectionResolver
		want  string
	}{
		{
			name:  "no egress skips the probe",
			at:    AgentType{Connectivity: Connectivity{Mode: ConnectivityNone}},
			conns: stubConns{},
			want:  "",
		},
		{
			name:  "open egress with no named host probes the fixed host",
			at:    AgentType{Connectivity: Connectivity{Mode: ConnectivityOpen}},
			conns: stubConns{},
			want:  openEgressCanaryHost,
		},
		{
			name: "open egress with a connector probes the connector host",
			at: AgentType{
				Connectivity: Connectivity{Mode: ConnectivityOpen},
				Connectors:   []Connector{{Connection: "github", Actions: []string{"contents:read"}}},
			},
			conns: stubConns{entries: github},
			want:  "github.com",
		},
		{
			name:  "whitelist skips wildcard hosts",
			at:    AgentType{Connectivity: Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"*.example.com", "pypi.org"}}},
			conns: stubConns{},
			want:  "pypi.org",
		},
		{
			name: "connector hosts come before the whitelist",
			at: AgentType{
				Connectivity: Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"pypi.org"}},
				Connectors:   []Connector{{Connection: "github", Actions: []string{"contents:read"}}},
			},
			conns: stubConns{entries: github},
			want:  "github.com",
		},
		{
			name:  "a resolver error skips the probe",
			at:    AgentType{Connectivity: Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"pypi.org"}}, Connectors: []Connector{{Connection: "github"}}},
			conns: errConns{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _, _ := newTestService(t)
			svc.run.SetScopeResolver(NewScopeResolver(nil, tt.conns))
			if got := svc.run.egressCanaryHost(context.Background(), tt.at); got != tt.want {
				t.Errorf("egressCanaryHost = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("no resolver skips the probe", func(t *testing.T) {
		svc, _, _, _ := newTestService(t)
		if got := svc.run.egressCanaryHost(context.Background(), AgentType{Connectivity: Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"pypi.org"}}}); got != "" {
			t.Errorf("egressCanaryHost = %q, want empty", got)
		}
	})
}
