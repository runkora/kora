package egressgw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
)

type rcConnector struct{ calls *int }

func (rcConnector) Classify(*http.Request) (credentials.Classification, error) {
	return credentials.Classification{}, nil
}
func (rcConnector) Mint(context.Context, credentials.CallerIdentity) (credentials.Credential, error) {
	return credentials.Credential{}, nil
}
func (c rcConnector) ListQualifierValues(context.Context) ([]string, error) {
	*c.calls++
	return []string{"org/a", "org/b"}, nil
}

type rcAdapter struct {
	typ   credentials.AdapterType
	calls *int
	plain bool
}

func (a rcAdapter) Type() credentials.AdapterType { return a.typ }
func (a rcAdapter) Actions() []credentials.ActionDeclaration {
	return nil
}
func (a rcAdapter) Open(credentials.Connection) (credentials.Connector, error) {
	if a.plain {
		return struct{ credentials.Connector }{rcConnector{a.calls}}, nil
	}
	return rcConnector{a.calls}, nil
}

type rcLister struct{ conns []*credentials.Connection }

func (l rcLister) ListByTenant(context.Context, string) ([]*credentials.Connection, error) {
	return l.conns, nil
}

func rcRequest(t *testing.T, c *ConnectionsReadController, name string, withTenant bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/connections/"+name+"/resources", nil)
	req.SetPathValue("name", name)
	if withTenant {
		claims := &platformtoken.PlatformClaims{}
		claims.Kora.TenantID = "t1"
		req = req.WithContext(platformtoken.WithClaims(req.Context(), claims))
	}
	rec := httptest.NewRecorder()
	c.handleListConnectionResources(rec, req)
	return rec
}

func newRC(adapterType credentials.AdapterType, plain bool, pol *stubEvaluator, calls *int) *ConnectionsReadController {
	reg := NewAdapterRegistry()
	reg.Register(rcAdapter{typ: adapterType, calls: calls, plain: plain})
	lister := rcLister{conns: []*credentials.Connection{{Name: "gh", Adapter: adapterType}}}
	return NewConnectionsReadController(lister, reg, pol, nil, logging.NewLoggerWithOutput(io.Discard))
}

func TestListConnectionResources_PolicyDenied(t *testing.T) {
	calls := 0
	rec := rcRequest(t, newRC(credentials.AdapterGitHubApp, false, &stubEvaluator{allow: false, reason: "deny:no_matching_grant"}, &calls), "gh", true)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != api.ErrCodeForbidden {
		t.Errorf("code = %q, want %q (body %s)", body.Code, api.ErrCodeForbidden, rec.Body.String())
	}
	if calls != 0 {
		t.Errorf("ListQualifierValues called %d times, want 0", calls)
	}
}

func TestListConnectionResources_PolicyAllowed(t *testing.T) {
	calls := 0
	pol := &stubEvaluator{allow: true}
	rec := rcRequest(t, newRC(credentials.AdapterGitHubApp, false, pol, &calls), "gh", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Resources []string `json:"resources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Resources) != 2 {
		t.Fatalf("unexpected body %s (err %v)", rec.Body.String(), err)
	}
	if len(pol.calls) != 1 {
		t.Fatalf("policy checks = %d, want 1", len(pol.calls))
	}
	in := pol.calls[0]
	if in.ResourceType != resourceType || len(in.PathSegments) != 2 ||
		in.PathSegments[0] != "gh" || in.PathSegments[1] != "metadata" || in.Action != "read" {
		t.Errorf("unexpected CheckInput: %+v", in)
	}
}

func TestListConnectionResources_PolicyError(t *testing.T) {
	calls := 0
	rec := rcRequest(t, newRC(credentials.AdapterGitHubApp, false, &stubEvaluator{err: errors.New("bundle down")}, &calls), "gh", true)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if calls != 0 {
		t.Errorf("ListQualifierValues called %d times, want 0", calls)
	}
}

func TestListConnectionResources_NoBaselineAction(t *testing.T) {
	calls := 0
	pol := &stubEvaluator{allow: true}
	rec := rcRequest(t, newRC(credentials.AdapterType("no_baseline"), false, pol, &calls), "gh", true)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if calls != 0 || len(pol.calls) != 0 {
		t.Errorf("enumerated %d times, policy checked %d times; want 0/0", calls, len(pol.calls))
	}
}

func TestListConnectionResources_ExistingBehaviour(t *testing.T) {
	calls := 0
	pol := &stubEvaluator{allow: true}
	c := newRC(credentials.AdapterGitHubApp, false, pol, &calls)
	if rec := rcRequest(t, c, "gh", false); rec.Code != http.StatusUnauthorized {
		t.Errorf("missing tenant: status = %d, want 401", rec.Code)
	}
	if rec := rcRequest(t, c, "nope", true); rec.Code != http.StatusNotFound {
		t.Errorf("unknown connection: status = %d, want 404", rec.Code)
	}
	unreg := NewConnectionsReadController(
		rcLister{conns: []*credentials.Connection{{Name: "gh", Adapter: credentials.AdapterGitHubApp}}},
		NewAdapterRegistry(), pol, nil, logging.NewLoggerWithOutput(io.Discard))
	if rec := rcRequest(t, unreg, "gh", true); rec.Code != http.StatusNotFound {
		t.Errorf("unregistered adapter: status = %d, want 404", rec.Code)
	}
	noEnum := newRC(credentials.AdapterGitHubApp, true, pol, &calls)
	if rec := rcRequest(t, noEnum, "gh", true); rec.Code != http.StatusNotFound {
		t.Errorf("no enumerator: status = %d, want 404", rec.Code)
	}
}
