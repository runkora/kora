package main

import (
	"context"
	"testing"

	"github.com/clear-route/orbit/internal/agents/sandbox/execauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeStream is a ServerStream whose only job is to carry a context into the
// interceptor.
type fakeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f fakeStream) Context() context.Context { return f.ctx }

func incoming(pairs ...string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(pairs...))
}

// The exec server is the only thing standing between a run's sandbox and every
// other pod that can reach its IP, so anything short of the run's exact token is
// refused. See docs/adr/2026-09-18-sandbox-exec-trust-boundary.md and
// https://github.com/clear-route/orbit/issues/202.
func TestRequireExecToken(t *testing.T) {
	const want = "correct-horse-battery-staple"

	tests := []struct {
		name    string
		ctx     context.Context
		allowed bool
	}{
		{"exact token", incoming(execauth.MetadataKey, want), true},
		{"no metadata at all", context.Background(), false},
		{"metadata without the key", incoming("some-other-key", want), false},
		{"wrong token", incoming(execauth.MetadataKey, "wrong"), false},
		{"empty token", incoming(execauth.MetadataKey, ""), false},
		{"token as a prefix", incoming(execauth.MetadataKey, want+"-extra"), false},
		// A caller must not be able to send a list and have one member matched.
		{"correct token among several", incoming(execauth.MetadataKey, "wrong", execauth.MetadataKey, want), false},
	}

	interceptor := requireExecTokenStream(want)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := func(any, grpc.ServerStream) error {
				called = true
				return nil
			}
			err := interceptor(nil, fakeStream{ctx: tc.ctx}, &grpc.StreamServerInfo{}, handler)

			// The unary surface has to answer identically, so adding a unary RPC
			// cannot land unauthenticated.
			unaryCalled := false
			_, unaryErr := requireExecTokenUnary(want)(tc.ctx, nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
				unaryCalled = true
				return nil, nil
			})
			if unaryCalled != tc.allowed || (unaryErr != nil) == tc.allowed {
				t.Errorf("unary guard disagreed with the stream guard: called=%v err=%v", unaryCalled, unaryErr)
			}

			if tc.allowed {
				if err != nil {
					t.Fatalf("expected the call to be allowed, got %v", err)
				}
				if !called {
					t.Fatal("expected the handler to run")
				}
				return
			}
			if called {
				t.Fatal("expected the handler not to run")
			}
			if status.Code(err) != codes.Unauthenticated {
				t.Fatalf("expected Unauthenticated, got %v", err)
			}
		})
	}
}
