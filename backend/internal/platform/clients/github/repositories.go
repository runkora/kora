package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
)

// Repositories resolves numeric repository ids to their repositories, using an
// installation token. Ids the token cannot see are absent from the result. At
// most MaxNodeLookup ids are sent.
func (c *Client) Repositories(ctx context.Context, installationToken string, ids []string) (map[string]Repository, error) {
	ids = ids[:min(len(ids), MaxNodeLookup)]
	repos := make(map[string]Repository, len(ids))
	for _, id := range ids {
		ref, ok, err := c.repository(ctx, installationToken, id)
		if err != nil {
			return nil, err
		}
		if ok {
			repos[id] = ref
		}
	}
	return repos, nil
}

func (c *Client) repository(ctx context.Context, installationToken, id string) (Repository, bool, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+installationToken)
	header.Set("Accept", acceptHeader)

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/repositories/{id}", Params: []string{id}, Header: header,
	})
	defer span.End()
	if err != nil {
		return Repository{}, false, fmt.Errorf("repository lookup call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Repository{}, false, nil
	default:
		return Repository{}, false, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
	var out repositoryJSON
	if err := c.DecodeJSON(span, resp, &out); err != nil {
		return Repository{}, false, err
	}
	ref, ok := out.ref()
	return ref, ok, nil
}
