package declared

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

func newClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	tok := func(context.Context) (string, error) { return "tok", nil }
	return NewClient(srv.URL+"/api/v1", tok, logging.NewLoggerWithOutput(io.Discard))
}

func TestApply(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/internal/declared/triggers" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("request %s %s auth %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var item declarative.Item
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil || item.Name != "t1" || string(item.Secret) != "s" {
			t.Errorf("item %+v (%v)", item, err)
		}
		_, _ = w.Write([]byte(`{"outcome":"Projected","id":"x","ready":true}`))
	})
	res, err := c.Apply(context.Background(), declarative.KindTriggers, declarative.Item{TenantID: "acme", Name: "t1", UID: "u", Spec: json.RawMessage(`{}`), Secret: []byte("s")})
	if err != nil || res != (declarative.Result{Outcome: declarative.OutcomeProjected, ID: "x", Ready: true}) {
		t.Errorf("Apply = %+v, %v", res, err)
	}
}

func TestDeleteAndList(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "DELETE /api/v1/internal/declared/agents/acme/a1":
			w.WriteHeader(http.StatusNoContent)
		case "GET /api/v1/internal/declared/agents/acme":
			_, _ = w.Write([]byte(`{"items":[{"name":"a1","uid":"u1"}]}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	if err := c.Delete(context.Background(), declarative.KindAgents, "acme", "a1"); err != nil {
		t.Errorf("Delete: %v", err)
	}
	items, err := c.List(context.Background(), declarative.KindAgents, "acme")
	if err != nil || len(items) != 1 || items[0] != (declarative.Declared{Name: "a1", UID: "u1"}) {
		t.Errorf("List = %+v, %v", items, err)
	}
}

func TestErrors(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	if err := c.Delete(context.Background(), declarative.KindAgents, "acme", "a1"); !errors.Is(err, ErrUnexpectedStatus) {
		t.Errorf("err = %v, want ErrUnexpectedStatus", err)
	}

	tokenErr := errors.New("no svid")
	c.token = func(context.Context) (string, error) { return "", tokenErr }
	if _, err := c.List(context.Background(), declarative.KindAgents, "acme"); !errors.Is(err, tokenErr) {
		t.Errorf("err = %v, want the token error", err)
	}
}
