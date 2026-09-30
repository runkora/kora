package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/anthropicapi"
	"github.com/clear-route/orbit/internal/inferencegw/providers/azure"
	"github.com/clear-route/orbit/internal/inferencegw/providers/bedrock"
	"github.com/clear-route/orbit/internal/inferencegw/providers/openaicompat"
	"github.com/clear-route/orbit/internal/inferencegw/providers/vertexai"
	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders"
	"github.com/clear-route/orbit/internal/platform/logging"
)

var discard = logging.NewLoggerWithOutput(io.Discard)

// mount writes an aggregate the way the operator renders it.
func mount(t *testing.T, cfg, sec string, entries []ip.Entry, secrets map[string]string) {
	t.Helper()
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, ip.ConfigMapKey), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for k, v := range secrets {
		if err := os.WriteFile(filepath.Join(sec, k), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

const fakeServiceAccount = `{"type":"service_account","project_id":"p","private_key_id":"k",
"private_key":"-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----\n",
"client_email":"sa@p.iam.gserviceaccount.com","client_id":"1","token_uri":"https://oauth2.googleapis.com/token"}`

func TestFromResourceBuildsEveryType(t *testing.T) {
	cfg, sec := t.TempDir(), t.TempDir()
	entries := []ip.Entry{
		{Name: "bedrock:aus", Resource: "aus", Type: ip.TypeBedrock, Bedrock: &ip.Bedrock{
			Region: "ap-southeast-2", AccessKeyIDKey: "aus.accessKeyID", SecretAccessKeyKey: "aus.secretAccessKey",
		}},
		{Name: "bedrock:irsa", Resource: "irsa", Type: ip.TypeBedrock, Bedrock: &ip.Bedrock{
			Region: "us-west-2", AssumeRoleARN: "arn:aws:iam::123456789012:role/b",
		}},
		{Name: "vertexai:gcp", Resource: "gcp", Type: ip.TypeVertexAI, VertexAI: &ip.VertexAI{
			ProjectID: "p", Location: "us-central1", CredentialsKey: "gcp.credentials",
		}},
		{Name: "azure:eastus", Resource: "eastus", Type: ip.TypeAzure, Azure: &ip.Azure{
			Endpoint: "https://r.openai.azure.com", APIVersion: "2024-10-21", APIKeyKey: "eastus.apiKey",
		}},
		{Name: "openai:router", Resource: "router", Type: ip.TypeOpenAI, OpenAI: &ip.OpenAI{
			BaseURL: "https://openrouter.ai/api/v1", APIKeyKey: "router.apiKey",
		}},
		{Name: "anthropic:claude", Resource: "claude", Type: ip.TypeAnthropic, Anthropic: &ip.Anthropic{
			BaseURL: ip.DefaultAnthropicBaseURL, APIKeyKey: "claude.apiKey",
		}},
	}
	mount(t, cfg, sec, entries, map[string]string{
		"aus.accessKeyID": "AKIA", "aus.secretAccessKey": "s", "gcp.credentials": fakeServiceAccount,
		"eastus.apiKey": "az", "router.apiKey": "or", "claude.apiKey": "ant",
	})
	providers, err := ip.Load(cfg, sec)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]struct {
		mode    string
		checkFn func(inferencegw.ProviderConnector) bool
	}{
		"bedrock:aus":  {inferencegw.CredentialModeSecret, func(c inferencegw.ProviderConnector) bool { _, ok := c.(*bedrock.Connector); return ok }},
		"bedrock:irsa": {inferencegw.CredentialModeWorkloadIdentity, func(c inferencegw.ProviderConnector) bool { _, ok := c.(*bedrock.Connector); return ok }},
		"vertexai:gcp": {inferencegw.CredentialModeSecret, func(c inferencegw.ProviderConnector) bool { _, ok := c.(*vertexai.Connector); return ok }},
		"azure:eastus": {inferencegw.CredentialModeSecret, func(c inferencegw.ProviderConnector) bool { _, ok := c.(*azure.Connector); return ok }},
		"openai:router": {inferencegw.CredentialModeSecret, func(c inferencegw.ProviderConnector) bool {
			_, ok := c.(*openaicompat.Connector)
			return ok
		}},
		"anthropic:claude": {inferencegw.CredentialModeSecret, func(c inferencegw.ProviderConnector) bool {
			_, ok := c.(*anthropicapi.Connector)
			return ok
		}},
	}
	for _, p := range providers {
		b := FromResource(p, discard)
		w := want[p.Name]
		if b.Info.BuildError != "" {
			t.Errorf("%s: build error %s", p.Name, b.Info.BuildError)
			continue
		}
		if !w.checkFn(b.Connector) {
			t.Errorf("%s: connector %T", p.Name, b.Connector)
		}
		if b.Connector.Name() != inferencegw.ProviderName(p.Name) {
			t.Errorf("%s: connector name %q", p.Name, b.Connector.Name())
		}
		if b.Info.CredentialMode != w.mode || b.Info.Source != inferencegw.ProviderSourceResource || b.Info.Resource != p.Resource {
			t.Errorf("%s: info %+v", p.Name, b.Info)
		}
	}
}

func TestFromResourceRecordsBuildErrors(t *testing.T) {
	cfg, sec := t.TempDir(), t.TempDir()
	mount(t, cfg, sec, []ip.Entry{
		{Name: "anthropic:nokey", Resource: "nokey", Type: ip.TypeAnthropic, Anthropic: &ip.Anthropic{
			BaseURL: ip.DefaultAnthropicBaseURL, APIKeyKey: "nokey.apiKey",
		}},
		{Name: "openai:mismatch", Resource: "mismatch", Type: ip.TypeOpenAI},
	}, nil)
	providers, _ := ip.Load(cfg, sec)
	for _, p := range providers {
		b := FromResource(p, discard)
		if b.Connector != nil || b.Info.BuildError == "" {
			t.Errorf("%s: want a build error, got %+v", p.Name, b.Info)
		}
	}
}

func TestMergeResourceReplacesEnv(t *testing.T) {
	env := []Built{{Info: inferencegw.ProviderInfo{Name: "bedrock:aus", Source: inferencegw.ProviderSourceEnvironment}}}
	res := []Built{{Info: inferencegw.ProviderInfo{Name: "bedrock:aus", Source: inferencegw.ProviderSourceResource}}}
	_, infos := Merge(env, res)
	if len(infos) != 1 || infos[0].Source != inferencegw.ProviderSourceResource {
		t.Errorf("infos = %+v", infos)
	}
}

// stubConnector lists fixed models and fails on demand.
type stubConnector struct {
	inferencegw.ProviderConnector
	name   inferencegw.ProviderName
	models []inferencegw.ProviderModel
	err    error
}

func (s stubConnector) Name() inferencegw.ProviderName { return s.name }
func (s stubConnector) ListModels(context.Context) ([]inferencegw.ProviderModel, error) {
	return s.models, s.err
}

func TestModelListFiltersAndFallsBack(t *testing.T) {
	stub := stubConnector{name: "openai:router", models: []inferencegw.ProviderModel{
		{Provider: "openai:router", ModelID: "a"}, {Provider: "openai:router", ModelID: "b"},
	}}
	got, _ := withModelList(stub, ip.TypeOpenAI, []string{"b"}).ListModels(context.Background())
	if len(got) != 1 || got[0].ModelID != "b" {
		t.Errorf("filtered = %+v", got)
	}

	stub.err, stub.models = io.ErrUnexpectedEOF, nil
	got, err := withModelList(stub, ip.TypeOpenAI, []string{"x/y"}).ListModels(context.Background())
	if err != nil || len(got) != 1 || got[0].ModelID != "x/y" || got[0].Family != "x" {
		t.Errorf("fallback = %+v, %v", got, err)
	}
}

// sink records the provider sets the reloader hands it.
type sink struct {
	mu   sync.Mutex
	sets []map[inferencegw.ProviderName]inferencegw.ProviderConnector
}

func (s *sink) SetProviders(c map[inferencegw.ProviderName]inferencegw.ProviderConnector, _ []inferencegw.ProviderInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets = append(s.sets, c)
}

func (s *sink) ListModels(context.Context) ([]inferencegw.ProviderModel, error) { return nil, nil }

func (s *sink) last() map[inferencegw.ProviderName]inferencegw.ProviderConnector {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sets[len(s.sets)-1]
}

func TestReloaderAddsChangesAndRemovesProviders(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	cfg, sec := t.TempDir(), t.TempDir()
	out := &sink{}
	r := &Reloader{ConfigDir: cfg, SecretDir: sec, Sink: out, Logger: discard}

	r.Reload(context.Background())
	if len(out.last()) != 0 {
		t.Fatalf("empty mount gave %v", out.last())
	}

	router := ip.Entry{Name: "openai:router", Resource: "router", Type: ip.TypeOpenAI, OpenAI: &ip.OpenAI{BaseURL: upstream.URL}}
	mount(t, cfg, sec, []ip.Entry{router}, nil)
	r.Reload(context.Background())
	if _, ok := out.last()["openai:router"]; !ok {
		t.Fatalf("created provider missing: %v", out.last())
	}

	if err := os.WriteFile(filepath.Join(cfg, ip.ConfigMapKey), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	sets := len(out.sets)
	r.Reload(context.Background())
	if len(out.sets) != sets {
		t.Error("an unreadable aggregate replaced the provider set")
	}

	mount(t, cfg, sec, []ip.Entry{}, nil)
	r.Reload(context.Background())
	if len(out.last()) != 0 {
		t.Errorf("deleted provider still loaded: %v", out.last())
	}
}
