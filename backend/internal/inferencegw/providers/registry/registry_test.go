package registry

import (
	"context"
	"errors"
	"io"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/azure"
	"github.com/clear-route/orbit/internal/inferencegw/providers/bedrock"
	"github.com/clear-route/orbit/internal/inferencegw/providers/compatible"
	"github.com/clear-route/orbit/internal/inferencegw/providers/vertexai"
	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const ns = "orbit-system"

var discard = logging.NewLoggerWithOutput(io.Discard)

const fakeServiceAccount = `{"type":"service_account","project_id":"p","private_key_id":"k",
"private_key":"-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----\n",
"client_email":"sa@p.iam.gserviceaccount.com","client_id":"1","token_uri":"https://oauth2.googleapis.com/token"}`

func resource(name string, spec ip.Spec) *ip.InferenceProvider {
	return &ip.InferenceProvider{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Generation: 1}, Spec: spec}
}

func secret(name string, data map[string]string) *corev1.Secret {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: map[string]string{ip.SecretLabel: "true"}}, Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

func newWatcher(t *testing.T, objs ...client.Object) (*Watcher, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = ip.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(&ip.InferenceProvider{}).Build()
	return &Watcher{reader: c, status: c.Status(), namespace: ns, logger: discard}, c
}

func TestLoadBuildsEveryType(t *testing.T) {
	w, _ := newWatcher(t,
		resource("aus", ip.Spec{Type: ip.TypeBedrock, Bedrock: &ip.Bedrock{
			Region: "ap-southeast-2", Credentials: &ip.BedrockCredentials{SecretRef: ip.SecretNameRef{Name: "aws"}},
		}}),
		resource("irsa", ip.Spec{Type: ip.TypeBedrock, Bedrock: &ip.Bedrock{Region: "us-west-2", AssumeRoleARN: "arn:aws:iam::123456789012:role/b"}}),
		resource("gcp", ip.Spec{Type: ip.TypeVertexAI, VertexAI: &ip.VertexAI{
			ProjectID: "p", Location: "us-central1", CredentialsSecretRef: &ip.SecretKeyRef{Name: "gcp", Key: "key.json"},
		}}),
		resource("eastus", ip.Spec{Type: ip.TypeAzure, Azure: &ip.Azure{
			Endpoint: "https://r.openai.azure.com", APIVersion: "2024-10-21", APIKeySecretRef: ip.SecretKeyRef{Name: "az", Key: "apiKey"},
		}}),
		resource("openrouter", ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{
			BaseURL: "https://openrouter.ai/api/v1", APIs: []ip.API{ip.APIChatCompletions, ip.APIMessages},
			APIKeySecretRef: &ip.SecretKeyRef{Name: "openrouter", Key: "apiKey"},
		}}),
		secret("aws", map[string]string{"accessKeyID": "AKIA", "secretAccessKey": "s"}),
		secret("gcp", map[string]string{"key.json": fakeServiceAccount}),
		secret("az", map[string]string{"apiKey": "k"}),
		secret("openrouter", map[string]string{"apiKey": "k"}),
	)
	connectors, infos := w.Load(context.Background())

	checks := map[inferencegw.ProviderName]func(inferencegw.ProviderConnector) bool{
		"bedrock:aus":           func(c inferencegw.ProviderConnector) bool { _, ok := c.(*bedrock.Connector); return ok },
		"bedrock:irsa":          func(c inferencegw.ProviderConnector) bool { _, ok := c.(*bedrock.Connector); return ok },
		"vertexai:gcp":          func(c inferencegw.ProviderConnector) bool { _, ok := c.(*vertexai.Connector); return ok },
		"azure:eastus":          func(c inferencegw.ProviderConnector) bool { _, ok := c.(*azure.Connector); return ok },
		"compatible:openrouter": func(c inferencegw.ProviderConnector) bool { _, ok := c.(*compatible.Connector); return ok },
	}
	for name, check := range checks {
		if c, ok := connectors[name]; !ok || !check(c) || c.Name() != name {
			t.Errorf("%s: connector %T", name, connectors[name])
		}
	}
	modes := map[inferencegw.ProviderName]string{}
	for _, info := range infos {
		modes[info.Name] = info.CredentialMode
	}
	if modes["bedrock:aus"] != inferencegw.CredentialModeSecret || modes["bedrock:irsa"] != inferencegw.CredentialModeWorkloadIdentity {
		t.Errorf("credential modes = %v", modes)
	}
}

func TestLoadReportsFailuresOnStatus(t *testing.T) {
	cases := []struct {
		name   string
		spec   ip.Spec
		reason string
	}{
		{"no-secret", ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{
			BaseURL: "https://x/v1", APIs: []ip.API{ip.APIChatCompletions}, APIKeySecretRef: &ip.SecretKeyRef{Name: "absent", Key: "k"},
		}}, ReasonSecretNotFound},
		{"no-key", ip.Spec{Type: ip.TypeBedrock, Bedrock: &ip.Bedrock{
			Region: "us-east-1", Credentials: &ip.BedrockCredentials{SecretRef: ip.SecretNameRef{Name: "partial"}},
		}}, ReasonSecretKeyNotFound},
		{"unlabelled", ip.Spec{Type: ip.TypeAzure, Azure: &ip.Azure{
			Endpoint: "https://r", APIVersion: "v", APIKeySecretRef: ip.SecretKeyRef{Name: "plain", Key: "apiKey"},
		}}, ReasonSecretNotFound},
		{"bad-messages-url", ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{
			BaseURL: "https://x/api", APIs: []ip.API{ip.APIMessages},
		}}, ReasonInvalidSpec},
		{"no-block", ip.Spec{Type: ip.TypeBedrock}, ReasonInvalidSpec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unlabelled := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "plain"}, Data: map[string][]byte{"apiKey": []byte("k")}}
			w, c := newWatcher(t, resource(tc.name, tc.spec), secret("partial", map[string]string{"accessKeyID": "x"}), unlabelled)
			w.reader = labelledOnly{c}
			connectors, infos := w.Load(context.Background())
			if len(connectors) != 0 || len(infos) != 1 || infos[0].BuildError == "" {
				t.Fatalf("connectors = %v, infos = %+v", connectors, infos)
			}
			got := &ip.InferenceProvider{}
			if err := c.Get(context.Background(), client.ObjectKey{Namespace: ns, Name: tc.name}, got); err != nil {
				t.Fatal(err)
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionLoaded)
			if got.Status.Phase != ip.PhaseInvalid || cond == nil || cond.Reason != tc.reason {
				t.Errorf("status = %+v", got.Status)
			}
		})
	}
}

// labelledOnly hides unlabelled Secrets, as the gateway's label-filtered cache does.
type labelledOnly struct{ client.Reader }

func (l labelledOnly) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := l.Reader.Get(ctx, key, obj, opts...); err != nil {
		return err
	}
	if s, ok := obj.(*corev1.Secret); ok && s.Labels[ip.SecretLabel] != "true" {
		return fakeNotFound(key.Name)
	}
	return nil
}

func TestLoadMarksReadyAndWritesOnlyChanges(t *testing.T) {
	w, c := newWatcher(t, resource("vllm", ip.Spec{Type: ip.TypeCompatible, Compatible: &ip.Compatible{
		BaseURL: "https://vllm/v1", APIs: []ip.API{ip.APIChatCompletions},
	}}))
	w.Load(context.Background())
	got := &ip.InferenceProvider{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: ns, Name: "vllm"}, got)
	if got.Status.Phase != ip.PhaseReady || got.Status.ProviderName != "compatible:vllm" {
		t.Fatalf("status = %+v", got.Status)
	}
	version := got.ResourceVersion
	w.Load(context.Background())
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: ns, Name: "vllm"}, got)
	if got.ResourceVersion != version {
		t.Error("an unchanged status was written again")
	}
}

func fakeNotFound(name string) error {
	return apierrors.NewNotFound(corev1.Resource("secrets"), name)
}

// stubLister lists fixed models with an optional error.
type stubLister struct {
	inferencegw.ProviderConnector
	models []inferencegw.ProviderModel
	err    error
}

func (s stubLister) Name() inferencegw.ProviderName { return "compatible:x" }
func (s stubLister) ListModels(context.Context) ([]inferencegw.ProviderModel, error) {
	return s.models, s.err
}

func TestModelListFiltersAndKeepsDiscoveryErrors(t *testing.T) {
	stub := stubLister{models: []inferencegw.ProviderModel{{ModelID: "a"}, {ModelID: "b"}}}
	got, err := withModelList(stub, []string{"b"}).ListModels(context.Background())
	if err != nil || len(got) != 1 || got[0].ModelID != "b" {
		t.Errorf("filtered = %+v, %v", got, err)
	}

	stub = stubLister{err: errors.New("list models: 401")}
	got, err = withModelList(stub, []string{"vendor/m"}).ListModels(context.Background())
	if err == nil || len(got) != 1 || got[0].ModelID != "vendor/m" || got[0].Family != "vendor" {
		t.Errorf("fallback = %+v, %v; want the declared model and the error", got, err)
	}
}
