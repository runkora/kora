package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

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
func (a *githubAdapter) DeliveryID(headers http.Header, _ []byte) string {
	return headers.Get("X-GitHub-Delivery")
}

func (a *githubAdapter) EventType(headers http.Header, _ []byte) string {
	return headers.Get("X-GitHub-Event")
}

// EventTypes lists the GitHub events this adapter models — exactly the cases in
// Fields' switch below, and nothing else.
//
// GitHub emits dozens more (release, workflow_run, deployment, …). They are not
// listed because the adapter extracts no fields for them: a trigger on one would
// see only the common set (event, action, repo.*, sender.login), and advertising
// it here would promise a richness that is not there. A source that starts
// sending one shows up under 'observed', which is the honest place for "this
// arrives but is not modeled yet" and the signal that the adapter should grow a
// case for it.
func (a *githubAdapter) EventTypes() []string {
	return []string{"issues", "pull_request", "push"}
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
