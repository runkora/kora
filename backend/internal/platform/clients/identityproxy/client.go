// Package identityproxy is the session-manager's client for the identity-proxy
// sidecar in its own pod. The client calls the sidecar's health server on loopback port 9090.
package identityproxy

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	tracerName    = "github.com/runkora/kora/backend/internal/platform/clients/identityproxy"
	clientTimeout = 2 * time.Second

	// LocalURL is the identity-proxy health server in the same pod.
	LocalURL = "http://127.0.0.1:9090"
)

var (
	// ErrNoToken is returned while identity-proxy holds no platform token.
	ErrNoToken = errors.New("identity-proxy holds no platform token")
	// ErrUnexpectedStatus is returned for any other status than 200 or 503.
	ErrUnexpectedStatus = errors.New("unexpected status from identity-proxy")
)

// Client calls the identity-proxy health server.
type Client struct {
	*httpclient.Base
	baseURL string
}

// NewClient creates a Client for the identity-proxy at baseURL.
func NewClient(logger logging.Logger, baseURL string) *Client {
	return &Client{Base: httpclient.New(tracerName, logger, clientTimeout), baseURL: baseURL}
}

// TokenReady returns nil while identity-proxy holds a platform token and
// ErrNoToken while it holds none.
func (c *Client) TokenReady(ctx context.Context) error {
	_, span, resp, err := c.Do(ctx, httpclient.Request{Method: http.MethodGet, BaseURL: c.baseURL, Path: "/tokenz"})
	defer span.End()
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// A 503 is the expected answer until the token lands. It does not fail the span.
	if resp.StatusCode == http.StatusServiceUnavailable {
		return ErrNoToken
	}
	if resp.StatusCode != http.StatusOK {
		return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
	return nil
}
