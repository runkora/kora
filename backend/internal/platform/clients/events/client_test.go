package events

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestStream_ReadsRecordsAfterTheCursor(t *testing.T) {
	var gotAfter, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAfter, gotAuth = r.URL.Query().Get("after"), r.Header.Get("Authorization")
		_, _ = io.WriteString(w, ": keepalive\n\n"+
			"id: 8\ndata: {\"seq\":8,\"event\":{\"id\":\"e-8\"}}\n\n"+
			"id: 9\ndata: {\"seq\":9,\"event\":{\"id\":\"e-9\"}}\n\n")
	}))
	defer srv.Close()
	c := NewClient(srv.URL, func(context.Context) (string, error) { return "tok", nil }, logging.NewLoggerWithOutput(io.Discard))

	var seqs []int64
	if err := c.Stream(context.Background(), 7, func(r Record) error {
		seqs = append(seqs, r.Seq)
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if gotAfter != "7" || gotAuth != "Bearer tok" || len(seqs) != 2 || seqs[1] != 9 {
		t.Errorf("after=%q auth=%q seqs=%v", gotAfter, gotAuth, seqs)
	}

	stop := errors.New("stop")
	if err := c.Stream(context.Background(), 7, func(Record) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("Stream = %v, want the handler's error", err)
	}
}

func TestStream_RefusedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, func(context.Context) (string, error) { return "tok", nil }, logging.NewLoggerWithOutput(io.Discard))
	if err := c.Stream(context.Background(), 0, func(Record) error { return nil }); !errors.Is(err, ErrUnexpectedStatus) {
		t.Errorf("Stream = %v, want ErrUnexpectedStatus", err)
	}
}
