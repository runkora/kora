// Package landlockmode is how strictly kora-session-runner confines an agent. The
// chart sets it as agents.landlock and the session-manager hands it to the runner
// in KORA_LANDLOCK (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
package landlockmode

import (
	"fmt"

	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
)

// Mode is enforce or bestEffort.
type Mode string

const (
	// Enforce refuses to run an agent on a kernel without the core restriction.
	Enforce Mode = "enforce"
	// BestEffort applies whatever the kernel supports, down to nothing.
	BestEffort Mode = "bestEffort"
)

// EnvVar carries the mode to the session-manager and to kora-session-runner.
const EnvVar = "KORA_LANDLOCK"

// Landlock ABIs that add confinement kora-session-runner uses. ABI v1 confines
// writes and execution. v2 lets an agent move and link files between its own
// directories, which enforce requires.
const (
	RequiredABI = 2
	truncateABI = 3
	scopeABI    = 6
)

// KernelABI is the kernel's Landlock ABI, or 0 without Landlock.
func KernelABI() int {
	abi, err := llsyscall.LandlockGetABIVersion()
	if err != nil {
		return 0
	}
	return abi
}

// Parse reads a mode. Empty is Enforce.
func Parse(s string) (Mode, error) {
	switch Mode(s) {
	case "", Enforce:
		return Enforce, nil
	case BestEffort:
		return BestEffort, nil
	}
	return "", fmt.Errorf("agents.landlock must be %q or %q, got %q", Enforce, BestEffort, s)
}

// Check returns an error when m cannot run agents on a kernel with Landlock ABI
// abi. Zero or less means the kernel has no Landlock.
func (m Mode) Check(abi int) error {
	if m == BestEffort || abi >= RequiredABI {
		return nil
	}
	return fmt.Errorf("agents.landlock is %s and this node's kernel has Landlock ABI v%d, below the required v%d. Set agents.landlock to %s to run agents without that confinement",
		m, max(abi, 0), RequiredABI, BestEffort)
}

// Missing lists the confinement a kernel with Landlock ABI abi does not apply.
func Missing(abi int) []string {
	var out []string
	switch {
	case abi < 1:
		out = append(out, "all Landlock confinement")
	case abi < RequiredABI:
		out = append(out, "moving and linking files between directories (ABI v2)")
	}
	if abi < truncateABI {
		out = append(out, "file truncation (ABI v3)")
	}
	if abi < scopeABI {
		out = append(out, "signal and abstract socket scoping (ABI v6)")
	}
	return out
}
