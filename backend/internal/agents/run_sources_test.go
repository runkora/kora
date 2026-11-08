package agents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/token"
)

var (
	srcSlack = RunSource{Kind: RunSourceTrigger, URL: "https://app.slack.com/client/T1/C1/thread/C1-1.2", Label: "Slack thread"}
	srcPR    = RunSource{Kind: RunSourcePullRequest, URL: "https://github.com/o/r/pull/12", Label: "PR #12"}
	srcRepo  = RunSource{Kind: RunSourceRepository, URL: "https://github.com/o/r", Label: "o/r"}
)

func serviceCtx() context.Context {
	return withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger-source:s1")
}

func TestAuthorizeRunSources(t *testing.T) {
	tooMany := make([]RunSource, maxRunSourcesPerRequest+1)
	for i := range tooMany {
		tooMany[i] = RunSource{Kind: RunSourceTrigger, URL: "https://x.example/" + string(rune('a'+i)), Label: "x"}
	}
	human := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "user-1")
	tests := []struct {
		name    string
		ctx     context.Context
		in      []RunSource
		wantErr error
		wantLen int
	}{
		{"empty is allowed for anyone", human, nil, nil, 0},
		{"human refused", human, []RunSource{srcPR}, ErrRunSourcesRefused, 0},
		{"no claims refused", context.Background(), []RunSource{srcPR}, ErrRunSourcesRefused, 0},
		{"service accepted", serviceCtx(), []RunSource{srcSlack, srcPR}, nil, 2},
		{"duplicates collapse", serviceCtx(), []RunSource{srcPR, srcPR}, nil, 1},
		{"http refused", serviceCtx(), []RunSource{{Kind: RunSourceTrigger, URL: "http://x.example/a", Label: "x"}}, ErrRunSourcesInvalid, 0},
		{"javascript refused", serviceCtx(), []RunSource{{Kind: RunSourceTrigger, URL: "javascript:alert(1)", Label: "x"}}, ErrRunSourcesInvalid, 0},
		{"relative refused", serviceCtx(), []RunSource{{Kind: RunSourceTrigger, URL: "/a", Label: "x"}}, ErrRunSourcesInvalid, 0},
		{"unknown kind refused", serviceCtx(), []RunSource{{Kind: "other", URL: srcPR.URL, Label: "x"}}, ErrRunSourcesInvalid, 0},
		{"empty label refused", serviceCtx(), []RunSource{{Kind: RunSourcePullRequest, URL: srcPR.URL, Label: " "}}, ErrRunSourcesInvalid, 0},
		{"long label refused", serviceCtx(), []RunSource{{Kind: RunSourcePullRequest, URL: srcPR.URL, Label: strings.Repeat("a", maxRunSourceLabelRunes+1)}}, ErrRunSourcesInvalid, 0},
		{"too many refused", serviceCtx(), tooMany, ErrRunSourcesInvalid, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := authorizeRunSources(tt.ctx, tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestStartRun_Sources(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")

	run, err := f.run.StartRun(withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1"),
		StartRunRequest{AgentTypeID: at.ID, TriggerID: "trig-1", Sources: []RunSource{srcSlack, srcPR}})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	stored, _ := repo.GetRun(context.Background(), run.ID)
	if len(stored.Sources) != 2 || stored.Sources[0] != srcSlack {
		t.Errorf("stored sources = %+v", stored.Sources)
	}

	human := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "user-1")
	if _, err := f.run.StartRun(human, StartRunRequest{AgentTypeID: at.ID, Sources: []RunSource{srcPR}}); !errors.Is(err, ErrRunSourcesRefused) {
		t.Errorf("human start with sources: err = %v, want ErrRunSourcesRefused", err)
	}
	bad := []RunSource{{Kind: RunSourcePullRequest, URL: "http://github.com/o/r/pull/1", Label: "PR"}}
	if _, err := f.run.StartRun(serviceCtx(), StartRunRequest{AgentTypeID: at.ID, Sources: bad}); !errors.Is(err, ErrRunSourcesInvalid) {
		t.Errorf("non-https start: err = %v, want ErrRunSourcesInvalid", err)
	}
}

func TestSendMessage_Sources(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateIdle, Sources: []RunSource{srcSlack}})

	svc := withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeService, "trigger-source:s1")
	if err := f.run.SendMessage(svc, "run-1", SendMessageRequest{Content: "more", Sources: []RunSource{srcSlack, srcPR, srcRepo}}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	got, _ := repo.GetRun(context.Background(), "run-1")
	if len(got.Sources) != 3 || got.Sources[1] != srcPR || got.Sources[2] != srcRepo {
		t.Errorf("sources after join = %+v, want slack, pr, repo without a duplicate", got.Sources)
	}

	human := withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1")
	if err := f.run.SendMessage(human, "run-1", SendMessageRequest{Content: "x", Sources: []RunSource{srcPR}}); !errors.Is(err, ErrRunSourcesRefused) {
		t.Errorf("human join with sources: err = %v, want ErrRunSourcesRefused", err)
	}
	bad := []RunSource{{Kind: RunSourcePullRequest, URL: "http://x.example/1", Label: "PR"}}
	if err := f.run.SendMessage(svc, "run-1", SendMessageRequest{Content: "x", Sources: bad}); !errors.Is(err, ErrRunSourcesInvalid) {
		t.Errorf("non-https join: err = %v, want ErrRunSourcesInvalid", err)
	}
}
