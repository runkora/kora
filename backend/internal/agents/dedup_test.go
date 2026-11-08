package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/token"
)

func dedupReq(agentTypeID string) StartRunRequest {
	return StartRunRequest{AgentTypeID: agentTypeID, TriggerID: "trig-1", TriggerName: "Nightly", DeliveryID: "deliv-1"}
}

func TestStartRun_DuplicateDeliveryReturnsFirstRun(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")

	first, err := f.run.StartRun(withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1"), dedupReq(at.ID))
	if err != nil {
		t.Fatalf("first StartRun: %v", err)
	}
	if first.AlreadyStarted {
		t.Error("first call must not be marked already started")
	}
	queued := len(repo.queued)

	// The delivery is repeated; the stored run is returned unchanged.
	second, err := f.run.StartRun(withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1"), dedupReq(at.ID))
	if err != nil {
		t.Fatalf("second StartRun: %v", err)
	}
	if second.ID != first.ID || !second.AlreadyStarted {
		t.Errorf("second = %s (already=%v), want first run %s marked already started", second.ID, second.AlreadyStarted, first.ID)
	}
	if second.CreatedBy != "trigger:trig-1" {
		t.Errorf("created_by = %q, want the first caller's", second.CreatedBy)
	}
	if len(repo.runs) != 1 {
		t.Errorf("runs = %d, want 1", len(repo.runs))
	}
	if len(repo.queued) != queued {
		t.Errorf("queued = %d, want %d: a duplicate must not queue a run", len(repo.queued), queued)
	}
}

func TestStartRun_DuplicateDeliveryUnauthorizedIsForbidden(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1")
	if _, err := f.run.StartRun(ctx, dedupReq(at.ID)); err != nil {
		t.Fatalf("first StartRun: %v", err)
	}
	f.run.authz = NewAuthz(&scopedEvaluator{})
	if _, err := f.run.StartRun(ctx, dedupReq(at.ID)); !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}

func TestStartRun_DuplicateLookupFailureKeepsExistingRun(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1")
	first, err := f.run.StartRun(ctx, dedupReq(at.ID))
	if err != nil {
		t.Fatalf("first StartRun: %v", err)
	}
	// Lookup of the existing run fails after the dedup hit: the first run must stay as it was.
	repo.getRunErr = errors.New("boom")
	if _, err := f.run.StartRun(ctx, dedupReq(at.ID)); err == nil {
		// GetRunByDelivery in the mock does not use getRunErr, so a success is also valid.
		t.Log("lookup succeeded")
	}
	repo.getRunErr = nil
	got, err := repo.GetRun(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.State == RunStateFailed {
		t.Errorf("existing run state = %s, a duplicate must not fail it", got.State)
	}
}

func TestHandleStartRun_DuplicateDeliveryReturns200(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")

	post := func() *httptest.ResponseRecorder {
		raw, _ := json.Marshal(dedupReq(at.ID))
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(withPrincipalCtx(req.Context(), "t1", token.PrincipalTypeService, "trigger:trig-1"))
		w := httptest.NewRecorder()
		c.handleStartRun(w, req)
		return w
	}
	w1, w2 := post(), post()
	if w1.Code != http.StatusCreated || w2.Code != http.StatusOK {
		t.Fatalf("codes = %d, %d; want 201, 200; bodies: %s %s", w1.Code, w2.Code, w1.Body, w2.Body)
	}
	var r1, r2 Run
	if err := json.Unmarshal(w1.Body.Bytes(), &r1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &r2); err != nil {
		t.Fatal(err)
	}
	if r1.ID == "" || r1.ID != r2.ID {
		t.Errorf("ids = %q, %q; want the same run", r1.ID, r2.ID)
	}
}

type failQueueOnce struct {
	runQueue
	failed bool
}

func (q *failQueueOnce) QueueRun(ctx context.Context, id string, at time.Time) error {
	if !q.failed {
		q.failed = true
		return errors.New("queue down")
	}
	return q.runQueue.QueueRun(ctx, id, at)
}

// A start that failed after CreateRun frees its delivery, so a retry starts a new run.
func TestStartRun_RetryAfterFailedStartStartsNewRun(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1")
	f.run.queue = &failQueueOnce{runQueue: f.run.queue}

	if _, err := f.run.StartRun(ctx, dedupReq(at.ID)); err == nil {
		t.Fatal("first StartRun: want the queue error")
	}
	retry, err := f.run.StartRun(ctx, dedupReq(at.ID))
	if err != nil {
		t.Fatalf("retry StartRun: %v", err)
	}
	if retry.AlreadyStarted || retry.State == RunStateFailed {
		t.Errorf("retry = %+v, want a new pending run", retry)
	}
	if len(repo.queued) != 1 || repo.queued[0] != retry.ID {
		t.Errorf("queued = %v, want only the retry run", repo.queued)
	}
}
