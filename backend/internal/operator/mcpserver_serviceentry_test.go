package operator

import (
	"context"
	"io"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func mcpEntry(name, endpoint string) mcpServerRegistryEntry {
	return mcpServerRegistryEntry{Name: name, Endpoint: endpoint, namespace: testTenantNS}
}

func TestMCPServiceEntries(t *testing.T) {
	ses, skipped := mcpServiceEntries([]mcpServerRegistryEntry{
		mcpEntry("github", "http://github-mcp.orbit-tenant-acme.svc:8000/mcp"),
		mcpEntry("local", "http://10.0.0.5:8000/mcp"),
		mcpEntry("cased", "http://Orbit-Identity.Orbit-System.SVC.Cluster.Local:8080/mcp"),
		mcpEntry("dotted", "http://orbit-identity.orbit-system.svc.cluster.local.:8080/mcp"),
		mcpEntry("short", "http://Orbit-Identity:8080/mcp"),
		mcpEntry("exa-dot", "https://mcp.exa.ai./mcp"),
		mcpEntry("exa", "https://MCP.exa.ai/mcp"),
		mcpEntry("exa-alt", "https://mcp.exa.ai:8443/mcp"),
		mcpEntry("exa-again", "https://mcp.exa.ai/other"),
		mcpEntry("plain", "http://tools.example.com/mcp"),
		mcpEntry("clash", "http://mcp.exa.ai:443/mcp"),
		mcpEntry("provider", "https://openrouter.ai/mcp"),
		mcpEntry("svc-ns", "http://orbit-identity.Orbit-System:8080/mcp"),
		mcpEntry("headless", "http://pod-0.mcp.orbit-tenant-acme:8080/mcp"),
		mcpEntry("pod", "http://10-42-2-6.orbit-system.pod:8080/mcp"),
		mcpEntry("pod-fqdn", "http://10-42-2-6.orbit-system.pod.cluster.local:8080/mcp"),
	}, map[string]bool{"openrouter.ai": true}, map[string]bool{"orbit-system": true, "orbit-tenant-acme": true})

	var names []string
	for _, sk := range skipped {
		names = append(names, sk.Name)
	}
	if want := []string{"clash", "provider", "svc-ns", "headless"}; !reflect.DeepEqual(names, want) {
		t.Errorf("skipped = %v, want %v", names, want)
	}
	got := map[string]any{}
	for _, se := range ses {
		if se.GetNamespace() != systemNamespace || se.GetLabels()[componentLabel] != mcpServerHostComponent {
			t.Errorf("%s: namespace %q labels %v", se.GetName(), se.GetNamespace(), se.GetLabels())
		}
		got[se.GetName()] = se.Object["spec"].(map[string]any)["ports"]
	}
	want := map[string]any{
		"mcp-server-mcp.exa.ai": []any{
			map[string]any{"number": int64(443), "name": "tls-443", "protocol": "TLS"},
			map[string]any{"number": int64(8443), "name": "tls-8443", "protocol": "TLS"},
		},
		"mcp-server-tools.example.com": []any{
			map[string]any{"number": int64(80), "name": "http-80", "protocol": "HTTP"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ServiceEntry ports = %v, want %v", got, want)
	}
}

func listServiceEntries(t *testing.T, c client.Client) map[string]bool {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(serviceEntryGVK.GroupVersion().WithKind("ServiceEntryList"))
	if err := c.List(context.Background(), list, client.InNamespace(systemNamespace)); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := map[string]bool{}
	for _, se := range list.Items {
		out[se.GetName()] = true
	}
	return out
}

func TestMCPAggregator_PrunesServiceEntries(t *testing.T) {
	exa := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "exa", Namespace: testTenantNS},
		Spec:       MCPServerSpec{Endpoint: "https://mcp.exa.ai/mcp"},
	}
	provider := externalServiceEntry("inference-provider-openrouter", systemNamespace,
		map[string]any{"app.kubernetes.io/managed-by": "orbit-operator"}, tlsEndpoints("openrouter.ai"))
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(tenant("acme", true), exa, provider).Build()
	a := NewMCPServerAggregator(c, logging.NewLoggerWithOutput(io.Discard))
	ctx := context.Background()

	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	want := map[string]bool{"mcp-server-mcp.exa.ai": true, "inference-provider-openrouter": true}
	if got := listServiceEntries(t, c); !reflect.DeepEqual(got, want) {
		t.Errorf("after register = %v, want %v", got, want)
	}

	// A second pass with nothing changed is a no-op.
	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if err := c.Delete(ctx, exa); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	want = map[string]bool{"inference-provider-openrouter": true}
	if got := listServiceEntries(t, c); !reflect.DeepEqual(got, want) {
		t.Errorf("after deregister = %v, want %v", got, want)
	}
}
