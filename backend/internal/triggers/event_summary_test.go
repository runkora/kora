package triggers

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// Trimmed GitHub webhook payloads. Field names and shapes follow the real deliveries.
const (
	ghRepo   = `"repository":{"full_name":"example-org/stats-lib","html_url":"https://github.com/example-org/stats-lib"}`
	ghSender = `"sender":{"login":"octo-reviewer","type":"User","html_url":"https://github.com/octo-reviewer"}`
	ghPR     = `"pull_request":{"number":6,"title":"Mean: handle empty input","html_url":"https://github.com/example-org/stats-lib/pull/6","state":"open","draft":false,"merged":false,"additions":17,"deletions":3,"head":{"ref":"fix/mean-empty"},"base":{"ref":"main"}}`
)

func ghDoc(t *testing.T, parts ...string) any {
	t.Helper()
	doc := decodeBody([]byte("{" + strings.Join(parts, ",") + "}"))
	if doc == nil {
		t.Fatalf("fixture does not decode: %v", parts)
	}
	return doc
}

func prPreview(status *agentsclient.RunEventStatus) *agentsclient.RunEventPreview {
	return &agentsclient.RunEventPreview{
		Title:   "Mean: handle empty input",
		Facts:   []string{"example-org/stats-lib #6", "fix/mean-empty → main"},
		Added:   new(17),
		Removed: new(3),
		Status:  status,
	}
}

var (
	reviewer = agentsclient.RunEventActor{Name: "octo-reviewer", Kind: "person", URL: "https://github.com/octo-reviewer"}
	open     = &agentsclient.RunEventStatus{Label: "Open", Tone: "ok"}
	pr6      = agentsclient.RunEventObject{Label: "PR #6", URL: "https://github.com/example-org/stats-lib/pull/6"}
)

func TestGitHubEventSummary(t *testing.T) {
	a := &githubAdapter{}
	tests := []struct {
		name, eventType string
		doc             any
		want            *agentsclient.RunEventSummary
	}{
		{
			name: "pull request opened", eventType: "pull_request",
			doc:  ghDoc(t, `"action":"opened"`, ghPR, ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{Actor: reviewer, Verb: "opened", Object: pr6, Preview: prPreview(open)},
		},
		{
			name: "review requested", eventType: "pull_request",
			doc:  ghDoc(t, `"action":"review_requested"`, ghPR, ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{Actor: reviewer, Verb: "requested a review on", Object: pr6, Preview: prPreview(open)},
		},
		{
			name: "pull request merged", eventType: "pull_request",
			doc: ghDoc(t, `"action":"closed"`, strings.Replace(strings.Replace(ghPR, `"merged":false`, `"merged":true`, 1), `"state":"open"`, `"state":"closed"`, 1), ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{Actor: reviewer, Verb: "merged", Object: pr6,
				Preview: prPreview(&agentsclient.RunEventStatus{Label: "Merged", Tone: "ok"})},
		},
		{
			name: "pull request closed unmerged", eventType: "pull_request",
			doc: ghDoc(t, `"action":"closed"`, strings.Replace(ghPR, `"state":"open"`, `"state":"closed"`, 1), ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{Actor: reviewer, Verb: "closed", Object: pr6,
				Preview: prPreview(&agentsclient.RunEventStatus{Label: "Closed"})},
		},
		{
			name: "a bot opens a pull request", eventType: "pull_request",
			doc: ghDoc(t, `"action":"opened"`, ghPR, ghRepo, `"sender":{"login":"helper-bot[bot]","type":"Bot","html_url":"https://github.com/apps/helper-bot"}`),
			want: &agentsclient.RunEventSummary{Actor: agentsclient.RunEventActor{Name: "helper-bot[bot]", Kind: "bot", URL: "https://github.com/apps/helper-bot"},
				Verb: "opened", Object: pr6, Preview: prPreview(open)},
		},
		{
			name: "issue opened", eventType: "issues",
			doc: ghDoc(t, `"action":"opened"`, `"issue":{"number":7,"title":"Mean panics on an empty slice","body":"Calling stats.Mean(nil) panics.","html_url":"https://github.com/example-org/stats-lib/issues/7","state":"open","labels":[{"name":"bug"},{"name":"good first issue"}]}`, ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{
				Actor: reviewer, Verb: "opened",
				Object: agentsclient.RunEventObject{Label: "issue #7", URL: "https://github.com/example-org/stats-lib/issues/7"},
				Quote:  "Calling stats.Mean(nil) panics.",
				Preview: &agentsclient.RunEventPreview{Title: "Mean panics on an empty slice",
					Facts: []string{"example-org/stats-lib #7", "bug, good first issue"}, Status: open},
			},
		},
		{
			name: "comment on a pull request", eventType: "issue_comment",
			doc: ghDoc(t, `"action":"created"`, `"issue":{"number":6,"title":"Mean: handle empty input","state":"open","pull_request":{"url":"https://api.github.com/repos/example-org/stats-lib/pulls/6"},"labels":[{"name":"bug"}]}`,
				`"comment":{"body":"@verify-agent should Mean return 0?","html_url":"https://github.com/example-org/stats-lib/pull/6#issuecomment-1"}`, ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{
				Actor: reviewer, Verb: "commented on",
				Object:  agentsclient.RunEventObject{Label: "PR #6", URL: "https://github.com/example-org/stats-lib/pull/6#issuecomment-1"},
				Quote:   "@verify-agent should Mean return 0?",
				Preview: &agentsclient.RunEventPreview{Title: "Mean: handle empty input", Facts: []string{"example-org/stats-lib #6"}, Status: open},
			},
		},
		{
			name: "comment on an issue", eventType: "issue_comment",
			doc: ghDoc(t, `"action":"created"`, `"issue":{"number":7,"title":"Mean panics","state":"closed","labels":[]}`,
				`"comment":{"body":"still broken","html_url":"https://github.com/example-org/stats-lib/issues/7#issuecomment-2"}`, ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{
				Actor: reviewer, Verb: "commented on",
				Object: agentsclient.RunEventObject{Label: "issue #7", URL: "https://github.com/example-org/stats-lib/issues/7#issuecomment-2"},
				Quote:  "still broken",
				Preview: &agentsclient.RunEventPreview{Title: "Mean panics", Facts: []string{"example-org/stats-lib #7"},
					Status: &agentsclient.RunEventStatus{Label: "Closed"}},
			},
		},
		{
			name: "an edited comment is not summarized", eventType: "issue_comment",
			doc: ghDoc(t, `"action":"edited"`, `"issue":{"number":7}`, `"comment":{"body":"x"}`, ghRepo, ghSender),
		},
		{
			name: "review requesting changes", eventType: "pull_request_review",
			doc: ghDoc(t, `"action":"submitted"`, `"review":{"state":"changes_requested","body":"Add a test for a single-element input.","html_url":"https://github.com/example-org/stats-lib/pull/6#pullrequestreview-9"}`,
				`"pull_request":{"number":6,"title":"Mean: handle empty input","state":"open","head":{"ref":"fix/mean-empty"},"base":{"ref":"main"}}`, ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{
				Actor: reviewer, Verb: "requested changes on",
				Object: agentsclient.RunEventObject{Label: "PR #6", URL: "https://github.com/example-org/stats-lib/pull/6#pullrequestreview-9"},
				Quote:  "Add a test for a single-element input.",
				Preview: &agentsclient.RunEventPreview{Title: "Mean: handle empty input", Facts: []string{"example-org/stats-lib #6", "fix/mean-empty → main"},
					Status: &agentsclient.RunEventStatus{Label: "Changes requested", Tone: "bad"}},
			},
		},
		{
			name: "review comment on a line", eventType: "pull_request_review_comment",
			doc: ghDoc(t, `"action":"created"`, `"comment":{"path":"stats/mean.go","line":12,"body":"does this handle a nil slice?","html_url":"https://github.com/example-org/stats-lib/pull/6#discussion_r1"}`, ghPR, ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{
				Actor: reviewer, Verb: "commented on stats/mean.go:12 in",
				Object:  agentsclient.RunEventObject{Label: "PR #6", URL: "https://github.com/example-org/stats-lib/pull/6#discussion_r1"},
				Quote:   "does this handle a nil slice?",
				Preview: prPreview(open),
			},
		},
		{
			name: "workflow run failed on a pull request", eventType: "workflow_run",
			doc: ghDoc(t, `"action":"completed"`, `"workflow_run":{"name":"test","run_number":42,"conclusion":"failure","head_branch":"fix/mean-empty","pull_requests":[{"number":6}]}`, ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{
				Actor: agentsclient.RunEventActor{Name: "GitHub Actions", Kind: "bot"}, Verb: "failed test on", Object: pr6,
				Preview: &agentsclient.RunEventPreview{Title: "test · run 42", Facts: []string{"example-org/stats-lib", "fix/mean-empty"},
					Status: &agentsclient.RunEventStatus{Label: "Failed", Tone: "bad"}},
			},
		},
		{
			name: "workflow run passed on a branch", eventType: "workflow_run",
			doc: ghDoc(t, `"action":"completed"`, `"workflow_run":{"name":"build","run_number":7,"conclusion":"success","head_branch":"main","pull_requests":[]}`, ghRepo, ghSender),
			want: &agentsclient.RunEventSummary{
				Actor: agentsclient.RunEventActor{Name: "GitHub Actions", Kind: "bot"}, Verb: "passed build on",
				Object: agentsclient.RunEventObject{Label: "main", URL: "https://github.com/example-org/stats-lib/tree/main"},
				Preview: &agentsclient.RunEventPreview{Title: "build · run 7", Facts: []string{"example-org/stats-lib", "main"},
					Status: &agentsclient.RunEventStatus{Label: "Passed", Tone: "ok"}},
			},
		},
		{name: "an unknown event type", eventType: "star", doc: ghDoc(t, `"action":"created"`, ghRepo, ghSender)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.EventSummary(tt.eventType, tt.doc)
			if !reflect.DeepEqual(got, tt.want) {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(tt.want)
				t.Errorf("summary =\n %s\nwant\n %s", g, w)
			}
		})
	}
}

func TestSlackEventSummary(t *testing.T) {
	a := &slackAdapter{}
	thread := agentsclient.RunEventObject{Label: "Slack thread", URL: "https://app.slack.com/client/T1/C1/thread/C1-1700000000.000100"}
	tests := []struct {
		name, payload, wantVerb string
	}{
		{"message", `{"team_id":"T1","event":{"type":"app_mention","user":"u1","text":"Can someone help?","ts":"1700000000.000100","channel":"C1"}}`, "wrote in"},
		{"thread reply", `{"team_id":"T1","event":{"type":"app_mention","user":"u1","text":"Can someone help?","ts":"1700000000.000200","thread_ts":"1700000000.000100","channel":"C1"}}`, "replied in"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := &agentsclient.RunEventSummary{
				Actor: agentsclient.RunEventActor{Name: "U1", Kind: "person"}, Verb: tt.wantVerb, Object: thread, Quote: "Can someone help?",
			}
			if got := a.EventSummary("mention", slackChatDoc(t, tt.payload)); !reflect.DeepEqual(got, want) {
				t.Errorf("summary = %+v, want %+v", got, want)
			}
		})
	}
}

func TestAlertmanagerEventSummary(t *testing.T) {
	a := &alertmanagerAdapter{}
	const alerts = `"alerts":[{"labels":{"alertname":"QueueDepthHigh"},"generatorURL":"https://prometheus.example.com/graph?g0.expr=x"},{"labels":{"alertname":"QueueDepthHigh"}}]`
	body := `{"status":"STATUS","commonLabels":{"alertname":"QueueDepthHigh","severity":"critical"},"commonAnnotations":{"summary":"Queue depth above 100"},` + alerts + `}`
	object := agentsclient.RunEventObject{Label: "QueueDepthHigh", URL: "https://prometheus.example.com/graph?g0.expr=x"}
	actor := agentsclient.RunEventActor{Name: "Alertmanager", Kind: "system"}
	tests := []struct {
		name, eventType, status string
		want                    *agentsclient.RunEventSummary
	}{
		{"firing", "alert.firing", "firing", &agentsclient.RunEventSummary{Actor: actor, Verb: "fired", Object: object,
			Preview: &agentsclient.RunEventPreview{Title: "Queue depth above 100", Facts: []string{"critical", "2 alerts"}, Status: &agentsclient.RunEventStatus{Label: "Firing", Tone: "bad"}}}},
		{"resolved", "alert.resolved", "resolved", &agentsclient.RunEventSummary{Actor: actor, Verb: "resolved", Object: object,
			Preview: &agentsclient.RunEventPreview{Title: "Queue depth above 100", Facts: []string{"critical", "2 alerts"}, Status: &agentsclient.RunEventStatus{Label: "Resolved", Tone: "ok"}}}},
		{"unknown", "", "pending", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := decodeBody([]byte(strings.Replace(body, "STATUS", tt.status, 1)))
			got := a.EventSummary(tt.eventType, doc)
			if !reflect.DeepEqual(got, tt.want) {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(tt.want)
				t.Errorf("summary =\n %s\nwant\n %s", g, w)
			}
		})
	}
}

func TestGenericWebhookHasNoSummary(t *testing.T) {
	if _, ok := any(&genericAdapter{}).(eventSummarizer); ok {
		t.Error("the generic webhook adapter summarizes; the page reads its trigger template instead")
	}
}

func TestManualSummary(t *testing.T) {
	ctx := token.WithClaims(context.Background(), &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"},
		Kora:             token.KoraClaims{PrincipalType: token.PrincipalTypeHuman, DisplayName: "Dev User"},
	})
	got := manualSummary(ctx, Trigger{Name: "nightly-check"}, map[string]string{"repo": "example-org/platform", "env": "prod", "empty": ""})
	want := &agentsclient.RunEventSummary{
		Actor: agentsclient.RunEventActor{Name: "Dev User", Kind: "person"}, Verb: "fired", Object: agentsclient.RunEventObject{Label: "nightly-check"},
		Preview: &agentsclient.RunEventPreview{Title: "Inputs", Facts: []string{"env prod", "repo example-org/platform"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
	if got := manualSummary(ctx, Trigger{Name: "nightly-check"}, map[string]string{"empty": ""}); got == nil || got.Preview != nil {
		t.Errorf("summary without inputs = %+v, want no preview", got)
	}
	if manualSummary(context.Background(), Trigger{Name: "x"}, nil) != nil {
		t.Error("a fire without claims names no actor")
	}
}

func TestScheduledSummary(t *testing.T) {
	got := scheduledSummary(Trigger{Name: "weekly-report", ScheduleConfig: &ScheduleConfig{CronExpression: "30 6 * * 2", Timezone: "Europe/Berlin"}})
	want := &agentsclient.RunEventSummary{
		Actor: agentsclient.RunEventActor{Name: "Schedule", Kind: "system"}, Verb: "fired", Object: agentsclient.RunEventObject{Label: "weekly-report"},
		Preview: &agentsclient.RunEventPreview{Title: "30 6 * * 2", Facts: []string{"Europe/Berlin"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
}

func TestPlatformEventSummary(t *testing.T) {
	const envelope = `{"id":"e1","type":"agents.run.failed.v1","subject":"runs/9a8b7c6d-0000","koraprincipaltype":"AGENT","koraprincipalname":"verify-agent",
		"data":{"entity":{"id":"9a8b7c6d-0000-0000-0000-000000000000","agent":"verify-agent","state":"failed"}}}`
	var head eventHead
	if err := json.Unmarshal([]byte(envelope), &head); err != nil {
		t.Fatal(err)
	}
	got := platformEventSummary(head, decodeBody([]byte(envelope)))
	want := &agentsclient.RunEventSummary{
		Actor: agentsclient.RunEventActor{Name: "verify-agent", Kind: "bot"}, Verb: "failed run", Object: agentsclient.RunEventObject{Label: "9a8b7c6d"},
		Preview: &agentsclient.RunEventPreview{Title: "verify-agent · run 9a8b7c6d", Status: &agentsclient.RunEventStatus{Label: "Failed", Tone: "bad"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("summary = %+v, want %+v", got, want)
	}

	const agentEnvelope = `{"id":"e2","type":"agents.agent.updated.v1","subject":"agents/coder","koraprincipaltype":"USER","koraprincipalname":"Dev User","data":{"entity":{"id":"a1","name":"Coder","slug":"coder"}}}`
	if err := json.Unmarshal([]byte(agentEnvelope), &head); err != nil {
		t.Fatal(err)
	}
	got = platformEventSummary(head, decodeBody([]byte(agentEnvelope)))
	want = &agentsclient.RunEventSummary{Actor: agentsclient.RunEventActor{Name: "Dev User", Kind: "person"}, Verb: "updated agent", Object: agentsclient.RunEventObject{Label: "Coder"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("agent summary = %+v, want %+v", got, want)
	}
}

// A summary over kora-agents' limits is clamped, never sent to be refused.
func TestSetEventSummary_ClampsToLimits(t *testing.T) {
	e := &agentsclient.RunEventSummary{
		Actor:   agentsclient.RunEventActor{Name: "a", Kind: "person", URL: "https://example.com/" + strings.Repeat("x", 400)},
		Verb:    strings.Repeat("v", 400),
		Object:  agentsclient.RunEventObject{Label: "o", URL: "javascript:alert(1)"},
		Quote:   strings.Repeat("q", 3000),
		Preview: &agentsclient.RunEventPreview{Title: "t", Facts: []string{"1", "2", "3", "4", "5", "6", "7", strings.Repeat("f", 400)}},
	}
	fields := map[string]string{}
	setEventSummary(fields, e)
	got := eventFromFields(fields)
	switch {
	case got == nil:
		t.Fatal("summary did not round-trip")
	case utf8.RuneCountInString(got.Verb) != 300, utf8.RuneCountInString(got.Quote) != 2000, len(got.Preview.Facts) != 6:
		t.Errorf("summary not clamped: verb %d, quote %d, facts %d", utf8.RuneCountInString(got.Verb), utf8.RuneCountInString(got.Quote), len(got.Preview.Facts))
	case got.Actor.URL != "" || got.Object.URL != "":
		t.Errorf("links = %q %q, want an overlong and a javascript link dropped", got.Actor.URL, got.Object.URL)
	}
	incomplete := map[string]string{}
	setEventSummary(incomplete, &agentsclient.RunEventSummary{Actor: agentsclient.RunEventActor{Name: "GitHub Actions", Kind: "bot"}, Verb: "started ci on"})
	if len(incomplete) != 0 {
		t.Errorf("a summary without an object label was stored: %v", incomplete)
	}
	if eventFromFields(map[string]string{metaEventSummary: "{not json"}) != nil {
		t.Error("a malformed summary decodes")
	}
}

// A GitHub delivery's summary reaches kora-agents on the run start.
func TestFanOut_GitHubStartCarriesEventSummary(t *testing.T) {
	f := newPayloadFixture(t)
	tr := f.trigger(t, CreateTriggerRequest{EventType: "pull_request", PromptTemplate: "Review {{ pull_request.html_url }}"})
	f.dispatch(t, "pull_request", []byte("{"+strings.Join([]string{`"action":"opened"`, ghPR, ghRepo, ghSender}, ",")+"}"))
	if len(f.deps.repo.pendingFor(tr.ID)) != 1 {
		t.Fatal("the delivery did not queue a fire")
	}
	drainPending(t, f.svc, f.deps.repo)

	call, ok := f.deps.agents.lastCall()
	if !ok {
		t.Fatal("no run was started")
	}
	want := &agentsclient.RunEventSummary{Actor: reviewer, Verb: "opened", Object: pr6, Preview: prPreview(open)}
	if !reflect.DeepEqual(call.event, want) {
		t.Errorf("start event = %+v, want %+v", call.event, want)
	}
}

// A manual fire's summary names the person who fired it.
func TestFireTrigger_StartCarriesFirer(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)
	ctx := token.WithClaims(context.Background(), &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user:1"},
		Kora:             token.KoraClaims{PrincipalType: token.PrincipalTypeHuman, DisplayName: "Dev User"},
	})
	if _, err := svc.FireTrigger(ctx, testTenant, trig.ID, "user:1", FireTriggerRequest{Variables: map[string]string{"env": "prod"}}); err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}
	drainPending(t, svc, deps.repo)
	call, ok := deps.agents.lastCall()
	if !ok || call.event == nil || call.event.Actor != (agentsclient.RunEventActor{Name: "Dev User", Kind: "person"}) {
		t.Errorf("start event = %+v, want Dev User as the actor", call.event)
	}
}

// A join sends the summary over the wire.
func TestJoinRun_SendsEventSummary(t *testing.T) {
	svc, deps := newTestService(t)
	event := &agentsclient.RunEventSummary{Actor: reviewer, Verb: "commented on", Object: pr6}
	if err := svc.joinRun(context.Background(), Trigger{ID: "t-1", Name: "n"}, "tok", "run-1", "hello", "d-1", eventChain{}, nil, event); err != nil {
		t.Fatalf("joinRun: %v", err)
	}
	deps.agents.mu.Lock()
	defer deps.agents.mu.Unlock()
	if len(deps.agents.messages) != 1 || !reflect.DeepEqual(deps.agents.messages[0].event, event) {
		t.Errorf("messages = %+v, want one carrying %+v", deps.agents.messages, event)
	}
}

// Only good and bad news is coloured. A closed or draft pull request, an
// approval and a cancelled workflow are neutral.
func TestEventSummaryTones(t *testing.T) {
	a := &githubAdapter{}
	tests := []struct {
		name, eventType string
		doc             any
		want            *agentsclient.RunEventStatus
	}{
		{"draft pull request", "pull_request", ghDoc(t, `"action":"opened"`, strings.Replace(ghPR, `"draft":false`, `"draft":true`, 1), ghRepo, ghSender), &agentsclient.RunEventStatus{Label: "Draft"}},
		{"approved review", "pull_request_review", ghDoc(t, `"action":"submitted"`, `"review":{"state":"approved","html_url":"https://github.com/example-org/stats-lib/pull/6#r1"}`, ghPR, ghRepo, ghSender), &agentsclient.RunEventStatus{Label: "Approved"}},
		{"cancelled workflow", "workflow_run", ghDoc(t, `"action":"completed"`, `"workflow_run":{"name":"test","run_number":3,"conclusion":"cancelled","head_branch":"main","pull_requests":[]}`, ghRepo, ghSender), &agentsclient.RunEventStatus{Label: "Cancelled"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.EventSummary(tt.eventType, tt.doc)
			if got == nil || got.Preview == nil || !reflect.DeepEqual(got.Preview.Status, tt.want) {
				t.Errorf("status = %+v, want %+v", got, tt.want)
			}
		})
	}
	var head eventHead
	envelope := `{"id":"e1","type":"agents.run.terminated.v1","koraprincipaltype":"USER","koraprincipalname":"Dev User","data":{"entity":{"id":"9a8b7c6d-1","agent":"verify-agent","state":"terminated"}}}`
	_ = json.Unmarshal([]byte(envelope), &head)
	if got := platformEventSummary(head, decodeBody([]byte(envelope))); got.Preview.Status.Tone != "" {
		t.Errorf("terminated run tone = %q, want none", got.Preview.Status.Tone)
	}
}
