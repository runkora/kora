package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// SourceAdapter is the per-source plug-in that makes the inbound pipeline generic.
// Each provider (GitHub, and later GitLab/generic HMAC) implements it. The
// pipeline (verify → match → template → dispatch → log) is adapter-agnostic.
type SourceAdapter interface {
	// Provider returns the adapter's stable identifier (e.g. "github").
	Provider() string
	// Verify checks the delivery signature against the source's signing secret.
	// Returns nil if the signature is valid, ErrSignatureInvalid otherwise.
	Verify(secret string, headers http.Header, body []byte) error
	// DeliveryID returns the provider's unique delivery id for redelivery dedup,
	// or "" if the provider supplies none.
	DeliveryID(headers http.Header) string
	// EventType returns the canonical event type for the delivery.
	EventType(headers http.Header, body []byte) string
	// Fields parses the payload into a flat, allow-listed map of dotted keys →
	// string values, used for both condition matching and prompt templating.
	Fields(eventType string, body []byte) map[string]string
}

// Registry maps provider id → adapter.
type Registry struct {
	adapters map[string]SourceAdapter
}

// NewRegistry builds the adapter registry with all v1 adapters registered.
func NewRegistry() *Registry {
	r := &Registry{adapters: make(map[string]SourceAdapter)}
	r.register(&githubAdapter{})
	return r
}

func (r *Registry) register(a SourceAdapter) { r.adapters[a.Provider()] = a }

// Get returns the adapter for a provider, or (nil, false) if unregistered.
func (r *Registry) Get(provider string) (SourceAdapter, bool) {
	a, ok := r.adapters[provider]
	return a, ok
}

// Providers returns the registered provider ids (for validation/UI).
func (r *Registry) Providers() []string {
	out := make([]string, 0, len(r.adapters))
	for p := range r.adapters {
		out = append(out, p)
	}
	return out
}

// ─── GitHub adapter ───────────────────────────────────────────────────────────

type githubAdapter struct{}

func (a *githubAdapter) Provider() string { return ProviderGitHub }

// Verify implements GitHub's HMAC-SHA256 scheme: the X-Hub-Signature-256 header
// carries "sha256=<hex>" over the raw request body keyed by the signing secret.
// Comparison is constant-time.
func (a *githubAdapter) Verify(secret string, headers http.Header, body []byte) error {
	sig := headers.Get("X-Hub-Signature-256")
	const prefix = "sha256="
	if !strings.HasPrefix(sig, prefix) {
		return ErrSignatureInvalid
	}
	want, err := hex.DecodeString(strings.TrimPrefix(sig, prefix))
	if err != nil {
		return ErrSignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(want, mac.Sum(nil)) {
		return ErrSignatureInvalid
	}
	return nil
}

// DeliveryID returns GitHub's per-delivery id. Replay protection relies entirely
// on this id (deduped via the partial unique index on trigger_deliveries) — the
// adapter does not inspect any timestamp header, so there is no time-window
// replay defense. A delivery with no id is rejected fail-closed in Service.Ingest
// rather than accepted unprotected. If a provider/requirement ever needs
// timestamp-based anti-replay, it belongs here alongside the id.
func (a *githubAdapter) DeliveryID(headers http.Header) string {
	return headers.Get("X-GitHub-Delivery")
}

func (a *githubAdapter) EventType(headers http.Header, _ []byte) string {
	return headers.Get("X-GitHub-Event")
}

// githubPayload covers the subset of the pull_request, push, and issues event
// payloads exposed to matching and templating. Unknown/absent fields decode to
// zero values.
type githubPayload struct {
	Action string `json:"action"`
	Number int    `json:"number"`
	Ref    string `json:"ref"`
	Before string `json:"before"`
	After  string `json:"after"`
	Issue  *struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"issue"`
	// Label is the top-level label on labeled/unlabeled actions (the one just
	// added/removed), so a trigger can gate on a specific label.
	Label *struct {
		Name string `json:"name"`
	} `json:"label"`
	PullRequest *struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	HeadCommit *struct {
		Message string `json:"message"`
	} `json:"head_commit"`
}

// Fields extracts the allow-listed templating/matching fields for GitHub events.
func (a *githubAdapter) Fields(eventType string, body []byte) map[string]string {
	var p githubPayload
	_ = json.Unmarshal(body, &p) // tolerant: missing fields become empty strings

	f := map[string]string{
		"event":               eventType,
		"action":              p.Action,
		"repo.full_name":      p.Repository.FullName,
		"repo.default_branch": p.Repository.DefaultBranch,
		"sender.login":        p.Sender.Login,
	}

	switch eventType {
	case "pull_request":
		if p.PullRequest != nil {
			f["pr.number"] = strconv.Itoa(p.PullRequest.Number)
			f["pr.title"] = p.PullRequest.Title
			f["pr.url"] = p.PullRequest.HTMLURL
			f["pr.head_ref"] = p.PullRequest.Head.Ref
			f["pr.head_sha"] = p.PullRequest.Head.SHA
			f["pr.base_ref"] = p.PullRequest.Base.Ref
			f["pr.author"] = p.PullRequest.User.Login
		} else if p.Number != 0 {
			f["pr.number"] = strconv.Itoa(p.Number)
		}
	case "push":
		f["push.ref"] = p.Ref
		f["push.branch"] = strings.TrimPrefix(p.Ref, "refs/heads/")
		f["push.before"] = p.Before
		f["push.after"] = p.After
		if p.HeadCommit != nil {
			f["push.commit_message"] = p.HeadCommit.Message
		}
	case "issues":
		// SECURITY: issue.title/issue.body are attacker-influenceable — any GitHub
		// user can open an issue — and are substituted into prompts verbatim
		// (template.go defers injection hardening platform-wide, as for pr.title /
		// push.commit_message). Treat them as untrusted data: the real boundary is
		// the agent's least-privilege tools + human PR review, not the prompt text.
		if p.Issue != nil {
			f["issue.number"] = strconv.Itoa(p.Issue.Number)
			f["issue.title"] = p.Issue.Title
			f["issue.body"] = p.Issue.Body
			f["issue.url"] = p.Issue.HTMLURL
			f["issue.author"] = p.Issue.User.Login
		}
		// On labeled/unlabeled actions, expose the affected label so triggers can
		// gate on it, e.g. match {"action":["labeled"],"label.name":["bug-smith"]}.
		if p.Label != nil {
			f["label.name"] = p.Label.Name
		}
	}
	return f
}

// matchesCondition reports whether the delivery fields satisfy every clause of a
// trigger's match condition. Each clause is `field → allowed values`; a clause
// is satisfied when the field's value is one of the allowed values. An empty
// condition matches everything. Empty allowed-value lists are ignored.
func matchesCondition(condition map[string][]string, fields map[string]string) bool {
	for key, allowed := range condition {
		if len(allowed) == 0 {
			continue
		}
		if !slices.Contains(allowed, fields[key]) {
			return false
		}
	}
	return true
}

// validateProvider returns nil if the provider has a registered adapter.
func (r *Registry) validateProvider(provider string) error {
	if _, ok := r.Get(provider); !ok {
		return fmt.Errorf("%w: %q (known: %s)", ErrUnknownProvider, provider, strings.Join(r.Providers(), ", "))
	}
	return nil
}
