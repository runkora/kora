package policy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func newTestClient(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, func(context.Context) (string, error) { return "tok", nil }, logging.NewLoggerWithOutput(io.Discard))
}

func TestCreateGrant(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantID     string
		wantVocab  bool
		wantErrSub string
	}{
		{
			name:   "created",
			status: http.StatusCreated,
			body:   `{"id":"g-1"}`,
			wantID: "g-1",
		},
		{
			name:       "scope outside the vocabulary",
			status:     http.StatusBadRequest,
			body:       `{"code":"invalid_field","message":"scope 'inference:liquid:missing:use' is not in the scope vocabulary","details":{"scope":"inference:liquid:missing:use"}}`,
			wantVocab:  true,
			wantErrSub: "inference:liquid:missing:use",
		},
		{
			name:       "other bad request",
			status:     http.StatusBadRequest,
			body:       `{"code":"invalid_field","message":"invalid principal type"}`,
			wantErrSub: "invalid principal type",
		},
		{
			name:       "server error",
			status:     http.StatusInternalServerError,
			body:       `{"code":"database_error"}`,
			wantErrSub: "status 500",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, tt.status, tt.body)
			id, err := c.CreateGrant(context.Background(), "tenant", "SERVICE", "trigger-source:s1", []string{"inference:invoke"}, nil, false)
			if tt.wantID != "" {
				if err != nil || id != tt.wantID {
					t.Fatalf("CreateGrant = %q, %v; want %q", id, err, tt.wantID)
				}
				return
			}
			if err == nil {
				t.Fatal("CreateGrant succeeded, want an error")
			}
			if got := errors.Is(err, ErrScopeNotInVocabulary); got != tt.wantVocab {
				t.Errorf("errors.Is(err, ErrScopeNotInVocabulary) = %v, want %v (err: %v)", got, tt.wantVocab, err)
			}
			var vocab *ScopeNotInVocabularyError
			if tt.wantVocab && (!errors.As(err, &vocab) || vocab.Scope != "inference:liquid:missing:use") {
				t.Errorf("err = %#v, want a ScopeNotInVocabularyError naming the scope", err)
			}
			if !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Errorf("err = %v, want it to contain %q", err, tt.wantErrSub)
			}
		})
	}
}
