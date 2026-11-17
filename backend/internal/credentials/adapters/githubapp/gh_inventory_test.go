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
// Every node and repository id the fixtures name resolves to example-org/widgets, as the broker's lookup would.
func TestClassify_GHInventory(t *testing.T) {
	resolved := map[string]string{}
	for _, id := range []string{"I_kwDOEXAMPLE0003", "LA_kwDOEXAMPLE0005", "PR_kwDOEXAMPLE0001", "PR_kwDOEXAMPLE0002", "PR_kwDOEXAMPLE0006", "PR_kwDORRWKBs8AAAABFbAsiQ", "R_kwDOEXAMPLE0004", "123456789"} {
		resolved[id] = "example-org/widgets"
	}
	// A push also carries workflows:write when the agent holds it.
	pushCls := credentials.Classification{Action: "contents:write", Optional: []string{"workflows:write"}, Qualifier: "widgets"}
	// A GraphQL document needs any repository category at its operation's level
	// on its target, so a row pins the level and the target.
	graphql := []struct {
		fixture   string
		verb      string
		qualifier string
	}{
		{"issue-comment-1-IssueByNumber", verbRead, "widgets"},
		{"issue-comment-2-CommentCreate", verbWrite, "widgets"},
		{"issue-create-1-IssueRepositoryInfo", verbRead, "widgets"},
		{"issue-create-2-IssueCreate", verbWrite, "widgets"},
		{"issue-edit-add-label-1-IssueByNumber", verbRead, "widgets"},
		{"issue-edit-add-label-2-RepositoryLabelList", verbRead, "widgets"},
		{"issue-edit-add-label-3-LabelAdd", verbWrite, "widgets"},
		{"issue-list-1-IssueList", verbRead, "widgets"},
		{"issue-list-search-1-SearchType_enumValues", verbRead, ""},
		{"issue-list-search-2-anonymous", verbRead, "widgets"},
		{"issue-view-1-IssueByNumber", verbRead, "widgets"},
		{"issue-view-2-IssueProjectItems", verbRead, "widgets"},
		{"issue-view-project-items", verbRead, "widgets"},
		{"issue-view", verbRead, "widgets"},
		{"pr-checkout-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-checks-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-checks-2-PullRequest_fields2", verbRead, ""},
		{"pr-checks-3-PullRequest_fields", verbRead, ""},
		{"pr-checks-4-PullRequestStatusChecks", verbRead, "widgets"},
		{"pr-checks-current-branch-1-PullRequestForBranch", verbRead, "widgets"},
		{"pr-checks-current-branch-2-PullRequest_fields2", verbRead, ""},
		{"pr-checks-current-branch-3-PullRequest_fields", verbRead, ""},
		{"pr-checks-current-branch-4-PullRequestStatusChecks", verbRead, "widgets"},
		{"pr-checks-introspect-pull-request", verbRead, ""},
		{"pr-checks-introspect-workflow-run", verbRead, ""},
		{"pr-checks-status", verbRead, "widgets"},
		{"pr-checks", verbRead, "widgets"},
		{"pr-comment-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-comment-2-CommentCreate", verbWrite, "widgets"},
		{"pr-create-1-RepositoryInfo", verbRead, "widgets"},
		{"pr-create-2-PullRequestForBranch", verbRead, "widgets"},
		{"pr-create-3-PullRequestCreate", verbWrite, "widgets"},
		{"pr-diff-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-diff", verbRead, "widgets"},
		{"pr-edit-add-label-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-edit-add-label-2-PullRequestProjectItems", verbRead, "widgets"},
		{"pr-edit-add-label-3-RepositoryLabelList", verbRead, "widgets"},
		{"pr-edit-add-label-4-LabelAdd", verbWrite, "widgets"},
		{"pr-edit-body-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-edit-body-2-PullRequestProjectItems", verbRead, "widgets"},
		{"pr-edit-body-3-PullRequestUpdate", verbWrite, "widgets"},
		{"pr-edit-title-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-edit-title-2-PullRequestProjectItems", verbRead, "widgets"},
		{"pr-edit-title-3-PullRequestUpdate", verbWrite, "widgets"},
		{"pr-list-1-PullRequestList", verbRead, "widgets"},
		{"pr-list-author-label-1-SearchType_enumValues", verbRead, ""},
		{"pr-list-author-label-2-PullRequestSearch", verbRead, "widgets"},
		{"pr-list-search-1-SearchType_enumValues", verbRead, ""},
		{"pr-list-search-2-PullRequestSearch", verbRead, "widgets"},
		{"pr-list", verbRead, "widgets"},
		{"pr-merge-1-PullRequest_fields2", verbRead, ""},
		{"pr-merge-2-PullRequest_fields", verbRead, ""},
		{"pr-merge-3-PullRequestByNumber", verbRead, "widgets"},
		{"pr-merge-4-PullRequestMerge", verbWrite, "widgets"},
		{"pr-ready-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-ready-2-PullRequestReadyForReview", verbWrite, "widgets"},
		{"pr-review-comment-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-review-comment-2-PullRequestReviewAdd", verbWrite, "widgets"},
		{"pr-status-1-PullRequest_fields", verbRead, ""},
		{"pr-status-2-PullRequest_fields2", verbRead, ""},
		{"pr-status-3-PullRequestStatus", verbRead, "widgets"},
		{"pr-status", verbRead, "widgets"},
		{"pr-view-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-view-2-PullRequestProjectItems", verbRead, "widgets"},
		{"pr-view-current-branch-1-PullRequestForBranch", verbRead, "widgets"},
		{"pr-view-current-branch-2-PullRequestProjectItems", verbRead, "widgets"},
		{"pr-view-current-branch", verbRead, "widgets"},
		{"pr-view-json-1-PullRequestByNumber", verbRead, "widgets"},
		{"pr-view-project-items", verbRead, "widgets"},
		{"pr-view", verbRead, "widgets"},
		{"release-download-1-RepositoryReleaseByTag", verbRead, "widgets"},
		{"release-list-1-Release_fields", verbRead, ""},
		{"release-list-2-RepositoryReleaseList", verbRead, "widgets"},
		{"release-view-1-RepositoryReleaseByTag", verbRead, "widgets"},
		{"repo-clone-1-RepositoryInfo", verbRead, "widgets"},
		{"repo-list-1-RepositoryList", verbRead, ""},
		{"repo-view-1-RepositoryInfo", verbRead, "widgets"},
		{"repo-view", verbRead, "widgets"},
		{"run-view-1-PullRequestForRun", verbRead, "widgets"},
		{"search-issues-1-SearchType_enumValues", verbRead, ""},
		{"search-prs-1-SearchType_enumValues", verbRead, ""},
	}
	rest := []struct {
		command, method, url string
		want                 credentials.Classification
	}{
		{"api-orgs-teams", "GET", "https://api.github.com/orgs/example-org/teams", cls("members:read", "")},
		{"api-paginate-pr-files", "GET", "https://api.github.com/repos/example-org/widgets/pulls/1206/files?per_page=100", cls("pulls:read", "widgets")},
		{"api-paginate-pr-files", "GET", "https://api.github.com/repositories/123456789/pulls/1206/files?per_page=100&page=2", cls("pulls:read", "widgets")},
		{"git-push", "GET", "https://github.com/example-org/widgets.git/info/refs?service=git-receive-pack", pushCls},
		{"git-push", "POST", "https://github.com/example-org/widgets.git/git-receive-pack", pushCls},
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
			got, err := c.(*connector).ClassifyResolved(graphqlPost(t, string(body)), resolved)
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			assertClassification(t, got, graphqlCls(tc.verb, tc.qualifier))
		})
	}
	for _, tc := range rest {
		t.Run(tc.command+" "+tc.method+" "+tc.url, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.url, http.NoBody)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			got, err := c.(*connector).ClassifyResolved(req, resolved)
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
