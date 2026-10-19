package triggers

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// seedRoutedTrigger stores a routed webhook trigger over two candidates.
func seedRoutedTrigger(t *testing.T, r *postgresRepository, tenant, sourceID string) *Trigger {
	t.Helper()
	gid := "grant-" + uuid.NewString()
	tr := admissionDefaults(&Trigger{
		ID: uuid.NewString(), TenantID: tenant, Kind: KindWebhook, TargetKind: TargetKindRouted,
		SourceID: sourceID, Name: "routed", EventType: "issue_comment", PromptTemplate: "p",
		GrantID: &gid, Enabled: true,
		Routing: &RoutingConfig{
			Candidates: []string{"at-a", "at-b"}, Model: "liquid/d1", Confidence: ConfidenceStrict,
			FallbackAgentTypeID: new("at-b"), JoinRuns: new(true), JoinKey: "{{ issue.number }}",
			candidateSlugs: map[string]string{"at-a": "coder", "at-b": "reviewer"},
		},
	})
	if err := r.CreateTrigger(context.Background(), tr); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	return tr
}

func seedRoutedDelivery(t *testing.T, r *postgresRepository, tenant, sourceID string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := r.InsertDelivery(context.Background(), &Delivery{
		ID: id, TenantID: tenant, SourceID: sourceID, Kind: KindWebhook, ProviderDeliveryID: id, Status: StatusVerified,
	}); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}
	return id
}

func TestPostgres_RoutedTriggerRoundTrip(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	src := seedSource(t, r, tenant)
	tr := seedRoutedTrigger(t, r, tenant, src.ID)

	got, err := r.GetTrigger(ctx, tenant, tr.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	rt := got.Routing
	if got.TargetKind != TargetKindRouted || got.AgentTypeID != "" || rt == nil ||
		!slices.Equal(rt.Candidates, []string{"at-a", "at-b"}) || rt.candidateSlugs["at-b"] != "reviewer" ||
		rt.Model != "liquid/d1" || rt.Confidence != ConfidenceStrict || deref(rt.FallbackAgentTypeID) != "at-b" ||
		!rt.joins() || rt.JoinKey != "{{ issue.number }}" {
		t.Fatalf("round trip = %+v routing %+v", got, rt)
	}

	// A routed trigger lists under each of its candidates.
	listed, err := r.ListTriggers(ctx, tenant, "at-b")
	if err != nil || len(listed) != 1 || listed[0].Routing == nil {
		t.Fatalf("ListTriggers by candidate = %+v, %v", listed, err)
	}
	matchable, err := r.ListMatchableTriggersForSource(ctx, src.ID)
	if err != nil || len(matchable) != 1 || matchable[0].Routing.JoinKey == "" {
		t.Fatalf("matchable = %+v, %v; the join key must reach fan-out", matchable, err)
	}

	// An update replaces the candidates and the grant in one write.
	newGrant := "grant-" + uuid.NewString()
	got.GrantID = &newGrant
	got.Routing = &RoutingConfig{
		Candidates: []string{"at-c", "at-a"}, Model: "liquid/d1-ultra", Confidence: ConfidenceLoose, JoinRuns: new(false),
		candidateSlugs: map[string]string{"at-a": "coder", "at-c": "security"},
	}
	if err := r.UpdateTriggerGrant(ctx, got, false, tr.GrantID); err != nil {
		t.Fatalf("UpdateTriggerGrant: %v", err)
	}
	after, _ := r.GetTrigger(ctx, tenant, tr.ID)
	if deref(after.GrantID) != newGrant || !slices.Equal(after.Routing.Candidates, []string{"at-a", "at-c"}) ||
		after.Routing.Model != "liquid/d1-ultra" || after.Routing.FallbackAgentTypeID != nil || after.Routing.joins() || after.Routing.JoinKey != "" {
		t.Errorf("after update = %+v routing %+v", after, after.Routing)
	}
}

// The key-first lookup returns the newest decision of the same trigger with the
// key and a run, ignoring decisions without a run and keys of other triggers.
func TestPostgres_JoinKeyLookup(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	src := seedSource(t, r, tenant)
	a := seedRoutedTrigger(t, r, tenant, src.ID)
	b := seedRoutedTrigger(t, r, tenant, src.ID)

	decide := func(tr *Trigger, key, runID, agentTypeID string, ageMinutes int) {
		t.Helper()
		d := &RoutingDecision{
			DeliveryID: seedRoutedDelivery(t, r, tenant, src.ID), TriggerID: tr.ID, TenantID: tenant,
			QuestionVersion: routingQuestionsVersion, Outcome: RouteOutcomeNewRun, JoinKey: &key,
			AgentTypeID: &agentTypeID,
		}
		if runID != "" {
			d.RunID = &runID
		} else {
			d.Outcome = RouteOutcomeNoRun
		}
		if err := r.InsertRoutingDecision(ctx, d); err != nil {
			t.Fatalf("InsertRoutingDecision: %v", err)
		}
		if _, err := r.pool.Exec(ctx, `UPDATE trigger_routing_decisions SET created_at = now() - make_interval(mins => $1) WHERE delivery_id = $2`,
			ageMinutes, d.DeliveryID); err != nil {
			t.Fatalf("age decision: %v", err)
		}
	}
	decide(a, "acme#7", "run-old", "at-a", 30)
	decide(a, "acme#7", "run-new", "at-b", 20)
	decide(a, "acme#7", "", "at-a", 10) // newest, but started nothing
	decide(b, "acme#7", "run-b", "at-a", 1)

	runID, agentTypeID, found, err := r.LatestJoinKeyRun(ctx, tenant, a.ID, "acme#7")
	if err != nil || !found || runID != "run-new" || agentTypeID != "at-b" {
		t.Errorf("lookup on a = %q %q %v %v, want run-new by at-b", runID, agentTypeID, found, err)
	}
	if runID, _, found, _ := r.LatestJoinKeyRun(ctx, tenant, b.ID, "acme#7"); !found || runID != "run-b" {
		t.Errorf("lookup on b = %q %v, want run-b", runID, found)
	}
	if _, _, found, err := r.LatestJoinKeyRun(ctx, tenant, a.ID, "acme#8"); found || err != nil {
		t.Errorf("unknown key found=%v err=%v", found, err)
	}
	if _, _, found, _ := r.LatestJoinKeyRun(ctx, "other-tenant", a.ID, "acme#7"); found {
		t.Error("lookup must be scoped to the tenant")
	}
}

// The decision is written once per delivery and trigger, and reads back on the
// delivery run row with its scores.
func TestPostgres_RoutingDecisionWrite(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	src := seedSource(t, r, tenant)
	tr := seedRoutedTrigger(t, r, tenant, src.ID)
	deliveryID := seedRoutedDelivery(t, r, tenant, src.ID)

	d := &RoutingDecision{
		DeliveryID: deliveryID, TriggerID: tr.ID, TenantID: tenant, QuestionVersion: routingQuestionsVersion,
		Outcome: RouteOutcomeJoinedRun, Model: new("liquid/d1"), JoinKey: new("acme#7"),
		AgentTypeID: new("at-b"), RunID: new("run-1"),
		Threshold: new(0.7), Probability: new(0.9137), Confidence: new(0.8),
		Scores: RoutingScores{Agents: map[string]float64{"at-a": 0.2, "at-b": 0.8}, Runs: map[string]float64{"run-1": 0.9137, "new": 0.0863}},
	}
	if err := r.InsertRoutingDecision(ctx, d); err != nil {
		t.Fatalf("InsertRoutingDecision: %v", err)
	}
	dup := *d
	dup.Outcome = RouteOutcomeNoRun
	if err := r.InsertRoutingDecision(ctx, &dup); err != nil {
		t.Fatalf("duplicate InsertRoutingDecision: %v", err)
	}
	if err := r.InsertDeliveryRun(ctx, &DeliveryRun{
		ID: uuid.NewString(), TenantID: tenant, DeliveryID: deliveryID, TriggerID: tr.ID,
		Status: RunStatusJoined, RunID: new("run-1"),
	}); err != nil {
		t.Fatalf("InsertDeliveryRun joined: %v", err)
	}

	runs, err := r.ListDeliveryRunsForDeliveries(ctx, tenant, []string{deliveryID})
	if err != nil || len(runs[deliveryID]) != 1 {
		t.Fatalf("ListDeliveryRunsForDeliveries = %+v, %v", runs, err)
	}
	got := runs[deliveryID][0].Routing
	if got == nil || got.Outcome != RouteOutcomeJoinedRun || deref(got.Model) != "liquid/d1" || deref(got.JoinKey) != "acme#7" ||
		deref(got.Probability) != 0.914 || deref(got.Threshold) != 0.7 || got.Scores.Agents["at-b"] != 0.8 || got.Scores.Runs["new"] != 0.0863 {
		t.Errorf("routing = %+v", got)
	}

	var version string
	if err := r.pool.QueryRow(ctx, `SELECT question_version FROM trigger_routing_decisions WHERE delivery_id = $1`, deliveryID).Scan(&version); err != nil || version != routingQuestionsVersion {
		t.Errorf("question_version = %q, %v", version, err)
	}

	// A joined delivery reads as dispatched.
	if err := r.DeriveDeliveryStatus(ctx, deliveryID); err != nil {
		t.Fatalf("DeriveDeliveryStatus: %v", err)
	}
	if del, _ := r.GetDelivery(ctx, tenant, deliveryID); del.Status != StatusDispatched {
		t.Errorf("joined delivery status = %s, want dispatched", del.Status)
	}

	// An unrouted delivery started nothing and reads as skipped.
	unrouted := seedRoutedDelivery(t, r, tenant, src.ID)
	if err := r.InsertDeliveryRun(ctx, &DeliveryRun{
		ID: uuid.NewString(), TenantID: tenant, DeliveryID: unrouted, TriggerID: tr.ID, Status: RunStatusUnrouted,
	}); err != nil {
		t.Fatalf("InsertDeliveryRun unrouted: %v", err)
	}
	_ = r.DeriveDeliveryStatus(ctx, unrouted)
	if del, _ := r.GetDelivery(ctx, tenant, unrouted); del.Status != StatusSkipped {
		t.Errorf("unrouted delivery status = %s, want skipped", del.Status)
	}
}

// The grant-guarded update lands only while the stored grant is the one the
// caller replaced.
func TestPostgres_UpdateTriggerGrantIsConditional(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	src := seedSource(t, r, tenant)
	tr := seedRoutedTrigger(t, r, tenant, src.ID)
	original := deref(tr.GrantID)

	winner, _ := r.GetTrigger(ctx, tenant, tr.ID)
	loser, _ := r.GetTrigger(ctx, tenant, tr.ID)

	winner.GrantID = new("grant-winner")
	if err := r.UpdateTriggerGrant(ctx, winner, false, &original); err != nil {
		t.Fatalf("first replacement: %v", err)
	}
	loser.GrantID = new("grant-loser")
	if err := r.UpdateTriggerGrant(ctx, loser, false, &original); !errors.Is(err, ErrTriggerConflict) {
		t.Fatalf("second replacement = %v, want ErrTriggerConflict", err)
	}
	if got, _ := r.GetTrigger(ctx, tenant, tr.ID); deref(got.GrantID) != "grant-winner" {
		t.Errorf("stored grant = %s, want grant-winner", deref(got.GrantID))
	}
}

// An update that did not replace the grant never writes grant_id, so a
// replacement that landed after its read survives.
func TestPostgres_UpdateTriggerKeepsAConcurrentGrant(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	src := seedSource(t, r, tenant)
	tr := seedRoutedTrigger(t, r, tenant, src.ID)

	stale, _ := r.GetTrigger(ctx, tenant, tr.ID)
	swapped, _ := r.GetTrigger(ctx, tenant, tr.ID)
	swapped.GrantID = new("grant-swapped-in")
	if err := r.UpdateTriggerGrant(ctx, swapped, false, tr.GrantID); err != nil {
		t.Fatalf("UpdateTriggerGrant: %v", err)
	}
	stale.Name = "renamed"
	if err := r.UpdateTrigger(ctx, stale, false); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}
	got, _ := r.GetTrigger(ctx, tenant, tr.ID)
	if deref(got.GrantID) != "grant-swapped-in" || got.Name != "renamed" {
		t.Errorf("after the stale edit: grant %s, name %q", deref(got.GrantID), got.Name)
	}
}
