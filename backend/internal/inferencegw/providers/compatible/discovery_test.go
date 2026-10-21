package compatible

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/inferencegw"
	ip "github.com/runkora/kora/backend/internal/platform/inferenceproviders/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
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

// openRouterChat is the default listing of a mixed endpoint: text models only.
const openRouterChat = `{"data":[
 {"id":"openai/gpt-5","architecture":{"modality":"text->text","output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"}},
 {"id":"typesafe/jev-1.13","architecture":{"modality":"text->text","output_modalities":["text"]},"pricing":{"prompt":"0.000009","completion":"0.000009"}}]}`

// openRouterDecisions is the answer to ?output_modalities=decisions.
const openRouterDecisions = `{"data":[
 {"id":"typesafe/jev-1.13","architecture":{"modality":"text->decisions","output_modalities":["decisions"]},"pricing":{"prompt":"0.000000042","completion":"0"}},
 {"id":"cloudflare/clef","architecture":{"modality":"text+image->decisions","output_modalities":["decisions"]},"pricing":{"prompt":"0.0000001","completion":"0"}}]}`

// jsonHeader marks a test response as JSON, which the OpenAI SDK requires.
func jsonHeader(w http.ResponseWriter) { w.Header().Set("Content-Type", "application/json") }

func TestMixedEndpointDiscovery(t *testing.T) {
	type want struct {
		decision []string
		chat     []string
	}
	cases := []struct {
		name           string
		decisionStatus int
		decisionBody   string
		decisionModels []string
		want           want
		wantJevPrice   float64
	}{
		{
			name:           "only metadata-declared decision models are marked",
			decisionStatus: http.StatusOK, decisionBody: openRouterDecisions,
			want:         want{decision: []string{"typesafe/jev-1.13", "cloudflare/clef"}, chat: []string{"openai/gpt-5"}},
			wantJevPrice: 0.042,
		},
		{
			name:           "an endpoint ignoring the parameter marks nothing",
			decisionStatus: http.StatusOK, decisionBody: openRouterChat,
			want:         want{chat: []string{"openai/gpt-5", "typesafe/jev-1.13"}},
			wantJevPrice: 9,
		},
		{
			name:           "entries without architecture metadata are not marked",
			decisionStatus: http.StatusOK, decisionBody: `{"data":[{"id":"openai/gpt-5"},{"id":"x","architecture":{}}]}`,
			want:         want{chat: []string{"openai/gpt-5", "typesafe/jev-1.13"}},
			wantJevPrice: 9,
		},
		{
			name:           "a failed decision listing keeps the chat models",
			decisionStatus: http.StatusInternalServerError, decisionBody: `{"error":"boom"}`,
			want:         want{chat: []string{"openai/gpt-5", "typesafe/jev-1.13"}},
			wantJevPrice: 9,
		},
		{
			name:           "an unparsable decision listing keeps the chat models",
			decisionStatus: http.StatusOK, decisionBody: `<html>`,
			want:         want{chat: []string{"openai/gpt-5", "typesafe/jev-1.13"}},
			wantJevPrice: 9,
		},
		{
			name:           "decisionModels adds unlisted ids",
			decisionStatus: http.StatusNotFound, decisionBody: `{}`,
			decisionModels: []string{"clef", "openai/gpt-5"},
			want:           want{decision: []string{"openai/gpt-5", "clef"}, chat: []string{"typesafe/jev-1.13"}},
			wantJevPrice:   9,
		},
		{
			name:           "decisionModels and metadata together",
			decisionStatus: http.StatusOK, decisionBody: openRouterDecisions,
			decisionModels: []string{"llama-clef"},
			want:           want{decision: []string{"typesafe/jev-1.13", "cloudflare/clef", "llama-clef"}, chat: []string{"openai/gpt-5"}},
			wantJevPrice:   0.042,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var decisionQueries int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				jsonHeader(w)
				if r.URL.Path != "/api/v1/models" {
					http.NotFound(w, r)
					return
				}
				if r.URL.Query().Get("output_modalities") == "decisions" {
					decisionQueries++
					w.WriteHeader(tc.decisionStatus)
					_, _ = io.WriteString(w, tc.decisionBody)
					return
				}
				_, _ = io.WriteString(w, openRouterChat)
			}))
			t.Cleanup(srv.Close)
			c, err := New(Config{
				BaseURL: srv.URL + "/api/v1", APIs: []ip.API{ip.APIChatCompletions, ip.APIResponses, ip.APISystemOne},
				DecisionModels: tc.decisionModels,
			}, "compatible:or", logging.NewLoggerWithOutput(io.Discard))
			if err != nil {
				t.Fatal(err)
			}
			models, err := c.ListModels(context.Background())
			if err != nil {
				t.Fatalf("ListModels: %v", err)
			}
			if decisionQueries != 1 {
				t.Errorf("decision listing requested %d times, want 1", decisionQueries)
			}
			var decision, chat []string
			for _, m := range models {
				if slices.Equal(m.Capabilities, []inferencegw.ModelCapability{inferencegw.CapabilityDecision}) {
					decision = append(decision, m.ModelID)
					if len(m.Dialects) != 0 || !slices.Equal(m.InputModalities, []inferencegw.Modality{inferencegw.ModalityText}) {
						t.Errorf("%s: dialects = %v, input = %v", m.ModelID, m.Dialects, m.InputModalities)
					}
					continue
				}
				chat = append(chat, m.ModelID)
				if len(m.Dialects) == 0 {
					t.Errorf("chat model %s lost its dialects", m.ModelID)
				}
				if m.ModelID == "typesafe/jev-1.13" && (m.Price == nil || !near(m.Price.InputCostPer1M, tc.wantJevPrice)) {
					t.Errorf("jev price = %+v, want %v", m.Price, tc.wantJevPrice)
				}
			}
			if !slices.Equal(decision, tc.want.decision) || !slices.Equal(chat, tc.want.chat) {
				t.Errorf("decision = %v, chat = %v; want %v", decision, chat, tc.want)
			}
			for _, m := range models {
				if m.ModelID == "typesafe/jev-1.13" && slices.Contains(decision, m.ModelID) && (m.Price == nil || !near(m.Price.InputCostPer1M, tc.wantJevPrice) || m.Price.OutputCostPer1M != 0) {
					t.Errorf("decision jev price = %+v, want input %v, output 0", m.Price, tc.wantJevPrice)
				}
			}
		})
	}
}

func TestChatOnlyEndpointMakesNoDecisionRequest(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonHeader(w)
		queries = append(queries, r.URL.RawQuery)
		_, _ = io.WriteString(w, openRouterChat)
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{BaseURL: srv.URL, APIs: []ip.API{ip.APIChatCompletions}, DecisionModels: []string{"x"}}, "compatible:c", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	models, err := c.ListModels(context.Background())
	if err != nil || len(models) != 2 {
		t.Fatalf("models = %v, %v", models, err)
	}
	if slices.ContainsFunc(queries, func(q string) bool { return q != "" }) {
		t.Errorf("queries = %q, want none with a parameter", queries)
	}
	for _, m := range models {
		if !slices.Equal(m.Capabilities, []inferencegw.ModelCapability{inferencegw.CapabilityChat}) {
			t.Errorf("%s capabilities = %v", m.ModelID, m.Capabilities)
		}
	}
}

func TestDecisionOnlyAcceptsDecisionModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"jev"}]}`)
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{BaseURL: srv.URL, APIs: []ip.API{ip.APISystemOne}, DecisionModels: []string{"jev", "clef"}}, "compatible:ts", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	models, err := c.ListModels(context.Background())
	var ids []string
	for _, m := range models {
		ids = append(ids, m.ModelID)
	}
	if err != nil || !slices.Equal(ids, []string{"jev", "clef"}) {
		t.Errorf("ids = %v, %v", ids, err)
	}
}

func TestEmbeddingsDiscoveryListsNonTextModels(t *testing.T) {
	textOnly := `{"data":[` +
		`{"id":"openai/gpt-x","architecture":{"output_modalities":["text"]}},` +
		`{"id":"vendor/vec-small","architecture":{"output_modalities":["text"]}}]}`
	all := `{"data":[` +
		`{"id":"openai/gpt-x","architecture":{"output_modalities":["text"]}},` +
		`{"id":"vendor/vec-small","architecture":{"output_modalities":["embeddings"]},"pricing":{"prompt":"0.00000002","completion":"0"}},` +
		`{"id":"openai/text-embedding-3","architecture":{"output_modalities":["embeddings"]}},` +
		`{"id":"cohere/rerank-x","architecture":{"output_modalities":["rerank"]}},` +
		`{"id":"img/gen","architecture":{"output_modalities":["image"]}},` +
		`{"id":"tts/voice","architecture":{"output_modalities":["speech"]}},` +
		`{"id":"vid/gen","architecture":{"output_modalities":["video"]}},` +
		`{"id":"stt/listen","architecture":{"output_modalities":["transcription"]}},` +
		`{"id":"no/metadata"}]}`
	chat, emb, rerank := inferencegw.CapabilityChat, inferencegw.CapabilityEmbedding, inferencegw.CapabilityRerank
	cases := []struct {
		name      string
		honours   bool
		defaultTo string
		allBody   string
		want      map[string]inferencegw.ModelCapability
		wantPrice string
	}{
		{
			name: "endpoint honours the filter", honours: true, defaultTo: textOnly, allBody: all,
			want: map[string]inferencegw.ModelCapability{
				"openai/gpt-x": chat, "vendor/vec-small": emb, "openai/text-embedding-3": emb, "cohere/rerank-x": rerank,
			},
			wantPrice: "vendor/vec-small",
		},
		{
			name: "endpoint ignores the filter", honours: false, defaultTo: textOnly,
			want: map[string]inferencegw.ModelCapability{"openai/gpt-x": chat, "vendor/vec-small": chat},
		},
		{
			name: "no architecture metadata keeps the name check", honours: false,
			defaultTo: `{"data":[{"id":"a"},{"id":"text-embed-1"}]}`,
			want:      map[string]inferencegw.ModelCapability{"a": chat, "text-embed-1": emb},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tc.honours && r.URL.Query().Get("output_modalities") == "all" {
					_, _ = io.WriteString(w, tc.allBody)
					return
				}
				_, _ = io.WriteString(w, tc.defaultTo)
			}))
			t.Cleanup(srv.Close)
			c, err := New(Config{BaseURL: srv.URL + "/v1", APIs: []ip.API{ip.APIChatCompletions, ip.APIEmbeddings}}, "compatible:or", logging.NewLoggerWithOutput(io.Discard))
			if err != nil {
				t.Fatal(err)
			}
			models, err := c.ListModels(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]inferencegw.ModelCapability{}
			for _, m := range models {
				if len(m.Capabilities) != 1 {
					t.Fatalf("%s capabilities = %v", m.ModelID, m.Capabilities)
				}
				got[m.ModelID] = m.Capabilities[0]
				if m.ModelID == tc.wantPrice && (m.Price == nil || !near(m.Price.InputCostPer1M, 0.02)) {
					t.Errorf("%s price = %+v", m.ModelID, m.Price)
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("catalog = %v, want %v", got, tc.want)
			}
			for id, want := range tc.want {
				if got[id] != want {
					t.Errorf("%s capability = %q, want %q", id, got[id], want)
				}
			}
		})
	}
}
