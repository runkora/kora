package policygw

import (
	"context"
	"strings"

	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/policygw/evaluation"
)

// delegationContextKey is the context key for parsed delegation claims.
type delegationContextKey struct{}

// withDelegation stores parsed DelegationClaims in the context.
func withDelegation(ctx context.Context, d *evaluation.DelegationClaims) context.Context {
	return context.WithValue(ctx, delegationContextKey{}, d)
}

// delegationFromContext retrieves DelegationClaims from context.
// Returns nil, false if no delegation token was present on the request.
func delegationFromContext(ctx context.Context) (*evaluation.DelegationClaims, bool) {
	d, ok := ctx.Value(delegationContextKey{}).(*evaluation.DelegationClaims)
	return d, ok && d != nil
}

// ExtAuthzAuthInterceptor returns a gRPC unary interceptor that validates the
// platform token embedded in the Envoy CheckRequest's forwarded Authorization
// header. On success it stores the verified PlatformClaims and any parsed
// DelegationClaims in the context — downstream handlers retrieve them with
// token.ClaimsFromContext and delegationFromContext.
//
// Mirrors the AuthMiddleware pattern for HTTP: extract → verify → store in
// context → call handler. The Check method never touches token validation.
//
// Returns UNAUTHENTICATED if the token is missing or invalid.
// Returns UNAVAILABLE if a delegation token is structurally malformed.
func ExtAuthzAuthInterceptor(verifier *token.Verifier, logger logging.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		checkReq, ok := req.(*authv3.CheckRequest)
		if !ok {
			// Not a CheckRequest — pass through (supports future additional methods).
			return handler(ctx, req)
		}

		headers := checkReq.GetAttributes().GetRequest().GetHttp().GetHeaders()

		// Extract and validate the platform token.
		authHeader := headers["authorization"]
		if authHeader == "" {
			authHeader = headers["Authorization"]
		}
		rawToken := strings.TrimPrefix(authHeader, "Bearer ")
		rawToken = strings.TrimSpace(rawToken)

		claims, err := verifier.Verify(ctx, rawToken)
		if err != nil {
			logger.Warn(ctx, "ext_authz: token validation failed", logging.Field{Key: "error", Value: err.Error()})
			return nil, grpcstatus.Error(codes.Unauthenticated, "invalid or missing token")
		}
		ctx = token.WithClaims(ctx, claims)

		// Extract and parse the optional delegation token.
		delegationRaw := headers["x-orbit-delegation-token"]
		if delegationRaw != "" {
			d, err := parseDelegationToken(delegationRaw)
			if err != nil {
				logger.Warn(ctx, "ext_authz: delegation token parse failed", logging.Field{Key: "error", Value: err.Error()})
				return nil, grpcstatus.Error(codes.Unauthenticated, "invalid delegation token")
			}
			ctx = withDelegation(ctx, d)
		}

		return handler(ctx, req)
	}
}
