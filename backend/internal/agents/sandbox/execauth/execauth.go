// Package execauth carries the per-run secret that authenticates a caller to a
// sandbox pod's exec server. The sandbox's gRPC port is excluded from the
// inbound mesh, so this token is what separates one run's sandbox from every
// other sandbox in the tenant namespace
// (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
//
// Three binaries have to agree on the same key and env var: orbit-agents sets
// the token on the pod, sandbox-exec reads it back, and session-manager and
// orbit-shell present it on every call.
package execauth

import (
	"context"

	"google.golang.org/grpc/metadata"
)

const (
	// TokenEnv is where the sandbox pod carries its own token.
	TokenEnv = "SANDBOX_EXEC_TOKEN"
	// MetadataKey is where a caller presents it. gRPC lowercases metadata keys,
	// so this is already in its wire form.
	MetadataKey = "orbit-sandbox-exec-token"
)

// WithToken returns ctx carrying the token as outgoing gRPC metadata.
func WithToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, MetadataKey, token)
}

// TokenFrom returns the token presented on an incoming call. It reports false
// when the metadata is absent or carries anything other than exactly one value,
// so a caller cannot send a list and have one member matched.
func TokenFrom(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	values := md.Get(MetadataKey)
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}
