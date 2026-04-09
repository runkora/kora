package policygw

import (
	"context"
	"net/http"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// routeScope maps an HTTP method + path prefix to the minimum required scope
// for Layer 1 (Envoy) enforcement.
type routeScope struct {
	pathPrefix    string
	method        string // "*" means any method
	requiredScope string
}

// defaultRouteScopes is the base set matching the route_scopes bundle document.
var defaultRouteScopes = []routeScope{
	{"/v1/inference/", "POST", "inference:invoke"},
	{"/v1/inference/", "GET", "inference:list"},
	{"/v1/mcp/", "POST", "mcp:invoke"},
	{"/v1/mcp/", "GET", "mcp:list"},
	{"/api/v1/policy/", "GET", "platform:grants:read"},
	{"/api/v1/policy/", "POST", "platform:grants:write"},
	{"/api/v1/policy/", "DELETE", "platform:grants:write"},
}

// AuthorizationServer implements envoy.service.auth.v3.AuthorizationServer
// for Envoy external authorization (Layer 1 enforcement).
// Token validation is handled by ExtAuthzAuthInterceptor — claims and
// delegation context are read from context, not extracted inline.
type AuthorizationServer struct {
	authv3.UnimplementedAuthorizationServer
	svc    *Service
	logger logging.Logger
}

// NewAuthorizationServer creates an AuthorizationServer.
func NewAuthorizationServer(svc *Service, logger logging.Logger) *AuthorizationServer {
	return &AuthorizationServer{
		svc:    svc,
		logger: logger,
	}
}

// Check implements Authorization.Check. Token validation has already been
// performed by ExtAuthzAuthInterceptor — this method reads verified claims
// from context and evaluates the policy decision.
func (s *AuthorizationServer) Check(ctx context.Context, req *authv3.CheckRequest) (*authv3.CheckResponse, error) {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		// Should not happen — interceptor rejects unauthenticated requests.
		return s.deny(401, "missing auth context"), nil
	}

	httpReq := req.GetAttributes().GetRequest().GetHttp()
	path := httpReq.GetPath()
	method := httpReq.GetMethod()

	requiredScope, found := matchRouteScope(path, method)
	if !found {
		s.logger.Warn(ctx, "ext_authz: no route scope mapping",
			logging.Field{Key: "path", Value: path},
			logging.Field{Key: "method", Value: method},
		)
		return s.deny(403, "no policy for route"), nil
	}

	delegation, _ := delegationFromContext(ctx)

	result, err := s.svc.EvaluateRoute(ctx, claims, delegation,
		requiredScope, resourceTypeFromPath(path), actionFromMethod(method),
		map[string]string{"path": path, "method": method},
	)
	if err != nil {
		s.logger.Error(ctx, "ext_authz: evaluation error",
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		)
		return s.deny(503, "policy evaluation unavailable"), nil
	}

	if !result.Allow {
		return s.deny(403, result.Reason), nil
	}
	return s.allow(claims.Subject, claims.Orbit.TenantID), nil
}

func (s *AuthorizationServer) allow(principalID, tenantID string) *authv3.CheckResponse {
	return &authv3.CheckResponse{
		Status: &status.Status{Code: int32(codes.OK)},
		HttpResponse: &authv3.CheckResponse_OkResponse{
			OkResponse: &authv3.OkHttpResponse{
				Headers: []*corev3.HeaderValueOption{
					{Header: &corev3.HeaderValue{Key: "x-orbit-principal-id", Value: principalID}},
					{Header: &corev3.HeaderValue{Key: "x-orbit-tenant-id", Value: tenantID}},
				},
			},
		},
	}
}

func (s *AuthorizationServer) deny(httpStatus int32, body string) *authv3.CheckResponse {
	return &authv3.CheckResponse{
		Status: &status.Status{Code: int32(codes.PermissionDenied)},
		HttpResponse: &authv3.CheckResponse_DeniedResponse{
			DeniedResponse: &authv3.DeniedHttpResponse{
				Status: &typev3.HttpStatus{Code: typev3.StatusCode(httpStatus)},
				Body:   body,
				Headers: []*corev3.HeaderValueOption{
					{Header: &corev3.HeaderValue{Key: "Content-Type", Value: "text/plain"}},
				},
			},
		},
	}
}

func matchRouteScope(path, method string) (string, bool) {
	for _, rs := range defaultRouteScopes {
		if strings.HasPrefix(path, rs.pathPrefix) {
			if rs.method == "*" || rs.method == method {
				return rs.requiredScope, true
			}
		}
	}
	return "", false
}

func resourceTypeFromPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/v1/inference/"):
		return "inference"
	case strings.HasPrefix(path, "/v1/mcp/"):
		return "mcp"
	case strings.HasPrefix(path, "/api/v1/policy/"):
		return "platform"
	default:
		return "unknown"
	}
}

// actionFromMethod maps an HTTP method to its canonical Orbit action string.
func actionFromMethod(method string) string {
	switch method {
	case http.MethodGet:
		return "read"
	case http.MethodPost:
		return "invoke"
	case http.MethodDelete:
		return "invoke"
	default:
		return "invoke"
	}
}
