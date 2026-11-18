package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestClient_NodeRepositories(t *testing.T) {
	tests := []struct {
		name         string
		reply        string
		want         map[string]Node
		wantComplete bool
		wantErr      error
	}{
		{
			name: "repository shapes, a user, a typeless node and an unseen id, with errors",
			reply: `{"data":{"n0":{"__typename":"Repository","name":"Widgets","owner":{"login":"Fabrikam"}},"n1":{"__typename":"PullRequest","repository":{"name":"gadgets","owner":{"login":"contoso"}}},` +
				`"n2":{"__typename":"PullRequestCommit","pullRequest":{"repository":{"name":"w","owner":{"login":"fabrikam"}}}},"n3":{"__typename":"StatusContext","commit":{"repository":{"name":"c","owner":{"login":"fabrikam"}}}},"n4":{"__typename":"User"},"n5":{}},` +
				`"errors":[{"message":"Could not resolve to a node"}]}`,
			want: map[string]Node{
				"R_1": {"Repository", Repository{"fabrikam", "widgets"}}, "PR_1": {"PullRequest", Repository{"contoso", "gadgets"}},
				"PRC_1": {"PullRequestCommit", Repository{"fabrikam", "w"}}, "SC_1": {"StatusContext", Repository{"fabrikam", "c"}}, "U_1": {Type: "User"},
			},
		},
		{
			name:         "no errors is complete",
			reply:        `{"data":{"n0":{"__typename":"Repository","name":"widgets","owner":{"login":"fabrikam"}},"n1":null,"n2":null,"n3":null,"n4":null,"n5":null}}`,
			want:         map[string]Node{"R_1": {"Repository", Repository{"fabrikam", "widgets"}}},
			wantComplete: true,
		},
		{name: "errors without data fail", reply: `{"data":null,"errors":[{"type":"RATE_LIMITED"}]}`, wantErr: ErrGraphQLQuery},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				_, _ = io.WriteString(w, tc.reply)
			}))
			defer srv.Close()
			got, complete, err := NewClient(srv.URL, logging.NewLoggerWithOutput(io.Discard)).NodeRepositories(t.Context(), "tok", []string{"R_1", "PR_1", "PRC_1", "SC_1", "U_1", "nope"})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || len(got) != len(tc.want) || complete != tc.wantComplete {
				t.Fatalf("got %v complete %v, err %v, want %v complete %v", got, complete, err, tc.want, tc.wantComplete)
			}
			for id, node := range tc.want {
				if got[id] != node {
					t.Errorf("node %s = %v, want %v", id, got[id], node)
				}
			}
			if body.Variables["i5"] != "nope" || !strings.Contains(body.Query, "n5: node(id: $i5)") {
				t.Errorf("ids are not passed as variables: %s %v", body.Query, body.Variables)
			}
		})
	}
}

func TestClient_Repositories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/repositories/522":
			_, _ = io.WriteString(w, `{"name":"Widgets","owner":{"login":"Fabrikam"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	got, err := NewClient(srv.URL, logging.NewLoggerWithOutput(io.Discard)).Repositories(t.Context(), "tok", []string{"522", "9"})
	if err != nil || len(got) != 1 || got["522"] != (Repository{"fabrikam", "widgets"}) {
		t.Errorf("got %v, err %v, want 522=fabrikam/widgets", got, err)
	}
}
