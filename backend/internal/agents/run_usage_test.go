package agents

import (
	"context"
	"encoding/json"
	"testing"
)

func seedUsage(repo *mockRepository, id string, u RunUsage) {
	if repo.usage == nil {
		repo.usage = map[string]RunUsage{}
	}
	repo.usage[id] = u
}

func TestListRuns_ReturnsUsageForThePageInOneQuery(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 30, 3)
	want := RunUsage{CostUSD: 1.25, InputTokens: 900, OutputTokens: 100, RequestCount: 4}
	seedUsage(repo, "run-0003", want)

	page, err := svc.run.ListRuns(withTenantCtx(context.Background(), "t-1"), nil, RunListQuery{Limit: 20})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(page.Runs) != 20 {
		t.Fatalf("got %d runs, want 20", len(page.Runs))
	}
	if repo.usageCalls != 1 {
		t.Errorf("usage queries = %d, want 1 for the whole page", repo.usageCalls)
	}
	for _, r := range page.Runs {
		switch r.ID {
		case "run-0003":
			if r.Usage != want {
				t.Errorf("run-0003 usage = %+v, want %+v", r.Usage, want)
			}
		default:
			if r.Usage != (RunUsage{}) {
				t.Errorf("run %s with no usage rows reads %+v, want zeros", r.ID, r.Usage)
			}
		}
	}
}

func TestGetRun_ReturnsUsage(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedRunsAcrossTypes(t, repo, "t-1", 2, 1)
	want := RunUsage{CostUSD: 0.5, InputTokens: 10, OutputTokens: 20, RequestCount: 2}
	seedUsage(repo, "run-0000", want)

	got, err := svc.GetRunWithUsage(context.Background(), "run-0000")
	if err != nil {
		t.Fatalf("GetRunWithUsage: %v", err)
	}
	if got.Usage != want {
		t.Errorf("usage = %+v, want %+v", got.Usage, want)
	}
	none, err := svc.GetRunWithUsage(context.Background(), "run-0001")
	if err != nil || none.Usage != (RunUsage{}) {
		t.Errorf("run without usage rows = %+v, %v; want zeros", none.Usage, err)
	}
}

func TestRun_UsageJSONAlwaysPresent(t *testing.T) {
	b, err := json.Marshal(Run{ID: "r"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if got := string(m["usage"]); got != `{"cost_usd":0,"input_tokens":0,"output_tokens":0,"request_count":0}` {
		t.Errorf("usage = %s", got)
	}
}
