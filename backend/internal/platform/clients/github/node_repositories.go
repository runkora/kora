package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
)

// ErrGraphQLQuery is returned when GitHub answers a GraphQL query with errors and no data.
var ErrGraphQLQuery = errors.New("github graphql query failed")

// MaxNodeLookup is the most ids NodeRepositories or Repositories resolves in one call.
const MaxNodeLookup = 100

// Repository names a repository by its owner login and name, both lowercased.
type Repository struct {
	Owner string
	Name  string
}

// repoFields selects a repository's owner login and name.
const repoFields = `{ name owner { login } }`

// nodeRepositoryFields selects the repository of a repository or a
// repository-scoped object. A pull request commit reaches it through its pull
// request and a status context through its commit. Users and organizations
// select nothing, so ids such as assignees resolve to no repository.
const nodeRepositoryFields = `{ ... on Repository ` + repoFields +
	` ... on RepositoryNode { repository ` + repoFields + ` }` +
	` ... on Label { repository ` + repoFields + ` } ... on Milestone { repository ` + repoFields + ` }` +
	` ... on PullRequestReviewThread { repository ` + repoFields + ` }` +
	` ... on Commit { repository ` + repoFields + ` } ... on Tree { repository ` + repoFields + ` }` +
	` ... on Blob { repository ` + repoFields + ` } ... on Tag { repository ` + repoFields + ` }` +
	` ... on Ref { repository ` + repoFields + ` } ... on Release { repository ` + repoFields + ` }` +
	` ... on CheckRun { repository ` + repoFields + ` } ... on CheckSuite { repository ` + repoFields + ` }` +
	` ... on PullRequestCommit { pullRequest { repository ` + repoFields + ` } }` +
	` ... on StatusContext { commit { repository ` + repoFields + ` } } }`

// repositoryJSON is a repository in a lookup response.
type repositoryJSON struct {
	Name  string `json:"name"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (r *repositoryJSON) ref() (Repository, bool) {
	if r == nil || r.Name == "" || r.Owner.Login == "" {
		return Repository{}, false
	}
	return Repository{Owner: strings.ToLower(r.Owner.Login), Name: strings.ToLower(r.Name)}, true
}

// nodeRepository is one aliased node in the lookup response.
type nodeRepository struct {
	repositoryJSON
	Repository  *repositoryJSON `json:"repository"`
	PullRequest *struct {
		Repository *repositoryJSON `json:"repository"`
	} `json:"pullRequest"`
	Commit *struct {
		Repository *repositoryJSON `json:"repository"`
	} `json:"commit"`
}

func (n *nodeRepository) ref() (Repository, bool) {
	switch {
	case n.Repository != nil:
		return n.Repository.ref()
	case n.PullRequest != nil:
		return n.PullRequest.Repository.ref()
	case n.Commit != nil:
		return n.Commit.Repository.ref()
	}
	return n.repositoryJSON.ref()
}

// NodeRepositories resolves global node ids to the repository each object is,
// or belongs to, using an installation token. Ids the token cannot see, and
// ids of objects outside any repository, are absent from the result. Each id
// is a separate aliased field, so one unresolvable id does not fail the others.
// At most MaxNodeLookup ids are sent.
func (c *Client) NodeRepositories(ctx context.Context, installationToken string, ids []string) (map[string]Repository, error) {
	ids = ids[:min(len(ids), MaxNodeLookup)]
	params := make([]string, len(ids))
	fields := make([]string, len(ids))
	vars := make(map[string]any, len(ids))
	for i, id := range ids {
		params[i] = fmt.Sprintf("$i%d: ID!", i)
		fields[i] = fmt.Sprintf("n%d: node(id: $i%d) %s", i, i, nodeRepositoryFields)
		vars[fmt.Sprintf("i%d", i)] = id
	}
	body := map[string]any{
		"query":     fmt.Sprintf("query(%s) { %s }", strings.Join(params, ", "), strings.Join(fields, " ")),
		"variables": vars,
	}
	payload, err := c.EncodeJSON(body)
	if err != nil {
		return nil, fmt.Errorf("encode node repository lookup: %w", err)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+installationToken)
	header.Set("Content-Type", "application/json")

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: c.baseURL, Path: "/graphql", Header: header, Body: payload,
	})
	defer span.End()
	if err != nil {
		return nil, fmt.Errorf("node repository lookup call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	var out struct {
		Data   map[string]*nodeRepository `json:"data"`
		Errors []json.RawMessage          `json:"errors"`
	}
	if err := c.DecodeJSON(span, resp, &out); err != nil {
		return nil, err
	}
	// No data at all, with errors, is a failed query rather than unseen ids.
	if out.Data == nil && len(out.Errors) > 0 {
		return nil, c.RecordError(span, fmt.Errorf("%w: %s", ErrGraphQLQuery, out.Errors[0]))
	}
	repos := make(map[string]Repository, len(ids))
	for i, id := range ids {
		if n := out.Data[fmt.Sprintf("n%d", i)]; n != nil {
			if ref, ok := n.ref(); ok {
				repos[id] = ref
			}
		}
	}
	return repos, nil
}
