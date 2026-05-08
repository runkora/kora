package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Notifier delivers events to orbit-agents for live SSE fan-out.
// Implementations are best-effort — errors are logged but never fatal.
type Notifier interface {
	Notify(ctx context.Context, runID string, ev Event) error
}

// NoopNotifier silently discards all events.
// Used when ORBIT_AGENTS_URL is not configured (local dev without orbit-agents).
type NoopNotifier struct{}

func (NoopNotifier) Notify(_ context.Context, _ string, _ Event) error { return nil }

// HTTPNotifier POSTs events to orbit-agents via HTTP.
// Each call is fire-and-forget with a short timeout so a slow or absent
// orbit-agents instance does not block the session-manager drain loop.
type HTTPNotifier struct {
	baseURL string
	client  *http.Client
}

// NewHTTPNotifier creates a notifier that pushes to the given orbit-agents base URL.
func NewHTTPNotifier(baseURL string) *HTTPNotifier {
	return &HTTPNotifier{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 2 * time.Second},
	}
}

func (n *HTTPNotifier) Notify(ctx context.Context, runID string, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	url := fmt.Sprintf("%s/internal/runs/%s/events", n.baseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("post event: %w", err)
	}
	resp.Body.Close()
	return nil
}
