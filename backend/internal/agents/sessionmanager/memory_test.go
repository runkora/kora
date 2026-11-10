package sessionmanager

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// writeCgroup writes the cgroup v2 memory files a CgroupMemory reads.
func writeCgroup(t *testing.T, current, limit string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"memory.current": current + "\n",
		"memory.max":     limit + "\n",
		"memory.stat":    "anon 300000000\nfile 250000000\nactive_file 50000000\ninactive_file 200000000\nshmem 0\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCgroupMemory_WorkingSetExcludesInactiveFile(t *testing.T) {
	for name, tc := range map[string]struct {
		limit string
		want  MemoryReport
	}{
		"limited":   {"1073741824", MemoryReport{WorkingSetBytes: 350000000, LimitBytes: 1073741824}},
		"unlimited": {"max", MemoryReport{WorkingSetBytes: 350000000}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := CgroupMemory{Dir: writeCgroup(t, "550000000", tc.limit)}.ReadMemory()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("ReadMemory = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestHandleMemory_ReportsWorkingSetAndLimit(t *testing.T) {
	c := &Controller{
		logger: logging.NewLoggerWithOutput(io.Discard),
		cfg:    ControllerConfig{Memory: CgroupMemory{Dir: writeCgroup(t, "550000000", "1073741824")}},
	}
	rec := httptest.NewRecorder()
	c.handleMemory(rec, httptest.NewRequest(http.MethodGet, "/memory", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got map[string]int64
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["working_set_bytes"] != 350000000 || got["limit_bytes"] != 1073741824 {
		t.Errorf("body = %v, want working_set_bytes 350000000 and limit_bytes 1073741824", got)
	}
}

// memorySequence returns its reports in order, repeating the last.
type memorySequence struct {
	reports []MemoryReport
	reads   int
}

func (m *memorySequence) ReadMemory() (MemoryReport, error) {
	r := m.reports[min(m.reads, len(m.reports)-1)]
	m.reads++
	return r, nil
}

// endNotifier records the run_error messages and how each run ended.
type endNotifier struct {
	NoopNotifier
	mu       sync.Mutex
	messages map[string]string
	ended    chan string
}

func (n *endNotifier) Notify(_ context.Context, runID string, ev Event) error {
	if ev.Type == EventTypeRunError {
		var p RunErrorPayload
		_ = json.Unmarshal(ev.Payload, &p)
		n.mu.Lock()
		n.messages[runID] = p.Message
		n.mu.Unlock()
	}
	return nil
}

func (n *endNotifier) CompleteRun(_ context.Context, runID string, _ string, failed bool) error {
	if failed {
		n.ended <- runID
	}
	return nil
}

// startRuns creates one session per run ID, each started a second after the one before.
func startRuns(t *testing.T, runIDs ...string) (*Registry, *endNotifier) {
	t.Helper()
	n := &endNotifier{messages: map[string]string{}, ended: make(chan string, len(runIDs))}
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), n, time.Minute, testBrand)
	start := time.Now()
	for i, id := range runIDs {
		if err := reg.Create(context.Background(), SessionConfig{RunID: id, Runner: newStubRunner(nil)}); err != nil {
			t.Fatal(err)
		}
		reg.sessions[id].startedAt = start.Add(time.Duration(i) * time.Second)
	}
	return reg, n
}

// killed waits for want failed runs and checks each recorded the out-of-memory message.
func (n *endNotifier) killed(t *testing.T, want int) []string {
	t.Helper()
	var ids []string
	for range want {
		select {
		case id := <-n.ended:
			ids = append(ids, id)
		case <-time.After(2 * time.Second):
			t.Fatalf("runs failed = %v, want %d", ids, want)
		}
	}
	select {
	case id := <-n.ended:
		t.Fatalf("run %s also failed, want only %v", id, ids)
	case <-time.After(50 * time.Millisecond):
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, id := range ids {
		if n.messages[id] != outOfMemoryMessage {
			t.Errorf("run %s run_error = %q, want %q", id, n.messages[id], outOfMemoryMessage)
		}
	}
	slices.Sort(ids)
	return ids
}

func TestRelieveMemory(t *testing.T) {
	const gi = 1 << 30
	at := func(fraction float64) MemoryReport {
		return MemoryReport{WorkingSetBytes: int64(fraction * gi), LimitBytes: gi}
	}
	for name, tc := range map[string]struct {
		reports []MemoryReport
		want    []string
	}{
		"below the limit kills nothing":                  {[]MemoryReport{at(0.89)}, nil},
		"no limit kills nothing":                         {[]MemoryReport{{WorkingSetBytes: gi}}, nil},
		"above the limit kills the newest":               {[]MemoryReport{at(0.95), at(0.80)}, []string{"run-3"}},
		"still above after a kill kills the next newest": {[]MemoryReport{at(0.95), at(0.92), at(0.80)}, []string{"run-2", "run-3"}},
		"never below kills every run":                    {[]MemoryReport{at(0.99)}, []string{"run-1", "run-2", "run-3"}},
	} {
		t.Run(name, func(t *testing.T) {
			reg, n := startRuns(t, "run-1", "run-2", "run-3")
			reg.relieveMemory(context.Background(), &memorySequence{reports: tc.reports}, 0.90)
			if got := n.killed(t, len(tc.want)); !slices.Equal(got, tc.want) {
				t.Errorf("killed %v, want %v", got, tc.want)
			}
			if got := reg.ActiveCount(); got != 3-len(tc.want) {
				t.Errorf("active sessions = %d, want %d", got, 3-len(tc.want))
			}
		})
	}
}

// A kill that races an interrupt still ends the run failed, and a session whose
// resume is in flight is left to it.
func TestRelieveMemory_Interrupt(t *testing.T) {
	over := []MemoryReport{{WorkingSetBytes: 1 << 30, LimitBytes: 1 << 30}, {WorkingSetBytes: 0, LimitBytes: 1 << 30}}

	reg, n := startRuns(t, "run-1", "run-2")
	reg.sessions["run-2"].interruptClose.Store(true)
	reg.relieveMemory(context.Background(), &memorySequence{reports: over}, 0.90)
	if got := n.killed(t, 1); !slices.Equal(got, []string{"run-2"}) {
		t.Errorf("killed %v, want the interrupted run-2", got)
	}

	reg, n = startRuns(t, "run-1", "run-2")
	handoff := make(chan struct{})
	reg.sessions["run-2"].handoff.Store(&handoff)
	reg.relieveMemory(context.Background(), &memorySequence{reports: over}, 0.90)
	if got := n.killed(t, 1); !slices.Equal(got, []string{"run-1"}) {
		t.Errorf("killed %v, want run-1 while run-2 resumes", got)
	}
}
