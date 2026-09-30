package operator

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/inferenceproviders"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func provider(ns, name string, spec InferenceProviderSpec) *InferenceProvider {
	return &InferenceProvider{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Generation: 1}, Spec: spec}
}

func secret(name string, data map[string]string) *corev1.Secret {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: systemNamespace, Name: name}, Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

func readAggregate(t *testing.T, c client.Client) ([]inferenceproviders.Entry, map[string][]byte) {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: systemNamespace, Name: inferenceproviders.ConfigMapName}, cm); err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}
	var entries []inferenceproviders.Entry
	if err := json.Unmarshal([]byte(cm.Data[inferenceproviders.ConfigMapKey]), &entries); err != nil {
		t.Fatalf("parse providers.json: %v", err)
	}
	s := &corev1.Secret{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: systemNamespace, Name: inferenceproviders.SecretName}, s); err != nil {
		t.Fatalf("get Secret: %v", err)
	}
	return entries, s.Data
}

func TestInferenceProviderAggregator_RendersEachType(t *testing.T) {
	objs := []client.Object{
		provider(systemNamespace, "aus", InferenceProviderSpec{Type: "bedrock", Bedrock: &BedrockProviderSpec{
			Region:      "ap-southeast-2",
			Credentials: &BedrockCredentials{SecretRef: SecretNameRef{Name: "aws"}},
		}}),
		provider(systemNamespace, "irsa", InferenceProviderSpec{Type: "bedrock", Bedrock: &BedrockProviderSpec{
			Region: "us-west-2", AssumeRoleARN: "arn:aws:iam::123456789012:role/bedrock",
		}}),
		provider(systemNamespace, "openrouter", InferenceProviderSpec{Type: "openai", Models: []string{"openai/gpt-4o-mini"}, OpenAI: &OpenAIProviderSpec{
			BaseURL:         "https://openrouter.ai/api/v1",
			APIKeySecretRef: &SecretKeyRef{Name: "openrouter", Key: "apiKey"},
		}}),
		provider(systemNamespace, "vllm", InferenceProviderSpec{Type: "openai", OpenAI: &OpenAIProviderSpec{}}),
		provider(systemNamespace, "claude", InferenceProviderSpec{Type: "anthropic", Anthropic: &AnthropicProviderSpec{
			APIKeySecretRef: SecretKeyRef{Name: "anthropic", Key: "key"},
		}}),
		secret("aws", map[string]string{"accessKeyID": "AKIA", "secretAccessKey": "s3cret"}),
		secret("openrouter", map[string]string{"apiKey": "or-key"}),
		secret("anthropic", map[string]string{"key": "sk-ant"}),
	}
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(objs...).Build()
	agg := NewInferenceProviderAggregator(c, logging.NewLoggerWithOutput(io.Discard))

	results, err := agg.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	for key, res := range results {
		if !res.ok() {
			t.Errorf("%s: %s %s", key, res.reason, res.message)
		}
	}

	entries, data := readAggregate(t, c)
	byName := map[string]inferenceproviders.Entry{}
	for _, e := range entries {
		byName[e.Name] = e
	}

	aus := byName["bedrock:aus"]
	if aus.Bedrock == nil || aus.Bedrock.AccessKeyIDKey != "aus.accessKeyID" || aus.Bedrock.SessionTokenKey != "" {
		t.Errorf("bedrock:aus = %+v", aus.Bedrock)
	}
	if string(data["aus.accessKeyID"]) != "AKIA" || string(data["aus.secretAccessKey"]) != "s3cret" {
		t.Errorf("aggregate secret missing Bedrock keys: %v", data)
	}
	if irsa := byName["bedrock:irsa"]; irsa.Bedrock == nil || irsa.Bedrock.AccessKeyIDKey != "" || irsa.Bedrock.AssumeRoleARN == "" {
		t.Errorf("bedrock:irsa = %+v", irsa.Bedrock)
	}
	or := byName["openai:openrouter"]
	if or.OpenAI == nil || or.OpenAI.BaseURL != "https://openrouter.ai/api/v1" || or.OpenAI.APIKeyKey != "openrouter.apiKey" || len(or.Models) != 1 {
		t.Errorf("openai:openrouter = %+v", or)
	}
	if v := byName["openai:vllm"]; v.OpenAI == nil || v.OpenAI.BaseURL != inferenceproviders.DefaultOpenAIBaseURL || v.OpenAI.APIKeyKey != "" {
		t.Errorf("openai:vllm = %+v", v.OpenAI)
	}
	if a := byName["anthropic:claude"]; a.Anthropic == nil || a.Anthropic.BaseURL != inferenceproviders.DefaultAnthropicBaseURL {
		t.Errorf("anthropic:claude = %+v", a.Anthropic)
	}
	raw, _ := json.Marshal(entries)
	for _, v := range []string{"AKIA", "s3cret", "or-key", "sk-ant"} {
		if strings.Contains(string(raw), v) {
			t.Errorf("providers.json carries credential value %q", v)
		}
	}
}

func TestInferenceProviderAggregator_RejectsAndReports(t *testing.T) {
	cases := []struct {
		name   string
		obj    *InferenceProvider
		reason string
	}{
		{"wrong namespace", provider("orbit-tenant-acme", "p", InferenceProviderSpec{Type: "openai", OpenAI: &OpenAIProviderSpec{}}), reasonWrongNamespace},
		{"missing secret", provider(systemNamespace, "p", InferenceProviderSpec{Type: "anthropic", Anthropic: &AnthropicProviderSpec{
			APIKeySecretRef: SecretKeyRef{Name: "absent", Key: "k"},
		}}), reasonSecretNotFound},
		{"missing key", provider(systemNamespace, "p", InferenceProviderSpec{Type: "bedrock", Bedrock: &BedrockProviderSpec{
			Region: "us-east-1", Credentials: &BedrockCredentials{SecretRef: SecretNameRef{Name: "partial"}},
		}}), reasonSecretKeyNotFound},
		{"block mismatch", provider(systemNamespace, "p", InferenceProviderSpec{Type: "bedrock", OpenAI: &OpenAIProviderSpec{}}), reasonInvalidSpec},
		{"missing region", provider(systemNamespace, "p", InferenceProviderSpec{Type: "bedrock", Bedrock: &BedrockProviderSpec{}}), reasonInvalidSpec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(NewScheme()).
				WithObjects(tc.obj, secret("partial", map[string]string{"accessKeyID": "x"})).Build()
			agg := NewInferenceProviderAggregator(c, logging.NewLoggerWithOutput(io.Discard))
			results, err := agg.Sync(context.Background())
			if err != nil {
				t.Fatalf("Sync: %v", err)
			}
			res := results[types.NamespacedName{Namespace: tc.obj.Namespace, Name: tc.obj.Name}]
			if res.reason != tc.reason {
				t.Errorf("reason = %q (%s), want %q", res.reason, res.message, tc.reason)
			}
			entries, data := readAggregate(t, c)
			if len(entries) != 0 || len(data) != 0 {
				t.Errorf("rejected provider rendered: %v %v", entries, data)
			}
		})
	}
}

func TestInferenceProviderReconciler_StatusAndRemoval(t *testing.T) {
	p := provider(systemNamespace, "openrouter", InferenceProviderSpec{Type: "openai", OpenAI: &OpenAIProviderSpec{
		BaseURL: "https://openrouter.ai/api/v1", APIKeySecretRef: &SecretKeyRef{Name: "openrouter", Key: "apiKey"},
	}})
	c := fake.NewClientBuilder().WithScheme(NewScheme()).
		WithObjects(p, secret("openrouter", map[string]string{"apiKey": "k"})).
		WithStatusSubresource(p).Build()
	r := &InferenceProviderReconciler{
		Client:     c,
		Aggregator: NewInferenceProviderAggregator(c, logging.NewLoggerWithOutput(io.Discard)),
		Logger:     logging.NewLoggerWithOutput(io.Discard),
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: systemNamespace, Name: "openrouter"}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := &InferenceProvider{}
	if err := c.Get(context.Background(), req.NamespacedName, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != InferenceProviderPhaseReady || got.Status.ProviderName != "openai:openrouter" || got.Status.ObservedGeneration != 1 {
		t.Errorf("status = %+v", got.Status)
	}

	if err := c.Delete(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile after delete: %v", err)
	}
	entries, data := readAggregate(t, c)
	if len(entries) != 0 || len(data) != 0 {
		t.Errorf("deleted provider still rendered: %v %v", entries, data)
	}
}
