package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestClient_NodeOwners(t *testing.T) {
	tests := []struct {
		name    string
		reply   string
		want    map[string]string
		wantErr error
	}{
		{
			name:  "repository shapes, a user and an unseen id",
			reply: `{"data":{"n0":{"owner":{"login":"RunKora"}},"n1":{"repository":{"owner":{"login":"clear-route"}}},"n2":{"login":"octocat"},"n3":null},"errors":[{"message":"Could not resolve to a node"}]}`,
			want:  map[string]string{"R_1": "runkora", "PR_1": "clear-route"},
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
			got, err := NewClient(srv.URL, logging.NewLoggerWithOutput(io.Discard)).NodeOwners(t.Context(), "tok", []string{"R_1", "PR_1", "U_1", "nope"})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || len(got) != len(tc.want) {
				t.Fatalf("got %v, err %v, want %v", got, err, tc.want)
			}
			for id, owner := range tc.want {
				if got[id] != owner {
					t.Errorf("owner of %s = %q, want %q", id, got[id], owner)
				}
			}
			if body.Variables["i3"] != "nope" || !strings.Contains(body.Query, "n3: node(id: $i3)") {
				t.Errorf("ids are not passed as variables: %s %v", body.Query, body.Variables)
			}
		})
	}
}
