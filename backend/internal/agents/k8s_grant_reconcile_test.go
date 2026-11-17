package agents

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// grantServer is a policy service holding one stale grant, refusing or
// accepting the replacement create.
type grantServer struct {
	refuseCreate bool

	mu      sync.Mutex
	created int
	deleted []string
}

func (s *grantServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"grants": []policyclient.Grant{{ID: "old", Scopes: []string{"connection:github:contents:read"}}}})
	case r.Method == http.MethodPost && s.refuseCreate:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":"validation_failed","message":"scope is not in the scope vocabulary"}`)
	case r.Method == http.MethodPost:
		s.created++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(policyclient.Grant{ID: "new"})
	case r.Method == http.MethodDelete:
		s.deleted = append(s.deleted, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}
}

// The replacement grant is created before the stale one is deleted, so a
// refused create keeps the agent's previous authorization and returns the error.
func TestEnsureAgentGrant_CreatesBeforeDeleting(t *testing.T) {
	for _, refuse := range []bool{true, false} {
		srv := &grantServer{refuseCreate: refuse}
		ts := httptest.NewServer(srv)
		logger := logging.NewLoggerWithOutput(io.Discard)
		p := &K8sProvisioner{
			logger:       logger,
			policyClient: policyclient.NewClient(ts.URL, func(context.Context) (string, error) { return "tok", nil }, logger),
		}
		at := AgentType{TenantID: "example-org", Slug: "coder", GrantScopes: []string{"connection:github:pulls:read"}}

		err := p.ensureAgentGrant(context.Background(), at, "kora-tenant-example-org")
		ts.Close()
		switch {
		case refuse && (err == nil || len(srv.deleted) != 0):
			t.Errorf("refused create: err = %v, deleted = %v; want the error and the old grant kept", err, srv.deleted)
		case !refuse && (err != nil || srv.created != 1 || len(srv.deleted) != 1):
			t.Errorf("accepted create: err = %v, created = %d, deleted = %v; want one create then the old grant deleted", err, srv.created, srv.deleted)
		}
	}
}
