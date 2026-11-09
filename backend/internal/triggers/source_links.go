package triggers

import (
	"encoding/json"
	"net/url"
	"strings"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
)

// metaSourceLinks is the reserved meta key that carries a delivery's run source
// links, JSON-encoded, from fanOut to the worker that starts or joins the run.
// It is not a catalog field: no trigger template or condition reads it.
const metaSourceLinks = "meta.source_links"

// Run source kinds, as kora-agents stores them.
const (
	sourceKindTrigger     = "trigger"
	sourceKindPullRequest = "pull_request"
	sourceKindRepository  = "repository"
	sourceKindBranch      = "branch"
)

// sourceLinker is implemented by adapters that can point at what a delivery
// came from. doc is the document triggers read: the decoded payload, or the
// chat document for a chat adapter.
type sourceLinker interface {
	// SourceLinks returns the links the delivery names, or nil when it names none.
	SourceLinks(eventType string, doc any) []agentsclient.RunSource
}

// sourceLinks asks the adapter for a delivery's links.
func sourceLinks(adapter SourceAdapter, eventType string, doc any) []agentsclient.RunSource {
	sl, ok := adapter.(sourceLinker)
	if !ok || doc == nil {
		return nil
	}
	return sl.SourceLinks(eventType, doc)
}

// encodeSourceLinks returns links as the meta value, or "" when there are none.
func encodeSourceLinks(links []agentsclient.RunSource) string {
	if len(links) == 0 {
		return ""
	}
	b, err := json.Marshal(links)
	if err != nil {
		return ""
	}
	return string(b)
}

// sourcesFromFields decodes the links fanOut stored in a delivery's fields.
// Missing or malformed values yield none: a run never fails over its links.
func sourcesFromFields(fields map[string]string) []agentsclient.RunSource {
	raw := fields[metaSourceLinks]
	if raw == "" {
		return nil
	}
	var links []agentsclient.RunSource
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		return nil
	}
	return links
}

// SourceLinks links the Slack thread the message belongs to. It opens the
// thread, not a specific reply; a top-level message opens the thread rooted at
// it. DMs use the same form. doc is the chat document; with no workspace id
// there is no link.
func (a *slackAdapter) SourceLinks(_ string, doc any) []agentsclient.RunSource {
	workspace := resolvePath(doc, "workspace.id")
	conversation := resolvePath(doc, "conversation.id")
	thread := resolvePath(doc, "thread.id")
	if workspace == "" || conversation == "" || thread == "" {
		return nil
	}
	return []agentsclient.RunSource{{
		Kind: sourceKindTrigger,
		URL: "https://app.slack.com/client/" + url.PathEscape(workspace) + "/" + url.PathEscape(conversation) +
			"/thread/" + url.PathEscape(conversation+"-"+thread),
		Label: "Slack thread",
	}}
}

// SourceLinks links the comment, pull request or issue the event is about, plus
// the pull request, repository and branch when the payload names them.
func (a *githubAdapter) SourceLinks(_ string, doc any) []agentsclient.RunSource {
	var out []agentsclient.RunSource
	prURL := resolvePath(doc, "pull_request.html_url")
	repoURL := resolvePath(doc, "repository.html_url")

	switch {
	case resolvePath(doc, "comment.html_url") != "":
		out = append(out, agentsclient.RunSource{Kind: sourceKindTrigger, URL: resolvePath(doc, "comment.html_url"), Label: "GitHub comment"})
	case prURL != "":
		out = append(out, agentsclient.RunSource{Kind: sourceKindTrigger, URL: prURL, Label: "GitHub pull request"})
	case resolvePath(doc, "issue.html_url") != "":
		out = append(out, agentsclient.RunSource{Kind: sourceKindTrigger, URL: resolvePath(doc, "issue.html_url"), Label: "GitHub issue"})
	}
	if prURL != "" {
		label := "Pull request"
		if n := resolvePath(doc, "pull_request.number"); n != "" {
			label = "PR #" + n
		}
		out = append(out, agentsclient.RunSource{Kind: sourceKindPullRequest, URL: prURL, Label: label})
	}
	if repoURL != "" {
		label := resolvePath(doc, "repository.full_name")
		if label == "" {
			label = "Repository"
		}
		out = append(out, agentsclient.RunSource{Kind: sourceKindRepository, URL: repoURL, Label: label})
		if ref := resolvePath(doc, "pull_request.head.ref"); ref != "" {
			out = append(out, agentsclient.RunSource{
				Kind:  sourceKindBranch,
				URL:   strings.TrimRight(repoURL, "/") + "/tree/" + escapeRefPath(ref),
				Label: "branch " + ref,
			})
		}
	}
	return out
}

// escapeRefPath escapes each segment of a git ref, keeping the slashes.
func escapeRefPath(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// SourceLinks links the alert's generator, else the Alertmanager itself.
func (a *alertmanagerAdapter) SourceLinks(_ string, doc any) []agentsclient.RunSource {
	link := resolvePath(doc, "alerts.0.generatorURL")
	if link == "" {
		link = resolvePath(doc, "externalURL")
	}
	if link == "" {
		return nil
	}
	return []agentsclient.RunSource{{Kind: sourceKindTrigger, URL: link, Label: "Alert"}}
}

// addSourceLinks records the delivery's links in meta, when it has any.
func addSourceLinks(meta map[string]string, adapter SourceAdapter, eventType string, doc any) {
	if encoded := encodeSourceLinks(sourceLinks(adapter, eventType, doc)); encoded != "" {
		meta[metaSourceLinks] = encoded
	}
}
