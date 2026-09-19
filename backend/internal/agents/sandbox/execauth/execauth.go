// Package execauth carries the per-run secret a caller presents to a sandbox
// pod's exec server. The exec port is excluded from the inbound mesh, leaving
// this token as the only thing distinguishing one run's sandbox from its
// siblings (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
//
// Four binaries agree on the key and env var here: orbit-agents sets the token
// on the pod, sandbox-exec reads it back, session-manager and orbit-shell
// present it.
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

// TokenFrom returns the token presented on an incoming call. False when the
// metadata is absent, or carries anything but exactly one value: a list must not
// match on one of its members.
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
