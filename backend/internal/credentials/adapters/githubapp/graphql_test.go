package githubapp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// graphqlConnector opens a connection whose installation belongs to
// acme. GraphQL classification makes no GitHub calls.
func graphqlConnector(t *testing.T) credentials.Connector {
	t.Helper()
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	conn := credentials.Connection{
		ID: "conn-1", TenantID: "example", Name: "github", SecretRef: "ref", Adapter: credentials.AdapterGitHubApp,
		Config: mustConfig(t, config{AppID: "1", InstallationID: 1, Owner: "acme"}),
	}
	return open(t, a, conn)
}

func graphqlPost(t *testing.T, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://api.github.com/graphql", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return req
}

// grant models an agent's connector attachment the way the policy service
// evaluates it: each action is held unqualified (nil) or for listed
// repositories, and an unqualified hold covers every repository.
type grant map[string][]string

func (g grant) holds(action, qualifier string) bool {
	repos, ok := g[action]
	if !ok {
		return false
	}
	return repos == nil || (qualifier != "" && slices.Contains(repos, qualifier))
}

// allows mirrors the broker: the action or one alternate, and every
// AlsoRequired action.
func (g grant) allows(cls credentials.Classification) bool {
	any := g.holds(cls.Action, cls.Qualifier)
	for _, alt := range cls.Alternates {
		any = any || g.holds(alt, cls.Qualifier)
	}
	for _, req := range cls.AlsoRequired {
		if !g.holds(req, cls.Qualifier) {
			return false
		}
	}
	return any
}

var (
	widgetsOnly = []string{"widgets"}

	// codeReviewer is the code-reviewer agent's attachment: granular actions
	// on one repository, plus the unqualified metadata baseline.
	codeReviewer = grant{
		"metadata:read": nil, "contents:read": widgetsOnly, "pulls:read": widgetsOnly,
		"pulls:write": widgetsOnly, "issues:read": widgetsOnly, "issues:write": widgetsOnly,
		"checks:read": widgetsOnly, "actions:read": widgetsOnly,
	}
	// codeReviewerAllRepos is the same attachment without a repository
	// qualifier, which also covers unqualified requests.
	codeReviewerAllRepos = grant{
		"metadata:read": nil, "contents:read": nil, "pulls:read": nil,
		"pulls:write": nil, "issues:read": nil, "issues:write": nil, "checks:read": nil, "actions:read": nil,
	}
	issuesOnly = grant{"metadata:read": nil, "issues:read": widgetsOnly, "issues:write": widgetsOnly}
)

// TestClassifyGraphQL_GHCommands replays the GraphQL documents the gh CLI
// sends (testdata/gh, captured with GH_DEBUG=api from gh 2.101.0; the
// mutations are reconstructed from the gh source) against a code-reviewer
// grant on one repository, the same grant on every repository, and an
// issues-only grant. Requests addressed by node ID are unqualified, so only the
// unqualified grant covers them.
func TestClassifyGraphQL_GHCommands(t *testing.T) {
	cases := []struct {
		fixture      string
		want         credentials.Classification
		reviewerOK   bool
		allReposOK   bool
		issuesOnlyOK bool
	}{
		{"pr-view", checksCls("widgets"), true, true, false},
		{"pr-view-project-items", pullsCls("pulls:read", "widgets"), true, true, false},
		{"pr-view-current-branch", credentials.Classification{Action: "actions:read", AlsoRequired: []string{"checks:read", "contents:read", "pulls:read"}, Qualifier: "widgets"}, true, true, false},
		{"pr-diff", pullsCls("pulls:read", "widgets"), true, true, false},
		{"pr-checks", pullsCls("pulls:read", "widgets"), true, true, false},
		{"pr-checks-introspect-pull-request", cls("metadata:read", ""), true, true, true},
		{"pr-checks-introspect-workflow-run", cls("metadata:read", ""), true, true, true},
		{"pr-checks-status", checksCls(""), false, true, false},
		{"pr-list", pullsCls("pulls:read", "widgets"), true, true, false},
		{"issue-view", credentials.Classification{Action: "issues:read", Alternates: []string{"pulls:read"}, Qualifier: "widgets"}, true, true, true},
		{"issue-view-project-items", cls("issues:read", "widgets"), true, true, true},
		{"repo-view", cls("metadata:read", "widgets"), true, true, true},
		// search has no category to check against, so it keeps the umbrella.
		{"pr-status", cls("repo:read", "widgets"), false, false, false},
		{"pr-comment", credentials.Classification{Action: "issues:write", Alternates: []string{"pulls:write"}}, false, true, false},
		{"pr-review", pullsCls("pulls:write", ""), false, true, false},
		{"pr-merge", cls("contents:write", ""), false, false, false},
		{"pr-create", pullsCls("pulls:write", ""), false, true, false},
		{"issue-create", cls("issues:write", ""), false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", "gh", tc.fixture+".json"))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			c := graphqlConnector(t)
			got, err := c.Classify(graphqlPost(t, string(body)))
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			assertClassification(t, got, tc.want)
			if ok := codeReviewer.allows(got); ok != tc.reviewerOK {
				t.Errorf("code-reviewer allowed = %v, want %v", ok, tc.reviewerOK)
			}
			if ok := codeReviewerAllRepos.allows(got); ok != tc.allReposOK {
				t.Errorf("code-reviewer on all repositories allowed = %v, want %v", ok, tc.allReposOK)
			}
			if ok := issuesOnly.allows(got); ok != tc.issuesOnlyOK {
				t.Errorf("issues-only allowed = %v, want %v", ok, tc.issuesOnlyOK)
			}
		})
	}
}

func cls(action, qualifier string) credentials.Classification {
	return credentials.Classification{Action: action, Qualifier: qualifier}
}

// pullsCls is a pull request classification, which also requires the
// contents:read its token carries.
func pullsCls(action, qualifier string) credentials.Classification {
	return credentials.Classification{Action: action, AlsoRequired: []string{"contents:read"}, Qualifier: qualifier}
}

// checksCls is a pull request classification that also selects the checks
// rollup and its workflow runs.
func checksCls(qualifier string) credentials.Classification {
	return credentials.Classification{Action: "actions:read", AlsoRequired: []string{"checks:read", "pulls:read", "contents:read"}, Qualifier: qualifier}
}

func assertClassification(t *testing.T, got, want credentials.Classification) {
	t.Helper()
	if got.Action != want.Action || got.Qualifier != want.Qualifier ||
		!slices.Equal(got.Alternates, want.Alternates) || !slices.Equal(got.AlsoRequired, want.AlsoRequired) {
		t.Errorf("classification = %+v, want %+v", got, want)
	}
}

func TestClassifyGraphQL(t *testing.T) {
	q := func(query string, vars map[string]any) string {
		b, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
		return string(b)
	}
	const repoWidgets = `repository(owner:"acme", name:"widgets")`

	cases := []struct {
		name   string
		body   string
		want   credentials.Classification
		denied bool
	}{
		{name: "repository scalars need metadata", body: q(`{ `+repoWidgets+` { id nameWithOwner } }`, nil), want: cls("metadata:read", "widgets")},
		{name: "fields of two categories need both", body: q(`{ `+repoWidgets+` { issues(first:1){totalCount} pullRequests(first:1){totalCount} } }`, nil),
			want: credentials.Classification{Action: "issues:read", AlsoRequired: []string{"pulls:read", "contents:read"}, Qualifier: "widgets"}},
		{name: "ambiguous field satisfied by a strict one", body: q(`{ `+repoWidgets+` { issue(number:1){id} issueOrPullRequest(number:2){__typename} } }`, nil), want: cls("issues:read", "widgets")},
		{name: "ambiguous field alongside another category", body: q(`{ `+repoWidgets+` { object(expression:"HEAD"){id} issueOrPullRequest(number:2){__typename} } }`, nil),
			want: credentials.Classification{Action: "issues:read", Alternates: []string{"pulls:read"}, AlsoRequired: []string{"contents:read"}, Qualifier: "widgets"}},
		{name: "unknown repository field needs the umbrella", body: q(`{ `+repoWidgets+` { pullRequest(number:1){id} deployments(first:1){totalCount} } }`, nil), want: cls("repo:read", "widgets")},
		{name: "unknown root field needs the umbrella", body: q(`{ search(query:"x", type:ISSUE, first:1){issueCount} }`, nil), want: cls("repo:read", "")},
		{name: "field names are case sensitive", body: q(`{ REPOSITORY(OWNER:"evil", NAME:"widgets") { id } }`, nil), want: cls("repo:read", "")},
		{name: "labelable mutation needs the umbrella", body: q(`mutation { addLabelsToLabelable(input:{labelableId:"D_kwDORRWKBs4AAAAB", labelIds:[]}){clientMutationId} }`, nil), want: cls("repo:write", "")},
		{name: "unknown mutation needs the umbrella", body: q(`mutation { deleteIssue(input:{issueId:"x"}){clientMutationId} }`, nil), want: cls("repo:write", "")},
		{name: "viewer is metadata", body: q(`{ viewer { login } }`, nil), want: cls("metadata:read", "")},
		{name: "node without a type condition is metadata", body: q(`{ node(id:"PR_kwDORRWKBs8AAAABFbAsiQ") { id __typename } }`, nil), want: cls("metadata:read", "")},
		{name: "node of an unmapped type needs the umbrella", body: q(`{ node(id:"PR_kwDORRWKBs8AAAABFbAsiQ") { ... on Discussion { title } } }`, nil), want: cls("repo:read", "")},
		{name: "node repository walks repository fields", body: q(`{ node(id:"R_kgDORRWKBg") { ... on Repository { pullRequests(first:1){totalCount} } } }`, nil), want: pullsCls("pulls:read", "")},
		{name: "nodes by fragment spread", body: q(`query { nodes(ids:["I_kwDORRWKBs8AAAABTb6V6w"]) { ...F } } fragment F on Issue { title }`, nil), want: cls("issues:read", "")},
		{name: "variables resolve owner and name", body: q(`query($o:String!,$n:String!){ repository(owner:$o,name:$n){ issues(first:1){totalCount} } }`, map[string]any{"o": "Acme", "n": "Widgets"}), want: cls("issues:read", "widgets")},
		{name: "two repositories are unqualified", body: q(`{ a: `+repoWidgets+` { id } b: repository(owner:"acme", name:"tools") { id } }`, nil), want: cls("metadata:read", "")},
		{name: "repository without a string owner is unqualified", body: q(`query($o:String){ repository(owner:$o, name:"widgets") { id } }`, nil), want: cls("metadata:read", "")},
		{name: "mutation by node ID is unqualified", body: q(`mutation { requestReviews(input:{pullRequestId:"PR_kwDORRWKBs8AAAABFbAsiQ", userIds:["U_kgDOBlrQ6Q"], teamIds:["T_kwDOBlrQ6c4AvZ86"]}){clientMutationId} }`, nil), want: pullsCls("pulls:write", "")},
		{name: "named operation is selected", body: `{"query":"query A { viewer { login } } mutation B { mergePullRequest(input:{pullRequestId:\"PR_kwDORRWKBs8AAAABFbAsiQ\"}){clientMutationId} }","operationName":"B"}`, want: cls("contents:write", "")},

		{name: "foreign owner is denied", body: q(`{ repository(owner:"evil", name:"widgets") { id } }`, nil), denied: true},
		{name: "foreign owner in a second alias is denied", body: q(`{ a: `+repoWidgets+` { id } b: repository(owner:"evil", name:"widgets") { id } }`, nil), denied: true},
		{name: "foreign owner in a nested repository is denied", body: q(`{ viewer { repository(owner:"evil", name:"x") { id } } }`, nil), denied: true},
		{name: "foreign owner hidden in a variable default is denied", body: q(`query($o:String="evil"){ repository(owner:$o, name:"widgets") { id } }`, nil), denied: true},
		{name: "foreign owner inside a fragment is denied", body: q(`query { ...F } fragment F on Query { repository(owner:"evil", name:"widgets") { id } }`, nil), denied: true},

		{name: "fragment spread into two contexts is classified in both", body: q(`query { node(id:"RE_kwDORRWKBs4Xqs06"){ ...F } `+repoWidgets+` { ...F } } fragment F on Node { ... on Release { name description } }`, nil), want: cls("contents:read", "")},
		{name: "fragment spread into two contexts, reversed", body: q(`query { `+repoWidgets+` { ...F } node(id:"RE_kwDORRWKBs4Xqs06"){ ...F } } fragment F on Node { ... on Release { name description } }`, nil), want: cls("contents:read", "")},
		{name: "case variant envelope key fails closed", body: `{"query":"{ repository(owner:\"evil\",name:\"x\"){ id } }","QUERY":"{ viewer { login } }"}`, want: cls("repo:write", "")},
		{name: "case variant operationName fails closed", body: `{"query":"query B { viewer { login } } mutation A { mergePullRequest(input:{}){clientMutationId} }","operationName":"A","OperationName":"B"}`, want: cls("repo:write", "")},

		{name: "no body fails closed", body: "", want: cls("repo:write", "")},
		{name: "malformed JSON fails closed", body: "not json", want: cls("repo:write", "")},
		{name: "empty query fails closed", body: `{"query":""}`, want: cls("repo:write", "")},
		{name: "unparseable document fails closed", body: q(`{ repository(owner:"acme" { id }`, nil), want: cls("repo:write", "")},
		{name: "two anonymous operations fail closed", body: q(`query A { viewer { login } } query B { viewer { login } }`, nil), want: cls("repo:write", "")},
		{name: "unknown operation name fails closed", body: `{"query":"query A { viewer { login } }","operationName":"Z"}`, want: cls("repo:write", "")},
		{name: "subscription fails closed", body: q(`subscription { viewer { login } }`, nil), want: cls("repo:write", "")},
		{name: "undefined fragment needs the unqualified umbrella", body: q(`{ `+repoWidgets+` { ...Missing } }`, nil), want: cls("repo:read", "")},
		{name: "fragment cycle needs the unqualified umbrella", body: q(`query { `+repoWidgets+` { ...A } } fragment A on Repository { id ...B } fragment B on Repository { name ...A }`, nil), want: cls("repo:read", "")},
		{name: "token limit fails closed", body: q(`{ viewer { `+strings.Repeat("login ", graphqlTokenLimit)+`} }`, nil), want: cls("repo:write", "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := graphqlConnector(t)
			req := graphqlPost(t, tc.body)
			if tc.body == "" {
				req.Body = http.NoBody
			}
			got, err := c.Classify(req)
			if tc.denied {
				if !errors.Is(err, credentials.ErrUnsupportedAction) {
					t.Fatalf("want ErrUnsupportedAction, got err=%v cls=%+v", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			assertClassification(t, got, tc.want)
		})
	}
}

// A connection without a declared owner never qualifies GraphQL, so it never
// needs to list the installation's repositories.
func TestClassifyGraphQL_NoDeclaredOwner(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	c := open(t, a, validConn(t))
	got, err := c.Classify(graphqlPost(t, `{"query":"{ node(id:\"PR_kwDORRWKBs8AAAABFbAsiQ\") { ... on PullRequest { title } } }"}`))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	assertClassification(t, got, pullsCls("pulls:read", ""))
}

// TestClassifyGraphQL_BodyPreservation verifies that Classify leaves the full
// original body readable for forwarding.
func TestClassifyGraphQL_BodyPreservation(t *testing.T) {
	body := `{"query":"query { repository(owner:\"acme\", name:\"widgets\") { id } }"}`
	c := graphqlConnector(t)
	req := graphqlPost(t, body)
	if _, err := c.Classify(req); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	remaining, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body after Classify: %v", err)
	}
	if string(remaining) != body {
		t.Errorf("body after Classify = %q, want %q", remaining, body)
	}
	replay, err := req.GetBody()
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}
	if again, _ := io.ReadAll(replay); string(again) != body {
		t.Errorf("GetBody replay = %q, want %q", again, body)
	}
}

// trackingBody records whether Close was called, so a test can assert the
// classifier does not leak the original body reader.
type trackingBody struct {
	r      *strings.Reader
	closed bool
}

func (b *trackingBody) Read(p []byte) (int, error) { return b.r.Read(p) }
func (b *trackingBody) Close() error               { b.closed = true; return nil }

func TestClassifyGraphQL_ClosesOriginalBody(t *testing.T) {
	body := `{"query":"{ viewer { login } }"}`
	tb := &trackingBody{r: strings.NewReader(body)}
	req := graphqlPost(t, "")
	req.Body = tb
	c := graphqlConnector(t)
	if _, err := c.Classify(req); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !tb.closed {
		t.Error("original request body was not closed")
	}
	if got, _ := io.ReadAll(req.Body); string(got) != body {
		t.Errorf("forwarded body = %q, want %q", got, body)
	}
}

// An oversized body fails closed to the umbrella write but is forwarded whole.
func TestClassifyGraphQL_OversizedBodyForwardedIntact(t *testing.T) {
	body := `{"query":"{ viewer { login } }","padding":"` + strings.Repeat("x", graphqlBodySizeLimit+4096) + `"}`
	c := graphqlConnector(t)
	req := graphqlPost(t, body)
	got, err := c.Classify(req)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	assertClassification(t, got, cls("repo:write", ""))
	forwarded, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read forwarded body: %v", err)
	}
	if string(forwarded) != body {
		t.Errorf("oversized body truncated: forwarded %d bytes, want %d", len(forwarded), len(body))
	}
}

// The GitHub Enterprise GraphQL path is classified the same way.
func TestClassifyGraphQL_GHEPath(t *testing.T) {
	c := graphqlConnector(t)
	req, _ := http.NewRequest(http.MethodPost, "https://ghe.example.com/api/graphql",
		strings.NewReader(`{"query":"{ repository(owner:\"acme\", name:\"widgets\") { pullRequest(number:1){ title } } }"}`))
	got, err := c.Classify(req)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	assertClassification(t, got, pullsCls("pulls:read", "widgets"))
}

// A pull request token carries contents:read, so a pulls-only grant must not
// reach file contents through a nested selection.
func TestClassifyGraphQL_PullsNeedContentsForNestedReads(t *testing.T) {
	c := graphqlConnector(t)
	got, err := c.Classify(graphqlPost(t, `{"query":"{ repository(owner:\"acme\",name:\"widgets\"){ pullRequests(first:1){ nodes { repository { object(expression:\"HEAD:secrets.env\"){ ... on Blob { text } } } } } } }"}`))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	pullsOnly := grant{"metadata:read": nil, "pulls:read": widgetsOnly, "pulls:write": widgetsOnly}
	if pullsOnly.allows(got) {
		t.Errorf("pulls-only grant allowed %+v, which mints contents:read", got)
	}
	if !codeReviewer.allows(got) {
		t.Errorf("code-reviewer grant denied %+v", got)
	}
}

// GitHub refuses statusCheckRollup to a token without checks, and its workflow
// runs without actions, so gh pr view needs both while gh pr diff does not.
func TestClassifyGraphQL_StatusCheckRollupNeedsChecks(t *testing.T) {
	noChecks := grant{
		"metadata:read": nil, "contents:read": widgetsOnly, "pulls:read": widgetsOnly, "issues:read": widgetsOnly,
	}
	for fixture, want := range map[string]bool{"pr-view": false, "pr-diff": true} {
		body, err := os.ReadFile(filepath.Join("testdata", "gh", fixture+".json"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		got, err := graphqlConnector(t).Classify(graphqlPost(t, string(body)))
		if err != nil {
			t.Fatalf("Classify %s: %v", fixture, err)
		}
		if ok := noChecks.allows(got); ok != want {
			t.Errorf("%s: grant without checks allowed = %v, want %v", fixture, ok, want)
		}
	}
}
