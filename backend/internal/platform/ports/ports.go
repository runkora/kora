// Package ports holds well-known service port numbers that must agree across
// more than one binary, so a single value cannot drift between consumers — for
// example the sandbox's HTTPS_PROXY URL (internal/agents) and the NetworkPolicy
// that must allow it (internal/operator).
package ports

// EgressGatewayCONNECT is the egress gateway's HTTP CONNECT port. It must match
// charts/orbit/values.yaml and charts/orbit/templates/egress-gateway/service.yaml.
const EgressGatewayCONNECT = 8085

// SandboxExecGRPC is the sandbox-exec gRPC port. orbit-agents renders it onto
// the sandbox pod and the operator's ingress NetworkPolicy admits it. A constant
// rather than configuration, because a pod and a policy disagreeing on it makes
// the sandbox unreachable with nothing to show for it.
const SandboxExecGRPC = 50051
