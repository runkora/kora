package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Notifier delivers events to orbit-agents for live SSE fan-out and run
// lifecycle callbacks.
// Implementations are best-effort — errors are logged but never fatal.
type Notifier interface {
	Notify(ctx context.Context, runID string, ev Event) error
	// CompleteRun transitions a run from active/completing to completed or
	// failed. Called by the session drain loop when the subprocess exits.
	CompleteRun(ctx context.Context, runID string, failed bool) error
	// ProvisionSandbox requests orbit-agents to provision a sandbox pod for the
	// given run and returns its gRPC endpoint ("podIP:port").
	ProvisionSandbox(ctx context.Context, runID string) (string, error)
}

// NoopNotifier silently discards all events.
// Used when ORBIT_AGENTS_URL is not configured (local dev without orbit-agents).
type NoopNotifier struct{}

func (NoopNotifier) Notify(_ context.Context, _ string, _ Event) error            { return nil }
func (NoopNotifier) CompleteRun(_ context.Context, _ string, _ bool) error        { return nil }
func (NoopNotifier) ProvisionSandbox(_ context.Context, _ string) (string, error) { return "", nil }

// HTTPNotifier POSTs events to orbit-agents via HTTP.
// Each call is fire-and-forget with a short timeout so a slow or absent
// orbit-agents instance does not block the session-manager drain loop.
// ProvisionSandbox uses a longer timeout since pod cold-start can take ~60s.
type HTTPNotifier struct {
	baseURL       string
	client        *http.Client
	sandboxClient *http.Client
}

// NewHTTPNotifier creates a notifier that pushes to the given orbit-agents base URL.
func NewHTTPNotifier(baseURL string) *HTTPNotifier {
	return &HTTPNotifier{
		baseURL:       baseURL,
		client:        &http.Client{Timeout: 2 * time.Second},
		sandboxClient: &http.Client{Timeout: 120 * time.Second},
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
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("orbit-agents returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}

func (n *HTTPNotifier) ProvisionSandbox(ctx context.Context, runID string) (string, error) {
	url := fmt.Sprintf("%s/internal/runs/%s/provision-sandbox", n.baseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := n.sandboxClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("post provision-sandbox: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", fmt.Errorf("orbit-agents returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	var result struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.Endpoint, nil
}

func (n *HTTPNotifier) CompleteRun(ctx context.Context, runID string, failed bool) error {
	toState := "completed"
	if failed {
		toState = "failed"
	}
	// The run is always in "active" state when the subprocess exits because the
	// current flow has no intermediate states between active and terminal states.
	// RunStateCompleting is defined for future snapshot support; if that transition
	// is added, from must be updated to match.
	body, err := json.Marshal(map[string]string{"from": "active", "to": toState})
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	url := fmt.Sprintf("%s/internal/runs/%s/state", n.baseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("post state: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("orbit-agents returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}
