package httpclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestDo_NamesTheSpanAfterTheURLTemplate(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.EscapedPath(), r.URL.RawQuery
	}))
	t.Cleanup(srv.Close)

	ctx, parent := provider.Tracer("test").Start(context.Background(), "parent")
	defer parent.End()
	base := httpclient.New("test", logging.NewLoggerWithOutput(io.Discard), 0)
	_, span, resp, err := base.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: srv.URL + "/api", Path: "/runs/{runID}/messages",
		Params: []string{"run/1"}, Query: url.Values{"limit": {"5"}},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	span.End()

	if gotPath != "/api/runs/run%2F1/messages" || gotQuery != "limit=5" {
		t.Errorf("request = %s?%s, want the escaped param and the query", gotPath, gotQuery)
	}
	ended := recorder.Ended()
	got := ended[len(ended)-1]
	if got.Name() != "POST /api/runs/{runID}/messages" {
		t.Errorf("span name = %q", got.Name())
	}
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	want := map[attribute.Key]attribute.Value{
		semconv.URLTemplateKey:            attribute.StringValue("/api/runs/{runID}/messages"),
		semconv.ServerAddressKey:          attribute.StringValue(u.Hostname()),
		semconv.ServerPortKey:             attribute.IntValue(port),
		semconv.HTTPResponseStatusCodeKey: attribute.IntValue(http.StatusOK),
	}
	for _, kv := range got.Attributes() {
		if kv.Key == semconv.HTTPRouteKey {
			t.Errorf("client span carries %s", kv.Key)
		}
		if w, ok := want[kv.Key]; ok && w != kv.Value {
			t.Errorf("%s = %v, want %v", kv.Key, kv.Value.Emit(), w.Emit())
		}
		delete(want, kv.Key)
	}
	for k := range want {
		t.Errorf("missing %s", k)
	}
}

func TestDo_RejectsARequestItCannotBuild(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	t.Cleanup(srv.Close)
	base := httpclient.New("test", logging.NewLoggerWithOutput(io.Discard), 0)

	tests := map[string]httpclient.Request{
		"invalid method":     {Method: "BAD METHOD", BaseURL: srv.URL, Path: "/runs"},
		"too few params":     {Method: http.MethodGet, BaseURL: srv.URL, Path: "/runs/{runID}/messages/{id}", Params: []string{"r"}},
		"too many params":    {Method: http.MethodGet, BaseURL: srv.URL, Path: "/runs/{runID}", Params: []string{"r", "x"}},
		"params, no holders": {Method: http.MethodGet, BaseURL: srv.URL, Path: "/runs", Params: []string{"r"}},
	}
	for name, r := range tests {
		t.Run(name, func(t *testing.T) {
			_, span, resp, err := base.Do(context.Background(), r)
			span.End()
			if !errors.Is(err, httpclient.ErrBuildRequest) || resp != nil {
				t.Errorf("Do = %v, %v, want ErrBuildRequest and no response", resp, err)
			}
		})
	}
	if hits != 0 {
		t.Errorf("the server was called %d times", hits)
	}
}

func TestHTTPFailures_SetErrorTypeToTheStatusCode(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	ctx, parent := provider.Tracer("test").Start(context.Background(), "parent")
	defer parent.End()

	base := httpclient.New("test", logging.NewLoggerWithOutput(io.Discard), 0)
	_, span, resp, err := base.Do(ctx, httpclient.Request{Method: http.MethodGet, BaseURL: srv.URL, Path: "/health"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = base.HandleErrorResponse(span, resp, errors.New("unexpected status"))
	_ = resp.Body.Close()
	span.End()

	sdk := httpclient.NewHTTPClient(0, httpclient.NewTracingTransport("test", "/v1/chat/completions", nil))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/chat/completions", nil)
	resp, err = sdk.Do(req)
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	_ = resp.Body.Close()

	ended := recorder.Ended()
	for i, wantName := range []string{"GET /health", "POST /v1/chat/completions"} {
		got := ended[len(ended)-2+i]
		if got.Name() != wantName {
			t.Errorf("span name = %q, want %q", got.Name(), wantName)
		}
		errorType := ""
		for _, kv := range got.Attributes() {
			if kv.Key == semconv.ErrorTypeKey {
				errorType = kv.Value.AsString()
			}
		}
		if errorType != "503" {
			t.Errorf("%s: error.type = %q, want 503", wantName, errorType)
		}
	}
}
