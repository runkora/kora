//go:build linux

package main

import (
	"syscall"
	"testing"
)

func TestHideEnviron_ClearsDumpable(t *testing.T) {
	if err := hideEnviron(); err != nil {
		t.Fatalf("hideEnviron: %v", err)
	}
	dumpable, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_GET_DUMPABLE, 0, 0)
	if errno != 0 {
		t.Fatalf("PR_GET_DUMPABLE: %v", errno)
	}
	if dumpable != 0 {
		t.Errorf("dumpable = %d, want 0", dumpable)
	}
}
