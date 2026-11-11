package triggers

import (
	"context"
	"encoding/json"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// metaEventSummary is the reserved meta key that carries a fire's event
// summary, JSON-encoded, to the worker that starts or joins the run. Like
// metaSourceLinks it is not a catalog field. See docs/adr/2026-11-11-run-page-record.md.
const metaEventSummary = "meta.event_summary"

// Limits kora-agents enforces on a summary. A summary is clamped to them here:
// a long comment shortens its quote and does not fail the run.
const (
	maxSummaryStringRunes = 300
	maxSummaryQuoteRunes  = 2000
	maxSummaryFacts       = 6
)

// Actor kinds and status tones, as kora-agents stores them.
const (
	actorPerson = "person"
	actorBot    = "bot"
	actorSystem = "system"
	toneOK      = "ok"
	toneBad     = "bad"
)

// eventSummarizer is implemented by adapters that can describe a delivery.
// doc is the document triggers read: the decoded payload, or the chat document
// for a chat adapter.
type eventSummarizer interface {
	// EventSummary returns the delivery's summary, or nil for an event type it does not describe.
	EventSummary(eventType string, doc any) *agentsclient.RunEventSummary
}

// addEventSummary records the delivery's summary in meta, when the adapter has one.
func addEventSummary(meta map[string]string, adapter SourceAdapter, eventType string, doc any) {
	es, ok := adapter.(eventSummarizer)
	if !ok || doc == nil {
		return
	}
	setEventSummary(meta, es.EventSummary(eventType, doc))
}

// setEventSummary stores e, clamped to the limits, in fields. A nil summary stores nothing.
func setEventSummary(fields map[string]string, e *agentsclient.RunEventSummary) {
	if e == nil {
		return
	}
	clampSummary(e)
	if b, err := json.Marshal(e); err == nil {
		fields[metaEventSummary] = string(b)
	}
}

// eventFromFields decodes the summary stored in a fire's fields. A missing or
// malformed value yields none: a run never fails over its summary.
func eventFromFields(fields map[string]string) *agentsclient.RunEventSummary {
	raw := fields[metaEventSummary]
	if raw == "" {
		return nil
	}
	var e agentsclient.RunEventSummary
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return nil
	}
	return &e
}

func clampSummary(e *agentsclient.RunEventSummary) {
	for _, s := range []*string{&e.Actor.Name, &e.Verb, &e.Object.Label} {
		*s = clampRunes(*s, maxSummaryStringRunes)
	}
	e.Quote = clampRunes(e.Quote, maxSummaryQuoteRunes)
	// A link that would be cut is dropped: half a URL points somewhere else.
	for _, s := range []*string{&e.Actor.URL, &e.Object.URL} {
		if utf8.RuneCountInString(*s) > maxSummaryStringRunes || !isWebURL(*s) {
			*s = ""
		}
	}
	if p := e.Preview; p != nil {
		p.Title = clampRunes(p.Title, maxSummaryStringRunes)
		p.Facts = p.Facts[:min(len(p.Facts), maxSummaryFacts)]
		for i := range p.Facts {
			p.Facts[i] = clampRunes(p.Facts[i], maxSummaryStringRunes)
		}
		if p.Status != nil {
			p.Status.Label = clampRunes(p.Status.Label, maxSummaryStringRunes)
		}
	}
}

// clampRunes cuts s to at most n runes, ending in an ellipsis when cut.
func clampRunes(s string, n int) string {
	s = cleanText(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func isWebURL(s string) bool {
	if s == "" {
		return true
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// words turns an identifier such as review_requested into "review requested".
func words(s string) string { return strings.ReplaceAll(s, "_", " ") }

// capitalized is s with its first letter upper-cased.
func capitalized(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// intPtr parses a payload number, nil when the payload has none.
func intPtr(s string) *int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}

// ─── GitHub ──────────────────────────────────────────────────────────────────

// EventSummary describes pull request, issue, comment, review and workflow run deliveries.
func (a *githubAdapter) EventSummary(eventType string, doc any) *agentsclient.RunEventSummary {
	action := resolvePath(doc, "action")
	switch eventType {
	case "pull_request":
		verb := map[string]string{
			"review_requested": "requested a review on",
			"ready_for_review": "marked ready for review",
			"synchronize":      "pushed to",
		}[action]
		if action == "closed" && resolvePath(doc, "pull_request.merged") == "true" {
			verb = "merged"
		}
		return githubSummary(doc, verbOr(verb, action), githubPR(doc, "pull_request.html_url"), "", githubPRPreview(doc))
	case "issues":
		quote := ""
		if action == "opened" {
			quote = resolvePath(doc, "issue.body")
		}
		return githubSummary(doc, verbOr("", action), githubIssue(doc, "issue.html_url"), quote, githubIssuePreview(doc))
	case "issue_comment":
		if action != "created" {
			return nil
		}
		object := githubIssue(doc, "comment.html_url")
		if resolvePath(doc, "issue.pull_request.url") != "" {
			object.Label = "PR #" + resolvePath(doc, "issue.number")
		}
		// A comment on a pull request carries no branch or diff. Its preview is the issue view.
		return githubSummary(doc, "commented on", object, resolvePath(doc, "comment.body"), githubIssuePreview(doc))
	case "pull_request_review":
		if action != "submitted" {
			return nil
		}
		preview := githubPRPreview(doc)
		verb := "reviewed"
		switch resolvePath(doc, "review.state") {
		case "approved":
			verb, preview.Status = "approved", &agentsclient.RunEventStatus{Label: "Approved", Tone: toneOK}
		case "changes_requested":
			verb, preview.Status = "requested changes on", &agentsclient.RunEventStatus{Label: "Changes requested", Tone: toneBad}
		}
		return githubSummary(doc, verb, githubPR(doc, "review.html_url"), resolvePath(doc, "review.body"), preview)
	case "pull_request_review_comment":
		if action != "created" {
			return nil
		}
		verb := "commented on"
		if path := resolvePath(doc, "comment.path"); path != "" {
			if line := resolvePath(doc, "comment.line"); line != "" {
				path += ":" + line
			}
			verb += " " + path + " in"
		}
		return githubSummary(doc, verb, githubPR(doc, "comment.html_url"), resolvePath(doc, "comment.body"), githubPRPreview(doc))
	case "workflow_run":
		return githubWorkflowRun(doc, action)
	}
	return nil
}

// verbOr is verb, else the action in words, else "updated".
func verbOr(verb, action string) string {
	switch {
	case verb != "":
		return verb
	case action != "":
		return words(action)
	}
	return "updated"
}

func githubSummary(doc any, verb string, object agentsclient.RunEventObject, quote string, preview *agentsclient.RunEventPreview) *agentsclient.RunEventSummary {
	login := resolvePath(doc, "sender.login")
	if login == "" {
		return nil
	}
	kind := actorPerson
	if resolvePath(doc, "sender.type") == "Bot" {
		kind = actorBot
	}
	return &agentsclient.RunEventSummary{
		Actor:   agentsclient.RunEventActor{Name: login, Kind: kind, URL: resolvePath(doc, "sender.html_url")},
		Verb:    verb,
		Object:  object,
		Quote:   quote,
		Preview: preview,
	}
}

// githubPR is the pull request as the object, linked at urlPath.
func githubPR(doc any, urlPath string) agentsclient.RunEventObject {
	return agentsclient.RunEventObject{Label: "PR #" + resolvePath(doc, "pull_request.number"), URL: resolvePath(doc, urlPath)}
}

// githubIssue is the issue as the object, linked at urlPath.
func githubIssue(doc any, urlPath string) agentsclient.RunEventObject {
	return agentsclient.RunEventObject{Label: "issue #" + resolvePath(doc, "issue.number"), URL: resolvePath(doc, urlPath)}
}

// githubPRPreview is the pull request's title, repository, branches, diff and state.
// Review payloads carry no diff. Added and removed stay nil there.
func githubPRPreview(doc any) *agentsclient.RunEventPreview {
	p := &agentsclient.RunEventPreview{
		Title:   resolvePath(doc, "pull_request.title"),
		Facts:   []string{resolvePath(doc, "repository.full_name") + " #" + resolvePath(doc, "pull_request.number")},
		Added:   intPtr(resolvePath(doc, "pull_request.additions")),
		Removed: intPtr(resolvePath(doc, "pull_request.deletions")),
	}
	if head, base := resolvePath(doc, "pull_request.head.ref"), resolvePath(doc, "pull_request.base.ref"); head != "" && base != "" {
		p.Facts = append(p.Facts, head+" → "+base)
	}
	switch {
	case resolvePath(doc, "pull_request.merged") == "true":
		p.Status = &agentsclient.RunEventStatus{Label: "Merged", Tone: toneOK}
	case resolvePath(doc, "pull_request.state") == "closed":
		p.Status = &agentsclient.RunEventStatus{Label: "Closed", Tone: toneBad}
	case resolvePath(doc, "pull_request.draft") == "true":
		p.Status = &agentsclient.RunEventStatus{Label: "Draft", Tone: toneOK}
	case resolvePath(doc, "pull_request.state") == "open":
		p.Status = &agentsclient.RunEventStatus{Label: "Open", Tone: toneOK}
	}
	return p
}

// githubIssuePreview is the issue's title, repository, labels and state.
func githubIssuePreview(doc any) *agentsclient.RunEventPreview {
	p := &agentsclient.RunEventPreview{
		Title: resolvePath(doc, "issue.title"),
		Facts: []string{resolvePath(doc, "repository.full_name") + " #" + resolvePath(doc, "issue.number")},
	}
	// Only an issue lists its labels. A pull request's preview leaves them out.
	if resolvePath(doc, "issue.pull_request.url") == "" {
		if labels, _ := resolveNode(doc, "issue.labels").([]any); len(labels) > 0 {
			names := make([]string, 0, len(labels))
			for i := range labels {
				names = append(names, resolvePath(labels, strconv.Itoa(i)+".name"))
			}
			p.Facts = append(p.Facts, strings.Join(names, ", "))
		}
	}
	switch resolvePath(doc, "issue.state") {
	case "open":
		p.Status = &agentsclient.RunEventStatus{Label: "Open", Tone: toneOK}
	case "closed":
		p.Status = &agentsclient.RunEventStatus{Label: "Closed", Tone: toneBad}
	}
	return p
}

// githubWorkflowRun describes a workflow run by GitHub Actions on its pull request, or else its branch.
func githubWorkflowRun(doc any, action string) *agentsclient.RunEventSummary {
	name := resolvePath(doc, "workflow_run.name")
	repo := resolvePath(doc, "repository.html_url")
	branch := resolvePath(doc, "workflow_run.head_branch")
	if name == "" || repo == "" {
		return nil
	}
	object := agentsclient.RunEventObject{Label: branch, URL: strings.TrimRight(repo, "/") + "/tree/" + escapeRefPath(branch)}
	if n := resolvePath(doc, "workflow_run.pull_requests.0.number"); n != "" {
		object = agentsclient.RunEventObject{Label: "PR #" + n, URL: strings.TrimRight(repo, "/") + "/pull/" + n}
	}
	if object.Label == "" {
		object = agentsclient.RunEventObject{Label: resolvePath(doc, "repository.full_name"), URL: repo}
	}
	verb := "started " + name + " on"
	var status *agentsclient.RunEventStatus
	if action == "completed" {
		conclusion := resolvePath(doc, "workflow_run.conclusion")
		switch conclusion {
		case "success":
			verb, status = "passed "+name+" on", &agentsclient.RunEventStatus{Label: "Passed", Tone: toneOK}
		case "failure", "timed_out", "startup_failure":
			verb, status = "failed "+name+" on", &agentsclient.RunEventStatus{Label: "Failed", Tone: toneBad}
		case "":
			verb = "finished " + name + " on"
		default:
			tone := toneBad
			if conclusion == "neutral" || conclusion == "skipped" {
				tone = toneOK
			}
			verb, status = "finished "+name+" on", &agentsclient.RunEventStatus{Label: capitalized(words(conclusion)), Tone: tone}
		}
	}
	facts := []string{resolvePath(doc, "repository.full_name")}
	if branch != "" {
		facts = append(facts, branch)
	}
	return &agentsclient.RunEventSummary{
		Actor:  agentsclient.RunEventActor{Name: "GitHub Actions", Kind: actorBot},
		Verb:   verb,
		Object: object,
		Preview: &agentsclient.RunEventPreview{
			Title:  name + " · run " + resolvePath(doc, "workflow_run.run_number"),
			Facts:  facts,
			Status: status,
		},
	}
}

// ─── Slack ───────────────────────────────────────────────────────────────────

// EventSummary describes a message or a thread reply. doc is the chat
// document, which names the user by id only.
func (a *slackAdapter) EventSummary(_ string, doc any) *agentsclient.RunEventSummary {
	user := resolvePath(doc, "user.id")
	if user == "" {
		return nil
	}
	verb := "wrote in"
	if resolvePath(doc, "message.id") != resolvePath(doc, "thread.id") {
		verb = "replied in"
	}
	object := agentsclient.RunEventObject{Label: "Slack thread"}
	if links := a.SourceLinks("", doc); len(links) > 0 {
		object.URL = links[0].URL
	}
	return &agentsclient.RunEventSummary{
		Actor:  agentsclient.RunEventActor{Name: user, Kind: actorPerson},
		Verb:   verb,
		Object: object,
		Quote:  resolvePath(doc, "text"),
	}
}

// ─── Alertmanager ────────────────────────────────────────────────────────────

// EventSummary describes a firing or a resolved notification.
func (a *alertmanagerAdapter) EventSummary(eventType string, doc any) *agentsclient.RunEventSummary {
	var verb string
	var status *agentsclient.RunEventStatus
	switch eventType {
	case "alert.firing":
		verb, status = "fired", &agentsclient.RunEventStatus{Label: "Firing", Tone: toneBad}
	case "alert.resolved":
		verb, status = "resolved", &agentsclient.RunEventStatus{Label: "Resolved", Tone: toneOK}
	default:
		return nil
	}
	name := resolvePath(doc, "commonLabels.alertname")
	if name == "" {
		name = resolvePath(doc, "alerts.0.labels.alertname")
	}
	if name == "" {
		return nil
	}
	title := resolvePath(doc, "commonAnnotations.summary")
	if title == "" {
		title = name
	}
	var facts []string
	if severity := resolvePath(doc, "commonLabels.severity"); severity != "" {
		facts = append(facts, severity)
	}
	if alerts, _ := resolveNode(doc, "alerts").([]any); len(alerts) > 1 {
		facts = append(facts, strconv.Itoa(len(alerts))+" alerts")
	}
	return &agentsclient.RunEventSummary{
		Actor:   agentsclient.RunEventActor{Name: "Alertmanager", Kind: actorSystem},
		Verb:    verb,
		Object:  agentsclient.RunEventObject{Label: name, URL: resolvePath(doc, "alerts.0.generatorURL")},
		Preview: &agentsclient.RunEventPreview{Title: title, Facts: facts, Status: status},
	}
}

// ─── Manual, scheduled and platform event fires ─────────────────────────────

// manualSummary names the person who fired the trigger and the inputs they gave.
func manualSummary(ctx context.Context, t Trigger, values map[string]string) *agentsclient.RunEventSummary {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return nil
	}
	var facts []string
	for _, k := range slices.Sorted(maps.Keys(values)) {
		if values[k] != "" {
			facts = append(facts, k+" "+values[k])
		}
	}
	kind := actorPerson
	if claims.Kora.PrincipalType == token.PrincipalTypeAgent {
		kind = actorBot
	}
	return &agentsclient.RunEventSummary{
		Actor:   agentsclient.RunEventActor{Name: events.PrincipalNameOf(claims), Kind: kind},
		Verb:    "fired",
		Object:  agentsclient.RunEventObject{Label: t.Name},
		Preview: &agentsclient.RunEventPreview{Title: t.Name, Facts: facts},
	}
}

// scheduledSummary names the schedule that fired the trigger.
func scheduledSummary(t Trigger) *agentsclient.RunEventSummary {
	if t.ScheduleConfig == nil {
		return nil
	}
	var facts []string
	if tz := t.ScheduleConfig.Timezone; tz != "" {
		facts = append(facts, tz)
	}
	return &agentsclient.RunEventSummary{
		Actor:   agentsclient.RunEventActor{Name: "Schedule", Kind: actorSystem},
		Verb:    "fired",
		Object:  agentsclient.RunEventObject{Label: t.Name},
		Preview: &agentsclient.RunEventPreview{Title: t.ScheduleConfig.CronExpression, Facts: facts},
	}
}

// platformEventSummary names the principal behind a platform event and the
// entity it changed: a run with its agent and state, or an agent.
func platformEventSummary(head eventHead, doc any) *agentsclient.RunEventSummary {
	parts := strings.Split(strings.TrimSuffix(head.Type, ".v1"), ".")
	if len(parts) < 3 {
		return nil
	}
	entity, action := parts[len(parts)-2], parts[len(parts)-1]
	kind := actorSystem
	switch head.PrincipalType {
	case events.PrincipalUser:
		kind = actorPerson
	case events.PrincipalAgent:
		kind = actorBot
	}
	name := head.PrincipalName
	if name == "" {
		name = "Platform"
	}
	e := &agentsclient.RunEventSummary{
		Actor:  agentsclient.RunEventActor{Name: name, Kind: kind},
		Verb:   words(action) + " " + entity,
		Object: agentsclient.RunEventObject{Label: resolvePath(doc, "subject")},
	}
	id := resolvePath(doc, "data.entity.id")
	switch entity {
	case "run":
		short := id[:min(len(id), 8)]
		e.Object.Label = short
		e.Preview = &agentsclient.RunEventPreview{Title: resolvePath(doc, "data.entity.agent") + " · run " + short}
		if state := resolvePath(doc, "data.entity.state"); state != "" {
			tone := toneOK
			if state == "failed" || state == "budget_exhausted" || state == "terminated" {
				tone = toneBad
			}
			e.Preview.Status = &agentsclient.RunEventStatus{Label: capitalized(words(state)), Tone: tone}
		}
	case "agent":
		if n := resolvePath(doc, "data.entity.name"); n != "" {
			e.Object.Label = n
		}
	}
	if e.Object.Label == "" {
		e.Object.Label = id
	}
	if e.Object.Label == "" {
		return nil
	}
	return e
}
