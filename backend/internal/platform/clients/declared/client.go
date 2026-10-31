// Package declared is the operator's client for the declared-item endpoints
// every owning service serves through platform/declarative. The operator holds
// one Client per service base URL.
//
// See docs/adr/2026-10-24-declarative-resources.md.
package declared

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	tracerName    = "github.com/runkora/kora/backend/internal/platform/clients/declared"
	clientTimeout = 30 * time.Second
)

// ErrUnexpectedStatus is returned when a service answers with a status the
// endpoint does not define.
var ErrUnexpectedStatus = errors.New("unexpected status from declared-item endpoint")

// TokenSource returns the bearer token each call carries.
type TokenSource func(context.Context) (string, error)

// Client calls one service's declared-item endpoints.
type Client struct {
	*httpclient.Base
	baseURL string
	token   TokenSource
}

// NewClient returns a Client for the service at baseURL, including the
// service's base path, e.g. "http://kora-triggers.kora-system.svc:8097/api/v1".
func NewClient(baseURL string, token TokenSource, logger logging.Logger) *Client {
	return &Client{Base: httpclient.New(tracerName, logger, clientTimeout), baseURL: baseURL, token: token}
}

// Apply asks the service to apply item as a resource of kind.
func (c *Client) Apply(ctx context.Context, kind string, item declarative.Item) (declarative.Result, error) {
	body, err := c.EncodeJSON(item)
	if err != nil {
		return declarative.Result{}, fmt.Errorf("encode declared item: %w", err)
	}
	var res declarative.Result
	err = c.call(ctx, http.MethodPost, "/internal/declared/{kind}", []string{kind}, body, http.StatusOK, &res)
	return res, err
}

// Delete asks the service to delete the declared item name in tenantID.
func (c *Client) Delete(ctx context.Context, kind, tenantID, name string) error {
	return c.call(ctx, http.MethodDelete, "/internal/declared/{kind}/{tenantID}/{name}", []string{kind, tenantID, name}, nil, http.StatusNoContent, nil)
}

// List returns the declared items of kind the service holds for tenantID.
func (c *Client) List(ctx context.Context, kind, tenantID string) ([]declarative.Declared, error) {
	var out struct {
		Items []declarative.Declared `json:"items"`
	}
	err := c.call(ctx, http.MethodGet, "/internal/declared/{kind}/{tenantID}", []string{kind, tenantID}, nil, http.StatusOK, &out)
	return out.Items, err
}

// call sends one request to the path template filled with params and decodes
// a want-status response into dest.
func (c *Client) call(ctx context.Context, method, path string, params []string, body io.Reader, want int, dest any) error {
	tok, err := c.token(ctx)
	if err != nil {
		return fmt.Errorf("get platform token: %w", err)
	}
	header := http.Header{"Authorization": {"Bearer " + tok}}
	if body != nil {
		header.Set("Content-Type", "application/json")
	}

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: method, BaseURL: c.baseURL, Path: path, Params: params, Header: header, Body: body,
	})
	defer span.End()
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != want {
		return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
	if dest == nil {
		return nil
	}
	return c.DecodeJSON(span, resp, dest)
}
