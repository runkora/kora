//go:build !linux

package main

import "errors"

// nsChildEnvKey is empty on non-Linux — the check in main() is always false.
const nsChildEnvKey = ""

// nsChildExec is a no-op stub; it is never called on non-Linux platforms since
// nsChildEnvKey is "" and os.Getenv("") is always "".
func nsChildExec(_ []string) {}

// runInPIDNamespace is unsupported on non-Linux. The caller falls back to
// direct syscall.Exec without namespace isolation.
func runInPIDNamespace(_ string, _ []string) error {
	return errors.New("pid namespace isolation not supported on this platform")
}
