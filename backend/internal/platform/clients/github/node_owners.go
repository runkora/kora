package github

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrGraphQLQuery is returned when GitHub answers a GraphQL query with errors and no data.
var ErrGraphQLQuery = errors.New("github graphql query failed")

// MaxNodeLookup is the most node ids NodeOwners resolves in one call.
const MaxNodeLookup = 100

// nodeOwnerFields selects the owner login of a repository, a repository-scoped
// object, an organization or a user.
const nodeOwnerFields = `{ ... on Repository { owner { login } } ... on RepositoryNode { repository { owner { login } } } ... on Organization { login } ... on User { login } }`

// nodeOwner is one aliased node in the lookup response.
type nodeOwner struct {
	Login string `json:"login"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Repository struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

// NodeOwners resolves global node ids to the login of the account that owns each
// object, using an installation token. Ids the token cannot see are absent from
// the result. Each id is a separate aliased field, so one unresolvable id does
// not fail the others. At most MaxNodeLookup ids are sent.
func (c *Client) NodeOwners(ctx context.Context, installationToken string, ids []string) (map[string]string, error) {
	ids = ids[:min(len(ids), MaxNodeLookup)]
	params := make([]string, len(ids))
	fields := make([]string, len(ids))
	vars := make(map[string]any, len(ids))
	for i, id := range ids {
		params[i] = fmt.Sprintf("$i%d: ID!", i)
		fields[i] = fmt.Sprintf("n%d: node(id: $i%d) %s", i, i, nodeOwnerFields)
		vars[fmt.Sprintf("i%d", i)] = id
	}
	body := map[string]any{
		"query":     fmt.Sprintf("query(%s) { %s }", strings.Join(params, ", "), strings.Join(fields, " ")),
		"variables": vars,
	}
	payload, err := c.EncodeJSON(body)
	if err != nil {
		return nil, fmt.Errorf("encode node owner lookup: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/graphql", payload)
	if err != nil {
		return nil, fmt.Errorf("build node owner lookup: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+installationToken)
	req.Header.Set("Content-Type", "application/json")

	_, span, resp, err := c.Do(ctx, "github.Client.NodeOwners", req)
	defer span.End()
	if err != nil {
		return nil, fmt.Errorf("node owner lookup call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	var out struct {
		Data   map[string]*nodeOwner `json:"data"`
		Errors []json.RawMessage     `json:"errors"`
	}
	if err := c.DecodeJSON(span, resp, &out); err != nil {
		return nil, err
	}
	// No data at all, with errors, is a failed query rather than unseen ids.
	if out.Data == nil && len(out.Errors) > 0 {
		return nil, c.RecordError(span, fmt.Errorf("%w: %s", ErrGraphQLQuery, out.Errors[0]))
	}
	owners := make(map[string]string, len(ids))
	for i, id := range ids {
		n := out.Data[fmt.Sprintf("n%d", i)]
		if n == nil {
			continue
		}
		if login := strings.ToLower(cmp.Or(n.Owner.Login, n.Repository.Owner.Login, n.Login)); login != "" {
			owners[id] = login
		}
	}
	return owners, nil
}
