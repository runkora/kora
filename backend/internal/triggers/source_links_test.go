package triggers

import (
	"context"
	"reflect"
	"testing"
	"time"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
)

func slackChatDoc(t *testing.T, payload string) any {
	t.Helper()
	a := &slackAdapter{}
	doc, ok := a.ChatEvent("mention", decodeBody([]byte(payload)))
	if !ok {
		t.Fatalf("ChatEvent refused %s", payload)
	}
	return doc
}

func TestSlackSourceLinks(t *testing.T) {
	a := &slackAdapter{}
	const base = "https://app.slack.com/client/T1/C1/thread/"
	tests := []struct {
		name, payload, want string
	}{
		{"top-level message", `{"team_id":"T1","event":{"type":"app_mention","user":"u1","ts":"1700000000.000100","channel":"C1"}}`, base + "C1-1700000000.000100"},
		{"thread reply", `{"team_id":"T1","event":{"type":"app_mention","user":"u1","ts":"1700000000.000200","thread_ts":"1700000000.000100","channel":"C1"}}`, base + "C1-1700000000.000100"},
		{"direct message", `{"team_id":"T1","event":{"type":"message","channel_type":"im","user":"u1","ts":"1700000000.000100","channel":"C1"}}`, base + "C1-1700000000.000100"},
		{"no workspace", `{"event":{"type":"app_mention","user":"u1","ts":"1700000000.000100","channel":"C1"}}`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := a.SourceLinks("mention", slackChatDoc(t, tc.payload))
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("links = %+v, want none", got)
				}
				return
			}
			want := []agentsclient.RunSource{{Kind: "trigger", URL: tc.want, Label: "Slack thread"}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("links = %+v, want %+v", got, want)
			}
		})
	}
}

func TestGitHubSourceLinks(t *testing.T) {
	a := &githubAdapter{}
	t.Run("pull request event", func(t *testing.T) {
		doc := decodeBody([]byte(`{"pull_request":{"html_url":"https://github.com/o/r/pull/12","number":12,"head":{"ref":"feat/x y"}},"repository":{"html_url":"https://github.com/o/r","full_name":"o/r"}}`))
		want := []agentsclient.RunSource{
			{Kind: "trigger", URL: "https://github.com/o/r/pull/12", Label: "GitHub pull request"},
			{Kind: "pull_request", URL: "https://github.com/o/r/pull/12", Label: "PR #12"},
			{Kind: "repository", URL: "https://github.com/o/r", Label: "o/r"},
			{Kind: "branch", URL: "https://github.com/o/r/tree/feat/x%20y", Label: "branch feat/x y"},
		}
		if got := a.SourceLinks("pull_request", doc); !reflect.DeepEqual(got, want) {
			t.Errorf("links = %+v, want %+v", got, want)
		}
	})
	t.Run("comment on a pull request", func(t *testing.T) {
		doc := decodeBody([]byte(`{"comment":{"html_url":"https://github.com/o/r/pull/12#c1"},"pull_request":{"html_url":"https://github.com/o/r/pull/12","number":12,"head":{"ref":"main"}},"repository":{"html_url":"https://github.com/o/r","full_name":"o/r"}}`))
		got := a.SourceLinks("pull_request_review_comment", doc)
		if len(got) == 0 || got[0].URL != "https://github.com/o/r/pull/12#c1" || got[0].Kind != "trigger" {
			t.Errorf("first link = %+v, want the comment", got)
		}
	})
	t.Run("issue event", func(t *testing.T) {
		doc := decodeBody([]byte(`{"issue":{"html_url":"https://github.com/o/r/issues/3"}}`))
		want := []agentsclient.RunSource{{Kind: "trigger", URL: "https://github.com/o/r/issues/3", Label: "GitHub issue"}}
		if got := a.SourceLinks("issues", doc); !reflect.DeepEqual(got, want) {
			t.Errorf("links = %+v, want %+v", got, want)
		}
	})
}

func TestAlertmanagerSourceLinks(t *testing.T) {
	a := &alertmanagerAdapter{}
	got := a.SourceLinks("firing", decodeBody([]byte(`{"externalURL":"https://am.example","alerts":[{"generatorURL":"https://prom.example/graph"}]}`)))
	want := []agentsclient.RunSource{{Kind: "trigger", URL: "https://prom.example/graph", Label: "Alert"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("links = %+v, want %+v", got, want)
	}
	got = a.SourceLinks("firing", decodeBody([]byte(`{"externalURL":"https://am.example","alerts":[{}]}`)))
	if len(got) != 1 || got[0].URL != "https://am.example" {
		t.Errorf("fallback links = %+v, want externalURL", got)
	}
	if got := a.SourceLinks("firing", decodeBody([]byte(`{}`))); len(got) != 0 {
		t.Errorf("links = %+v, want none", got)
	}
}

func TestGenericSourceLinks_None(t *testing.T) {
	if got := sourceLinks(&genericAdapter{}, "x", decodeBody([]byte(`{"url":"https://x.example"}`))); got != nil {
		t.Errorf("links = %+v, want none", got)
	}
}

func TestSourcesFromFields_IgnoresMalformed(t *testing.T) {
	if got := sourcesFromFields(map[string]string{metaSourceLinks: "{not json"}); got != nil {
		t.Errorf("got %+v, want none", got)
	}
	links := []agentsclient.RunSource{{Kind: "trigger", URL: "https://x.example", Label: "L"}}
	if got := sourcesFromFields(map[string]string{metaSourceLinks: encodeSourceLinks(links)}); !reflect.DeepEqual(got, links) {
		t.Errorf("round trip = %+v, want %+v", got, links)
	}
}

// A Slack delivery fans out to a start call that carries the thread link.
func TestIngest_SlackStartCarriesSourceLink(t *testing.T) {
	svc, deps := newTestService(t)
	ctx := context.Background()
	const secret = "slack-signing-secret"
	resp, err := deps.sources.CreateSource(ctx, testTenant, CreateSourceRequest{Name: "slack", Provider: ProviderSlack, SigningSecret: secret})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	tr, err := deps.triggers.CreateTrigger(ctx, testTenant, nil, CreateTriggerRequest{
		SourceID: resp.ID, Name: "mention", AgentTypeID: "at-1", EventType: "mention", PromptTemplate: "{{ text }}",
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	body := []byte(`{"type":"event_callback","team_id":"T1","event_id":"Ev1","event":{"type":"app_mention","user":"u1","text":"hi","ts":"1700000000.000200","thread_ts":"1700000000.000100","channel":"C1","channel_type":"channel"}}`)
	if res, err := svc.Ingest(ctx, resp.ID, slackHeaders(secret, time.Now().Unix(), body), body); err != nil || res.Outcome != IngestAccepted {
		t.Fatalf("Ingest = (%+v, %v)", res, err)
	}
	waitFor(t, func() bool { return len(deps.repo.pendingFor(tr.ID)) == 1 })
	drainPending(t, svc, deps.repo)

	call, ok := deps.agents.lastCall()
	if !ok {
		t.Fatal("no run was started")
	}
	want := []agentsclient.RunSource{{Kind: "trigger", URL: "https://app.slack.com/client/T1/C1/thread/C1-1700000000.000100", Label: "Slack thread"}}
	if !reflect.DeepEqual(call.sources, want) {
		t.Errorf("start sources = %+v, want %+v", call.sources, want)
	}
}

// A join sends the links over the wire.
func TestJoinRun_SendsSources(t *testing.T) {
	svc, deps := newTestService(t)
	links := []agentsclient.RunSource{{Kind: "trigger", URL: "https://x.example/t", Label: "L"}}
	if err := svc.joinRun(context.Background(), Trigger{ID: "t-1", Name: "n"}, "tok", "run-1", "hello", "d-1", eventChain{}, links); err != nil {
		t.Fatalf("joinRun: %v", err)
	}
	deps.agents.mu.Lock()
	defer deps.agents.mu.Unlock()
	if len(deps.agents.messages) != 1 || !reflect.DeepEqual(deps.agents.messages[0].sources, links) {
		t.Errorf("messages = %+v, want one carrying %+v", deps.agents.messages, links)
	}
}
