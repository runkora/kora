package agents

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// RunSourceKind says what a RunSource link points at.
type RunSourceKind string

const (
	RunSourceTrigger     RunSourceKind = "trigger"
	RunSourcePullRequest RunSourceKind = "pull_request"
	RunSourceRepository  RunSourceKind = "repository"
	RunSourceBranch      RunSourceKind = "branch"
)

// RunSource is a link to something that started or joined a run: the event that
// triggered it, or the pull request, repository or branch the event names.
// See docs/adr/2026-11-07-run-sources.md.
type RunSource struct {
	Kind  RunSourceKind `json:"kind"`
	URL   string        `json:"url"`
	Label string        `json:"label"`
	// Provider names the system the link points into, so the UI can show its icon.
	Provider RunSourceProvider `json:"provider,omitempty"`
}

// RunSourceProvider is the system a RunSource link points into. Empty when unknown.
type RunSourceProvider string

const (
	RunSourceProviderGitHub       RunSourceProvider = "github"
	RunSourceProviderSlack        RunSourceProvider = "slack"
	RunSourceProviderAlertmanager RunSourceProvider = "alertmanager"
)

// Limits on the sources one request may carry.
const (
	maxRunSourcesPerRequest = 10
	// maxRunSourcesPerRun bounds runs.sources; later joins past it are dropped.
	maxRunSourcesPerRun    = 50
	maxRunSourceLabelRunes = 100
	maxRunSourceURLBytes   = 2048
)

var (
	// ErrRunSourcesRefused is returned when a caller that is not a SERVICE principal sends sources.
	ErrRunSourcesRefused = errors.New("only a service principal may record run sources")
	// ErrRunSourcesInvalid is returned when a source is malformed or over a limit.
	ErrRunSourcesInvalid = errors.New("invalid run sources")
)

// authorizeRunSources validates the sources of a start or join request. An empty
// list is always allowed. Sources are an audit record of why a run exists, so
// only a SERVICE principal may write them, like trigger attribution. The result
// is de-duplicated by URL.
func authorizeRunSources(ctx context.Context, sources []RunSource) ([]RunSource, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil || claims.Kora.PrincipalType != token.PrincipalTypeService {
		return nil, ErrRunSourcesRefused
	}
	if len(sources) > maxRunSourcesPerRequest {
		return nil, fmt.Errorf("%w: at most %d sources per request", ErrRunSourcesInvalid, maxRunSourcesPerRequest)
	}
	for i, s := range sources {
		switch s.Kind {
		case RunSourceTrigger, RunSourcePullRequest, RunSourceRepository, RunSourceBranch:
		default:
			return nil, fmt.Errorf("%w: source %d has unknown kind %q", ErrRunSourcesInvalid, i, s.Kind)
		}
		switch s.Provider {
		case "", RunSourceProviderGitHub, RunSourceProviderSlack, RunSourceProviderAlertmanager:
		default:
			return nil, fmt.Errorf("%w: source %d has unknown provider %q", ErrRunSourcesInvalid, i, s.Provider)
		}
		if strings.TrimSpace(s.Label) == "" || utf8.RuneCountInString(s.Label) > maxRunSourceLabelRunes {
			return nil, fmt.Errorf("%w: source %d label must be 1 to %d characters", ErrRunSourcesInvalid, i, maxRunSourceLabelRunes)
		}
		if len(s.URL) > maxRunSourceURLBytes {
			return nil, fmt.Errorf("%w: source %d url is too long", ErrRunSourcesInvalid, i)
		}
		u, err := url.Parse(s.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, fmt.Errorf("%w: source %d url must be an absolute https url", ErrRunSourcesInvalid, i)
		}
	}
	return mergeRunSources(nil, sources), nil
}

// mergeRunSources appends add to existing, skipping any source whose URL is
// already present. The result is never nil.
func mergeRunSources(existing, add []RunSource) []RunSource {
	out := make([]RunSource, 0, len(existing)+len(add))
	seen := make(map[string]bool, len(existing)+len(add))
	for _, s := range existing {
		if !seen[s.URL] {
			seen[s.URL] = true
			out = append(out, s)
		}
	}
	for _, s := range add {
		if !seen[s.URL] {
			seen[s.URL] = true
			out = append(out, s)
		}
	}
	return out
}
