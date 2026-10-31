package httpclient_test

import (
	"context"
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
