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

	"github.com/clear-route/orbit/internal/credentials"
)

// GraphQL requests are authorized by the granular actions of the fields they
// select. See docs/adr/2026-09-29-github-graphql-field-classification.md.

// graphqlBodySizeLimit caps how much of a GraphQL request body is read for
// classification. A larger body falls back to the umbrella write but is still
// forwarded whole.
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

// requirement is what one selected field needs: any one of its actions.
type requirement []string

// Requirements per selected field. The first action is the one reported when
// a field is ambiguous between issues and pull requests; either satisfies it.
var (
	reqMetadata   = requirement{ActionMetadataRead}
	reqContents   = requirement{ActionContentsRead}
	reqIssues     = requirement{catIssues + ":" + verbRead}
	reqPulls      = requirement{catPulls + ":" + verbRead}
	reqChecks     = requirement{catChecks + ":" + verbRead}
	reqActions    = requirement{catActions + ":" + verbRead}
	reqIssueOrPR  = requirement{catIssues + ":" + verbRead, catPulls + ":" + verbRead}
	reqWContents  = requirement{ActionContentsWrite}
	reqWIssues    = requirement{catIssues + ":" + verbWrite}
	reqWPulls     = requirement{catPulls + ":" + verbWrite}
	reqWIssueOrPR = requirement{catIssues + ":" + verbWrite, catPulls + ":" + verbWrite}
)

// graphqlRootFields maps top-level query fields that need no repository to
// their requirement. repository, node and nodes are walked instead.
var graphqlRootFields = map[string]requirement{
	"viewer": reqMetadata, "rateLimit": reqMetadata,
	"__typename": reqMetadata, "__schema": reqMetadata, "__type": reqMetadata,
}

// graphqlRepositoryFields maps the fields of Repository to their requirement,
// mirroring the REST route families under /repos/{owner}/{repo}/.
var graphqlRepositoryFields = map[string]requirement{
	"pullRequest": reqPulls, "pullRequests": reqPulls,

	"issue": reqIssues, "issues": reqIssues, "label": reqIssues, "labels": reqIssues,
	"milestone": reqIssues, "milestones": reqIssues, "assignableUsers": reqIssues,
	"issueOrPullRequest": reqIssueOrPR,

	"object": reqContents, "ref": reqContents, "refs": reqContents, "defaultBranchRef": reqContents,
	"release": reqContents, "releases": reqContents, "latestRelease": reqContents,

	"__typename": reqMetadata, "id": reqMetadata, "databaseId": reqMetadata, "name": reqMetadata,
	"nameWithOwner": reqMetadata, "owner": reqMetadata, "url": reqMetadata, "description": reqMetadata,
	"homepageUrl": reqMetadata, "visibility": reqMetadata, "isPrivate": reqMetadata,
	"isArchived": reqMetadata, "isFork": reqMetadata, "isTemplate": reqMetadata, "isEmpty": reqMetadata,
	"hasIssuesEnabled": reqMetadata, "hasWikiEnabled": reqMetadata, "hasProjectsEnabled": reqMetadata,
	"hasDiscussionsEnabled": reqMetadata, "viewerPermission": reqMetadata,
	"createdAt": reqMetadata, "updatedAt": reqMetadata, "pushedAt": reqMetadata,
	"mergeCommitAllowed": reqMetadata, "squashMergeAllowed": reqMetadata,
	"rebaseMergeAllowed": reqMetadata, "deleteBranchOnMerge": reqMetadata,
}

// graphqlNodeTypes maps the type conditions a node or nodes lookup selects
// to their requirement. Repository is walked with graphqlRepositoryFields.
var graphqlNodeTypes = map[string]requirement{
	"PullRequest": reqPulls, "PullRequestReview": reqPulls, "PullRequestReviewComment": reqPulls,
	"PullRequestReviewThread": reqPulls, "PullRequestCommit": reqPulls,
	"Issue": reqIssues, "Label": reqIssues, "Milestone": reqIssues,
	"IssueComment": reqIssueOrPR,
	"Commit":       reqContents, "Tree": reqContents, "Blob": reqContents, "Ref": reqContents,
	"Tag": reqContents, "Release": reqContents,
	"CheckRun": reqChecks, "CheckSuite": reqChecks, "StatusContext": reqChecks,
}

// graphqlNestedFields are fields that reach another category wherever they are
// selected. GitHub refuses a pull request's statusCheckRollup to a token
// without checks, and a check suite's workflowRun without actions.
var graphqlNestedFields = map[string]requirement{
	"statusCheckRollup": reqChecks, "checkSuites": reqChecks, "checkRuns": reqChecks,
	"workflowRun": reqActions, "workflowRuns": reqActions,
}

// graphqlMutationFields maps mutations to the write they perform, matching the
// REST classification of the same operation (merging is contents:write).
// Labelable and Lockable mutations are classified separately, by the target
// node ID, since the same mutations also accept discussions.
var graphqlMutationFields = map[string]requirement{
	"createPullRequest": reqWPulls, "updatePullRequest": reqWPulls, "closePullRequest": reqWPulls,
	"reopenPullRequest": reqWPulls, "markPullRequestReadyForReview": reqWPulls,
	"convertPullRequestToDraft": reqWPulls, "updatePullRequestBranch": reqWPulls,
	"addPullRequestReview": reqWPulls, "submitPullRequestReview": reqWPulls,
	"deletePullRequestReview": reqWPulls, "dismissPullRequestReview": reqWPulls,
	"addPullRequestReviewComment": reqWPulls, "addPullRequestReviewThread": reqWPulls,
	"resolveReviewThread": reqWPulls, "unresolveReviewThread": reqWPulls,
	"requestReviews": reqWPulls, "requestReviewsByLogin": reqWPulls,
	"mergePullRequest": reqWContents,

	"createIssue": reqWIssues, "updateIssue": reqWIssues, "closeIssue": reqWIssues,
	"reopenIssue": reqWIssues, "addSubIssue": reqWIssues, "removeSubIssue": reqWIssues,

	"addComment": reqWIssueOrPR, "updateIssueComment": reqWIssueOrPR, "deleteIssueComment": reqWIssueOrPR,
	"addAssigneesToAssignable": reqWIssueOrPR, "removeAssigneesFromAssignable": reqWIssueOrPR,
	"replaceActorsForAssignable": reqWIssueOrPR,
}

// graphqlLabelableMutations maps a Labelable or Lockable mutation to the
// input field holding its target's node ID. The same mutations also accept a
// discussion's node ID, which carries no category, so these are classified by
// the ID's type prefix (graphqlNodeIDPrefixes) rather than a static
// requirement.
var graphqlLabelableMutations = map[string]string{
	"addLabelsToLabelable": "labelableId", "removeLabelsFromLabelable": "labelableId",
	"lockLockable": "lockableId", "unlockLockable": "lockableId",
}

// graphqlNodeIDPrefixes maps a GitHub global node ID's type prefix to the
// requirement of a Labelable or Lockable mutation targeting it. A prefix
// outside this map, including a discussion's "D_", is unknown and needs the
// umbrella.
var graphqlNodeIDPrefixes = map[string]requirement{
	"I_":  reqWIssues,
	"PR_": reqWPulls,
}

// classifyGraphQL maps a GraphQL request to the granular actions of the fields
// it selects and the repository it names. The body is always restored so the
// gateway forwards it byte for byte. A body that cannot be read, parsed, or
// attributed to one operation classifies as the unqualified umbrella write.
func classifyGraphQL(req *http.Request, owner string) (credentials.Classification, error) {
	fallback := credentials.Classification{Action: ActionRepoWrite}

	buf, ok := bufferGraphQLBody(req)
	if !ok {
		return fallback, nil
	}
	gql, ok := decodeGraphQLRequest(buf)
	if !ok || gql.Query == "" {
		return fallback, nil
	}
	doc, err := parser.ParseQueryWithTokenLimit(&ast.Source{Input: gql.Query}, graphqlTokenLimit)
	if err != nil {
		return fallback, nil
	}
	op := selectOperation(doc, gql.OperationName)
	if op == nil || op.Operation == ast.Subscription {
		return fallback, nil
	}

	w := &graphqlWalker{doc: doc, vars: operationVariables(op, gql.Variables), active: map[string]bool{}}
	cls := w.classification(op)

	if owner == "" {
		return cls, nil
	}
	qualifier, err := w.qualifier(op, owner)
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
// the only one. nil when that is ambiguous, which GitHub also rejects.
func selectOperation(doc *ast.QueryDocument, name string) *ast.OperationDefinition {
	if name != "" {
		return doc.Operations.ForName(name)
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

	reqs       []requirement
	incomplete bool
}

// classification collects the requirement of every top-level field and turns
// them into the broker's classification. Any field outside the maps makes the
// whole request need the umbrella, as before this classification existed.
func (w *graphqlWalker) classification(op *ast.OperationDefinition) credentials.Classification {
	verb := verbRead
	if op.Operation == ast.Mutation {
		verb = verbWrite
		w.fields(op.SelectionSet, w.mutationField)
	} else {
		w.fields(op.SelectionSet, w.queryField)
	}
	w.nestedFields(op.SelectionSet)
	if w.incomplete {
		return credentials.Classification{Action: catRepo + ":" + verb}
	}
	return classificationFor(w.reqs)
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

// nestedFields adds the requirement of every graphqlNestedFields field at any
// depth.
func (w *graphqlWalker) nestedFields(set ast.SelectionSet) {
	w.fields(set, func(f *ast.Field) {
		if r, ok := graphqlNestedFields[f.Name]; ok {
			w.require(r, true)
		}
		w.nestedFields(f.SelectionSet)
	})
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

// require records a field's requirement; a field outside the maps makes the
// request need the umbrella.
func (w *graphqlWalker) require(r requirement, known bool) {
	if !known {
		w.incomplete = true
		return
	}
	w.reqs = append(w.reqs, r)
}

// queryField classifies a top-level query field.
func (w *graphqlWalker) queryField(f *ast.Field) {
	switch f.Name {
	case "repository":
		w.require(reqMetadata, true)
		w.fields(f.SelectionSet, w.repositoryField)
	case "node", "nodes":
		w.require(reqMetadata, true)
		w.nodeSelections(f.SelectionSet)
	default:
		r, ok := graphqlRootFields[f.Name]
		w.require(r, ok)
	}
}

// repositoryField classifies a field of Repository.
func (w *graphqlWalker) repositoryField(f *ast.Field) {
	r, ok := graphqlRepositoryFields[f.Name]
	w.require(r, ok)
}

// mutationField classifies a top-level mutation.
func (w *graphqlWalker) mutationField(f *ast.Field) {
	if f.Name == "__typename" {
		return
	}
	if idField, ok := graphqlLabelableMutations[f.Name]; ok {
		w.require(w.labelableRequirement(f, idField))
		return
	}
	r, ok := graphqlMutationFields[f.Name]
	w.require(r, ok)
}

// labelableRequirement classifies a Labelable or Lockable mutation by the
// type prefix of its target's node ID, read from the mutation's input
// object. An ID that is not a string, or whose prefix is not in
// graphqlNodeIDPrefixes (including a discussion's "D_"), is unknown.
func (w *graphqlWalker) labelableRequirement(f *ast.Field, idField string) (requirement, bool) {
	input, _ := w.argument(f, "input").(map[string]any)
	id, _ := input[idField].(string)
	for prefix, r := range graphqlNodeIDPrefixes {
		if strings.HasPrefix(id, prefix) {
			return r, true
		}
	}
	return nil, false
}

// nodeSelections classifies a node lookup by the types it selects. Fields on
// the Node interface itself (id, __typename) need only metadata.
func (w *graphqlWalker) nodeSelections(set ast.SelectionSet) {
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			w.require(reqMetadata, s.Name == "id" || s.Name == "__typename")
		case *ast.InlineFragment:
			w.nodeType(s.TypeCondition, s.SelectionSet)
		case *ast.FragmentSpread:
			w.spread(s.Name, func(frag *ast.FragmentDefinition) { w.nodeType(frag.TypeCondition, frag.SelectionSet) })
		}
	}
}

// nodeType classifies a node lookup's type condition.
func (w *graphqlWalker) nodeType(typeCondition string, set ast.SelectionSet) {
	switch typeCondition {
	case "", "Node":
		w.nodeSelections(set)
	case "Repository":
		w.fields(set, w.repositoryField)
	default:
		r, ok := graphqlNodeTypes[typeCondition]
		w.require(r, ok)
	}
}

// classificationFor reduces the collected requirements to one classification:
// every single-action requirement is required, and one ambiguous requirement
// becomes the action and its alternate. Any further ambiguous requirement is
// held to its first action, which only narrows what is allowed.
func classificationFor(reqs []requirement) credentials.Classification {
	strict := map[string]bool{}
	for _, r := range reqs {
		if len(r) == 1 {
			strict[r[0]] = true
		}
	}
	var choice requirement
	for _, r := range reqs {
		if len(r) == 1 || slices.ContainsFunc(r, func(a string) bool { return strict[a] }) {
			continue
		}
		if choice == nil {
			choice = r
		} else if !slices.Equal(choice, r) {
			strict[r[0]] = true
		}
	}
	// Metadata is minted with every category, so it is only required alone.
	if len(strict) > 1 || choice != nil {
		delete(strict, ActionMetadataRead)
	}
	required := slices.Sorted(maps.Keys(strict))
	// A pull request token carries contents:read (see permissionsFor), and a
	// nested selection can use it, so GraphQL requires the grant to hold it.
	if (strict[catPulls+":"+verbRead] || strict[catPulls+":"+verbWrite]) && !strict[ActionContentsRead] {
		required = append(required, ActionContentsRead)
	}

	if choice != nil {
		return credentials.Classification{Action: choice[0], Alternates: choice[1:], AlsoRequired: required}
	}
	if len(required) == 0 {
		return credentials.Classification{Action: ActionMetadataRead}
	}
	return credentials.Classification{Action: required[0], AlsoRequired: required[1:]}
}

// qualifier attributes the operation to a repository of the connection's
// owner, denying a repository() call that names another owner. It returns ""
// when the operation names no repository, several, or one it cannot pin down,
// including any node or nodes lookup; only an unqualified grant covers that.
func (w *graphqlWalker) qualifier(op *ast.OperationDefinition, owner string) (string, error) {
	refs := repoRefs{names: map[string]bool{}, owner: owner}
	w.spreads, w.incomplete = 0, false
	// A root node lookup names objects by global ID, whose repository is not
	// decoded.
	w.fields(op.SelectionSet, func(f *ast.Field) {
		refs.unattributed = refs.unattributed || f.Name == "node" || f.Name == "nodes"
	})
	w.collectRefs(op.SelectionSet, &refs)
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

// repoRefs is every repository an operation names.
type repoRefs struct {
	names        map[string]bool
	foreign      bool
	unattributed bool
	owner        string
}

// collectRefs walks the whole operation, every depth, for repository() calls.
func (w *graphqlWalker) collectRefs(set ast.SelectionSet, refs *repoRefs) {
	w.fields(set, func(f *ast.Field) {
		// A repository field without arguments is the related object's own
		// repository, which the minted token already bounds.
		if f.Name == "repository" && len(f.Arguments) > 0 {
			w.repositoryRef(f, refs)
		}
		w.collectRefs(f.SelectionSet, refs)
	})
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
