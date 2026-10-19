// Package korajson is a generic authenticated JSON client over httpclient.Base.
//
// It GET/POST/PATCH/DELETEs a kora API endpoint as the caller (bearer and any delegation token)
// and returns the raw JSON body. It is the forwarding primitive for services that proxy
// authenticated JSON without re-modeling every backend field — notably the
// kora MCP, whose tools return backend JSON to an agent verbatim. It uses
// httpclient.Base (OTel client spans, W3C trace propagation, structured logging);
// it never constructs a raw http.Client.
package korajson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

const (
	tracerName    = "orbit.json-client"
	clientTimeout = 30 * time.Second
)

var (
	// ErrUnauthorized is returned when the upstream rejects the caller's token.
	ErrUnauthorized = errors.New("unauthorized: caller token rejected upstream")
	// ErrUpstream is returned for any other non-2xx upstream status.
	ErrUpstream = errors.New("unexpected status from upstream")
)

// Caller is the identity a request is sent as: the caller's platform bearer and,
// for a delegated run, its delegation token.
type Caller struct {
	Bearer     string
	Delegation string
}

// Client forwards authenticated JSON requests to kora APIs.
type Client struct {
	*httpclient.Base
}

// NewClient returns a Client. Share it across calls; it is safe for concurrent use.
func NewClient(logger logging.Logger) *Client {
	return &Client{Base: httpclient.New(tracerName, logger, clientTimeout)}
}

// Get performs an authenticated GET and returns the raw JSON body.
func (c *Client) Get(ctx context.Context, caller Caller, rawURL string, query url.Values) (json.RawMessage, error) {
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build GET request: %w", err)
	}
	return c.send(ctx, "orbitjson.Get", caller, req)
}

// Post performs an authenticated POST of a JSON body and returns the raw JSON body.
func (c *Client) Post(ctx context.Context, caller Caller, rawURL string, body any) (json.RawMessage, error) {
	r, err := c.EncodeJSON(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, r)
	if err != nil {
		return nil, fmt.Errorf("build POST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.send(ctx, "orbitjson.Post", caller, req)
}

// Put performs an authenticated PUT of a JSON body and returns the raw JSON body.
func (c *Client) Put(ctx context.Context, caller Caller, rawURL string, body any) (json.RawMessage, error) {
	r, err := c.EncodeJSON(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, r)
	if err != nil {
		return nil, fmt.Errorf("build PUT request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.send(ctx, "orbitjson.Put", caller, req)
}

// Patch performs an authenticated PATCH of a JSON body and returns the raw JSON body.
func (c *Client) Patch(ctx context.Context, caller Caller, rawURL string, body any) (json.RawMessage, error) {
	r, err := c.EncodeJSON(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, rawURL, r)
	if err != nil {
		return nil, fmt.Errorf("build PATCH request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.send(ctx, "orbitjson.Patch", caller, req)
}

// Delete performs an authenticated DELETE and returns the raw JSON body (or a
// synthetic {"status":"ok"} for a 204 No Content).
func (c *Client) Delete(ctx context.Context, caller Caller, rawURL string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build DELETE request: %w", err)
	}
	return c.send(ctx, "orbitjson.Delete", caller, req)
}

func (c *Client) send(ctx context.Context, spanName string, caller Caller, req *http.Request) (json.RawMessage, error) {
	if caller.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+caller.Bearer)
	}
	if caller.Delegation != "" {
		req.Header.Set(mw.DelegationTokenHeader, caller.Delegation)
	}
	// This is a JSON client: always ask for JSON. Lets content-negotiated routes
	// (e.g. the agents run-events endpoint, which is SSE by default) serve us JSON.
	req.Header.Set("Accept", "application/json")
	_, span, resp, err := c.Do(ctx, spanName, req)
	defer span.End()
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, c.HandleErrorResponse(span, resp, ErrUnauthorized)
	case resp.StatusCode == http.StatusNoContent:
		return json.RawMessage(`{"status":"ok"}`), nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		// Some endpoints return a 2xx with an empty body (e.g. send-message,
		// interrupt). Treat an empty body as a success acknowledgement rather than
		// failing to decode it as JSON.
		if len(bytes.TrimSpace(body)) == 0 {
			return json.RawMessage(`{"status":"ok"}`), nil
		}
		return json.RawMessage(body), nil
	default:
		return nil, c.HandleErrorResponse(span, resp, ErrUpstream)
	}
}
