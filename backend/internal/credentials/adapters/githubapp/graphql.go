package githubapp

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"

	"github.com/runkora/kora/backend/internal/credentials"
)

// A GraphQL query needs a read or write grant, or the metadata baseline, and a
// mutation a write grant, on the repository the document targets. See
// docs/adr/2026-11-15-github-connector-route-coverage.md, items 11 to 14.

// graphqlBodySizeLimit caps how much of a GraphQL request body is read for
// classification. A larger body is denied as unclassified.
const graphqlBodySizeLimit = 512 * 1024

// graphqlTokenLimit bounds the parser's work on a hostile document. gh's
// largest query is under a thousand tokens.
const graphqlTokenLimit = 20000

// graphqlRequest is the JSON envelope GitHub accepts for GraphQL.
type graphqlRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName"`
}

// graphqlCategories are the repository categories a GraphQL token can carry.
// contents comes first, as the action a denial reports.
var graphqlCategories = []string{catContents, catWorkflows, catIssues, catPulls, catCI, catDeployments, catSecurity, catSecrets, catAdmin}

// fieldSearch is the Query field for search, which is qualified by its own rules.
const fieldSearch = "search"

// classifyGraphQL requires any repository category at the operation's level on
// the repository the document targets, or for a query the metadata baseline;
// the broker also accepts each read's write. It mints every one the grant
// holds. The body is always restored
// so the gateway forwards it byte for byte. A body that cannot be read, parsed
// or attributed to one operation, and a subscription, are unclassified. repos
// holds the resolved node ids.
func classifyGraphQL(req *http.Request, owner string, repos map[string]string) (credentials.Classification, error) {
	buf, ok := bufferGraphQLBody(req)
	if !ok {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}
	gql, ok := decodeGraphQLRequest(buf)
	if !ok || gql.Query == "" {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}
	doc, err := parser.ParseQueryWithTokenLimit(&ast.Source{Input: gql.Query}, graphqlTokenLimit)
	if err != nil {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}
	op := selectOperation(doc, gql.OperationName)
	if op == nil || op.Operation == ast.Subscription {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}

	verb := verbRead
	if op.Operation == ast.Mutation {
		verb = verbWrite
	}
	cls := credentials.Classification{Action: graphqlCategories[0] + ":" + verb}
	for _, category := range graphqlCategories[1:] {
		// GitHub grants workflows only at write.
		if category == catWorkflows && verb == verbRead {
			continue
		}
		cls.Alternates = append(cls.Alternates, category+":"+verb)
	}
	// The metadata baseline also allows a query, such as gh's introspection
	// probes; the token then carries only what else the grant holds.
	if verb == verbRead {
		cls.Alternates = append(cls.Alternates, ActionMetadataRead)
	}
	if owner == "" {
		return cls, nil
	}
	w := &graphqlWalker{doc: doc, vars: operationVariables(op, gql.Variables), active: map[string]bool{}}
	qualifier, err := w.qualifier(op, owner, repos)
	if err != nil {
		return credentials.Classification{}, err
	}
	cls.Qualifier = qualifier
	return cls, nil
}

// graphqlEnvelopeKeys are the envelope keys GitHub reads, matched exactly.
var graphqlEnvelopeKeys = []string{"query", "variables", "operationName"}

// decodeGraphQLRequest decodes the envelope with exact key names. Go matches
// struct keys case-insensitively, so a "QUERY" beside "query" could classify a
// document other than the one GitHub runs; such a body is rejected.
func decodeGraphQLRequest(buf []byte) (graphqlRequest, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf, &raw); err != nil {
		return graphqlRequest{}, false
	}
	for key := range raw {
		for _, want := range graphqlEnvelopeKeys {
			if key != want && strings.EqualFold(key, want) {
				return graphqlRequest{}, false
			}
		}
	}
	var gql graphqlRequest
	targets := map[string]any{"query": &gql.Query, "variables": &gql.Variables, "operationName": &gql.OperationName}
	for key, target := range targets {
		if v, ok := raw[key]; ok {
			if err := json.Unmarshal(v, target); err != nil {
				return graphqlRequest{}, false
			}
		}
	}
	return gql, true
}

// bufferGraphQLBody reads the body for classification and restores it for
// forwarding. ok is false when the body is absent, unreadable, or over the
// size limit; an oversized body is still forwarded whole.
func bufferGraphQLBody(req *http.Request) ([]byte, bool) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, false
	}
	original := req.Body
	buf, err := io.ReadAll(io.LimitReader(original, graphqlBodySizeLimit+1))
	if err != nil || len(buf) > graphqlBodySizeLimit {
		// Forward what was read followed by the unread remainder. GetBody is left
		// alone: one that replays only buf would truncate a retry.
		req.Body = &chainedBody{r: io.MultiReader(bytes.NewReader(buf), original), c: original}
		return nil, false
	}
	_ = original.Close()
	req.Body = io.NopCloser(bytes.NewReader(buf))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf)), nil
	}
	return buf, true
}

// chainedBody closes the original request body once an oversized GraphQL
// request has been forwarded from the already-read bytes and the remainder.
type chainedBody struct {
	r io.Reader
	c io.Closer
}

func (b *chainedBody) Read(p []byte) (int, error) { return b.r.Read(p) }
func (b *chainedBody) Close() error               { return b.c.Close() }

// selectOperation returns the operation GitHub will execute: the named one, or
// the only one. nil when that is ambiguous, including two operations sharing
// the name, which GitHub also rejects.
func selectOperation(doc *ast.QueryDocument, name string) *ast.OperationDefinition {
	if name != "" {
		named := slices.DeleteFunc(slices.Clone(doc.Operations), func(op *ast.OperationDefinition) bool { return op.Name != name })
		if len(named) != 1 {
			return nil
		}
		return named[0]
	}
	if len(doc.Operations) != 1 {
		return nil
	}
	return doc.Operations[0]
}

// operationVariables overlays the supplied variables on the operation's
// declared defaults, so an argument hidden in a default is still seen.
func operationVariables(op *ast.OperationDefinition, supplied map[string]any) map[string]any {
	vars := map[string]any{}
	for _, def := range op.VariableDefinitions {
		if def.DefaultValue != nil {
			if v, err := def.DefaultValue.Value(nil); err == nil {
				vars[def.Variable] = v
			}
		}
	}
	maps.Copy(vars, supplied)
	return vars
}

// graphqlSpreadLimit bounds fragment expansions per walk, since a fragment is
// expanded afresh wherever it is spread.
const graphqlSpreadLimit = 1000

// graphqlWalker walks one operation. A fragment is expanded in every context
// it is spread into; a cycle, an undefined fragment, or more than
// graphqlSpreadLimit expansions marks the walk incomplete.
type graphqlWalker struct {
	doc     *ast.QueryDocument
	vars    map[string]any
	active  map[string]bool
	spreads int

	incomplete bool
}

// fields visits every field of a selection set, looking through inline
// fragments and fragment spreads whatever their type condition.
func (w *graphqlWalker) fields(set ast.SelectionSet, visit func(*ast.Field)) {
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			visit(s)
		case *ast.InlineFragment:
			w.fields(s.SelectionSet, visit)
		case *ast.FragmentSpread:
			w.spread(s.Name, func(frag *ast.FragmentDefinition) { w.fields(frag.SelectionSet, visit) })
		}
	}
}

// spread expands a named fragment into the current context.
func (w *graphqlWalker) spread(name string, expand func(*ast.FragmentDefinition)) {
	w.spreads++
	frag := w.doc.Fragments.ForName(name)
	if frag == nil || w.active[name] || w.spreads > graphqlSpreadLimit {
		w.incomplete = true
		return
	}
	w.active[name] = true
	expand(frag)
	delete(w.active, name)
}

// qualifier attributes the operation to a repository of the connection's
// owner, from its repository() calls and the repo: terms of its searches,
// and the repositories its node ids resolve to in repos, denying a
// repository() call that names another owner. It returns "" when the operation
// names no repository, several, or one it cannot pin down, including a node id
// that is unresolved or outside the owner; only an unqualified grant covers that.
// With repos nil, node ids are ignored and any node lookup is unqualified.
func (w *graphqlWalker) qualifier(op *ast.OperationDefinition, owner string, repos map[string]string) (string, error) {
	refs := repoRefs{names: map[string]bool{}, owner: owner}
	w.spreads, w.incomplete = 0, false
	// repositoryOwner lists every repository of an account, and a node lookup
	// is unattributed unless its literal ids were resolved.
	w.fields(op.SelectionSet, func(f *ast.Field) {
		refs.unattributed = refs.unattributed || f.Name == "repositoryOwner" ||
			(f.Name == "node" && (repos == nil || !w.stringIDs(f, "id"))) ||
			(f.Name == "nodes" && (repos == nil || !w.stringIDs(f, "ids")))
	})
	w.collectRefs(op.SelectionSet, &refs)
	if repos != nil {
		w.nodeIDRefs(op, repos, &refs)
	}
	switch {
	case refs.foreign:
		return "", credentials.ErrUnsupportedAction
	case refs.unattributed || w.incomplete || len(refs.names) != 1:
		return "", nil
	}
	for name := range refs.names {
		return name, nil
	}
	return "", nil
}

// nodeIDRefs records the repository of every node id the operation names. An
// account id addresses no repository and is skipped; any other id outside one
// of the owner's repositories leaves the operation unattributed.
func (w *graphqlWalker) nodeIDRefs(op *ast.OperationDefinition, repos map[string]string, refs *repoRefs) {
	ids := map[string]bool{}
	w.collectOwners(op.SelectionSet, map[string]bool{}, ids)
	for id := range ids {
		repoOwner, name, ok := strings.Cut(repos[id], "/")
		if !ok && graphqlAccountTypes[repos[id]] {
			continue
		}
		if !ok || repoOwner != refs.owner {
			refs.unattributed = true
			return
		}
		refs.names[name] = true
	}
}

// stringIDs reports whether a node or nodes argument is one or more non-empty
// strings, the only ids collectNodeIDs records.
func (w *graphqlWalker) stringIDs(f *ast.Field, arg string) bool {
	switch v := w.argument(f, arg).(type) {
	case string:
		return v != ""
	case []any:
		return len(v) > 0 && !slices.ContainsFunc(v, func(id any) bool { s, _ := id.(string); return s == "" })
	}
	return false
}

// repoRefs is every repository an operation names.
type repoRefs struct {
	names        map[string]bool
	foreign      bool
	unattributed bool
	owner        string
}

// collectRefs walks the whole operation, every depth, for repository() calls
// and searches.
func (w *graphqlWalker) collectRefs(set ast.SelectionSet, refs *repoRefs) {
	w.fields(set, func(f *ast.Field) {
		// A repository field without arguments is the related object's own
		// repository, which the minted token already bounds.
		if f.Name == "repository" && len(f.Arguments) > 0 {
			w.repositoryRef(f, refs)
		}
		if f.Name == fieldSearch {
			w.searchRef(f, refs)
		}
		w.collectRefs(f.SelectionSet, refs)
	})
}

// searchRef records the repository a search query is scoped to, by the same
// rule as REST search. A query not scoped to one repository of the owner
// leaves the operation unattributed.
func (w *graphqlWalker) searchRef(f *ast.Field, refs *repoRefs) {
	q, _ := w.argument(f, "query").(string)
	if name := searchRepoQualifier(q, refs.owner); name != "" {
		refs.names[name] = true
		return
	}
	refs.unattributed = true
}

// repositoryRef records a repository(owner:, name:) call.
func (w *graphqlWalker) repositoryRef(f *ast.Field, refs *repoRefs) {
	owner, _ := w.argument(f, "owner").(string)
	name, _ := w.argument(f, "name").(string)
	switch {
	case owner == "" || name == "":
		refs.unattributed = true
	case normalizeName(owner) != refs.owner:
		refs.foreign = true
	default:
		refs.names[normalizeName(name)] = true
	}
}

// argument resolves a field argument against the operation's variables.
func (w *graphqlWalker) argument(f *ast.Field, name string) any {
	arg := f.Arguments.ForName(name)
	if arg == nil {
		return nil
	}
	v, err := arg.Value.Value(w.vars)
	if err != nil {
		return nil
	}
	return v
}

// graphqlOwnerFields maps a field that names an account to the argument holding it.
var graphqlOwnerFields = map[string]string{
	"repository": "owner", "organization": "login", "repositoryOwner": "login", "user": "login",
}

// graphqlAccountTypes are the resolved node types that name a person or team,
// such as an assignee or reviewer. They address no repository, so they do not
// affect the qualifier; any other type outside a repository leaves it unset.
var graphqlAccountTypes = map[string]bool{"User": true, "Bot": true, "Team": true, "Mannequin": true}

// graphqlNonNodeIDs are arguments and input fields whose names end in Id or Ids
// but hold no node id in the GitHub schema.
var graphqlNonNodeIDs = map[string]bool{
	"clientMutationId": true, "externalId": true, "ghsaId": true,
	"iterationId": true, "multiSelectOptionIds": true, "singleSelectOptionId": true,
}

// graphqlOwners returns the owners and node ids a GraphQL request names. A body
// that cannot be read or parsed names neither. The body is restored for forwarding.
func graphqlOwners(req *http.Request) credentials.RequestOwners {
	out := credentials.RequestOwners{Subject: subjectGraphQL}
	buf, ok := bufferGraphQLBody(req)
	if !ok {
		return out
	}
	gql, ok := decodeGraphQLRequest(buf)
	if !ok || gql.Query == "" {
		return out
	}
	doc, err := parser.ParseQueryWithTokenLimit(&ast.Source{Input: gql.Query}, graphqlTokenLimit)
	if err != nil {
		return out
	}
	op := selectOperation(doc, gql.OperationName)
	if op == nil {
		return out
	}
	w := &graphqlWalker{doc: doc, vars: operationVariables(op, gql.Variables), active: map[string]bool{}}
	owners, ids := map[string]bool{}, map[string]bool{}
	w.collectOwners(op.SelectionSet, owners, ids)
	out.Owners, out.NodeIDs = slices.Sorted(maps.Keys(owners)), slices.Sorted(maps.Keys(ids))
	return out
}

// collectOwners walks every field at any depth for the accounts and node ids
// its arguments name.
func (w *graphqlWalker) collectOwners(set ast.SelectionSet, owners, ids map[string]bool) {
	w.fields(set, func(f *ast.Field) {
		if arg, ok := graphqlOwnerFields[f.Name]; ok {
			if owner, _ := w.argument(f, arg).(string); owner != "" {
				owners[normalizeName(owner)] = true
			}
		}
		if f.Name == fieldSearch {
			if q, _ := w.argument(f, "query").(string); q != "" {
				for _, owner := range searchOwners(q) {
					owners[owner] = true
				}
			}
		}
		for _, arg := range f.Arguments {
			if v, err := arg.Value.Value(w.vars); err == nil {
				collectNodeIDs(arg.Name, v, ids)
			}
		}
		w.collectOwners(f.SelectionSet, owners, ids)
	})
}

// collectNodeIDs records the string values of a node id argument, descending
// into input objects to find their node id fields.
func collectNodeIDs(name string, v any, ids map[string]bool) {
	switch val := v.(type) {
	case map[string]any:
		for k, inner := range val {
			collectNodeIDs(k, inner, ids)
		}
	case []any:
		for _, inner := range val {
			collectNodeIDs(name, inner, ids)
		}
	case string:
		if isNodeIDName(name) && val != "" {
			ids[val] = true
		}
	}
}

// isNodeIDName reports whether an argument or input field name carries node ids.
func isNodeIDName(name string) bool {
	if graphqlNonNodeIDs[name] {
		return false
	}
	return name == "id" || name == "ids" || strings.HasSuffix(name, "Id") || strings.HasSuffix(name, "Ids")
}
