package githubapp

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
)

// TestClassify_GHInventory pins how every captured gh request classifies (gh 2.103.3 for
// <command>-<n>-<operation> fixtures, 2.101.0 for the rest), so a classifier change shows as a changed row.
func TestClassify_GHInventory(t *testing.T) {
	prStatusCls := credentials.Classification{Action: "admin:read", AlsoRequired: []string{"ci:read", "contents:read", "pulls:read"}, Qualifier: "widgets"}
	graphql := []struct {
		fixture string
		want    credentials.Classification
	}{
		{"issue-comment-1-IssueByNumber", credentials.Classification{Action: "issues:read", Alternates: []string{"pulls:read"}, Qualifier: "widgets"}},
		{"issue-comment-2-CommentCreate", credentials.Classification{Action: "issues:write", Alternates: []string{"pulls:write"}}},
		{"issue-create-1-IssueRepositoryInfo", cls("metadata:read", "widgets")},
		{"issue-create-2-IssueCreate", cls("issues:write", "")},
		{"issue-edit-add-label-1-IssueByNumber", credentials.Classification{Action: "issues:read", Alternates: []string{"pulls:read"}, Qualifier: "widgets"}},
		{"issue-edit-add-label-2-RepositoryLabelList", cls("issues:read", "widgets")},
		{"issue-edit-add-label-3-LabelAdd", cls("issues:write", "")},
		{"issue-list-1-IssueList", cls("issues:read", "widgets")},
		{"issue-list-search-1-SearchType_enumValues", cls("metadata:read", "")},
		{"issue-list-search-2-anonymous", cls("issues:read", "widgets")},
		{"issue-view-1-IssueByNumber", credentials.Classification{Action: "issues:read", Alternates: []string{"pulls:read"}, Qualifier: "widgets"}},
		{"issue-view-2-IssueProjectItems", cls("issues:read", "widgets")},
		{"issue-view-project-items", cls("issues:read", "widgets")},
		{"issue-view", credentials.Classification{Action: "issues:read", Alternates: []string{"pulls:read"}, Qualifier: "widgets"}},
		{"pr-checkout-1-PullRequestByNumber", pullsCls("pulls:read", "widgets")},
		{"pr-checks-1-PullRequestByNumber", pullsCls("pulls:read", "widgets")},
		{"pr-checks-2-PullRequest_fields2", cls("metadata:read", "")},
		{"pr-checks-3-PullRequest_fields", cls("metadata:read", "")},
		{"pr-checks-4-PullRequestStatusChecks", checksCls("")},
		{"pr-checks-current-branch-1-PullRequestForBranch", credentials.Classification{Action: "contents:read", AlsoRequired: []string{"pulls:read"}, Qualifier: "widgets"}},
		{"pr-checks-current-branch-2-PullRequest_fields2", cls("metadata:read", "")},
		{"pr-checks-current-branch-3-PullRequest_fields", cls("metadata:read", "")},
		{"pr-checks-current-branch-4-PullRequestStatusChecks", checksCls("")},
		{"pr-checks-introspect-pull-request", cls("metadata:read", "")},
		{"pr-checks-introspect-workflow-run", cls("metadata:read", "")},
		{"pr-checks-status", checksCls("")},
		{"pr-checks", pullsCls("pulls:read", "widgets")},
		{"pr-comment-1-PullRequestByNumber", pullsCls("pulls:read", "widgets")},
		{"pr-comment-2-CommentCreate", credentials.Classification{Action: "issues:write", Alternates: []string{"pulls:write"}}},
		{"pr-create-1-RepositoryInfo", cls("contents:read", "widgets")},
		{"pr-create-2-PullRequestForBranch", credentials.Classification{Action: "contents:read", AlsoRequired: []string{"pulls:read"}, Qualifier: "widgets"}},
		{"pr-create-3-PullRequestCreate", pullsCls("pulls:write", "")},
		{"pr-diff-1-PullRequestByNumber", pullsCls("pulls:read", "widgets")},
		{"pr-diff", pullsCls("pulls:read", "widgets")},
		{"pr-edit-add-label-1-PullRequestByNumber", pullsCls("pulls:read", "widgets")},
		{"pr-edit-add-label-2-PullRequestProjectItems", pullsCls("pulls:read", "widgets")},
		{"pr-edit-add-label-3-RepositoryLabelList", cls("issues:read", "widgets")},
		{"pr-edit-add-label-4-LabelAdd", pullsCls("pulls:write", "")},
		{"pr-edit-body-1-PullRequestByNumber", pullsCls("pulls:read", "widgets")},
		{"pr-edit-body-2-PullRequestProjectItems", pullsCls("pulls:read", "widgets")},
		{"pr-edit-body-3-PullRequestUpdate", pullsCls("pulls:write", "")},
		{"pr-edit-title-1-PullRequestByNumber", pullsCls("pulls:read", "widgets")},
		{"pr-edit-title-2-PullRequestProjectItems", pullsCls("pulls:read", "widgets")},
		{"pr-edit-title-3-PullRequestUpdate", pullsCls("pulls:write", "")},
		{"pr-list-1-PullRequestList", pullsCls("pulls:read", "widgets")},
		{"pr-list-author-label-1-SearchType_enumValues", cls("metadata:read", "")},
		{"pr-list-author-label-2-PullRequestSearch", pullsCls("pulls:read", "widgets")},
		{"pr-list-search-1-SearchType_enumValues", cls("metadata:read", "")},
		{"pr-list-search-2-PullRequestSearch", pullsCls("pulls:read", "widgets")},
		{"pr-list", pullsCls("pulls:read", "widgets")},
		{"pr-merge-1-PullRequest_fields2", cls("metadata:read", "")},
		{"pr-merge-2-PullRequest_fields", cls("metadata:read", "")},
		{"pr-merge-3-PullRequestByNumber", pullsCls("pulls:read", "widgets")},
		{"pr-merge-4-PullRequestMerge", cls("contents:write", "")},
		{"pr-ready-1-PullRequestByNumber", pullsCls("pulls:read", "widgets")},
		{"pr-ready-2-PullRequestReadyForReview", pullsCls("pulls:write", "")},
		{"pr-review-comment-1-PullRequestByNumber", pullsCls("pulls:read", "widgets")},
		{"pr-review-comment-2-PullRequestReviewAdd", pullsCls("pulls:write", "")},
		{"pr-status-1-PullRequest_fields", cls("metadata:read", "")},
		{"pr-status-2-PullRequest_fields2", cls("metadata:read", "")},
		{"pr-status-3-PullRequestStatus", prStatusCls},
		{"pr-status", prStatusCls},
		{"pr-view-1-PullRequestByNumber", checksCls("widgets")},
		{"pr-view-2-PullRequestProjectItems", pullsCls("pulls:read", "widgets")},
		{"pr-view-current-branch-1-PullRequestForBranch", credentials.Classification{Action: "ci:read", AlsoRequired: []string{"contents:read", "pulls:read"}, Qualifier: "widgets"}},
		{"pr-view-current-branch-2-PullRequestProjectItems", pullsCls("pulls:read", "widgets")},
		{"pr-view-current-branch", credentials.Classification{Action: "ci:read", AlsoRequired: []string{"contents:read", "pulls:read"}, Qualifier: "widgets"}},
		{"pr-view-json-1-PullRequestByNumber", checksCls("widgets")},
		{"pr-view-project-items", pullsCls("pulls:read", "widgets")},
		{"pr-view", checksCls("widgets")},
		{"release-download-1-RepositoryReleaseByTag", cls("contents:read", "widgets")},
		{"release-list-1-Release_fields", cls("metadata:read", "")},
		{"release-list-2-RepositoryReleaseList", cls("contents:read", "widgets")},
		{"release-view-1-RepositoryReleaseByTag", cls("contents:read", "widgets")},
		{"repo-clone-1-RepositoryInfo", cls("contents:read", "widgets")},
		{"repo-list-1-RepositoryList", cls("metadata:read", "")},
		{"repo-view-1-RepositoryInfo", cls("metadata:read", "widgets")},
		{"repo-view", cls("metadata:read", "widgets")},
		{"run-view-1-PullRequestForRun", pullsCls("pulls:read", "widgets")},
		{"search-issues-1-SearchType_enumValues", cls("metadata:read", "")},
		{"search-prs-1-SearchType_enumValues", cls("metadata:read", "")},
	}
	rest := []struct {
		command, method, url string
		want                 credentials.Classification
	}{
		{"api-orgs-teams", "GET", "https://api.github.com/orgs/example-org/teams", cls("members:read", "")},
		{"api-paginate-pr-files", "GET", "https://api.github.com/repos/example-org/widgets/pulls/1206/files?per_page=100", cls("pulls:read", "widgets")},
		{"api-paginate-pr-files", "GET", "https://api.github.com/repositories/123456789/pulls/1206/files?per_page=100&page=2", cls("repo:read", "")},
		{"git-push", "GET", "https://github.com/example-org/widgets.git/info/refs?service=git-receive-pack", cls("contents:write", "widgets")},
		{"git-push", "POST", "https://github.com/example-org/widgets.git/git-receive-pack", cls("contents:write", "widgets")},
		{"pr-checkout", "GET", "https://github.com/example-org/widgets.git/info/refs?service=git-upload-pack", cls("contents:read", "widgets")},
		{"pr-checkout", "POST", "https://github.com/example-org/widgets.git/git-upload-pack", cls("contents:read", "widgets")},
		{"pr-diff", "GET", "https://api.github.com/repos/example-org/widgets/pulls/1313", credentials.Classification{Action: "pulls:read", Alternates: []string{"contents:read"}, Qualifier: "widgets"}},
		{"release-download", "GET", "https://api.github.com/repos/example-org/widgets/releases/tags/v1.17.0", cls("contents:read", "widgets")},
		{"release-download", "GET", "https://api.github.com/repos/example-org/widgets/tarball/v1.17.0", cls("contents:read", "widgets")},
		{"release-download", "GET", "https://codeload.github.com/example-org/widgets/tar.gz/refs/tags/v1.17.0", cls("contents:read", "widgets")},
		{"repo-view", "GET", "https://api.github.com/repos/example-org/widgets/readme", cls("contents:read", "widgets")},
		{"run-list", "GET", "https://api.github.com/repos/example-org/widgets/actions/runs?exclude_pull_requests=true&per_page=3", cls("ci:read", "widgets")},
		{"run-list", "GET", "https://api.github.com/repos/example-org/widgets/actions/workflows?page=1&per_page=100", cls("ci:read", "widgets")},
		{"run-view", "GET", "https://api.github.com/repos/example-org/widgets/actions/runs/1001?exclude_pull_requests=true", cls("ci:read", "widgets")},
		{"run-view", "GET", "https://api.github.com/repos/example-org/widgets/actions/workflows/2002", cls("ci:read", "widgets")},
		{"run-view", "GET", "https://api.github.com/repos/example-org/widgets/actions/runs/1001/jobs?per_page=100", cls("ci:read", "widgets")},
		{"run-view", "GET", "https://api.github.com/repos/example-org/widgets/actions/runs/1001/artifacts?per_page=100", cls("ci:read", "widgets")},
		{"run-view", "GET", "https://api.github.com/repos/example-org/widgets/check-runs/3003/annotations", cls("ci:read", "widgets")},
		{"search-code", "GET", "https://api.github.com/search/code?page=1&per_page=3&q=kustomization+repo%3Aexample-org%2Fwidgets", cls("contents:read", "widgets")},
		{"search-issues", "GET", "https://api.github.com/search/issues?advanced_search=true&page=1&per_page=3&q=%28+deploy+%29+repo%3Aexample-org%2Fwidgets+type%3Aissue", credentials.Classification{Action: "issues:read", Alternates: []string{"pulls:read"}, Qualifier: "widgets"}},
		{"search-prs", "GET", "https://api.github.com/search/issues?advanced_search=true&page=1&per_page=3&q=%28+traefik+%29+repo%3Aexample-org%2Fwidgets+type%3Apr", credentials.Classification{Action: "issues:read", Alternates: []string{"pulls:read"}, Qualifier: "widgets"}},
	}

	c := graphqlConnector(t)
	covered := map[string]bool{}
	for _, tc := range graphql {
		covered[tc.fixture] = true
		t.Run(tc.fixture, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", "gh", tc.fixture+".json"))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			got, err := c.Classify(graphqlPost(t, string(body)))
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			assertClassification(t, got, tc.want)
		})
	}
	for _, tc := range rest {
		t.Run(tc.command+" "+tc.method+" "+tc.url, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.url, http.NoBody)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			got, err := c.Classify(req)
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			assertClassification(t, got, tc.want)
		})
	}

	// A fixture added without a row would go unasserted.
	files, err := filepath.Glob(filepath.Join("testdata", "gh", "*.json"))
	if err != nil {
		t.Fatalf("list fixtures: %v", err)
	}
	for _, f := range files {
		if name := strings.TrimSuffix(filepath.Base(f), ".json"); !covered[name] {
			t.Errorf("fixture %s has no inventory row", name)
		}
	}
}
