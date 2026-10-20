// Package probes holds probe settings that must agree across more than one
// binary: orbit-agents renders them onto the sandbox pod and the operator's
// webhook onto its sidecars.
package probes

// DefaultStartupSeconds is how long a sandbox container may take to start
// before its startup probe restarts it. It is a ceiling for a stuck container,
// deliberately far above a normal start, not a copy of any readiness budget.
// The chart's agents.sandboxStartupSeconds overrides it.
const DefaultStartupSeconds = 120

// StartupPeriodSeconds is the startup probes' check interval. A short period
// makes the pod Ready soon after the container is up, and costs nothing on the
// fast path because the probe stops at its first success.
const StartupPeriodSeconds = 1

// StartupFailureThreshold is the failures a startup probe tolerates to allow
// seconds before a restart. Zero or negative takes DefaultStartupSeconds.
func StartupFailureThreshold(seconds int) int32 {
	if seconds <= 0 {
		seconds = DefaultStartupSeconds
	}
	return int32(seconds / StartupPeriodSeconds) //nolint:gosec // bounded by the chart
}
