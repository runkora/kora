package passthrough_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/passthrough"
)

func open(t *testing.T) credentials.Connector {
	t.Helper()
	c, err := passthrough.New().Open(credentials.Connection{Name: "ipify"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return c
}

func TestPassthrough_TypeAndActions(t *testing.T) {
	a := passthrough.New()
	if a.Type() != credentials.AdapterPassthrough {
		t.Errorf("Type() = %q, want %q", a.Type(), credentials.AdapterPassthrough)
	}
	actions := a.Actions()
	if len(actions) != 1 || actions[0].Action != "use" {
		t.Fatalf("Actions() = %+v, want a single \"use\"", actions)
	}
}

func TestPassthrough_ClassifyIsUniform(t *testing.T) {
	c := open(t)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		req, err := http.NewRequest(method, "https://api.ipify.org/", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		got, err := c.Classify(req)
		if err != nil {
			t.Fatalf("Classify(%s): %v", method, err)
		}
		if got.Action != "use" {
			t.Errorf("Classify(%s) = %q, want \"use\"", method, got.Action)
		}
	}
}

func TestPassthrough_MintInjectsNothing(t *testing.T) {
	cred, err := open(t).Mint(context.Background(), credentials.CallerIdentity{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if cred.Header != "" || cred.Value != "" || cred.Token != "" {
		t.Errorf("Mint returned %+v, want an empty credential", cred)
	}
}
