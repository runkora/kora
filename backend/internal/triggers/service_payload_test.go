package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// commentBody is an issue_comment delivery whose comment author is the sender.
func commentBody(sender, author, association string) []byte {
	return []byte(`{"action":"created",
		"comment":{"body":"please review","user":{"login":"` + author + `"},"author_association":"` + association + `"},
		"issue":{"number":7,"user":{"login":"someone"},"author_association":"NONE"},
		"repository":{"full_name":"acme/widgets"},"sender":{"login":"` + sender + `"}}`)
}

type payloadFixture struct {
	svc  *Service
	deps *testDeps
	src  WebhookSource
}

func newPayloadFixture(t *testing.T) payloadFixture {
	t.Helper()
	svc, deps := newTestService(t)
	resp := mustSource(t, deps.sources)
	src, _ := deps.repo.GetSourceByID(context.Background(), resp.ID)
	return payloadFixture{svc: svc, deps: deps, src: *src}
}

func (f payloadFixture) trigger(t *testing.T, req CreateTriggerRequest) *Trigger {
	t.Helper()
	req.SourceID = f.src.ID
	req.AgentTypeID = "at-1"
	if req.Name == "" {
		req.Name = "t"
	}
	if req.EventType == "" {
		req.EventType = "issue_comment"
	}
	tr, err := f.deps.triggers.CreateTrigger(context.Background(), testTenant, nil, req)
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	return tr
}

// dispatch fans a delivery out and returns the per-trigger outcomes it recorded.
func (f payloadFixture) dispatch(t *testing.T, eventType string, body []byte) []DeliveryRun {
	t.Helper()
	delID := "deliv-" + eventType
	_, _ = f.deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: delID, TenantID: testTenant, SourceID: f.src.ID, EventType: eventType, Status: StatusVerified,
	})
	adapter, _ := mustRegistry().Get(ProviderGitHub)
	f.svc.dispatch(context.Background(), f.src, adapter, arrival{
		deliveryID: delID, providerDeliveryID: "gh-" + eventType, eventType: eventType, body: body,
	})
	return f.deps.repo.deliveryRunsFor(delID)
}

func TestFanOut_SenderRestriction(t *testing.T) {
	tests := []struct {
		name        string
		restriction *SenderRestriction
		body        []byte
		wantQueued  bool
	}{
		{"unrestricted", nil, commentBody("stranger", "stranger", "NONE"), true},
		{"allowed association", &SenderRestriction{Roles: []string{"MEMBER"}}, commentBody("alice", "alice", "MEMBER"), true},
		{"other association", &SenderRestriction{Roles: []string{"MEMBER"}}, commentBody("stranger", "stranger", "NONE"), false},
		{"listed login, any case", &SenderRestriction{IDs: []string{"Alice"}}, commentBody("ALICE", "ALICE", "NONE"), true},
		{"login or association", &SenderRestriction{IDs: []string{"bob"}, Roles: []string{"OWNER"}}, commentBody("bob", "bob", "NONE"), true},
		{"unlisted login", &SenderRestriction{IDs: []string{"bob"}}, commentBody("mallory", "mallory", "OWNER"), false},
		{"body not JSON", &SenderRestriction{IDs: []string{"bob"}}, []byte(`not json`), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPayloadFixture(t)
			tr := f.trigger(t, CreateTriggerRequest{PromptTemplate: "{{ comment.body }}", SenderRestriction: tt.restriction})
			runs := f.dispatch(t, "issue_comment", tt.body)

			queued := len(f.deps.repo.pendingFor(tr.ID)) == 1
			if queued != tt.wantQueued {
				t.Fatalf("queued = %v, want %v (outcomes %+v)", queued, tt.wantQueued, runs)
			}
			if !tt.wantQueued {
				if len(runs) != 1 || runs[0].Status != RunStatusFiltered || runs[0].Error == nil ||
					*runs[0].Error != "event excluded by the trigger's sender restriction" {
					t.Errorf("outcomes = %+v, want one filtered row naming the sender restriction", runs)
				}
			}
		})
	}
}

// The queued row carries the meta fields and only the paths the trigger references.
func TestFanOut_StoresPerTriggerFields(t *testing.T) {
	f := newPayloadFixture(t)
	reply := f.trigger(t, CreateTriggerRequest{
		Name:           "reply",
		PromptTemplate: "Reply to {{ comment.body }}",
		MatchCondition: map[string][]string{"action": {"created"}},
	})
	triage := f.trigger(t, CreateTriggerRequest{Name: "triage", PromptTemplate: "Triage {{ issue.number }}"})

	f.dispatch(t, "issue_comment", commentBody("alice", "alice", "MEMBER"))

	want := map[string]map[string]string{
		reply.ID:  {"comment.body": "please review", "action": "created"},
		triage.ID: {"issue.number": "7"},
	}
	for id, paths := range want {
		pending := f.deps.repo.pendingFor(id)
		if len(pending) != 1 {
			t.Fatalf("trigger %s queued %d fires, want 1", id, len(pending))
		}
		got := pending[0].Fields
		if len(got) != len(paths)+len(metaFields) {
			t.Errorf("trigger %s stored %v, want its own paths plus the meta fields", id, got)
		}
		for k, v := range paths {
			if got[k] != v {
				t.Errorf("trigger %s %s = %q, want %q", id, k, got[k], v)
			}
		}
		if got[metaEventType] != "issue_comment" || got[metaDeliveryID] != "gh-issue_comment" || got[metaSource] != f.src.Name {
			t.Errorf("trigger %s meta = %v", id, got)
		}
	}
}

// A filter on a field the delivery does not contain compares against empty.
func TestFanOut_FilterOnMissingField(t *testing.T) {
	f := newPayloadFixture(t)
	needsValue := f.trigger(t, CreateTriggerRequest{Name: "needs", PromptTemplate: "p",
		MatchCondition: map[string][]string{"comment.missing": {"x"}}})
	acceptsEmpty := f.trigger(t, CreateTriggerRequest{Name: "empty", PromptTemplate: "p",
		MatchCondition: map[string][]string{"comment.missing": {""}}})

	f.dispatch(t, "issue_comment", commentBody("alice", "alice", "MEMBER"))

	if len(f.deps.repo.pendingFor(needsValue.ID)) != 0 {
		t.Error("a filter needing a value must exclude a delivery without the field")
	}
	if len(f.deps.repo.pendingFor(acceptsEmpty.ID)) != 1 {
		t.Error("a filter listing the empty value must match a delivery without the field")
	}
}

// A trigger only fires on its own event type; there is no catch-all.

// A stored body cut at the size ceiling is not trusted for field values on replay.
func TestRedeliver_TruncatedPayloadResolvesNoFields(t *testing.T) {
	f := newPayloadFixture(t)
	resp := mustSource(t, f.deps.sources)
	body := commentBody("alice", "alice", "MEMBER")
	headers := sign(resp.Secret, body)
	headers.Set("X-Github-Event", "issue_comment")
	if _, err := f.svc.Ingest(context.Background(), resp.ID, headers, body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	waitFor(t, f.deps.repo.anyDerived)
	original := f.deps.repo.lastDelivery()
	f.deps.repo.mu.Lock()
	for _, d := range f.deps.repo.deliveries {
		if d.ID == original.ID {
			d.PayloadBytes = maxPayloadBytes + 1
		}
	}
	f.deps.repo.mu.Unlock()

	tr, err := f.deps.triggers.CreateTrigger(context.Background(), testTenant, nil, CreateTriggerRequest{
		SourceID: resp.ID, Name: "late", AgentTypeID: "at-1", EventType: "issue_comment",
		PromptTemplate: "{{ comment.body }}",
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	if _, err := f.svc.Redeliver(context.Background(), testTenant, original.ID, "user:1"); err != nil {
		t.Fatalf("Redeliver: %v", err)
	}
	waitFor(t, func() bool { return len(f.deps.repo.pendingFor(tr.ID)) == 1 })
	if got := f.deps.repo.pendingFor(tr.ID)[0].Fields["comment.body"]; got != "" {
		t.Errorf("comment.body = %q, want empty for a truncated body", got)
	}
}

// The first message a webhook trigger's run receives carries the data block.
func TestDispatch_WebhookPromptHasDataBlock(t *testing.T) {
	f := newPayloadFixture(t)
	f.trigger(t, CreateTriggerRequest{PromptTemplate: "Reply to {{ comment.body }}"})
	f.dispatch(t, "issue_comment", commentBody("alice", "alice", "MEMBER"))
	drainPending(t, f.svc, f.deps.repo)

	call, ok := f.deps.agents.lastCall()
	if !ok {
		t.Fatal("no run started")
	}
	if !strings.HasPrefix(call.initialMessage, "Reply to {{ comment.body }}\n\n<delivery-data>\n") ||
		!strings.Contains(call.initialMessage, `"comment.body": "please review"`) {
		t.Errorf("initial message = %q", call.initialMessage)
	}
}

func TestCreateTrigger_WebhookValidation(t *testing.T) {
	tests := []struct {
		name string
		req  CreateTriggerRequest
	}{
		{"malformed placeholder", CreateTriggerRequest{PromptTemplate: "{{ comment..body }}"}},
		{"unknown meta field", CreateTriggerRequest{PromptTemplate: "{{ meta.nope }}"}},
		{"malformed filter key", CreateTriggerRequest{PromptTemplate: "p", MatchCondition: map[string][]string{"a b": {"x"}}}},
		{"unknown association", CreateTriggerRequest{PromptTemplate: "p", SenderRestriction: &SenderRestriction{Roles: []string{"CONTRIBUTOR"}}}},
		{"blank login", CreateTriggerRequest{PromptTemplate: "p", SenderRestriction: &SenderRestriction{IDs: []string{" "}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPayloadFixture(t)
			tt.req.SourceID, tt.req.AgentTypeID, tt.req.Name, tt.req.EventType = f.src.ID, "at-1", "t", "issue_comment"
			if _, err := f.deps.triggers.CreateTrigger(context.Background(), testTenant, nil, tt.req); !errors.Is(err, ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestCreateTrigger_NormalizesSenderRestriction(t *testing.T) {
	f := newPayloadFixture(t)
	tr := f.trigger(t, CreateTriggerRequest{PromptTemplate: "p", SenderRestriction: &SenderRestriction{
		IDs: []string{"Alice", "alice ", "Bob"}, Roles: []string{"MEMBER", "MEMBER"},
	}})
	got := tr.SenderRestriction
	if got == nil || strings.Join(got.IDs, ",") != "alice,bob" || strings.Join(got.Roles, ",") != "MEMBER" {
		t.Errorf("restriction = %+v, want lowercased, trimmed and deduped", got)
	}
}

// A restriction on an Alertmanager source is refused.
func TestCreateTrigger_SenderRestrictionNeedsASenderIdentifier(t *testing.T) {
	_, deps := newTestService(t)
	src, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "am", Provider: ProviderAlertmanager})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	_, err = deps.triggers.CreateTrigger(context.Background(), testTenant, nil, CreateTriggerRequest{
		SourceID: src.ID, Name: "t", AgentTypeID: "at-1", EventType: "alert.firing", PromptTemplate: "p",
		SenderRestriction: &SenderRestriction{IDs: []string{"x"}},
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestUpdateTrigger_SenderRestriction(t *testing.T) {
	f := newPayloadFixture(t)
	tr := f.trigger(t, CreateTriggerRequest{PromptTemplate: "p"})
	ctx := context.Background()

	got, err := f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, nil, UpdateTriggerRequest{
		SenderRestriction: &SenderRestriction{Roles: []string{"OWNER"}},
	})
	if err != nil || got.SenderRestriction == nil || got.SenderRestriction.Roles[0] != "OWNER" {
		t.Fatalf("set restriction = %+v, %v", got, err)
	}

	name := "renamed"
	got, err = f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, nil, UpdateTriggerRequest{Name: &name})
	if err != nil || got.SenderRestriction == nil {
		t.Fatalf("an update without the field must keep the restriction: %+v, %v", got, err)
	}

	got, err = f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, nil, UpdateTriggerRequest{SenderRestriction: &SenderRestriction{}})
	if err != nil || got.SenderRestriction != nil {
		t.Fatalf("an empty restriction must remove it: %+v, %v", got, err)
	}

	if _, err = f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, nil, UpdateTriggerRequest{
		SenderRestriction: &SenderRestriction{Roles: []string{"NONE"}},
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("unknown association on update = %v, want ErrValidation", err)
	}
}

func TestUpdateTrigger_WebhookValidation(t *testing.T) {
	f := newPayloadFixture(t)
	tr := f.trigger(t, CreateTriggerRequest{PromptTemplate: "p"})
	empty, bad := "", "{{ meta.nope }}"
	for name, req := range map[string]UpdateTriggerRequest{
		"empty event type":   {EventType: &empty},
		"unknown meta field": {PromptTemplate: &bad},
		"malformed filter":   {MatchCondition: map[string][]string{"a..b": {"x"}}},
	} {
		if _, err := f.deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, nil, req); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
}

// An excluded sender records the sender reason even when the filter would also exclude.
func TestFanOut_SenderReasonWinsOverFilter(t *testing.T) {
	f := newPayloadFixture(t)
	f.trigger(t, CreateTriggerRequest{
		PromptTemplate:    "p",
		MatchCondition:    map[string][]string{"action": {"deleted"}},
		SenderRestriction: &SenderRestriction{IDs: []string{"bob"}},
	})
	runs := f.dispatch(t, "issue_comment", commentBody("mallory", "mallory", "NONE"))
	if len(runs) != 1 || runs[0].Error == nil || *runs[0].Error != "event excluded by the trigger's sender restriction" {
		t.Errorf("outcomes = %+v, want one row naming the sender restriction", runs)
	}
}

// A redelivered duplicate reaches a dedupe-off trigger with the provider's delivery ID.
func TestFanOut_DuplicateCarriesMetaDeliveryID(t *testing.T) {
	f := newPayloadFixture(t)
	dedupe := false
	tr := f.trigger(t, CreateTriggerRequest{
		PromptTemplate:    "{{ meta.delivery_id }}",
		AdmissionSettings: AdmissionSettings{DedupeEnabled: &dedupe},
	})
	_, _ = f.deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: "dup-1", TenantID: testTenant, SourceID: f.src.ID, EventType: "issue_comment", Status: StatusDuplicate,
	})
	adapter, _ := mustRegistry().Get(ProviderGitHub)
	f.svc.dispatchDuplicate(context.Background(), f.src, adapter, arrival{
		deliveryID: "dup-1", providerDeliveryID: "gh-retry", eventType: "issue_comment",
		body: commentBody("alice", "alice", "MEMBER"),
	})
	pending := f.deps.repo.pendingFor(tr.ID)
	if len(pending) != 1 || pending[0].Fields[metaDeliveryID] != "gh-retry" {
		t.Errorf("pending = %+v, want one fire with meta.delivery_id gh-retry", pending)
	}
}

// A stored webhook trigger with no event type can still be disabled.
func TestUpdateTrigger_LegacyEmptyEventTypeCanBeDisabled(t *testing.T) {
	f := newPayloadFixture(t)
	legacy := &Trigger{
		ID: "legacy", TenantID: testTenant, Kind: KindWebhook, TargetKind: TargetKindAgent,
		SourceID: f.src.ID, Name: "old", AgentTypeID: "at-1", AgentSlug: "code-reviewer",
		PromptTemplate: "{{ pr.number }}", Enabled: true,
	}
	if err := f.deps.repo.CreateTrigger(context.Background(), legacy); err != nil {
		t.Fatalf("seed: %v", err)
	}
	off := false
	got, err := f.deps.triggers.UpdateTrigger(context.Background(), testTenant, legacy.ID, nil, UpdateTriggerRequest{Enabled: &off})
	if err != nil || got.Enabled {
		t.Fatalf("disable = %+v, %v; want it disabled", got, err)
	}
	prompt := "{{ comment.body }}"
	if _, err := f.deps.triggers.UpdateTrigger(context.Background(), testTenant, legacy.ID, nil, UpdateTriggerRequest{PromptTemplate: &prompt}); !errors.Is(err, ErrValidation) {
		t.Errorf("editing the prompt without an event type = %v, want ErrValidation", err)
	}
}

// Scheduled and manual triggers have no sender to restrict.
func TestSenderRestriction_WebhookOnly(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	r := &SenderRestriction{IDs: []string{"alice"}}
	for _, req := range []CreateTriggerRequest{
		{Kind: KindManual, Name: "m", AgentTypeID: "at-1", PromptTemplate: "p", SenderRestriction: r},
		{Kind: KindScheduled, Name: "s", AgentTypeID: "at-1", PromptTemplate: "p", SenderRestriction: r,
			ScheduleConfig: &ScheduleConfig{CronExpression: "0 9 * * *", Timezone: "UTC"}},
	} {
		if _, err := deps.triggers.CreateTrigger(ctx, testTenant, nil, req); !errors.Is(err, ErrValidation) {
			t.Errorf("%s create = %v, want ErrValidation", req.Kind, err)
		}
	}
	manual := mustManualTrigger(t, deps.triggers)
	if _, err := deps.triggers.UpdateTrigger(ctx, testTenant, manual.ID, nil, UpdateTriggerRequest{SenderRestriction: r}); !errors.Is(err, ErrValidation) {
		t.Errorf("manual update = %v, want ErrValidation", err)
	}
}

func TestNormalizeRestriction_Bounds(t *testing.T) {
	for name, logins := range map[string][]string{
		"too many ids":   slices.Repeat([]string{"a"}, maxSenderIDs+1),
		"an id too long": {strings.Repeat("a", maxSenderIDLen+1)},
	} {
		if _, err := normalizeRestriction(mustRegistry(), ProviderGitHub, &SenderRestriction{IDs: logins}); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
}

func TestWebhookEventType_WhitespaceIsMissing(t *testing.T) {
	f := newPayloadFixture(t)
	ctx := context.Background()
	req := CreateTriggerRequest{Kind: KindWebhook, SourceID: f.src.ID, Name: "w", AgentTypeID: "at-1", EventType: "   ", PromptTemplate: "p"}
	if _, err := f.deps.triggers.CreateTrigger(ctx, testTenant, nil, req); !errors.Is(err, ErrValidation) {
		t.Errorf("create with a blank event type = %v, want ErrValidation", err)
	}
	req.EventType = " issue_comment "
	created, err := f.deps.triggers.CreateTrigger(ctx, testTenant, nil, req)
	if err != nil || created.EventType != "issue_comment" {
		t.Fatalf("create = %+v, %v; want event type issue_comment", created, err)
	}
	blank := "  "
	if _, err := f.deps.triggers.UpdateTrigger(ctx, testTenant, created.ID, nil, UpdateTriggerRequest{EventType: &blank}); !errors.Is(err, ErrValidation) {
		t.Errorf("update to a blank event type = %v, want ErrValidation", err)
	}
}

// commentText is an issue_comment delivery from a member with the given body.
func commentText(action, text string) []byte {
	return []byte(`{"action":"` + action + `","comment":{"body":"` + text + `","user":{"login":"alice"},"author_association":"MEMBER"},
		"issue":{"number":7},"repository":{"full_name":"acme/widgets"},"sender":{"login":"alice"}}`)
}

func TestFanOut_MatchContains(t *testing.T) {
	mention := map[string][]string{"comment.body": {"@kora-agent"}}
	tests := []struct {
		name       string
		condition  map[string][]string
		contains   map[string][]string
		body       []byte
		wantQueued bool
	}{
		{"mention in any case", nil, mention, commentText("created", "Hey @Kora-Agent, can you look?"), true},
		{"no mention", nil, mention, commentText("created", "Looks good to me"), false},
		{"mention but the exact filter fails", map[string][]string{"action": {"edited"}}, mention, commentText("created", "@kora-agent please"), false},
		{"mention and the exact filter passes", map[string][]string{"action": {"created"}}, mention, commentText("created", "@kora-agent please"), true},
		{"missing field", nil, map[string][]string{"comment.missing": {"x"}}, commentText("created", "@kora-agent"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPayloadFixture(t)
			tr := f.trigger(t, CreateTriggerRequest{PromptTemplate: "{{ comment.body }}", MatchCondition: tt.condition, MatchContains: tt.contains})
			runs := f.dispatch(t, "issue_comment", tt.body)

			queued := len(f.deps.repo.pendingFor(tr.ID)) == 1
			if queued != tt.wantQueued {
				t.Fatalf("queued = %v, want %v (outcomes %+v)", queued, tt.wantQueued, runs)
			}
			if !tt.wantQueued && (len(runs) != 1 || runs[0].Status != RunStatusFiltered) {
				t.Errorf("outcomes = %+v, want one filtered row", runs)
			}
		})
	}
}

func TestMatchContains_Validation(t *testing.T) {
	tooMany := make([]string, maxContainsValues+1)
	for i := range tooMany {
		tooMany[i] = "x"
	}
	for name, contains := range map[string]map[string][]string{
		"empty substring":    {"comment.body": {""}},
		"too many values":    {"comment.body": tooMany},
		"substring too long": {"comment.body": {strings.Repeat("a", maxContainsValueLen+1)}},
		"invalid key":        {"reactions.+1": {"x"}},
		"unknown meta field": {"meta.nope": {"x"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPayloadFixture(t)
			_, err := f.deps.triggers.CreateTrigger(context.Background(), testTenant, nil, CreateTriggerRequest{
				SourceID: f.src.ID, AgentTypeID: "at-1", Name: "t", EventType: "issue_comment", PromptTemplate: "p", MatchContains: contains,
			})
			if !errors.Is(err, ErrValidation) {
				t.Errorf("create = %v, want ErrValidation", err)
			}
		})
	}
}

func TestMatchContains_WebhookOnly(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	contains := map[string][]string{"var.env": {"prod"}}
	for _, req := range []CreateTriggerRequest{
		{Kind: KindManual, Name: "m", AgentTypeID: "at-1", PromptTemplate: "p", MatchContains: contains},
		{Kind: KindScheduled, Name: "s", AgentTypeID: "at-1", PromptTemplate: "p", MatchContains: contains,
			ScheduleConfig: &ScheduleConfig{CronExpression: "0 9 * * *", Timezone: "UTC"}},
	} {
		if _, err := deps.triggers.CreateTrigger(ctx, testTenant, nil, req); !errors.Is(err, ErrValidation) {
			t.Errorf("%s create = %v, want ErrValidation", req.Kind, err)
		}
	}
	manual := mustManualTrigger(t, deps.triggers)
	if _, err := deps.triggers.UpdateTrigger(ctx, testTenant, manual.ID, nil, UpdateTriggerRequest{MatchContains: contains}); !errors.Is(err, ErrValidation) {
		t.Errorf("manual update = %v, want ErrValidation", err)
	}
}

func TestUpdateTrigger_MatchContainsReplaceAndClear(t *testing.T) {
	f := newPayloadFixture(t)
	ctx := context.Background()
	tr := f.trigger(t, CreateTriggerRequest{PromptTemplate: "p", MatchContains: map[string][]string{"comment.body": {"@kora-agent"}}})

	replaced, err := f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, nil, UpdateTriggerRequest{MatchContains: map[string][]string{"comment.body": {"@other"}}})
	if err != nil || !slices.Equal(replaced.MatchContains["comment.body"], []string{"@other"}) {
		t.Fatalf("replace = %+v, %v", replaced, err)
	}
	cleared, err := f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, nil, UpdateTriggerRequest{MatchContains: map[string][]string{}})
	if err != nil || len(cleared.MatchContains) != 0 {
		t.Fatalf("clear = %+v, %v", cleared, err)
	}
	if _, err := f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, nil, UpdateTriggerRequest{MatchContains: map[string][]string{"comment.body": {""}}}); !errors.Is(err, ErrValidation) {
		t.Errorf("update with an empty substring = %v, want ErrValidation", err)
	}
}

func TestMarshalKindConfig_MatchContains(t *testing.T) {
	tr := &Trigger{Kind: KindWebhook, MatchContains: map[string][]string{"comment.body": {"@kora-agent"}}}
	b, err := marshalKindConfig(tr)
	if err != nil || string(b) != `{"match_contains":{"comment.body":["@kora-agent"]}}` {
		t.Fatalf("marshalKindConfig = %s, %v", b, err)
	}
}

func TestSenderAllowed(t *testing.T) {
	cases := []struct {
		name        string
		restriction *SenderRestriction
		sender      Sender
		ok          bool
		want        bool
	}{
		{name: "no restriction", sender: Sender{ID: "a"}, ok: true, want: true},
		{name: "no restriction, no sender", want: true},
		{name: "listed id", restriction: &SenderRestriction{IDs: []string{"a"}}, sender: Sender{ID: "a"}, ok: true, want: true},
		{name: "any listed role", restriction: &SenderRestriction{Roles: []string{"OWNER"}}, sender: Sender{ID: "b", Roles: []string{"MEMBER", "OWNER"}}, ok: true, want: true},
		{name: "no listed role", restriction: &SenderRestriction{Roles: []string{"OWNER"}}, sender: Sender{ID: "b", Roles: []string{"MEMBER"}}, ok: true},
		{name: "empty role never matches", restriction: &SenderRestriction{Roles: []string{""}}, sender: Sender{ID: "b", Roles: []string{""}}, ok: true},
		{name: "no sender fails a restriction", restriction: &SenderRestriction{IDs: []string{""}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := senderAllowed(tc.restriction, tc.sender, tc.ok); got != tc.want {
				t.Errorf("senderAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSenderRestriction_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		body    string
		want    SenderRestriction
		wantErr bool
	}{
		{body: `{"ids":["alice"],"roles":["MEMBER"]}`, want: SenderRestriction{IDs: []string{"alice"}, Roles: []string{"MEMBER"}}},
		{body: `{}`},
		{body: `{"logins":["alice"]}`, wantErr: true},
		{body: `{"associations":["MEMBER"]}`, wantErr: true},
		{body: `{"Logins":["alice"]}`, wantErr: true},
		{body: `{"ASSOCIATIONS":["MEMBER"]}`, wantErr: true},
		{body: `{"ids":["alice"],"logins":[]}`, wantErr: true},
		{body: `{"IDS":["alice"]}`, want: SenderRestriction{IDs: []string{"alice"}}},
		{body: `{"id":["alice"]}`, wantErr: true},
		{body: `{"ids":["alice"],"role":["MEMBER"]}`, wantErr: true},
		{body: `["alice"]`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			var got SenderRestriction
			err := json.Unmarshal([]byte(tc.body), &got)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && (!slices.Equal(got.IDs, tc.want.IDs) || !slices.Equal(got.Roles, tc.want.Roles)) {
				t.Errorf("decoded %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A request with a legacy or misspelt key is refused rather than stored as no restriction.
func TestHandleTrigger_UnknownSenderKeys(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)
	for _, key := range []string{"logins", "associations", "id"} {
		body := []byte(`{"source_id":"` + src.ID + `","name":"t","agent_type_id":"at-1","event_type":"issue_comment",` +
			`"prompt_template":"p","sender_restriction":{"` + key + `":["x"]}}`)
		w := call(t, c.handleCreateTrigger, http.MethodPost, "/triggers", nil, body)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "takes ids and roles") {
			t.Errorf("create with %s: status = %d (%s), want 400 naming ids and roles", key, w.Code, w.Body.String())
		}
	}

	tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID: src.ID, Name: "t", AgentTypeID: "at-1", EventType: "issue_comment", PromptTemplate: "p",
		SenderRestriction: &SenderRestriction{IDs: []string{"alice"}},
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	for _, restriction := range []string{`{"logins":[]}`, `{"rolse":[]}`} {
		w := call(t, c.handleUpdateTrigger, http.MethodPatch, "/triggers/"+tr.ID, map[string]string{"id": tr.ID},
			[]byte(`{"sender_restriction":`+restriction+`}`))
		if w.Code != http.StatusBadRequest {
			t.Errorf("update with %s: status = %d, want 400", restriction, w.Code)
		}
		if got, _ := deps.repo.GetTrigger(context.Background(), testTenant, tr.ID); got.SenderRestriction == nil {
			t.Errorf("a refused update with %s cleared the restriction", restriction)
		}
	}
}

// Storage keeps the keys rows were written with, so existing rows read
// unchanged and a rollback reads new ones.
func TestKindConfig_SenderRestrictionStorage(t *testing.T) {
	tr := &Trigger{Kind: KindWebhook, SenderRestriction: &SenderRestriction{IDs: []string{"alice"}, Roles: []string{"MEMBER"}}}
	raw, err := marshalKindConfig(tr)
	if err != nil {
		t.Fatalf("marshalKindConfig: %v", err)
	}
	if want := `{"sender_restriction":{"logins":["alice"],"associations":["MEMBER"]}}`; string(raw) != want {
		t.Errorf("stored %s, want %s", raw, want)
	}

	cases := []struct {
		stored string
		want   *SenderRestriction
	}{
		{stored: `{"sender_restriction":{"logins":["alice"],"associations":["MEMBER"]}}`,
			want: &SenderRestriction{IDs: []string{"alice"}, Roles: []string{"MEMBER"}}},
		{stored: `{"sender_restriction":{"associations":["OWNER"]},"match_contains":{"comment.body":["x"]}}`,
			want: &SenderRestriction{Roles: []string{"OWNER"}}},
		{stored: `{"sender_restriction":{}}`},
		{stored: `{"match_contains":{"comment.body":["x"]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.stored, func(t *testing.T) {
			got := Trigger{Kind: KindWebhook}
			if err := decodeKindConfig(&got, []byte(tc.stored)); err != nil {
				t.Fatalf("decodeKindConfig: %v", err)
			}
			if (got.SenderRestriction == nil) != (tc.want == nil) {
				t.Fatalf("restriction = %+v, want %+v", got.SenderRestriction, tc.want)
			}
			if tc.want != nil && (!slices.Equal(got.SenderRestriction.IDs, tc.want.IDs) || !slices.Equal(got.SenderRestriction.Roles, tc.want.Roles)) {
				t.Errorf("restriction = %+v, want %+v", got.SenderRestriction, tc.want)
			}
		})
	}
}
