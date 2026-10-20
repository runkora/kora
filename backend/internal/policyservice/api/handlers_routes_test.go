package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
)

// knownVocab reports every scope as present, so a tenant registration passes
// the vocabulary check.
type knownVocab struct{ idempotentVocab }

func (knownVocab) Exists(context.Context, string) (bool, error) { return true, nil }

const serviceSubject = "spiffe://orbit.local/tenant/orbit-tenant-example/service/billing"

var registeredRoutes = []routes.RouteScope{{PathPrefix: "/v1/thing", Method: "GET", RequiredScope: "thing:read"}}

// newRoutesController returns a controller whose store already holds the
// registrations the test callers send. A permitted call then changes nothing
// and schedules no recompile.
func newRoutesController() *Controller {
	s := &Service{
		vocabRepo:               knownVocab{},
		routeStore:              routes.NewStore(),
		tenants:                 fakeTenantLister{},
		logger:                  logging.NewLoggerWithOutput(io.Discard),
		recompileTimers:         make(map[string]*time.Timer),
		lastWriteAt:             make(map[string]time.Time),
		recompileAllHealthy:     true,
		gatewayRecompileHealthy: true,
	}
	s.routeStore.Register(&routes.Registration{Service: "inference-gateway", Routes: registeredRoutes})
	s.routeStore.Register(&routes.Registration{Service: "orbit-agents", Routes: registeredRoutes})
	s.routeStore.Register(&routes.Registration{Service: serviceSubject, TenantID: "example", Routes: registeredRoutes})
	return &Controller{svc: s, logger: s.logger}
}

func TestHandleRegisterRoutes_CallerService(t *testing.T) {
	system := func(name string) *token.PlatformClaims {
		return &token.PlatformClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "spiffe://orbit.local/system/" + name},
			Kora:             token.KoraClaims{PrincipalType: token.PrincipalTypeSystem, DisplayName: name},
		}
	}
	service := &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: serviceSubject},
		Kora:             token.KoraClaims{PrincipalType: token.PrincipalTypeService, TenantID: "example", DisplayName: "billing"},
	}
	unnamedSystem := &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "orbit-identity-service"},
		Kora:             token.KoraClaims{PrincipalType: token.PrincipalTypeSystem},
	}

	declared := []routes.ScopeDeclaration{{Scope: "thing:read", ResourceType: "thing", Action: "read"}}

	tests := []struct {
		name    string
		claims  *token.PlatformClaims
		service string
		scopes  []routes.ScopeDeclaration
		want    int
	}{
		{"system registers its own service", system("inference-gateway"), "inference-gateway", nil, http.StatusNoContent},
		{"system registers another service", system("orbit-agents"), "inference-gateway", nil, http.StatusForbidden},
		{"system token without a component name", unnamedSystem, "inference-gateway", nil, http.StatusForbidden},
		{"service registers its own identity", service, serviceSubject, nil, http.StatusNoContent},
		{"service registers another service", service, "inference-gateway", nil, http.StatusForbidden},
		{"service declares a scope", service, serviceSubject, declared, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newRoutesController()
			body, _ := json.Marshal(registerRoutesRequest{Service: tt.service, Routes: registeredRoutes, Scopes: tt.scopes, SkipPrune: true})
			req := httptest.NewRequest(http.MethodPost, "/policy/routes/register", bytes.NewReader(body))
			req = req.WithContext(token.WithClaims(req.Context(), tt.claims))
			rec := httptest.NewRecorder()

			c.handleRegisterRoutes(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}
