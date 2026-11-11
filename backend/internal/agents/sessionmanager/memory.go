package sessionmanager

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// CgroupDir is the container's own cgroup v2 directory, the root of its cgroup namespace.
const CgroupDir = "/sys/fs/cgroup"

// outOfMemoryMessage is the run_error message of a run the pressure monitor kills.
const outOfMemoryMessage = "The agent pod ran out of memory."

// errNoInactiveFile means memory.stat has no inactive_file line, so no working set can be worked out.
var errNoInactiveFile = errors.New("memory.stat has no inactive_file")

// MemoryReport is the pod's memory as kora-agents places runs by it
// (docs/adr/2026-11-09-demand-driven-agent-scaling.md).
type MemoryReport struct {
	// WorkingSetBytes is memory.current minus inactive_file, the measure the kubelet evicts by.
	WorkingSetBytes int64 `json:"working_set_bytes"`
	// LimitBytes is memory.max. Zero means the container has no memory limit.
	LimitBytes int64 `json:"limit_bytes"`
}

// memoryReader reads the pod's memory. CgroupMemory reads it; tests substitute fixtures.
type memoryReader interface {
	ReadMemory() (MemoryReport, error)
}

// CgroupMemory reads memory from the cgroup v2 files in Dir.
type CgroupMemory struct {
	Dir string
}

// ReadMemory reads memory.current, memory.stat and memory.max.
func (m CgroupMemory) ReadMemory() (MemoryReport, error) {
	current, err := readCgroupInt(filepath.Join(m.Dir, "memory.current"))
	if err != nil {
		return MemoryReport{}, err
	}
	inactive, err := readInactiveFile(filepath.Join(m.Dir, "memory.stat"))
	if err != nil {
		return MemoryReport{}, err
	}
	report := MemoryReport{WorkingSetBytes: max(current-inactive, 0)}
	raw, err := os.ReadFile(filepath.Join(m.Dir, "memory.max"))
	if err != nil {
		return MemoryReport{}, fmt.Errorf("read memory.max: %w", err)
	}
	if limit := strings.TrimSpace(string(raw)); limit != "max" {
		if report.LimitBytes, err = strconv.ParseInt(limit, 10, 64); err != nil {
			return MemoryReport{}, fmt.Errorf("parse memory.max: %w", err)
		}
	}
	return report, nil
}

func readCgroupInt(path string) (int64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return v, nil
}

func readInactiveFile(path string) (int64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read memory.stat: %w", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		if v, ok := strings.CutPrefix(scanner.Text(), "inactive_file "); ok {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse inactive_file: %w", err)
			}
			return n, nil
		}
	}
	return 0, errNoInactiveFile
}

// WatchMemory checks the working set every interval until ctx ends, and kills
// runs above limit × memory.max so the kernel OOM kill, which takes the whole container, does not fire.
func (reg *Registry) WatchMemory(ctx context.Context, mem memoryReader, limit float64, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		reg.relieveMemory(ctx, mem, limit)
	}
}

// relieveMemory kills the most recently started run while the working set is
// above limit × memory.max, re-reading after each kill.
func (reg *Registry) relieveMemory(ctx context.Context, mem memoryReader, limit float64) {
	for {
		report, err := mem.ReadMemory()
		if err != nil {
			reg.logger.Warn(ctx, "Failed to read pod memory", logging.Field{Key: "error", Value: err.Error()})
			return
		}
		if report.LimitBytes == 0 || float64(report.WorkingSetBytes) <= limit*float64(report.LimitBytes) {
			return
		}
		s := reg.takeNewest()
		if s == nil {
			return
		}
		reg.logger.Warn(ctx, "Pod memory above the pressure limit, killing the newest run",
			logging.Field{Key: "run_id", Value: s.runID},
			logging.Field{Key: "working_set_bytes", Value: report.WorkingSetBytes},
			logging.Field{Key: "limit_bytes", Value: report.LimitBytes},
		)
		// Canceling the run context kills the subprocess; Close waits for it to exit before the next read.
		s.cancel()
		_ = s.runner.Close()
	}
}

// takeNewest removes the most recently started session from the registry, marks
// it failed for its drain to end, and returns it, or nil when none is left. A
// session whose resume after an interrupt is in flight is skipped: its resumed session replaces it.
func (reg *Registry) takeNewest() *session {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	var newest *session
	for _, s := range reg.sessions {
		if s.handoff.Load() != nil {
			continue
		}
		if newest == nil || s.startedAt.After(newest.startedAt) {
			newest = s
		}
	}
	if newest != nil {
		delete(reg.sessions, newest.runID)
		newest.failure.Store(&RunErrorPayload{Code: events.RunErrorSubprocessExit, Message: outOfMemoryMessage})
	}
	return newest
}
