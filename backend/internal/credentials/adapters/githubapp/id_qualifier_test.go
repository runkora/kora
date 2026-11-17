package githubapp

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
)

// A request addressing objects by node id is qualified by their repository only
// when every id resolves to one repository of the connection's owner. See
// docs/adr/2026-11-15-github-connector-route-coverage.md, item 16.
func TestClassifyResolved_NodeIDs(t *testing.T) {
	resolved := map[string]string{
		"PR_w": "example-org/widgets", "I_w": "example-org/widgets", "LA_w": "example-org/widgets",
		"PR_g": "example-org/gadgets", "PR_foreign": "contoso/widgets", "R_w": "example-org/widgets",
		"U_1": "User", "BOT_1": "Bot", "T_1": "Team", "M_1": "Mannequin", "PVT_1": "ProjectV2",
	}
	update := `{"query":"mutation($in: UpdatePullRequestInput!) { updatePullRequest(input: $in) { clientMutationId } }","variables":{"in":{"pullRequestId":"%s"}}}`
	tests := []struct {
		name      string
		body      string
		verb      string
		qualifier string
	}{
		{"one resolved id", fmt.Sprintf(update, "PR_w"), verbWrite, "widgets"},
		{"an unresolved id stays unqualified", fmt.Sprintf(update, "PR_unknown"), verbWrite, ""},
		// A foreign id is not a repository of the owner, so it is unqualified rather than denied.
		{"a foreign-owner id stays unqualified", fmt.Sprintf(update, "PR_foreign"), verbWrite, ""},
		{"ids across two repositories stay unqualified", `{"query":"{ nodes(ids: [\"PR_w\", \"PR_g\"]) { ... on PullRequest { title } } }"}`, verbRead, ""},
		{"an unresolved id beside a resolved one stays unqualified", `{"query":"mutation { addLabelsToLabelable(input: {labelableId: \"I_w\", labelIds: [\"LA_w\", \"LA_x\"]}) { clientMutationId } }"}`, verbWrite, ""},
		{"label ids in the same repository", `{"query":"mutation { addLabelsToLabelable(input: {labelableId: \"I_w\", labelIds: [\"LA_w\"]}) { clientMutationId } }"}`, verbWrite, "widgets"},
		{"node id agreeing with a repository() call", `{"query":"{ repository(owner: \"example-org\", name: \"widgets\") { name } node(id: \"PR_w\") { id } }"}`, verbRead, "widgets"},
		{"node id disagreeing with a repository() call", `{"query":"{ repository(owner: \"example-org\", name: \"gadgets\") { name } node(id: \"PR_w\") { id } }"}`, verbRead, ""},
		{"a node id from an unsupplied variable stays unqualified", `{"query":"query($id: ID!) { node(id: $id) { ... on PullRequest { title } } }"}`, verbRead, ""},
		{"an empty nodes list stays unqualified", `{"query":"{ nodes(ids: []) { id } }"}`, verbRead, ""},
		// Account ids address no repository, so the repository-addressed ids alone qualify the request.
		{"reviewers by user id", `{"query":"mutation { requestReviews(input: {pullRequestId: \"PR_w\", userIds: [\"U_1\", \"BOT_1\", \"M_1\"]}) { clientMutationId } }"}`, verbWrite, "widgets"},
		{"reviewers by team id", `{"query":"mutation { requestReviews(input: {pullRequestId: \"PR_w\", teamIds: [\"T_1\"]}) { clientMutationId } }"}`, verbWrite, "widgets"},
		{"issue created with an assignee", `{"query":"mutation { createIssue(input: {repositoryId: \"R_w\", title: \"t\", assigneeIds: [\"U_1\"]}) { clientMutationId } }"}`, verbWrite, "widgets"},
		{"a project id stays unqualified", `{"query":"mutation { updatePullRequest(input: {pullRequestId: \"PR_w\", projectIds: [\"PVT_1\"]}) { clientMutationId } }"}`, verbWrite, ""},
		{"a user id alone stays unqualified", `{"query":"{ node(id: \"U_1\") { id } }"}`, verbRead, ""},
	}
	c := graphqlConnector(t).(*connector)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.ClassifyResolved(graphqlPost(t, tc.body), resolved)
			if err != nil {
				t.Fatalf("ClassifyResolved: %v", err)
			}
			assertClassification(t, got, graphqlCls(tc.verb, tc.qualifier))
		})
	}
}

// A /repositories/{id} path classifies as the /repos path its id resolves to,
// and a foreign owner is denied as that /repos path is.
func TestClassifyResolved_RepositoryIDPath(t *testing.T) {
	resolved := map[string]string{"1": "example-org/widgets", "2": "contoso/widgets"}
	tests := []struct {
		method, url string
		want        credentials.Classification
		wantErr     error
	}{
		{http.MethodGet, "https://api.github.com/repositories/1/pulls/7/files?page=2", readCls("pulls:read", "widgets"), nil},
		{http.MethodGet, "https://api.github.com/repositories/1", cls("metadata:read", "widgets"), nil},
		{http.MethodPost, "https://api.github.com/repositories/1/issues/3/comments", credentials.Classification{Action: "issues:write", Alternates: []string{"pulls:write"}, Qualifier: "widgets"}, nil},
		{http.MethodGet, "https://ghe.example.com/api/v3/repositories/1/contents/README.md", readCls("contents:read", "widgets"), nil},
		{http.MethodGet, "https://api.github.com/repositories/2/pulls", credentials.Classification{}, credentials.ErrUnsupportedAction},
		{http.MethodGet, "https://api.github.com/repositories/3/pulls", credentials.Classification{}, credentials.ErrUnsupportedAction},
	}
	c := graphqlConnector(t).(*connector)
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.url, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.url, http.NoBody)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			got, err := c.ClassifyResolved(req, resolved)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			assertClassification(t, got, tc.want)
		})
	}
}
