package triggers

import (
	"context"
	"errors"
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
	tr, err := f.deps.triggers.CreateTrigger(context.Background(), testTenant, req)
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
		{"allowed association", &SenderRestriction{Associations: []string{"MEMBER"}}, commentBody("alice", "alice", "MEMBER"), true},
		{"other association", &SenderRestriction{Associations: []string{"MEMBER"}}, commentBody("stranger", "stranger", "NONE"), false},
		{"listed login, any case", &SenderRestriction{Logins: []string{"Alice"}}, commentBody("ALICE", "ALICE", "NONE"), true},
		{"login or association", &SenderRestriction{Logins: []string{"bob"}, Associations: []string{"OWNER"}}, commentBody("bob", "bob", "NONE"), true},
		{"unlisted login", &SenderRestriction{Logins: []string{"bob"}}, commentBody("mallory", "mallory", "OWNER"), false},
		{"association of another author", &SenderRestriction{Associations: []string{"MEMBER"}}, commentBody("mallory", "alice", "MEMBER"), false},
		{"no sender", &SenderRestriction{Logins: []string{"bob"}}, []byte(`{"action":"created","comment":{"body":"x"}}`), false},
		{"body not JSON", &SenderRestriction{Logins: []string{"bob"}}, []byte(`not json`), false},
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
func TestFanOut_ExactEventType(t *testing.T) {
	f := newPayloadFixture(t)
	tr := f.trigger(t, CreateTriggerRequest{EventType: "pull_request", PromptTemplate: "p"})
	f.dispatch(t, "issue_comment", commentBody("alice", "alice", "MEMBER"))
	if len(f.deps.repo.pendingFor(tr.ID)) != 0 {
		t.Error("a pull_request trigger fired on issue_comment")
	}
}

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

	tr, err := f.deps.triggers.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
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
		{"unknown association", CreateTriggerRequest{PromptTemplate: "p", SenderRestriction: &SenderRestriction{Associations: []string{"CONTRIBUTOR"}}}},
		{"blank login", CreateTriggerRequest{PromptTemplate: "p", SenderRestriction: &SenderRestriction{Logins: []string{" "}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPayloadFixture(t)
			tt.req.SourceID, tt.req.AgentTypeID, tt.req.Name, tt.req.EventType = f.src.ID, "at-1", "t", "issue_comment"
			if _, err := f.deps.triggers.CreateTrigger(context.Background(), testTenant, tt.req); !errors.Is(err, ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestCreateTrigger_NormalizesSenderRestriction(t *testing.T) {
	f := newPayloadFixture(t)
	tr := f.trigger(t, CreateTriggerRequest{PromptTemplate: "p", SenderRestriction: &SenderRestriction{
		Logins: []string{"Alice", "alice ", "Bob"}, Associations: []string{"MEMBER", "MEMBER"},
	}})
	got := tr.SenderRestriction
	if got == nil || strings.Join(got.Logins, ",") != "alice,bob" || strings.Join(got.Associations, ",") != "MEMBER" {
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
	_, err = deps.triggers.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID: src.ID, Name: "t", AgentTypeID: "at-1", EventType: "alert.firing", PromptTemplate: "p",
		SenderRestriction: &SenderRestriction{Logins: []string{"x"}},
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestUpdateTrigger_SenderRestriction(t *testing.T) {
	f := newPayloadFixture(t)
	tr := f.trigger(t, CreateTriggerRequest{PromptTemplate: "p"})
	ctx := context.Background()

	got, err := f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, UpdateTriggerRequest{
		SenderRestriction: &SenderRestriction{Associations: []string{"OWNER"}},
	})
	if err != nil || got.SenderRestriction == nil || got.SenderRestriction.Associations[0] != "OWNER" {
		t.Fatalf("set restriction = %+v, %v", got, err)
	}

	name := "renamed"
	got, err = f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, UpdateTriggerRequest{Name: &name})
	if err != nil || got.SenderRestriction == nil {
		t.Fatalf("an update without the field must keep the restriction: %+v, %v", got, err)
	}

	got, err = f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, UpdateTriggerRequest{SenderRestriction: &SenderRestriction{}})
	if err != nil || got.SenderRestriction != nil {
		t.Fatalf("an empty restriction must remove it: %+v, %v", got, err)
	}

	if _, err = f.deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, UpdateTriggerRequest{
		SenderRestriction: &SenderRestriction{Associations: []string{"NONE"}},
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
		if _, err := f.deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, req); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
}

func TestProviderFields(t *testing.T) {
	_, deps := newTestService(t)

	got, err := deps.sources.ProviderFields(ProviderGitHub, "issue_comment")
	if err != nil {
		t.Fatalf("ProviderFields: %v", err)
	}
	paths := map[string]bool{}
	for _, fl := range got.Fields {
		paths[fl.Path] = true
	}
	for _, want := range []string{"comment.body", "issue.title", "sender.login", metaEventType} {
		if !paths[want] {
			t.Errorf("fields are missing %s", want)
		}
	}
	if _, err := deps.sources.ProviderFields(ProviderGitHub, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("missing event_type = %v, want ErrValidation", err)
	}
	if _, err := deps.sources.ProviderFields("gitlab", "push"); !errors.Is(err, ErrProviderNotFound) {
		t.Errorf("unknown provider = %v, want ErrProviderNotFound", err)
	}
}

func TestProviderEventTypes(t *testing.T) {
	_, deps := newTestService(t)

	gh, err := deps.sources.ProviderEventTypes(ProviderGitHub)
	if err != nil || len(gh.Declared) == 0 || len(gh.Associations) != 3 {
		t.Fatalf("github = %+v, %v", gh, err)
	}
	am, err := deps.sources.ProviderEventTypes(ProviderAlertmanager)
	if err != nil || strings.Join(am.Declared, ",") != "alert.firing,alert.resolved" || am.Associations == nil || len(am.Associations) != 0 {
		t.Fatalf("alertmanager = %+v, %v; want its two types and an empty association list", am, err)
	}
	if _, err := deps.sources.ProviderEventTypes("gitlab"); !errors.Is(err, ErrProviderNotFound) {
		t.Errorf("unknown provider = %v, want ErrProviderNotFound", err)
	}
}

// An excluded sender records the sender reason even when the filter would also exclude.
func TestFanOut_SenderReasonWinsOverFilter(t *testing.T) {
	f := newPayloadFixture(t)
	f.trigger(t, CreateTriggerRequest{
		PromptTemplate:    "p",
		MatchCondition:    map[string][]string{"action": {"deleted"}},
		SenderRestriction: &SenderRestriction{Logins: []string{"bob"}},
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
	got, err := f.deps.triggers.UpdateTrigger(context.Background(), testTenant, legacy.ID, UpdateTriggerRequest{Enabled: &off})
	if err != nil || got.Enabled {
		t.Fatalf("disable = %+v, %v; want it disabled", got, err)
	}
	prompt := "{{ comment.body }}"
	if _, err := f.deps.triggers.UpdateTrigger(context.Background(), testTenant, legacy.ID, UpdateTriggerRequest{PromptTemplate: &prompt}); !errors.Is(err, ErrValidation) {
		t.Errorf("editing the prompt without an event type = %v, want ErrValidation", err)
	}
}

// Scheduled and manual triggers have no sender to restrict.
func TestSenderRestriction_WebhookOnly(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	r := &SenderRestriction{Logins: []string{"alice"}}
	for _, req := range []CreateTriggerRequest{
		{Kind: KindManual, Name: "m", AgentTypeID: "at-1", PromptTemplate: "p", SenderRestriction: r},
		{Kind: KindScheduled, Name: "s", AgentTypeID: "at-1", PromptTemplate: "p", SenderRestriction: r,
			ScheduleConfig: &ScheduleConfig{CronExpression: "0 9 * * *", Timezone: "UTC"}},
	} {
		if _, err := deps.triggers.CreateTrigger(ctx, testTenant, req); !errors.Is(err, ErrValidation) {
			t.Errorf("%s create = %v, want ErrValidation", req.Kind, err)
		}
	}
	manual := mustManualTrigger(t, deps.triggers)
	if _, err := deps.triggers.UpdateTrigger(ctx, testTenant, manual.ID, UpdateTriggerRequest{SenderRestriction: r}); !errors.Is(err, ErrValidation) {
		t.Errorf("manual update = %v, want ErrValidation", err)
	}
}

func TestNormalizeRestriction_Bounds(t *testing.T) {
	many := make([]string, maxSenderLogins+1)
	for i := range many {
		many[i] = "user" + strings.Repeat("x", i%5) + string(rune('a'+i%26))
	}
	cases := map[string]struct {
		logins  []string
		wantErr bool
	}{
		"at the count limit":    {logins: many[:maxSenderLogins]},
		"over the count limit":  {logins: many, wantErr: true},
		"at the length limit":   {logins: []string{strings.Repeat("a", maxSenderLoginLen)}},
		"over the length limit": {logins: []string{strings.Repeat("a", maxSenderLoginLen+1)}, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := normalizeRestriction(mustRegistry(), ProviderGitHub, &SenderRestriction{Logins: tc.logins})
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}
