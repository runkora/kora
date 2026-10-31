package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
)

// RepoOwners resolves numeric repository ids to the login of the account that
// owns each repository, using an installation token. Ids the token cannot see
// are absent from the result. At most MaxNodeLookup ids are sent.
func (c *Client) RepoOwners(ctx context.Context, installationToken string, ids []string) (map[string]string, error) {
	ids = ids[:min(len(ids), MaxNodeLookup)]
	owners := make(map[string]string, len(ids))
	for _, id := range ids {
		login, err := c.repoOwner(ctx, installationToken, id)
		if err != nil {
			return nil, err
		}
		if login != "" {
			owners[id] = login
		}
	}
	return owners, nil
}

func (c *Client) repoOwner(ctx context.Context, installationToken, id string) (string, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+installationToken)
	header.Set("Accept", acceptHeader)

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/repositories/{id}", Params: []string{id}, Header: header,
	})
	defer span.End()
	if err != nil {
		return "", fmt.Errorf("repository owner lookup call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", nil
	default:
		return "", c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
	var out struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := c.DecodeJSON(span, resp, &out); err != nil {
		return "", err
	}
	return strings.ToLower(out.Owner.Login), nil
}
