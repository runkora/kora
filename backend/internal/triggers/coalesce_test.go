package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeCoalesce(t *testing.T) {
	tests := []struct {
		name    string
		in      *CoalesceConfig
		want    *CoalesceConfig
		wantErr bool
	}{
		{name: "absent is off", in: nil},
		{name: "empty object is off", in: &CoalesceConfig{}},
		{name: "valid", in: &CoalesceConfig{Key: "{{ repository.full_name }}", WindowSeconds: 30}, want: &CoalesceConfig{Key: "{{ repository.full_name }}", WindowSeconds: 30}},
		{name: "ceiling", in: &CoalesceConfig{Key: "k", WindowSeconds: 300}, want: &CoalesceConfig{Key: "k", WindowSeconds: 300}},
		{name: "over the ceiling", in: &CoalesceConfig{Key: "k", WindowSeconds: 301}, wantErr: true},
		{name: "negative window", in: &CoalesceConfig{Key: "k", WindowSeconds: -1}, wantErr: true},
		{name: "key without a window", in: &CoalesceConfig{Key: "k"}, wantErr: true},
		{name: "window without a key", in: &CoalesceConfig{WindowSeconds: 10}, wantErr: true},
		{name: "template at the cap", in: &CoalesceConfig{Key: strings.Repeat("k", 256), WindowSeconds: 1}, want: &CoalesceConfig{Key: strings.Repeat("k", 256), WindowSeconds: 1}},
		{name: "template over the cap", in: &CoalesceConfig{Key: strings.Repeat("k", 257), WindowSeconds: 1}, wantErr: true},
		{name: "unknown meta field", in: &CoalesceConfig{Key: "{{ meta.nope }}", WindowSeconds: 1}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeCoalesce(tt.in)
			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRenderCoalesceKey(t *testing.T) {
	on := func(key string) Trigger { return Trigger{Coalesce: &CoalesceConfig{Key: key, WindowSeconds: 10}} }
	fields := map[string]string{"repository.full_name": "acme/widgets", "pull_request.number": "42", "long": strings.Repeat("x", 257), "edge": strings.Repeat("x", 256)}

	tests := []struct {
		name        string
		trigger     Trigger
		wantKey     string
		wantOK      bool
		wantTooLong bool
	}{
		{name: "renders", trigger: on("{{ repository.full_name }}#{{ pull_request.number }}"), wantKey: "acme/widgets#42", wantOK: true},
		{name: "off", trigger: Trigger{}},
		{name: "fully empty render does not coalesce", trigger: on("{{ missing }}")},
		{name: "blank render does not coalesce", trigger: on("{{ missing }} {{ missing }}")},
		{name: "render is trimmed", trigger: on(" {{ repository.full_name }} "), wantKey: "acme/widgets", wantOK: true},
		{name: "partial render still coalesces", trigger: on("{{ repository.full_name }}#{{ missing }}"), wantKey: "acme/widgets#", wantOK: true},
		{name: "literal key", trigger: on("all"), wantKey: "all", wantOK: true},
		{name: "256 bytes coalesces", trigger: on("{{ edge }}"), wantKey: strings.Repeat("x", 256), wantOK: true},
		{name: "257 bytes does not", trigger: on("{{ long }}"), wantTooLong: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok, tooLong := renderCoalesceKey(tt.trigger, fields)
			if key != tt.wantKey || ok != tt.wantOK || tooLong != tt.wantTooLong {
				t.Errorf("got (%q, %v, %v), want (%q, %v, %v)", key, ok, tooLong, tt.wantKey, tt.wantOK, tt.wantTooLong)
			}
		})
	}
}

func coalesceReq(srcID string, mutate func(*CreateTriggerRequest)) CreateTriggerRequest {
	req := validReq(KindWebhook)
	req.SourceID = srcID
	req.Coalesce = &CoalesceConfig{Key: "{{ repository.full_name }}#{{ pull_request.number }}", WindowSeconds: 30}
	if mutate != nil {
		mutate(&req)
	}
	return req
}

func TestCreateTrigger_Coalesce(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	ctx := context.Background()

	tr, err := deps.triggers.CreateTrigger(ctx, testTenant, coalesceReq(src.ID, nil))
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	if !tr.Coalesce.Enabled() || tr.Coalesce.WindowSeconds != 30 {
		t.Errorf("stored coalesce = %+v, want it set", tr.Coalesce)
	}

	plain, err := deps.triggers.CreateTrigger(ctx, testTenant, coalesceReq(src.ID, func(r *CreateTriggerRequest) { r.Coalesce = nil }))
	if err != nil {
		t.Fatalf("CreateTrigger without coalesce: %v", err)
	}
	if plain.Coalesce != nil {
		t.Errorf("coalesce = %+v, want off by default", plain.Coalesce)
	}

	wait := OverflowWait
	if _, err := deps.triggers.CreateTrigger(ctx, testTenant, coalesceReq(src.ID, func(r *CreateTriggerRequest) { r.Overflow = &wait })); err != nil {
		t.Errorf("coalesce with an explicit wait: %v", err)
	}

	for _, overflow := range []string{OverflowReplace, OverflowRefuse} {
		_, err := deps.triggers.CreateTrigger(ctx, testTenant, coalesceReq(src.ID, func(r *CreateTriggerRequest) { r.Overflow = &overflow }))
		if !errors.Is(err, ErrValidation) {
			t.Errorf("coalesce with overflow %q: err = %v, want ErrValidation", overflow, err)
		}
	}
	for name, c := range map[string]*CoalesceConfig{
		"window over the cap": {Key: "k", WindowSeconds: 301},
		"key without window":  {Key: "k"},
		"unknown meta field":  {Key: "{{ meta.nope }}", WindowSeconds: 5},
	} {
		_, err := deps.triggers.CreateTrigger(ctx, testTenant, coalesceReq(src.ID, func(r *CreateTriggerRequest) { r.Coalesce = c }))
		if !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}

	for _, kind := range []string{KindScheduled, KindManual} {
		req := validReq(kind)
		req.Coalesce = &CoalesceConfig{Key: "k", WindowSeconds: 5}
		if _, err := deps.triggers.CreateTrigger(ctx, testTenant, req); !errors.Is(err, ErrValidation) {
			t.Errorf("%s trigger with coalesce: err = %v, want ErrValidation", kind, err)
		}
	}
}

func TestUpdateTrigger_Coalesce(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	ctx := context.Background()

	tr, err := deps.triggers.CreateTrigger(ctx, testTenant, coalesceReq(src.ID, func(r *CreateTriggerRequest) { r.Coalesce = nil }))
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	set := &CoalesceConfig{Key: "{{ pull_request.number }}", WindowSeconds: 20}
	got, err := deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, UpdateTriggerRequest{Coalesce: set})
	if err != nil || !got.Coalesce.Enabled() || got.Coalesce.WindowSeconds != 20 {
		t.Fatalf("set coalesce: %+v, %v", got, err)
	}

	// An edit that says nothing about coalesce leaves it alone.
	name := "renamed"
	got, err = deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, UpdateTriggerRequest{Name: &name})
	if err != nil || !got.Coalesce.Enabled() {
		t.Fatalf("rename dropped coalesce: %+v, %v", got, err)
	}

	// With coalesce on, moving to another overflow is refused and changes nothing.
	replace := OverflowReplace
	_, err = deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, UpdateTriggerRequest{AdmissionSettings: AdmissionSettings{Overflow: &replace}})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("overflow change while coalescing: err = %v, want ErrValidation", err)
	}
	if stored, _ := deps.triggers.GetTrigger(ctx, testTenant, tr.ID); stored.Overflow != OverflowWait {
		t.Errorf("overflow = %q after a rejected update, want wait", stored.Overflow)
	}

	// Turning coalesce off in the same request makes the overflow change legal.
	got, err = deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, UpdateTriggerRequest{
		Coalesce: &CoalesceConfig{}, AdmissionSettings: AdmissionSettings{Overflow: &replace},
	})
	if err != nil || got.Coalesce != nil || got.Overflow != OverflowReplace {
		t.Fatalf("clear coalesce and change overflow: %+v, %v", got, err)
	}

	// And with overflow now replace, coalesce cannot be turned back on.
	if _, err := deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, UpdateTriggerRequest{Coalesce: set}); !errors.Is(err, ErrValidation) {
		t.Errorf("coalesce on a replace trigger: err = %v, want ErrValidation", err)
	}

	manual, err := deps.triggers.CreateTrigger(ctx, testTenant, validReq(KindManual))
	if err != nil {
		t.Fatalf("CreateTrigger(manual): %v", err)
	}
	if _, err := deps.triggers.UpdateTrigger(ctx, testTenant, manual.ID, UpdateTriggerRequest{Coalesce: set}); !errors.Is(err, ErrValidation) {
		t.Errorf("coalesce on a manual trigger: err = %v, want ErrValidation", err)
	}
}

// ─── Fan-out ─────────────────────────────────────────────────────────────────

// coalesceBody is a pull request event; number and baseRef vary what the key and
// the prompt render, and title varies the body so the replay guard does not trip.
func coalesceBody(number int, title, baseRef string) []byte {
	return fmt.Appendf(nil, `{"action":"synchronize",
		"pull_request":{"number":%d,"title":%q,"base":{"ref":%q}},
		"repository":{"full_name":"acme/widgets"},"sender":{"login":"bob"}}`, number, title, baseRef)
}

type coalesceFixture struct {
	svc  *Service
	deps *testDeps
	src  *SourceResponse
	n    int
}

func newCoalesceFixture(t *testing.T) *coalesceFixture {
	t.Helper()
	svc, deps := newTestService(t)
	return &coalesceFixture{svc: svc, deps: deps, src: mustSource(t, deps.sources)}
}

func (f *coalesceFixture) trigger(t *testing.T, key string) *Trigger {
	t.Helper()
	req := coalesceReq(f.src.ID, func(r *CreateTriggerRequest) {
		r.PromptTemplate = "review on {{ pull_request.base.ref }}"
		if key == "" {
			r.Coalesce = nil
		} else {
			r.Coalesce = &CoalesceConfig{Key: key, WindowSeconds: 60}
		}
	})
	tr, err := f.deps.triggers.CreateTrigger(context.Background(), testTenant, req)
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	return tr
}

// fire ingests one event and returns the id of the delivery it created.
func (f *coalesceFixture) fire(t *testing.T, body []byte) string {
	t.Helper()
	f.n++
	h := sign(f.src.Secret, body)
	h.Set("X-GitHub-Delivery", fmt.Sprintf("gh-%d", f.n))
	if _, err := f.svc.Ingest(context.Background(), f.src.ID, h, body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	d := f.deps.repo.lastDelivery()
	return d.ID
}

const prKey = "{{ repository.full_name }}#{{ pull_request.number }}"

// Two fires with one rendered key inside the window start one run, from the
// second fire's payload. The first delivery is superseded and names the second.
func TestFanOut_Coalesce_SecondFireWins(t *testing.T) {
	f := newCoalesceFixture(t)
	tr := f.trigger(t, prKey)

	first := f.fire(t, coalesceBody(7, "first", "main"))
	second := f.fire(t, coalesceBody(7, "second", "next"))

	pending := f.deps.repo.pendingFor(tr.ID)
	if len(pending) != 1 || pending[0].DeliveryID != second {
		t.Fatalf("waiting fires = %+v, want only the second delivery", pending)
	}
	if got := pending[0].Fields["pull_request.base.ref"]; got != "next" {
		t.Errorf("waiting fire carries base ref %q, want the second payload's", got)
	}

	// Inside the window nothing dispatches.
	drainPending(t, f.svc, f.deps.repo)
	if n := f.deps.agents.callCount(); n != 0 {
		t.Fatalf("%d runs started inside the window, want none", n)
	}

	f.deps.repo.expireWindows(tr.ID)
	drainPending(t, f.svc, f.deps.repo)
	if n := f.deps.agents.callCount(); n != 1 {
		t.Fatalf("%d runs started, want exactly one", n)
	}
	call, _ := f.deps.agents.lastCall()
	if call.deliveryID != second || !strings.Contains(call.initialMessage, `"pull_request.base.ref": "next"`) {
		t.Errorf("run started for delivery %q with prompt %q, want the second fire's payload", call.deliveryID, call.initialMessage)
	}

	runs := f.deps.repo.deliveryRunsFor(first)
	if len(runs) != 1 || runs[0].Status != RunStatusSuperseded || runs[0].SupersededBy == nil || *runs[0].SupersededBy != second {
		t.Errorf("first delivery outcomes = %+v, want superseded by %s", runs, second)
	}
	if d := f.deps.repo.deliveryByID(second); d.Status != StatusDispatched {
		t.Errorf("second delivery status = %q, want dispatched", d.Status)
	}
}

// Each delivery records the key it rendered, from the moment it is queued.
func TestFanOut_Coalesce_RecordsTheKeyOnEveryDelivery(t *testing.T) {
	f := newCoalesceFixture(t)
	f.trigger(t, prKey)

	first := f.fire(t, coalesceBody(7, "first", "main"))
	second := f.fire(t, coalesceBody(7, "second", "next"))

	listed, err := f.svc.ListDeliveries(context.Background(), testTenant, DeliveryFilter{SourceID: f.src.ID})
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	keys := map[string][]DeliveryKey{}
	for _, d := range listed {
		keys[d.ID] = d.CoalesceKeys
	}
	for _, id := range []string{first, second} {
		if len(keys[id]) != 1 || keys[id][0].Key != "acme/widgets#7" || keys[id][0].KeyTooLong {
			t.Errorf("delivery %s keys = %+v, want acme/widgets#7", id, keys[id])
		}
	}
}

func TestFanOut_Coalesce_DistinctKeysStartOneRunEach(t *testing.T) {
	f := newCoalesceFixture(t)
	tr := f.trigger(t, prKey)

	f.fire(t, coalesceBody(7, "a", "main"))
	f.fire(t, coalesceBody(8, "b", "main"))

	if n := len(f.deps.repo.pendingFor(tr.ID)); n != 2 {
		t.Fatalf("waiting fires = %d, want one per key", n)
	}
	f.deps.repo.expireWindows(tr.ID)
	drainPending(t, f.svc, f.deps.repo)
	if n := f.deps.agents.callCount(); n != 2 {
		t.Errorf("%d runs started, want 2", n)
	}
}

// A render that is empty does not coalesce: the fire dispatches at once, as it
// does without the setting, and nothing is recorded for it.
func TestFanOut_Coalesce_EmptyKeyDispatchesAsToday(t *testing.T) {
	f := newCoalesceFixture(t)
	tr := f.trigger(t, "{{ pull_request.missing }}")

	f.fire(t, coalesceBody(7, "a", "main"))
	f.fire(t, coalesceBody(7, "b", "main"))

	if n := len(f.deps.repo.pendingFor(tr.ID)); n != 2 {
		t.Fatalf("waiting fires = %d, want both kept", n)
	}
	drainPending(t, f.svc, f.deps.repo)
	if n := f.deps.agents.callCount(); n != 2 {
		t.Errorf("%d runs started with no wait, want 2", n)
	}
	if len(f.deps.repo.deliveryKeys) != 0 {
		t.Errorf("recorded keys %+v for empty renders, want none", f.deps.repo.deliveryKeys)
	}
}

// Only a fully empty render skips: "acme/widgets#" is a key.
func TestFanOut_Coalesce_PartialRenderStillCoalesces(t *testing.T) {
	f := newCoalesceFixture(t)
	tr := f.trigger(t, "{{ repository.full_name }}#{{ pull_request.missing }}")

	f.fire(t, coalesceBody(7, "a", "main"))
	f.fire(t, coalesceBody(8, "b", "main"))

	pending := f.deps.repo.pendingFor(tr.ID)
	if len(pending) != 1 {
		t.Fatalf("waiting fires = %d, want the two to share the key acme/widgets#", len(pending))
	}
}

// A key over 256 bytes does not coalesce, and the delivery says why.
func TestFanOut_Coalesce_TooLongKeyDispatchesAsToday(t *testing.T) {
	f := newCoalesceFixture(t)
	tr := f.trigger(t, "{{ pull_request.title }}")
	long := strings.Repeat("t", 300)

	first := f.fire(t, coalesceBody(7, long, "main"))
	f.fire(t, coalesceBody(7, long+"!", "main"))

	if n := len(f.deps.repo.pendingFor(tr.ID)); n != 2 {
		t.Fatalf("waiting fires = %d, want both kept", n)
	}
	drainPending(t, f.svc, f.deps.repo)
	if n := f.deps.agents.callCount(); n != 2 {
		t.Errorf("%d runs started, want 2", n)
	}
	keys, _ := f.deps.repo.ListDeliveryKeysForDeliveries(context.Background(), testTenant, []string{first})
	if got := keys[first]; len(got) != 1 || !got[0].KeyTooLong || got[0].Key != "" {
		t.Errorf("keys = %+v, want a too-long marker with no text", got)
	}
}

// A fire after the window closes opens its own window and starts its own run.
func TestFanOut_Coalesce_FireAfterTheWindowStartsItsOwnRun(t *testing.T) {
	f := newCoalesceFixture(t)
	tr := f.trigger(t, prKey)

	first := f.fire(t, coalesceBody(7, "first", "main"))
	f.deps.repo.expireWindows(tr.ID)
	second := f.fire(t, coalesceBody(7, "second", "next"))

	if n := len(f.deps.repo.pendingFor(tr.ID)); n != 2 {
		t.Fatalf("waiting fires = %d, want the due one and a new window", n)
	}
	for _, id := range []string{first, second} {
		for _, r := range f.deps.repo.deliveryRunsFor(id) {
			if r.Status == RunStatusSuperseded {
				t.Errorf("delivery %s was superseded across the window boundary", id)
			}
		}
	}
	drainPending(t, f.svc, f.deps.repo)
	if n := f.deps.agents.callCount(); n != 1 {
		t.Errorf("%d runs started, want only the due one", n)
	}
	f.deps.repo.expireWindows(tr.ID)
	drainPending(t, f.svc, f.deps.repo)
	if n := f.deps.agents.callCount(); n != 2 {
		t.Errorf("%d runs started after the second window, want 2", n)
	}
}

// A run that has started is not touched: the next fire opens a new window.
func TestFanOut_Coalesce_StartedRunIsUnaffected(t *testing.T) {
	f := newCoalesceFixture(t)
	tr := f.trigger(t, prKey)

	first := f.fire(t, coalesceBody(7, "first", "main"))
	f.deps.repo.expireWindows(tr.ID)
	drainPending(t, f.svc, f.deps.repo)
	second := f.fire(t, coalesceBody(7, "second", "next"))

	if n := f.deps.agents.callCount(); n != 1 {
		t.Fatalf("%d runs started, want the first only", n)
	}
	if d := f.deps.repo.deliveryByID(first); d.Status != StatusDispatched {
		t.Errorf("first delivery status = %q, want dispatched", d.Status)
	}
	if pending := f.deps.repo.pendingFor(tr.ID); len(pending) != 1 || pending[0].DeliveryID != second {
		t.Errorf("waiting fires = %+v, want the second in its own window", pending)
	}
}

// A trigger without the setting dispatches every fire, with no hold.
func TestFanOut_Coalesce_OffByDefault(t *testing.T) {
	f := newCoalesceFixture(t)
	tr := f.trigger(t, "")

	f.fire(t, coalesceBody(7, "a", "main"))
	f.fire(t, coalesceBody(7, "b", "next"))

	drainPending(t, f.svc, f.deps.repo)
	if n := f.deps.agents.callCount(); n != 2 {
		t.Errorf("%d runs started, want one per fire", n)
	}
	if n := len(f.deps.repo.pendingFor(tr.ID)); n != 0 {
		t.Errorf("%d fires left waiting", n)
	}
}

// Each trigger on a delivery renders its own key.
func TestFanOut_Coalesce_KeysAreKeptPerTrigger(t *testing.T) {
	f := newCoalesceFixture(t)
	f.trigger(t, prKey)
	f.trigger(t, "{{ repository.full_name }}")

	id := f.fire(t, coalesceBody(7, "a", "main"))

	keys, _ := f.deps.repo.ListDeliveryKeysForDeliveries(context.Background(), testTenant, []string{id})
	got := map[string]bool{}
	for _, k := range keys[id] {
		got[k.Key] = true
	}
	if len(keys[id]) != 2 || !got["acme/widgets#7"] || !got["acme/widgets"] {
		t.Errorf("keys = %+v, want one per trigger", keys[id])
	}
}

// The setting uses the names the API documents: coalesce.key and
// coalesce.window_seconds.
func TestCoalesceConfig_WireNames(t *testing.T) {
	var req CreateTriggerRequest
	body := `{"name":"n","coalesce":{"key":"{{ a }}","window_seconds":45}}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Coalesce == nil || req.Coalesce.Key != "{{ a }}" || req.Coalesce.WindowSeconds != 45 {
		t.Fatalf("decoded %+v", req.Coalesce)
	}
	out, err := json.Marshal(Trigger{Coalesce: req.Coalesce})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"coalesce":{"key":"{{ a }}","window_seconds":45}`) {
		t.Errorf("encoded trigger = %s", out)
	}
	if out, _ := json.Marshal(Trigger{}); strings.Contains(string(out), "coalesce") {
		t.Errorf("a trigger without the setting encodes %s, want the field omitted", out)
	}
}
