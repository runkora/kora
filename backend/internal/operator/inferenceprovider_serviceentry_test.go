package operator

import (
	"context"
	"errors"
	"slices"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ip "github.com/runkora/kora/backend/internal/platform/inferenceproviders/v1alpha1"
)

func provider(name string, spec ip.Spec) *ip.InferenceProvider {
	return &ip.InferenceProvider{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: systemNamespace, UID: types.UID(name + "-uid")},
		Spec:       spec,
	}
}

func TestProviderEndpoints(t *testing.T) {
	tls := func(hosts ...string) []providerEndpoint { return tlsEndpoints(hosts...) }
	bedrockBase := []string{"bedrock-runtime.us-east-1.amazonaws.com", "bedrock.us-east-1.amazonaws.com", "bedrock-mantle.us-east-1.api.aws"}
	keys := &ip.BedrockCredentials{SecretRef: ip.SecretNameRef{Name: "aws"}}

	for _, tc := range []struct {
		name string
		spec ip.Spec
		want []providerEndpoint
	}{
		{
			name: "bedrock with static keys",
			spec: ip.Spec{Type: ip.TypeBedrock, Bedrock: &ip.Bedrock{Region: "us-east-1", Credentials: keys}},
			want: tls(bedrockBase...),
		},
		{
			name: "bedrock with static keys and assumeRole",
			spec: ip.Spec{Type: ip.TypeBedrock, Bedrock: &ip.Bedrock{Region: "us-east-1", Credentials: keys, AssumeRoleARN: "arn:aws:iam::1:role/r"}},
			want: tls(append(bedrockBase, "sts.us-east-1.amazonaws.com")...),
		},
		{
			name: "bedrock with workload identity",
			spec: ip.Spec{Type: ip.TypeBedrock, Bedrock: &ip.Bedrock{Region: "us-east-1"}},
			want: tls(append(bedrockBase, "sts.us-east-1.amazonaws.com")...),
		},
		{
			name: "vertexai with ADC",
			spec: ip.Spec{Type: ip.TypeVertexAI, VertexAI: &ip.VertexAI{ProjectID: "p", Location: "us-central1"}},
			want: tls("aiplatform.googleapis.com", "us-central1-aiplatform.googleapis.com", "us-central1-aiplatform.googleapis.com"),
		},
		{
			name: "vertexai with a service account and a separate Anthropic location",
			spec: ip.Spec{Type: ip.TypeVertexAI, VertexAI: &ip.VertexAI{
				ProjectID: "p", Location: "us-central1", AnthropicLocation: "us-east5",
				CredentialsSecretRef: &ip.SecretKeyRef{Name: "sa", Key: "key.json"},
			}},
			want: tls("aiplatform.googleapis.com", "us-central1-aiplatform.googleapis.com", "us-east5-aiplatform.googleapis.com", "oauth2.googleapis.com"),
		},
		{
			name: "vertexai with a multi-region Anthropic location",
			spec: ip.Spec{Type: ip.TypeVertexAI, VertexAI: &ip.VertexAI{ProjectID: "p", Location: "europe-west4", AnthropicLocation: "eu"}},
			want: tls("aiplatform.googleapis.com", "europe-west4-aiplatform.googleapis.com", "aiplatform.eu.rep.googleapis.com"),
		},
		{
			name: "azure with an Anthropic endpoint",
			spec: ip.Spec{Type: ip.TypeAzure, Azure: &ip.Azure{
				Endpoint: "https://r.openai.azure.com", APIVersion: "v", AnthropicEndpoint: "https://r.services.ai.azure.com/anthropic",
			}},
			want: tls("r.openai.azure.com", "r.services.ai.azure.com"),
		},
		{
			name: "azure with one endpoint",
			spec: ip.Spec{Type: ip.TypeAzure, Azure: &ip.Azure{Endpoint: "https://r.openai.azure.com", APIVersion: "v"}},
			want: tls("r.openai.azure.com"),
		},
		{
			name: "vertexai with a global Anthropic location",
			spec: ip.Spec{Type: ip.TypeVertexAI, VertexAI: &ip.VertexAI{ProjectID: "p", Location: "us-central1", AnthropicLocation: "global"}},
			want: tls("aiplatform.googleapis.com", "us-central1-aiplatform.googleapis.com", "aiplatform.googleapis.com"),
		},
		{
			name: "vertexai with a US multi-region Anthropic location",
			spec: ip.Spec{Type: ip.TypeVertexAI, VertexAI: &ip.VertexAI{ProjectID: "p", Location: "us-central1", AnthropicLocation: "us"}},
			want: tls("aiplatform.googleapis.com", "us-central1-aiplatform.googleapis.com", "aiplatform.us.rep.googleapis.com"),
		},
		{
			name: "compatible over https",
			spec: ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{BaseURL: "https://openrouter.ai/api/v1"}},
			want: tls("openrouter.ai"),
		},
		{
			name: "compatible over http with a port",
			spec: ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{BaseURL: "http://host.k3d.internal:11434/v1"}},
			want: []providerEndpoint{{host: "host.k3d.internal", port: 11434, plain: true}},
		},
		{
			name: "compatible over http without a port",
			spec: ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{BaseURL: "http://vllm.example.com/v1"}},
			want: []providerEndpoint{{host: "vllm.example.com", port: 80, plain: true}},
		},
		{
			name: "compatible in-cluster Service",
			spec: ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{BaseURL: "http://vllm.models.svc.cluster.local:8000/v1"}},
			want: []providerEndpoint{},
		},
		{
			name: "compatible at an IP",
			spec: ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{BaseURL: "http://10.0.0.5:8000/v1"}},
			want: []providerEndpoint{},
		},
		{
			name: "compatible over https with a port",
			spec: ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{BaseURL: "https://llm.example.com:8443/v1"}},
			want: []providerEndpoint{{host: "llm.example.com", port: 8443}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := providerEndpoints(provider("p", tc.spec))
			if err != nil {
				t.Fatalf("providerEndpoints: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("endpoints = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProviderEndpointsRejectsBadSpecs(t *testing.T) {
	for name, spec := range map[string]ip.Spec{
		"missing block":     {Type: ip.TypeCompatible},
		"missing azure":     {Type: ip.TypeAzure},
		"missing vertexai":  {Type: ip.TypeVertexAI},
		"unknown type":      {Type: "other"},
		"unsupported URL":   {Type: ip.TypeCompatible, Compatible: &ip.Compatible{BaseURL: "ftp://x"}},
		"bedrock no region": {Type: ip.TypeBedrock, Bedrock: &ip.Bedrock{}},
		"one port, two protocols": {Type: ip.TypeAzure, Azure: &ip.Azure{
			Endpoint: "http://a.example.com:8080", AnthropicEndpoint: "https://b.example.com:8080",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := providerEndpoints(provider("p", spec)); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestInMeshHost(t *testing.T) {
	for host, want := range map[string]bool{
		"vllm":                          true,
		"vllm.models.svc":               true,
		"vllm.models.svc.cluster.local": true,
		"10.0.0.5":                      true,
		"api.svc.example.com":           false,
		"openrouter.ai":                 false,
		"host.k3d.internal":             false,
	} {
		if got := inMeshHost(host); got != want {
			t.Errorf("inMeshHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestProviderServiceEntry(t *testing.T) {
	p := provider("mixed", ip.Spec{})
	se := providerServiceEntry(p, []providerEndpoint{
		{host: "a.example.com", port: 443},
		{host: "a.example.com", port: 443},
		{host: "b.example.com", port: 8080, plain: true},
		{host: "c.example.com", port: 8080},
	})
	hosts, _, _ := unstructured.NestedStringSlice(se.Object, "spec", "hosts")
	if !slices.Equal(hosts, []string{"a.example.com", "b.example.com", "c.example.com"}) {
		t.Errorf("hosts = %v", hosts)
	}
	ports, _, _ := unstructured.NestedSlice(se.Object, "spec", "ports")
	want := []any{
		map[string]any{"number": int64(443), "name": "tls-443", "protocol": "TLS"},
		map[string]any{"number": int64(8080), "name": "http-8080", "protocol": "HTTP"},
	}
	if len(ports) != len(want) {
		t.Fatalf("ports = %v, want %v", ports, want)
	}
	for i := range want {
		if ports[i].(map[string]any)["name"] != want[i].(map[string]any)["name"] ||
			ports[i].(map[string]any)["protocol"] != want[i].(map[string]any)["protocol"] {
			t.Errorf("port %d = %v, want %v", i, ports[i], want[i])
		}
	}
	for field, want := range map[string]string{"location": "MESH_EXTERNAL", "resolution": "DNS"} {
		if got, _, _ := unstructured.NestedString(se.Object, "spec", field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	if got, _, _ := unstructured.NestedStringSlice(se.Object, "spec", "exportTo"); !slices.Equal(got, []string{"."}) {
		t.Errorf("exportTo = %v, want [.]", got)
	}
	if se.GetName() != "inference-provider-mixed" || se.GetNamespace() != systemNamespace {
		t.Errorf("ServiceEntry = %s/%s", se.GetNamespace(), se.GetName())
	}
}

func getServiceEntry(t *testing.T, c client.Client, name string) *unstructured.Unstructured {
	t.Helper()
	se := &unstructured.Unstructured{}
	se.SetGroupVersionKind(serviceEntryGVK)
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: systemNamespace, Name: name}, se); err != nil {
		t.Fatalf("get ServiceEntry %q: %v", name, err)
	}
	return se
}

func TestInferenceProviderReconcile(t *testing.T) {
	ctx := context.Background()
	p := provider("openrouter", ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{BaseURL: "https://openrouter.ai/api/v1"}})
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(p).Build()
	r := &InferenceProviderReconciler{Client: c, Scheme: c.Scheme()}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)}

	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("create: %v", err)
	}
	se := getServiceEntry(t, c, "inference-provider-openrouter")
	owners := se.GetOwnerReferences()
	if len(owners) != 1 || owners[0].UID != p.UID || owners[0].Kind != "InferenceProvider" || owners[0].Controller == nil || !*owners[0].Controller {
		t.Errorf("owner references = %+v, want the provider as controller", owners)
	}

	if err := c.Get(ctx, req.NamespacedName, p); err != nil {
		t.Fatal(err)
	}
	p.Spec.Compatible.BaseURL = "http://host.k3d.internal:11434/v1"
	if err := c.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("update: %v", err)
	}
	se = getServiceEntry(t, c, "inference-provider-openrouter")
	if hosts, _, _ := unstructured.NestedStringSlice(se.Object, "spec", "hosts"); !slices.Equal(hosts, []string{"host.k3d.internal"}) {
		t.Errorf("hosts after update = %v", hosts)
	}
	ports, _, _ := unstructured.NestedSlice(se.Object, "spec", "ports")
	if len(ports) != 1 || ports[0].(map[string]any)["name"] != "http-11434" {
		t.Errorf("ports after update = %v", ports)
	}

	// A provider that moves in-mesh loses its external registration.
	if err := c.Get(ctx, req.NamespacedName, p); err != nil {
		t.Fatal(err)
	}
	p.Spec.Compatible.BaseURL = "http://vllm.models.svc.cluster.local:8000/v1"
	if err := c.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("move in-mesh: %v", err)
	}
	gone := NewServiceEntry()
	if err := c.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: "inference-provider-openrouter"}, gone); !apierrors.IsNotFound(err) {
		t.Errorf("ServiceEntry after moving in-mesh: err = %v, want NotFound", err)
	}

	// Deletion is the garbage collector's, through the owner reference above;
	// a reconcile for a provider that is gone does nothing.
	if err := c.Delete(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Errorf("reconcile after delete: %v", err)
	}
}

func TestInferenceProviderReconcileSpecErrorIsTerminal(t *testing.T) {
	p := provider("broken", ip.Spec{Type: ip.TypeCompatible})
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(p).Build()
	r := &InferenceProviderReconciler{Client: c, Scheme: c.Scheme()}

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)})
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("err = %v, want a terminal error", err)
	}
}
