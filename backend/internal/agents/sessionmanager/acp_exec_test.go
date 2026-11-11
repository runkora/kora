package sessionmanager

import (
	"bufio"
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Canceling the adapter's context kills its whole process group, so a harness the adapter started does not outlive it.
func TestACPExecSpawn_CancelKillsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proc, err := acpExecSpawn(ctx, "sh", []string{"-c", "sleep 30 & echo $!; wait"}, t.TempDir(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(proc.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = proc.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for !errors.Is(syscall.Kill(child, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("child %d outlived the adapter", child)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
