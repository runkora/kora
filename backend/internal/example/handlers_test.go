package example

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/api"
)

// newTestServer wires a Controller over a fake repo and mounts it on a real
// api.Mux, so tests exercise the actual routing (path params included).
func newTestServer(t *testing.T) (http.Handler, *fakeRepo) {
	t.Helper()
	repo := newFakeRepo()
	c := NewController(silentLogger(), repo)

	mux := api.NewMux(context.Background(), silentLogger())
	mux.SetBasePath(c.GroupedBy())
	c.Boot(mux)
	return mux, repo
}

func TestHandleHello(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/example/hello/orbit", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["message"] != "Hello, orbit!" {
		t.Errorf("message = %q, want %q", body["message"], "Hello, orbit!")
	}
}

func TestHandleCreateUser(t *testing.T) {
	srv, _ := newTestServer(t)
	raw, _ := json.Marshal(CreateUserRequest{ID: "u1", Email: "u1@example.com"})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/example/users", bytes.NewReader(raw)))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", w.Code, w.Body.String())
	}
	var resp UserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != "u1" || resp.Email != "u1@example.com" || resp.CreatedAt.IsZero() {
		t.Errorf("resp = %+v, want id/email set and CreatedAt stamped", resp)
	}
}

func TestHandleCreateUser_ValidationError(t *testing.T) {
	srv, _ := newTestServer(t)
	raw, _ := json.Marshal(CreateUserRequest{Email: "u@example.com"}) // no id
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/example/users", bytes.NewReader(raw)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", w.Code, w.Body.String())
	}
	var apiErr api.APIError
	if err := json.Unmarshal(w.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if apiErr.Code != api.ErrCodeMissingField {
		t.Errorf("code = %q, want %q", apiErr.Code, api.ErrCodeMissingField)
	}
}

func TestHandleCreateUser_Conflict(t *testing.T) {
	srv, repo := newTestServer(t)
	repo.users["u1"] = &User{ID: "u1", Email: "u1@example.com"}

	raw, _ := json.Marshal(CreateUserRequest{ID: "u1", Email: "u1@example.com"})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/example/users", bytes.NewReader(raw)))

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", w.Code, w.Body.String())
	}
}

func TestHandleGetUser_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/example/users/missing", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", w.Code, w.Body.String())
	}
	var apiErr api.APIError
	if err := json.Unmarshal(w.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if apiErr.Code != api.ErrCodeResourceNotFound {
		t.Errorf("code = %q, want %q", apiErr.Code, api.ErrCodeResourceNotFound)
	}
}
