package passthrough_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/passthrough"
)

func TestPassthrough_TypeAndActions(t *testing.T) {
	a := passthrough.New()
	if a.Type() != credentials.AdapterPassthrough {
		t.Errorf("Type() = %q, want %q", a.Type(), credentials.AdapterPassthrough)
	}
	actions := a.Actions()
	if len(actions) != 1 || actions[0].Action != "use" {
		t.Fatalf("Actions() = %+v, want single \"use\"", actions)
	}
}

func TestPassthrough_ClassifyActionIsUniform(t *testing.T) {
	a := passthrough.New()
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		req, _ := http.NewRequest(method, "https://api.ipify.org/", nil)
		got, err := a.ClassifyAction(req, credentials.Connection{})
		if err != nil {
			t.Fatalf("ClassifyAction(%s): unexpected error %v", method, err)
		}
		if got.Action != "use" {
			t.Errorf("ClassifyAction(%s) = %q, want \"use\"", method, got.Action)
		}
	}
}

func TestPassthrough_ResolveInjectsNothing(t *testing.T) {
	a := passthrough.New()
	cred, err := a.Resolve(context.Background(), credentials.Connection{Name: "ipify"}, credentials.CallerIdentity{})
	if err != nil {
		t.Fatalf("Resolve: unexpected error %v", err)
	}
	if cred.Header != "" || cred.Value != "" || cred.Token != "" {
		t.Errorf("Resolve returned a non-empty credential %+v, want empty (nothing injected)", cred)
	}
}
