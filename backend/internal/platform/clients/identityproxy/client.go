// Package identityproxy is the session-manager's client for the identity-proxy
// sidecar in its own pod. The sidecar's health server listens on loopback port 9090.
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

// ErrNoToken is returned while identity-proxy holds no platform token.
var ErrNoToken = errors.New("identity-proxy holds no platform token")

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
	if resp.StatusCode != http.StatusOK {
		return c.HandleErrorResponse(span, resp, ErrNoToken)
	}
	return nil
}
