// Package ports holds well-known service port numbers that must agree across
// more than one binary, so a single value cannot drift between consumers — for
// example the sandbox's HTTPS_PROXY URL (internal/agents) and the NetworkPolicy
// that must allow it (internal/operator).
package ports

// EgressGatewayCONNECT is the egress gateway's HTTP CONNECT port. It must match
// charts/orbit/values.yaml and charts/orbit/templates/egress-gateway/service.yaml.
const EgressGatewayCONNECT = 8085
