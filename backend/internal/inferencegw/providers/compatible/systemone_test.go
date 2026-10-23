package compatible

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/runkora/kora/backend/internal/inferencegw"
	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	ip "github.com/runkora/kora/backend/internal/platform/inferenceproviders/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// sysOneUpstream answers /v1/systemone with a fixed status and body and records the request.
type sysOneUpstream struct {
	status int
	body   string
	path   string
	method string
	bearer string
	got    string
}

func (u *sysOneUpstream) serve(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		u.path, u.method, u.bearer, u.got = r.URL.Path, r.Method, r.Header.Get("Authorization"), string(b)
		w.WriteHeader(u.status)
		_, _ = io.WriteString(w, u.body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

func newSysOne(t *testing.T, baseURL, key string, apis ...ip.API) *Connector {
	t.Helper()
	c, err := New(Config{BaseURL: baseURL, APIKey: key, APIs: apis}, "compatible:ts", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSystemOnePassesThroughAnswersAndRewritesModel(t *testing.T) {
	cases := []struct {
		name     string
		apis     []ip.API
		key      string
		request  string
		wantSent string
		response string
		wantIn   int64
		wantOut  int64
	}{
		{
			name:     "noul",
			apis:     []ip.API{ip.APISystemOne},
			key:      "sk-test",
			request:  `{"model":"jev-latest","state":"I loved it","questions":{"q1":{"type":"noul","instructions":"positive?"}}}`,
			wantSent: `{"model":"jev-1","state":"I loved it","questions":{"q1":{"type":"noul","instructions":"positive?"}}}`,
			response: `{"model":"jev-1","answers":{"q1":{"yes":0.97,"no":0.03}},"usage":{"input_tokens":392,"output_tokens":65}}`,
			wantIn:   392, wantOut: 65,
		},
		{
			name:     "choice, key order and whitespace kept",
			apis:     []ip.API{ip.APISystemOne, ip.APIChatCompletions},
			request:  "{ \"state\" : {\"a\":[1, 2] , \"html\":\"<b>&</b>\"},\n \"model\":\"x\", \"questions\":{\"q\":{\"type\":\"choice\",\"options\":[\"a\",\"b\"]}} }",
			wantSent: "{ \"state\" : {\"a\":[1, 2] , \"html\":\"<b>&</b>\"},\n \"model\":\"jev-1\", \"questions\":{\"q\":{\"type\":\"choice\",\"options\":[\"a\",\"b\"]}} }",
			response: "{\n  \"answers\": {\"q\": {\"a\": 0.7, \"b\": 0.3}},\n  \"usage\": {\"input_tokens\": 10, \"output_tokens\": 2}\n}\n",
			wantIn:   10, wantOut: 2,
		},
		{
			name:     "score",
			apis:     []ip.API{ip.APISystemOne},
			request:  `{"state":["a","b"],"model":"m","questions":{"q":{"type":"score","levels":5}}}`,
			wantSent: `{"state":["a","b"],"model":"jev-1","questions":{"q":{"type":"score","levels":5}}}`,
			response: `{"answers":{"q":{"1":0.1,"2":0.1,"3":0.2,"4":0.3,"5":0.3}},"usage":{"input_tokens":7,"output_tokens":5}}`,
			wantIn:   7, wantOut: 5,
		},
		{
			name:     "missing model is added",
			apis:     []ip.API{ip.APISystemOne},
			request:  `{"state":"s","questions":{}}`,
			wantSent: `{"model":"jev-1","state":"s","questions":{}}`,
			response: `{"answers":{}}`,
		},
		{
			name:     "empty object gets only the model",
			apis:     []ip.API{ip.APISystemOne},
			request:  `{}`,
			wantSent: `{"model":"jev-1"}`,
			response: `{"answers":{},"usage":null}`,
		},
		{
			name:     "usage with other fields and a non-JSON body are tolerated",
			apis:     []ip.API{ip.APISystemOne},
			request:  `{"model":"m"}`,
			wantSent: `{"model":"jev-1"}`,
			response: `not json`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := &sysOneUpstream{status: http.StatusOK, body: tc.response}
			c := newSysOne(t, up.serve(t), tc.key, tc.apis...)
			res, err := c.SystemOne(context.Background(), inferencegw.SystemOneRequest{Body: []byte(tc.request), Model: "jev-1"})
			if err != nil {
				t.Fatal(err)
			}
			if up.method != http.MethodPost || up.path != "/v1/systemone" {
				t.Errorf("request = %s %s", up.method, up.path)
			}
			if up.got != tc.wantSent {
				t.Errorf("sent %q, want %q", up.got, tc.wantSent)
			}
			wantBearer := ""
			if tc.key != "" {
				wantBearer = "Bearer " + tc.key
			}
			if up.bearer != wantBearer {
				t.Errorf("Authorization = %q, want %q", up.bearer, wantBearer)
			}
			if string(res.Body) != tc.response || res.StatusCode != http.StatusOK {
				t.Errorf("result = %d %q, want the upstream body unchanged", res.StatusCode, res.Body)
			}
			if res.InputTokens != tc.wantIn || res.OutputTokens != tc.wantOut {
				t.Errorf("tokens = %d/%d, want %d/%d", res.InputTokens, res.OutputTokens, tc.wantIn, tc.wantOut)
			}
		})
	}
}

func TestSystemOneRejectsNonObjectBody(t *testing.T) {
	up := &sysOneUpstream{status: http.StatusOK, body: `{}`}
	c := newSysOne(t, up.serve(t), "", ip.APISystemOne)
	for _, body := range []string{``, `[]`, `"x"`, `{"model":`, `{"a":1} {"b":2}`} {
		_, err := c.SystemOne(context.Background(), inferencegw.SystemOneRequest{Body: []byte(body), Model: "m"})
		var ce *inferencegw.ConnectorError
		if !errors.As(err, &ce) || ce.Code != "INVALID_REQUEST" || ce.Retryable {
			t.Errorf("body %q: err = %v", body, err)
		}
	}
	if up.path != "" {
		t.Error("a malformed body reached the upstream")
	}
}

func TestSystemOneUpstreamErrors(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		body          string
		wantCode      string
		wantRetryable bool
		wantMessage   string
		wantRejected  bool
	}{
		{"422 keeps the message", 422, `{"error":{"message":"question q1: choice needs 2 options"}}`, "INVALID_REQUEST", false, "question q1: choice needs 2 options", true},
		{"400 detail", 400, `{"detail":"state is required"}`, "INVALID_REQUEST", false, "state is required", true},
		{"429 throttled", 429, `{"error":"slow down"}`, "THROTTLED", true, "slow down", false},
		{"529 overloaded", 529, `{"message":"overloaded"}`, "PROVIDER_ERROR", true, "overloaded", false},
		{"500", 500, `boom`, "PROVIDER_ERROR", true, "boom", false},
		{"503", 503, ``, "PROVIDER_ERROR", true, "", false},
		{"401 is the gateway's credential", 401, `{"error":{"message":"bad key"}}`, "PROVIDER_ERROR", false, "bad key", false},
		{"403 is the gateway's credential", 403, `{}`, "PROVIDER_ERROR", false, "{}", false},
		{"404", 404, `not found`, "PROVIDER_ERROR", false, "not found", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := &sysOneUpstream{status: tc.status, body: tc.body}
			c := newSysOne(t, up.serve(t), "k", ip.APISystemOne)
			res, err := c.SystemOne(context.Background(), inferencegw.SystemOneRequest{Body: []byte(`{"model":"m"}`), Model: "m"})
			var ce *inferencegw.ConnectorError
			if res != nil || !errors.As(err, &ce) {
				t.Fatalf("res = %v, err = %v", res, err)
			}
			if ce.Code != tc.wantCode || ce.Retryable != tc.wantRetryable || ce.HTTPStatus != tc.status || ce.ProviderMessage != tc.wantMessage || ce.RequestRejected != tc.wantRejected {
				t.Errorf("error = %+v, want %s retryable=%v message %q rejected=%v", ce, tc.wantCode, tc.wantRetryable, tc.wantMessage, tc.wantRejected)
			}
			if ce.Provider != "compatible:ts" {
				t.Errorf("provider = %q", ce.Provider)
			}
		})
	}
}

// OpenRouter answers a request it cannot validate with a zod error whose body
// runs well past 512 bytes. The message must still be extracted, and capped.
func TestSystemOneLongUpstreamErrorKeepsTheMessage(t *testing.T) {
	issue := `[{\"code\":\"invalid_union\",\"errors\":[[{\"code\":\"invalid_type\",\"expected\":\"string\",\"path\":[\"questions\",\"q1\",\"instructions\"],\"message\":\"Required\"}]],\"path\":[\"questions\",\"q1\"],\"message\":\"Invalid input\"}]`
	short := `{"error":{"message":"` + issue + `","code":400},"user_id":"` + strings.Repeat("u", 600) + `"}`
	long := `{"error":{"message":"` + strings.Repeat(issue, 20) + `","code":400}}`
	cases := []struct {
		name, body, wantPrefix string
	}{
		{"body past 512 bytes", short, strings.ReplaceAll(issue, `\"`, `"`)},
		{"message past the cap", long, strings.ReplaceAll(issue, `\"`, `"`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := &sysOneUpstream{status: http.StatusBadRequest, body: tc.body}
			c := newSysOne(t, up.serve(t), "k", ip.APISystemOne)
			_, err := c.SystemOne(context.Background(), inferencegw.SystemOneRequest{Body: []byte(`{"model":"m"}`), Model: "m"})
			var ce *inferencegw.ConnectorError
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v", err)
			}
			if !strings.HasPrefix(ce.ProviderMessage, tc.wantPrefix) {
				t.Errorf("message = %q, want prefix %q", ce.ProviderMessage, tc.wantPrefix)
			}
			if len(ce.ProviderMessage) > maxSystemOneErrorMessage {
				t.Errorf("message is %d bytes, over the %d cap", len(ce.ProviderMessage), maxSystemOneErrorMessage)
			}
		})
	}
}

func TestSystemOneTransportFailureIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/v1"
	srv.Close()
	c := newSysOne(t, url, "", ip.APISystemOne)
	_, err := c.SystemOne(context.Background(), inferencegw.SystemOneRequest{Body: []byte(`{}`), Model: "m"})
	var ce *inferencegw.ConnectorError
	if !errors.As(err, &ce) || !ce.Retryable {
		t.Errorf("err = %v", err)
	}
}

func TestSystemOneTimeouts(t *testing.T) {
	if discoveryTimeout != 30*time.Second || decisionTimeout != 15*time.Second {
		t.Fatalf("timeouts = %s discovery, %s decision", discoveryTimeout, decisionTimeout)
	}
	c := newSysOne(t, "https://x/v1", "", ip.APISystemOne)
	if c.http == c.decide {
		t.Error("discovery and decision calls share a client")
	}

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })

	cases := []struct {
		name          string
		ctx           func() (context.Context, context.CancelFunc)
		wantCode      string
		wantRetryable bool
	}{
		{"slow upstream hits the decision timeout", func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}, "PROVIDER_ERROR", true},
		{"caller deadline is not retried", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 20*time.Millisecond)
		}, "CANCELED", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newSysOne(t, slow.URL+"/v1", "", ip.APISystemOne)
			c.decide = httpclient.New("kora.compatible", logging.NewLoggerWithOutput(io.Discard), 100*time.Millisecond)
			ctx, cancel := tc.ctx()
			defer cancel()
			start := time.Now()
			_, err := c.SystemOne(ctx, inferencegw.SystemOneRequest{Body: []byte(`{"model":"m"}`), Model: "m"})
			var ce *inferencegw.ConnectorError
			if !errors.As(err, &ce) || ce.Code != tc.wantCode || ce.Retryable != tc.wantRetryable {
				t.Fatalf("err = %+v, want %s retryable=%v", err, tc.wantCode, tc.wantRetryable)
			}
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Errorf("failed after %s, want the deadline to fire", elapsed)
			}
		})
	}
}

func TestSystemOneRefusedWithoutDeclaringIt(t *testing.T) {
	up := &sysOneUpstream{status: http.StatusOK, body: `{}`}
	for _, apis := range [][]ip.API{{ip.APIChatCompletions}, {ip.APIEmbeddings, ip.APIResponses}, {ip.APIMessages}} {
		c := newSysOne(t, up.serve(t), "k", apis...)
		_, err := c.SystemOne(context.Background(), inferencegw.SystemOneRequest{Body: []byte(`{"model":"m"}`), Model: "m"})
		var ce *inferencegw.ConnectorError
		if !errors.As(err, &ce) || ce.Code != "MODEL_NOT_SUPPORTED" || !ce.Retryable ||
			ce.ProviderMessage != "provider compatible:ts does not serve the systemone API" {
			t.Errorf("apis %v: err = %+v", apis, err)
		}
	}
	if up.path != "" {
		t.Error("a refused call reached the upstream")
	}
}

func TestSystemOneClientSpanStatus(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantError bool
		wantDesc  string
	}{
		{"429", 429, `{"error":"slow down"}`, true, errSystemOneStatus.Error()},
		{"529", 529, `{"message":"overloaded"}`, true, errSystemOneStatus.Error()},
		{"401", 401, `{"error":{"message":"bad key"}}`, true, errSystemOneStatus.Error()},
		{"200", 200, `{"usage":{"input_tokens":1,"output_tokens":2}}`, false, ""},
		{"oversized body", 200, strings.Repeat("x", maxSystemOneBody+1), true, errSystemOneTooLarge.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			otel.SetTracerProvider(tp)
			t.Cleanup(func() { otel.SetTracerProvider(tracenoop.NewTracerProvider()) })

			up := &sysOneUpstream{status: tc.status, body: tc.body}
			c := newSysOne(t, up.serve(t), "k", ip.APISystemOne)
			res, err := c.SystemOne(context.Background(), inferencegw.SystemOneRequest{Body: []byte(`{"model":"m"}`), Model: "m"})

			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("got %d spans, want 1", len(spans))
			}
			st := spans[0].Status
			if tc.wantError {
				var ce *inferencegw.ConnectorError
				if res != nil || !errors.As(err, &ce) {
					t.Fatalf("res = %v, err = %v", res, err)
				}
				if st.Code != codes.Error || st.Description != tc.wantDesc {
					t.Errorf("span status = %v %q, want Error %q", st.Code, st.Description, tc.wantDesc)
				}
				return
			}
			if err != nil || res == nil {
				t.Fatalf("res = %v, err = %v", res, err)
			}
			if st.Code == codes.Error {
				t.Errorf("span status = %v %q, want not Error", st.Code, st.Description)
			}
		})
	}
}
