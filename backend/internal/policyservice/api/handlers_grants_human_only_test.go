package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// writeRecordingGrants is an in-memory grants repository that counts the writes
// that reach it, so a test can tell a refused request from an allowed one.
type writeRecordingGrants struct {
	grants.Repository

	mu      sync.Mutex
	stored  *grants.Grant
	created int
	updated int
	deleted int
}

func (r *writeRecordingGrants) Create(_ context.Context, g *grants.Grant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	g.ID = "new-grant"
	r.created++
	return nil
}

func (r *writeRecordingGrants) GetByID(context.Context, string) (*grants.Grant, error) {
	if r.stored == nil {
		return nil, grants.ErrGrantNotFound
	}
	return r.stored, nil
}

func (r *writeRecordingGrants) UpdateBudget(context.Context, string, string, *grants.BudgetAssignment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updated++
	return nil
}

func (r *writeRecordingGrants) Delete(context.Context, string, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted++
	return nil
}

// List backs the caller's effective scopes for the anti-escalation ceiling: every
// caller holds inference:invoke.
func (r *writeRecordingGrants) List(context.Context, string, grants.PrincipalType, string, int, int) ([]*grants.Grant, int, error) {
	return []*grants.Grant{{Scopes: []string{"inference:invoke"}}}, 1, nil
}

// anyScopeVocab accepts every scope.
type anyScopeVocab struct{ vocabulary.Repository }

func (anyScopeVocab) Exists(context.Context, string) (bool, error) { return true, nil }
func (anyScopeVocab) List(context.Context, string) ([]*vocabulary.ScopeEntry, error) {
	return nil, nil
}

type grantOp string

const (
	opCreate grantOp = "create"
	opBudget grantOp = "budget"
	opDelete grantOp = "delete"
)

var allGrantOps = []grantOp{opCreate, opBudget, opDelete}

// allowedStatus is the success status of each write.
var allowedStatus = map[grantOp]int{opCreate: http.StatusCreated, opBudget: http.StatusOK, opDelete: http.StatusNoContent}

// runGrantWrite sends one grant write as a caller of callerType in tenant t1 and
// returns the status, the response body and the repository, for write-count
// assertions. A SYSTEM caller has no tenant in its token and names the tenant in
// the create body, as kora-agents and identity do. The grant
// in the request (create) or in the store (budget, delete) has principal type
// grantType in tenant grantTenant.
func runGrantWrite(t *testing.T, op grantOp, callerType token.PrincipalType, grantType, grantTenant string) (int, string, *writeRecordingGrants) {
	t.Helper()
	repo := &writeRecordingGrants{stored: &grants.Grant{
		ID: "g1", TenantID: grantTenant, PrincipalType: grants.PrincipalType(grantType), PrincipalID: "p1",
	}}
	svc := &Service{
		grantRepo:       repo,
		vocabRepo:       anyScopeVocab{},
		roleRepo:        fixedRoles{},
		logger:          logging.NewLoggerWithOutput(io.Discard),
		recompileTimers: map[string]*time.Timer{},
		lastWriteAt:     map[string]time.Time{},
	}
	// An allowed write schedules a debounced recompile that needs a bundle
	// server this test does not have; stop it before it fires.
	t.Cleanup(func() {
		svc.recompileMu.Lock()
		defer svc.recompileMu.Unlock()
		for _, tm := range svc.recompileTimers {
			tm.Stop()
		}
		if svc.gatewayRecompileTimer != nil {
			svc.gatewayRecompileTimer.Stop()
		}
	})
	c := &Controller{svc: svc, logger: svc.logger}

	claims := &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "caller-1"},
		Kora:             token.KoraClaims{PrincipalType: callerType, TenantID: "t1", PatID: "pat-1"},
	}
	bodyTenant := "t1"
	if callerType == token.PrincipalTypeSystem {
		claims.Kora.TenantID = ""
		bodyTenant = grantTenant
	}

	var body string
	var handler http.HandlerFunc
	switch op {
	case opCreate:
		// tenant_id is only honoured from a SYSTEM caller; others are pinned to
		// their token's tenant and must not name another.
		body = `{"principal_type":"` + grantType + `","principal_id":"p1","scopes":["inference:invoke"],"tenant_id":"` + bodyTenant + `"}`
		handler = c.handleCreateGrant
	case opBudget:
		body = `{"budget":{"limit_usd":5,"reset_cadence":"monthly"}}`
		handler = c.handleUpdateGrantBudget
	case opDelete:
		handler = c.handleDeleteGrant
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req.SetPathValue("id", "g1")
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	handler(w, req)
	return w.Code, w.Body.String(), repo
}

func writeCount(repo *writeRecordingGrants, op grantOp) int {
	switch op {
	case opCreate:
		return repo.created
	case opBudget:
		return repo.updated
	default:
		return repo.deleted
	}
}

func TestGrantWrites_NonHumanPrincipalsAreSystemOnly(t *testing.T) {
	human, system, service := token.PrincipalTypeHuman, token.PrincipalTypeSystem, token.PrincipalTypeService
	pat, agent := token.PrincipalTypePAT, token.PrincipalTypeAgent
	nonHuman := []string{"AGENT", "SERVICE", "PAT"}

	type tc struct {
		name      string
		caller    token.PrincipalType
		grantType string
		allowed   bool
	}
	var cases []tc
	cases = append(cases,
		tc{"human admin writes a HUMAN grant", human, "HUMAN", true},
		tc{"service caller writes a HUMAN grant", service, "HUMAN", true},
	)
	for _, gt := range nonHuman {
		cases = append(cases,
			tc{"human caller, " + gt + " grant", human, gt, false},
			tc{"service caller, " + gt + " grant", service, gt, false},
			tc{"pat caller, " + gt + " grant", pat, gt, false},
			tc{"agent caller, " + gt + " grant", agent, gt, false},
		)
	}
	for _, gt := range append([]string{"HUMAN"}, nonHuman...) {
		cases = append(cases, tc{"system caller, " + gt + " grant", system, gt, true})
	}

	for _, c := range cases {
		for _, op := range allGrantOps {
			t.Run(c.name+"/"+string(op), func(t *testing.T) {
				status, body, repo := runGrantWrite(t, op, c.caller, c.grantType, "t1")
				want, writes := http.StatusForbidden, 0
				if c.allowed {
					want, writes = allowedStatus[op], 1
				}
				if status != want {
					t.Errorf("status = %d, want %d", status, want)
				}
				if !c.allowed {
					assertNonHumanRefusal(t, body)
				}
				if got := writeCount(repo, op); got != writes {
					t.Errorf("repository %s calls = %d, want %d", op, got, writes)
				}
			})
		}
	}
}

// An unrecognised principal type from a non-SYSTEM caller is refused with 403,
// not 400: the check fails closed and does not depend on the type being valid.
func TestGrantCreate_UnknownPrincipalTypeFailsClosed(t *testing.T) {
	for _, op := range allGrantOps {
		t.Run(string(op), func(t *testing.T) {
			status, body, repo := runGrantWrite(t, op, token.PrincipalTypeHuman, "FOO", "t1")
			if status != http.StatusForbidden {
				t.Errorf("status = %d, want 403", status)
			}
			assertNonHumanRefusal(t, body)
			if n := writeCount(repo, op); n != 0 {
				t.Errorf("repository %s calls = %d, want 0", op, n)
			}
		})
	}
}

// A SYSTEM caller has no tenant of its own and writes into any tenant: the real
// kora-agents and identity path.
func TestGrantWrites_SystemWritesAnotherTenant(t *testing.T) {
	for _, op := range allGrantOps {
		t.Run(string(op), func(t *testing.T) {
			status, _, repo := runGrantWrite(t, op, token.PrincipalTypeSystem, "AGENT", "t2")
			if status != allowedStatus[op] {
				t.Errorf("status = %d, want %d", status, allowedStatus[op])
			}
			if n := writeCount(repo, op); n != 1 {
				t.Errorf("repository %s calls = %d, want 1", op, n)
			}
		})
	}
}

// assertNonHumanRefusal checks a body is the refusal from refuseNonHumanGrantWrite,
// not a 403 from the tenant, scope-ceiling or delegation checks.
func assertNonHumanRefusal(t *testing.T, body string) {
	t.Helper()
	if !strings.Contains(body, "can only be written by a SYSTEM caller") || !strings.Contains(body, platformapi.ErrCodeForbidden) {
		t.Errorf("body %q is not the non-HUMAN grant refusal", body)
	}
}

// A grant in another tenant is a 404 whatever its type: the type check runs after
// the tenant-ownership check, so it never reveals that the grant exists.
func TestGrantWrites_CrossTenantNonHumanGrantIs404(t *testing.T) {
	for _, op := range []grantOp{opBudget, opDelete} {
		t.Run(string(op), func(t *testing.T) {
			status, _, repo := runGrantWrite(t, op, token.PrincipalTypeHuman, "AGENT", "t2")
			if status != http.StatusNotFound {
				t.Errorf("status = %d, want 404", status)
			}
			if n := writeCount(repo, op); n != 0 {
				t.Errorf("repository %s calls = %d, want 0", op, n)
			}
		})
	}
}

// The refusal says who may write, so a caller knows what to do.
func TestRefuseNonHumanGrantWrite_MessageNamesSystemCaller(t *testing.T) {
	c := &Controller{logger: logging.NewLoggerWithOutput(io.Discard)}
	claims := &token.PlatformClaims{Kora: token.KoraClaims{PrincipalType: token.PrincipalTypeHuman, TenantID: "t1"}}
	w := httptest.NewRecorder()
	if !c.refuseNonHumanGrantWrite(context.Background(), w, claims, grants.PrincipalTypeAgent) {
		t.Fatal("expected refusal")
	}
	if !strings.Contains(w.Body.String(), "SYSTEM caller") {
		t.Errorf("body %q does not name the SYSTEM caller", w.Body)
	}
}
