//go:build linux

package main

import "syscall"

// hideEnviron makes this process's /proc environ unreadable to the commands it runs.
// They share its uid, and its environment holds the exec token.
func hideEnviron() error {
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 0, 0); errno != 0 {
		return errno
	}
	return nil
}
