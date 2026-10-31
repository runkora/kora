// Package landlockmode is how strictly kora-session-runner confines an agent. The
// chart sets it as agents.landlock and the session-manager hands it to the runner
// in KORA_LANDLOCK (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
package landlockmode

import "fmt"

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

// RequiredABI is the oldest Landlock ABI enforce accepts. It confines writes and
// execution and lets an agent move files between its own directories.
const RequiredABI = 2

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
	if abi < RequiredABI {
		out = append(out, "filesystem write and execute confinement (ABI v2)")
	}
	if abi < 3 {
		out = append(out, "file truncation (ABI v3)")
	}
	if abi < 6 {
		out = append(out, "signal and abstract socket scoping (ABI v6)")
	}
	return out
}
