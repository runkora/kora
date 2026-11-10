package sessionmanager

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// interruptIntoGap interrupts run-1 and returns once the run is reported idle,
// with the resumed runner's Start held on resumed.startGate.
func interruptIntoGap(t *testing.T, resumed *scriptRunner) (*Registry, *scriptRunner) {
	t.Helper()
	reg, r, n := startScripted(t, time.Minute)
	resumed.startGate = make(chan struct{})
	r.resume = resumed
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })
	if err := reg.Interrupt(context.Background(), "run-1"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	waitFor(t, "idle", func() bool { c, _ := n.snapshot(); return slices.Contains(c, "idle") })
	return reg, resumed
}

func sendAsync(reg *Registry, ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() { done <- reg.Send(ctx, "run-1", "hello") }()
	return done
}

func TestSend_InResumeGapReachesResumedSession(t *testing.T) {
	reg, resumed := interruptIntoGap(t, newScriptRunner())
	done := sendAsync(reg, context.Background())
	select {
	case err := <-done:
		t.Fatalf("Send returned %v before the resume finished", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(resumed.startGate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return after the resume")
	}
	if got, want := resumed.sentTexts(), []string{stopNote("Acme") + "hello"}; !slices.Equal(got, want) {
		t.Errorf("sent = %q, want %q", got, want)
	}
}

func TestSend_InResumeGapFailedStartIsNotFound(t *testing.T) {
	resumed := newScriptRunner()
	resumed.startErr = errors.New("spawn failed")
	reg, _ := interruptIntoGap(t, resumed)
	ctrl := &Controller{logger: logging.NewLoggerWithOutput(io.Discard), registry: reg}

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodPost, "/session/run-1/messages", strings.NewReader(`{"content":"hi"}`))
		req.SetPathValue("runID", "run-1")
		req.Header.Set("Content-Type", "application/json")
		ctrl.handleSendMessage(rec, req)
	}()
	time.Sleep(50 * time.Millisecond)
	close(resumed.startGate)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if err := reg.Send(context.Background(), "run-1", "again"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Send after failed resume = %v, want ErrSessionNotFound", err)
	}
}

func TestSend_InResumeGapHonorsContext(t *testing.T) {
	resumed := newScriptRunner()
	reg, _ := interruptIntoGap(t, resumed)
	defer close(resumed.startGate)
	ctx, cancel := context.WithCancel(context.Background())
	done := sendAsync(reg, ctx)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Send = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send hung after the context was cancelled")
	}
}

func TestSend_InResumeGapTimesOutWith503(t *testing.T) {
	old := resumeWaitTimeout
	resumeWaitTimeout = 50 * time.Millisecond
	t.Cleanup(func() { resumeWaitTimeout = old })
	resumed := newScriptRunner()
	reg, _ := interruptIntoGap(t, resumed)
	defer close(resumed.startGate)

	if err := reg.Send(context.Background(), "run-1", "hello"); !errors.Is(err, ErrResuming) {
		t.Fatalf("Send = %v, want ErrResuming", err)
	}
	ctrl := &Controller{logger: logging.NewLoggerWithOutput(io.Discard), registry: reg}
	req := httptest.NewRequest(http.MethodPost, "/session/run-1/messages", strings.NewReader(`{"content":"hi"}`))
	req.SetPathValue("runID", "run-1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctrl.handleSendMessage(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
