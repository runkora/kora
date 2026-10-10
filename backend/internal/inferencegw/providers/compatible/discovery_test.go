package compatible

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/clear-route/orbit/internal/inferencegw"
	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders/v1alpha1"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestSystemOneDiscovery(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		key        string
		wantIDs    []string
		wantErr    bool
		wantBearer string
	}{
		{
			name:    "data shape",
			status:  http.StatusOK,
			body:    `{"object":"list","data":[{"id":"jev-latest"},{"id":"jev-preview"}]}`,
			wantIDs: []string{"jev-latest", "jev-preview"},
		},
		{
			name:       "models shape sends the key",
			status:     http.StatusOK,
			body:       `{"models":[{"name":"jev-latest"},{"name":"clef"}]}`,
			key:        "sk-test",
			wantIDs:    []string{"jev-latest", "clef"},
			wantBearer: "Bearer sk-test",
		},
		{
			name:    "entries without an id are skipped",
			status:  http.StatusOK,
			body:    `{"data":[{"id":"a"},{"object":"model"},"junk"]}`,
			wantIDs: []string{"a"},
		},
		{
			name:    "empty list",
			status:  http.StatusOK,
			body:    `{}`,
			wantIDs: nil,
		},
		{name: "upstream error", status: http.StatusUnauthorized, body: `{"error":"bad key"}`, wantErr: true},
		{name: "not json", status: http.StatusOK, body: `<html>`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotBearer string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotBearer = r.URL.Path, r.Header.Get("Authorization")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			c, err := New(Config{BaseURL: srv.URL + "/v1/", APIKey: tc.key, APIs: []ip.API{ip.APISystemOne}}, "compatible:ts", logging.NewLoggerWithOutput(io.Discard))
			if err != nil {
				t.Fatal(err)
			}
			models, err := c.ListModels(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if gotPath != "/v1/models" || gotBearer != tc.wantBearer {
				t.Errorf("request = %q with %q, want /v1/models with %q", gotPath, gotBearer, tc.wantBearer)
			}
			var ids []string
			for _, m := range models {
				ids = append(ids, m.ModelID)
			}
			if !slices.Equal(ids, tc.wantIDs) {
				t.Errorf("ids = %v, want %v", ids, tc.wantIDs)
			}
		})
	}
}

func TestSystemOneDiscoveryReadsPublishedPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"jev","pricing":{"prompt":"0.000000042","completion":"0"}},{"id":"free"}]}`)
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{BaseURL: srv.URL, APIs: []ip.API{ip.APISystemOne}}, "compatible:ts", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	models, err := c.ListModels(context.Background())
	if err != nil || len(models) != 2 {
		t.Fatalf("models = %v, %v", models, err)
	}
	if p := models[0].Price; p == nil || !near(p.InputCostPer1M, 0.042) || p.OutputCostPer1M != 0 {
		t.Errorf("price = %+v", models[0].Price)
	}
	if models[1].Price != nil {
		t.Errorf("unpriced model got %+v", models[1].Price)
	}
}

func TestDescribeCapabilityByDeclaredAPIs(t *testing.T) {
	text := []inferencegw.Modality{inferencegw.ModalityText}
	chat := []inferencegw.ModelCapability{inferencegw.CapabilityChat}
	decision := []inferencegw.ModelCapability{inferencegw.CapabilityDecision}
	cases := []struct {
		name         string
		apis         []ip.API
		id           string
		wantCaps     []inferencegw.ModelCapability
		wantDialects []inferencegw.APIDialect
	}{
		{"systemone alone", []ip.API{ip.APISystemOne}, "jev-latest", decision, nil},
		{"systemone alone, embed-like name", []ip.API{ip.APISystemOne}, "text-embed-1", decision, nil},
		{
			"systemone with chat keeps chat",
			[]ip.API{ip.APISystemOne, ip.APIChatCompletions}, "jev-latest", chat,
			[]inferencegw.APIDialect{inferencegw.DialectOpenAI, inferencegw.DialectResponses},
		},
		{
			"systemone with embeddings keeps the embed heuristic",
			[]ip.API{ip.APISystemOne, ip.APIEmbeddings}, "text-embed-1",
			[]inferencegw.ModelCapability{inferencegw.CapabilityEmbedding}, nil,
		},
		{"systemone with embeddings, other name", []ip.API{ip.APISystemOne, ip.APIEmbeddings}, "jev-latest", chat, nil},
		{
			"systemone with messages",
			[]ip.API{ip.APISystemOne, ip.APIMessages}, "decision-1", chat,
			[]inferencegw.APIDialect{inferencegw.DialectAnthropic, inferencegw.DialectOpenAI, inferencegw.DialectResponses},
		},
		{"duplicate systemone is still alone", []ip.API{ip.APISystemOne, ip.APISystemOne}, "jev", decision, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(Config{BaseURL: "https://x/v1", APIs: tc.apis}, "compatible:x", logging.NewLoggerWithOutput(io.Discard))
			if err != nil {
				t.Fatal(err)
			}
			m := c.Describe(tc.id)
			if !slices.Equal(m.Capabilities, tc.wantCaps) {
				t.Errorf("capabilities = %v, want %v", m.Capabilities, tc.wantCaps)
			}
			if !slices.Equal(m.Dialects, tc.wantDialects) {
				t.Errorf("dialects = %v, want %v", m.Dialects, tc.wantDialects)
			}
			if !slices.Equal(m.InputModalities, text) {
				t.Errorf("input modalities = %v", m.InputModalities)
			}
		})
	}
}

func TestSystemOneOnlyRefusesChatAPIs(t *testing.T) {
	c, err := New(Config{BaseURL: "https://x/v1", APIs: []ip.API{ip.APISystemOne}}, "compatible:ts", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"chat":      func() error { _, err := c.ChatCompletion(ctx, chatParams()); return err },
		"responses": func() error { _, err := c.Responses(ctx, responsesParams()); return err },
		"messages":  func() error { _, err := c.AnthropicMessages(ctx, messageParams()); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s on a systemone-only provider succeeded", name)
		}
	}
}
