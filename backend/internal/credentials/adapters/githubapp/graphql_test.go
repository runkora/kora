package githubapp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// graphqlConnector opens a connection whose installation belongs to
// example-org. GraphQL classification makes no GitHub calls.
func graphqlConnector(t *testing.T) credentials.Connector {
	t.Helper()
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	conn := credentials.Connection{
		ID: "conn-1", TenantID: "example", Name: "github", SecretRef: "ref", Adapter: credentials.AdapterGitHubApp,
		Config: mustConfig(t, config{AppID: "1", InstallationID: 1, Owner: "example-org"}),
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

func cls(action, qualifier string) credentials.Classification {
	return credentials.Classification{Action: action, Qualifier: qualifier}
}

// graphqlCls is a GraphQL classification: any repository category at verb on
// the qualifier, or for a query the metadata baseline.
func graphqlCls(verb, qualifier string) credentials.Classification {
	var alts []string
	if verb == verbWrite {
		alts = append(alts, "workflows:write")
	}
	for _, c := range []string{"issues", "pulls", "ci", "deployments", "security", "secrets", "admin"} {
		alts = append(alts, c+":"+verb)
	}
	if verb == verbRead {
		alts = append(alts, "metadata:read")
	}
	return credentials.Classification{Action: "contents:" + verb, Alternates: alts, Qualifier: qualifier}
}

func assertClassification(t *testing.T, got, want credentials.Classification) {
	t.Helper()
	if got.Action != want.Action || got.Qualifier != want.Qualifier || !slices.Equal(got.Alternates, want.Alternates) ||
		!slices.Equal(got.AlsoRequired, want.AlsoRequired) || !slices.Equal(got.Optional, want.Optional) {
		t.Errorf("classification = %+v, want %+v", got, want)
	}
}

func TestClassifyGraphQL(t *testing.T) {
	q := func(query string, vars map[string]any) string {
		b, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
		return string(b)
	}
	const repoWidgets = `repository(owner:"example-org", name:"widgets")`
	const searchQ = `query($q:String!){ search(query:$q, type:ISSUE, first:1){ issueCount } }`
	read, write := verbRead, verbWrite

	cases := []struct {
		name      string
		body      string
		verb      string
		qualifier string
		denied    bool
		// unclassified is the Detail the request is denied with.
		unclassified string
	}{
		{name: "a query reads its repository", body: q(`{ `+repoWidgets+` { issues(first:1){totalCount} pullRequests(first:1){totalCount} } }`, nil), verb: read, qualifier: "widgets"},
		{name: "a mutation writes", body: q(`mutation { mergePullRequest(input:{pullRequestId:"PR_x"}){clientMutationId} }`, nil), verb: write},
		{name: "any field is accepted", body: q(`{ `+repoWidgets+` { deployments(first:1){totalCount} vulnerabilityAlerts(first:1){totalCount} } }`, nil), verb: read, qualifier: "widgets"},
		{name: "introspection is a query", body: q(`{ __type(name:"WorkflowRun") { fields { name } } }`, nil), verb: read},
		{name: "viewer is a query", body: q(`{ viewer { login } }`, nil), verb: read},
		{name: "search is qualified by its repo term", body: q(`{ search(query:"repo:example-org/widgets", type:ISSUE, first:1){ issueCount } }`, nil), verb: read, qualifier: "widgets"},
		{name: "search naming a foreign repository is unqualified", body: q(`{ search(query:"repo:evil/widgets", type:ISSUE, first:1){ issueCount } }`, nil), verb: read},
		{name: "search with NOT is unqualified", body: q(searchQ, map[string]any{"q": "NOT repo:example-org/widgets"}), verb: read},
		{name: "search naming two repositories is unqualified", body: q(`{ search(query:"repo:example-org/widgets repo:example-org/other", type:ISSUE, first:1){ issueCount } }`, nil), verb: read},
		{name: "search scoped apart from the repository lookup is unqualified", body: q(`{ `+repoWidgets+` { id } search(query:"repo:example-org/other", type:ISSUE, first:1){ issueCount } }`, nil), verb: read},
		{name: "search query from a variable", body: q(searchQ, map[string]any{"q": "repo:example-org/widgets"}), verb: read, qualifier: "widgets"},
		{name: "owner repositories are never one repository", body: q(`{ `+repoWidgets+` { id } repositoryOwner(login:"example-org") { repositories(first:1) { nodes { name } } } }`, nil), verb: read},
		{name: "variables resolve owner and name", body: q(`query($o:String!,$n:String!){ repository(owner:$o,name:$n){ id } }`, map[string]any{"o": "Example-Org", "n": "Widgets"}), verb: read, qualifier: "widgets"},
		{name: "two repositories are unqualified", body: q(`{ a: `+repoWidgets+` { id } b: repository(owner:"example-org", name:"tools") { id } }`, nil), verb: read},
		{name: "repository without a string owner is unqualified", body: q(`query($o:String){ repository(owner:$o, name:"widgets") { id } }`, nil), verb: read},
		{name: "a node lookup with no resolution is unqualified", body: q(`{ node(id:"PR_x") { id } }`, nil), verb: read},
		{name: "undefined fragment is unqualified", body: q(`{ `+repoWidgets+` { ...Missing } }`, nil), verb: read},
		{name: "fragment cycle is unqualified", body: q(`query { `+repoWidgets+` { ...A } } fragment A on Repository { id ...B } fragment B on Repository { name ...A }`, nil), verb: read},
		{name: "named operation is selected", body: `{"query":"query A { viewer { login } } mutation B { mergePullRequest(input:{pullRequestId:\"PR_x\"}){clientMutationId} }","operationName":"B"}`, verb: write},

		{name: "foreign owner is denied", body: q(`{ repository(owner:"evil", name:"widgets") { id } }`, nil), denied: true},
		{name: "foreign owner in a second alias is denied", body: q(`{ a: `+repoWidgets+` { id } b: repository(owner:"evil", name:"widgets") { id } }`, nil), denied: true},
		{name: "foreign owner in a nested repository is denied", body: q(`{ viewer { repository(owner:"evil", name:"x") { id } } }`, nil), denied: true},
		{name: "foreign owner hidden in a variable default is denied", body: q(`query($o:String="evil"){ repository(owner:$o, name:"widgets") { id } }`, nil), denied: true},
		{name: "foreign owner inside a fragment is denied", body: q(`query { ...F } fragment F on Query { repository(owner:"evil", name:"widgets") { id } }`, nil), denied: true},
		{name: "case variant envelope key is denied", body: `{"query":"{ repository(owner:\"evil\",name:\"x\"){ id } }","QUERY":"{ viewer { login } }"}`, unclassified: "GraphQL body is not a valid request envelope"},
		{name: "case variant operationName is denied", body: `{"query":"query B { viewer { login } } mutation A { mergePullRequest(input:{}){clientMutationId} }","operationName":"A","OperationName":"B"}`, unclassified: "GraphQL body is not a valid request envelope"},
		{name: "no body is denied", body: "", unclassified: "GraphQL request has no body"},
		{name: "malformed JSON is denied", body: "not json", unclassified: "GraphQL body is not a valid request envelope"},
		{name: "empty query is denied", body: `{"query":""}`, unclassified: "GraphQL body has no query"},
		{name: "unparseable document is denied", body: q(`{ repository(owner:"example-org" { id }`, nil), unclassified: "GraphQL query does not parse"},
		{name: "two operations without a name are denied", body: q(`query A { viewer { login } } query B { viewer { login } }`, nil), unclassified: "GraphQL document names no single operation to run"},
		{name: "unknown operation name is denied", body: `{"query":"query A { viewer { login } }","operationName":"Z"}`, unclassified: "GraphQL document names no single operation to run"},
		{name: "two operations sharing the name are denied", body: `{"query":"query M { viewer { login } } mutation M { mergePullRequest(input:{}){clientMutationId} }","operationName":"M"}`, unclassified: "GraphQL document names no single operation to run"},
		{name: "subscription is denied", body: q(`subscription { viewer { login } }`, nil), unclassified: "GraphQL subscriptions are not supported"},
		{name: "token limit is denied", body: q(`{ viewer { `+strings.Repeat("login ", graphqlTokenLimit)+`} }`, nil), unclassified: "GraphQL query does not parse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := graphqlPost(t, tc.body)
			if tc.body == "" {
				req.Body = http.NoBody
			}
			got, err := graphqlConnector(t).Classify(req)
			if tc.denied {
				if !errors.Is(err, credentials.ErrUnsupportedAction) {
					t.Fatalf("want ErrUnsupportedAction, got err=%v cls=%+v", err, got)
				}
				return
			}
			if tc.unclassified != "" {
				assertUnclassified(t, err, tc.unclassified)
				return
			}
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			assertClassification(t, got, graphqlCls(tc.verb, tc.qualifier))
		})
	}
}

// assertUnclassified checks that err denies the request as unclassified, stating detail.
func assertUnclassified(t *testing.T, err error, detail string) {
	t.Helper()
	var unclassified *credentials.UnclassifiedError
	if !errors.As(err, &unclassified) || !errors.Is(err, credentials.ErrUnsupportedAction) || unclassified.Detail != detail {
		t.Errorf("err = %v, want an UnclassifiedError stating %q", err, detail)
	}
}

// A connection without a declared owner never qualifies GraphQL, so it never
// needs to list the installation's repositories.
func TestClassifyGraphQL_NoDeclaredOwner(t *testing.T) {
	a := New(nil, stubSecrets{}, logging.NewLoggerWithOutput(io.Discard))
	c := open(t, a, validConn(t))
	got, err := c.Classify(graphqlPost(t, `{"query":"{ repository(owner:\"example-org\", name:\"widgets\") { id } }"}`))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	assertClassification(t, got, graphqlCls(verbRead, ""))
}

// TestClassifyGraphQL_BodyPreservation verifies that Classify leaves the full
// original body readable for forwarding.
func TestClassifyGraphQL_BodyPreservation(t *testing.T) {
	body := `{"query":"query { repository(owner:\"example-org\", name:\"widgets\") { id } }"}`
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

// An oversized body is denied, and left whole for the anonymous fallback.
func TestClassifyGraphQL_OversizedBodyForwardedIntact(t *testing.T) {
	body := `{"query":"{ viewer { login } }","padding":"` + strings.Repeat("x", graphqlBodySizeLimit+4096) + `"}`
	c := graphqlConnector(t)
	req := graphqlPost(t, body)
	_, err := c.Classify(req)
	assertUnclassified(t, err, "GraphQL body over 512 KiB")
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
		strings.NewReader(`{"query":"{ repository(owner:\"example-org\", name:\"widgets\") { pullRequest(number:1){ title } } }"}`))
	got, err := c.Classify(req)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	assertClassification(t, got, graphqlCls(verbRead, "widgets"))
}
