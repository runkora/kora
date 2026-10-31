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
	tracerName    = "github.com/runkora/kora/backend/internal/platform/clients/korajson"
	clientTimeout = 30 * time.Second
)

var (
	// ErrUnauthorized is returned when the upstream rejects the caller's token.
	ErrUnauthorized = errors.New("unauthorized: caller token rejected upstream")
	// ErrForbidden is returned when the upstream accepts the token but denies
	// the caller access to the resource.
	ErrForbidden = errors.New("forbidden: caller lacks access upstream")
	// ErrUpstream is returned for any other non-2xx upstream status.
	ErrUpstream = errors.New("unexpected status from upstream")
)

// ForbiddenError is a platform 403. Message is the upstream error message, which
// is a policy reason ("deny:...") for a plain scope denial and a sentence for a
// business rule. It matches ErrForbidden with errors.Is.
type ForbiddenError struct {
	Message string
}

func (e *ForbiddenError) Error() string { return ErrForbidden.Error() + ": " + e.Message }

// Is reports ErrForbidden as the sentinel this error stands for.
func (e *ForbiddenError) Is(target error) bool { return target == ErrForbidden }

// upstreamMessage extracts the message field of the platform's error body.
func upstreamMessage(body string) string {
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(body), &payload) != nil {
		return ""
	}
	return payload.Message
}

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

// Get performs an authenticated GET and returns the raw JSON body. path is a
// template under baseURL whose placeholders params fill in order.
func (c *Client) Get(ctx context.Context, caller Caller, baseURL, path string, query url.Values, params ...string) (json.RawMessage, error) {
	return c.send(ctx, caller, httpclient.Request{Method: http.MethodGet, BaseURL: baseURL, Path: path, Params: params, Query: query})
}

// Post performs an authenticated POST of a JSON body and returns the raw JSON body.
func (c *Client) Post(ctx context.Context, caller Caller, baseURL, path string, body any, params ...string) (json.RawMessage, error) {
	return c.sendJSON(ctx, caller, http.MethodPost, baseURL, path, body, params)
}

// Put performs an authenticated PUT of a JSON body and returns the raw JSON body.
func (c *Client) Put(ctx context.Context, caller Caller, baseURL, path string, body any, params ...string) (json.RawMessage, error) {
	return c.sendJSON(ctx, caller, http.MethodPut, baseURL, path, body, params)
}

// Patch performs an authenticated PATCH of a JSON body and returns the raw JSON body.
func (c *Client) Patch(ctx context.Context, caller Caller, baseURL, path string, body any, params ...string) (json.RawMessage, error) {
	return c.sendJSON(ctx, caller, http.MethodPatch, baseURL, path, body, params)
}

// Delete performs an authenticated DELETE and returns the raw JSON body (or a
// synthetic {"status":"ok"} for a 204 No Content).
func (c *Client) Delete(ctx context.Context, caller Caller, baseURL, path string, query url.Values, params ...string) (json.RawMessage, error) {
	return c.send(ctx, caller, httpclient.Request{Method: http.MethodDelete, BaseURL: baseURL, Path: path, Params: params, Query: query})
}

func (c *Client) sendJSON(ctx context.Context, caller Caller, method, baseURL, path string, body any, params []string) (json.RawMessage, error) {
	r, err := c.EncodeJSON(body)
	if err != nil {
		return nil, err
	}
	return c.send(ctx, caller, httpclient.Request{
		Method: method, BaseURL: baseURL, Path: path, Params: params, Body: r,
		Header: http.Header{"Content-Type": {"application/json"}},
	})
}

func (c *Client) send(ctx context.Context, caller Caller, req httpclient.Request) (json.RawMessage, error) {
	if req.Header == nil {
		req.Header = http.Header{}
	}
	if caller.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+caller.Bearer)
	}
	if caller.Delegation != "" {
		req.Header.Set(mw.DelegationTokenHeader, caller.Delegation)
	}
	// This is a JSON client: always ask for JSON. Lets content-negotiated routes
	// (e.g. the agents run-events endpoint, which is SSE by default) serve us JSON.
	req.Header.Set("Accept", "application/json")
	_, span, resp, err := c.Do(ctx, req)
	defer span.End()
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, c.HandleErrorResponse(span, resp, ErrUnauthorized)
	case resp.StatusCode == http.StatusForbidden:
		forbidden := &ForbiddenError{Message: upstreamMessage(c.ReadErrorBody(span, resp))}
		_ = c.RecordError(span, ErrForbidden)
		return nil, forbidden
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
