package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	"github.com/runkora/kora/backend/internal/platform/declarative"
)

// trackerPayload is an issue tracker delivery with ids and no user name.
const trackerPayload = `{"event":"issue.updated","entity_id":"3f6b1c2e-8d4a-4f7e-9b1a-0c5d2e7f8a91",` +
	`"data":{"id":"9a2e4b6c-1d3f-4e5a-8b7c-6d9e0f1a2b3c","number":185,"title":"Rotate the build cache",` +
	`"project_id":"7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f","url":"https://tracker.example.com/i/185"}}`

const trackerIssueURL = "https://tracker.example.com/projects/{{ data.project_id }}/issues/{{ data.id }}"

// trackerAttribution is the attribution block from the ADR's example.
func trackerAttribution() map[string]any {
	return map[string]any{
		"links": []any{
			map[string]any{"label": "#{{ data.number }} {{ data.title }}", "url": trackerIssueURL},
			map[string]any{"label": "{{ data.url }}", "url": "{{ data.url }}"},
		},
		"actor":  map[string]any{"name": "Tracker", "kind": actorSystem},
		"object": map[string]any{"label": "#{{ data.number }} {{ data.title }}", "url": trackerIssueURL},
		"verbs":  map[string]any{"issue.created": "created", "issue.comment.created": "commented on"},
	}
}

func withAttribution(config, attribution map[string]any) map[string]any {
	config["attribution"] = attribution
	return config
}

func TestGenericAttribution_Validation(t *testing.T) {
	a := &genericAdapter{}
	bearer := func(attribution map[string]any) map[string]any {
		return withAttribution(methodConfig(genericBearer), attribution)
	}
	links := func(n int) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = map[string]any{"label": "L", "url": "https://tracker.example.com"}
		}
		return out
	}
	valid := map[string]map[string]any{
		"full block":    bearer(trackerAttribution()),
		"empty block":   bearer(map[string]any{}),
		"links only":    bearer(map[string]any{"links": links(agentsclient.MaxRunSourcesPerRequest)}),
		"summary only":  bearer(map[string]any{"actor": map[string]any{"name": "{{ user }}", "kind": actorPerson, "url": "{{ user_url }}"}, "object": map[string]any{"label": "x"}}),
		"verbs alone":   bearer(map[string]any{"verbs": map[string]any{"Any.Key": "did"}}),
		"url at bound":  bearer(map[string]any{"links": []any{map[string]any{"label": "L", "url": "https://t.example/" + strings.Repeat("a", agentsclient.MaxRunSourceURLBytes-18)}}}),
		"text at bound": bearer(map[string]any{"links": []any{map[string]any{"label": strings.Repeat("é", agentsclient.MaxEventSummaryStringRunes), "url": "https://t.example"}}}),
	}
	for name, c := range valid {
		if err := a.ValidateConfig(c); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	invalid := map[string]map[string]any{
		"unknown block key":     bearer(map[string]any{"link": links(1)}),
		"key in other case":     bearer(map[string]any{"Links": links(1)}),
		"unknown link key":      bearer(map[string]any{"links": []any{map[string]any{"label": "L", "url": "https://t.example", "icon": "x"}}}),
		"link key in case":      bearer(map[string]any{"links": []any{map[string]any{"Label": "L", "url": "https://t.example"}}}),
		"unknown actor key":     bearer(map[string]any{"actor": map[string]any{"name": "n", "kind": actorBot, "email": "x"}, "object": map[string]any{"label": "o"}}),
		"unknown object key":    bearer(map[string]any{"actor": map[string]any{"name": "n", "kind": actorBot}, "object": map[string]any{"label": "o", "title": "x"}}),
		"too many links":        bearer(map[string]any{"links": links(agentsclient.MaxRunSourcesPerRequest + 1)}),
		"link without url":      bearer(map[string]any{"links": []any{map[string]any{"label": "L"}}}),
		"link with blank label": bearer(map[string]any{"links": []any{map[string]any{"label": " ", "url": "https://t.example"}}}),
		"actor without object":  bearer(map[string]any{"actor": map[string]any{"name": "n", "kind": actorBot}}),
		"object without actor":  bearer(map[string]any{"object": map[string]any{"label": "o"}}),
		"actor without kind":    bearer(map[string]any{"actor": map[string]any{"name": "n"}, "object": map[string]any{"label": "o"}}),
		"unknown actor kind":    bearer(map[string]any{"actor": map[string]any{"name": "n", "kind": "robot"}, "object": map[string]any{"label": "o"}}),
		"bad placeholder path":  bearer(map[string]any{"links": []any{map[string]any{"label": "{{ data..id }}", "url": "https://t.example"}}}),
		"placeholder not path":  bearer(map[string]any{"links": []any{map[string]any{"label": "L", "url": "https://t.example/{{ data-id }}"}}}),
		"empty placeholder":     bearer(map[string]any{"actor": map[string]any{"name": "{{ }}", "kind": actorBot}, "object": map[string]any{"label": "o"}}),
		"url over bound":        bearer(map[string]any{"links": []any{map[string]any{"label": "L", "url": "https://t.example/" + strings.Repeat("a", agentsclient.MaxRunSourceURLBytes)}}}),
		"text over bound":       bearer(map[string]any{"actor": map[string]any{"name": strings.Repeat("n", agentsclient.MaxEventSummaryStringRunes+1), "kind": actorBot}, "object": map[string]any{"label": "o"}}),
		"verb over bound":       bearer(map[string]any{"verbs": map[string]any{"issue.created": strings.Repeat("v", agentsclient.MaxEventSummaryStringRunes+1)}}),
		"verb key over bound":   bearer(map[string]any{"verbs": map[string]any{strings.Repeat("e", agentsclient.MaxEventSummaryStringRunes+1): "did"}}),
		"links not a list":      bearer(map[string]any{"links": "https://t.example"}),
	}
	for name, c := range invalid {
		if err := a.ValidateConfig(c); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: got %v, want ErrValidation", name, err)
		}
	}
}

func TestGenericSourceLinks(t *testing.T) {
	doc := decodeBody([]byte(trackerPayload))
	issue := "https://tracker.example.com/projects/7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f/issues/9a2e4b6c-1d3f-4e5a-8b7c-6d9e0f1a2b3c"
	long := strings.Repeat("x", 150)
	attribution := map[string]any{"links": []any{
		map[string]any{"label": "#{{ data.number }} {{ data.title }}", "url": trackerIssueURL},
		map[string]any{"label": "{{ data.url }}", "url": "{{ data.url }}"},
		map[string]any{"label": "Plain http", "url": "http://tracker.example.com/{{ data.id }}"},
		map[string]any{"label": "Missing url", "url": "{{ data.missing }}"},
		map[string]any{"label": "Relative", "url": "/issues/{{ data.id }}"},
		map[string]any{"label": "{{ data.missing }}", "url": "https://tracker.example.com/empty-label"},
		map[string]any{"label": long, "url": "https://tracker.example.com/long"},
	}}
	want := []agentsclient.RunSource{
		{Kind: sourceKindTrigger, URL: issue, Label: "#185 Rotate the build cache"},
		{Kind: sourceKindTrigger, URL: "https://tracker.example.com/i/185", Label: "https://tracker.example.com/i/185"},
		{Kind: sourceKindTrigger, URL: "https://tracker.example.com/long", Label: strings.Repeat("x", agentsclient.MaxRunSourceLabelRunes-1) + "…"},
	}
	// A bearer source is a plain generic adapter; an hmac source is the supplied-secret one.
	for name, config := range map[string]map[string]any{
		"bearer": methodConfig(genericBearer),
		"hmac":   hmacConfig("X-Signature", "", ""),
	} {
		adapter := configuredGeneric(t, withAttribution(config, attribution))
		got := sourceLinks(adapter, "issue.updated", doc)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: links =\n %+v\nwant\n %+v", name, got, want)
		}
		if n := utf8.RuneCountInString(got[2].Label); n != agentsclient.MaxRunSourceLabelRunes {
			t.Errorf("%s: truncated label has %d runes, want %d", name, n, agentsclient.MaxRunSourceLabelRunes)
		}
	}
	if got := sourceLinks(configuredGeneric(t, methodConfig(genericBearer)), "issue.updated", doc); got != nil {
		t.Errorf("links without attribution = %+v, want none", got)
	}
}

func TestGenericEventSummary(t *testing.T) {
	doc := decodeBody([]byte(trackerPayload))
	issue := "https://tracker.example.com/projects/7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f/issues/9a2e4b6c-1d3f-4e5a-8b7c-6d9e0f1a2b3c"
	summarize := func(attribution map[string]any, eventType string) *agentsclient.RunEventSummary {
		t.Helper()
		meta := map[string]string{}
		addEventSummary(meta, configuredGeneric(t, withAttribution(hmacConfig("X-Signature", "", ""), attribution)), eventType, doc)
		return eventFromFields(meta)
	}

	got := summarize(trackerAttribution(), "issue.created")
	want := &agentsclient.RunEventSummary{
		Actor:  agentsclient.RunEventActor{Name: "Tracker", Kind: actorSystem},
		Verb:   "created",
		Object: agentsclient.RunEventObject{Label: "#185 Rotate the build cache", URL: issue},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
	if got := summarize(trackerAttribution(), "issue.updated"); got == nil || got.Verb != "issue.updated" {
		t.Errorf("unmapped event type: summary = %+v, want the event type as verb", got)
	}

	badLinks := map[string]any{
		"actor":  map[string]any{"name": "Tracker", "kind": actorSystem, "url": "ftp://tracker.example.com"},
		"object": map[string]any{"label": "{{ data.title }}", "url": "{{ data.missing }}"},
	}
	if got := summarize(badLinks, "issue.created"); got == nil || got.Actor.URL != "" || got.Object.URL != "" || got.Object.Label != "Rotate the build cache" {
		t.Errorf("summary with bad links = %+v, want the links empty and the summary kept", got)
	}

	missingLabel := map[string]any{
		"actor":  map[string]any{"name": "Tracker", "kind": actorSystem},
		"object": map[string]any{"label": "{{ data.missing }}"},
	}
	if got := summarize(missingLabel, "issue.created"); got != nil {
		t.Errorf("summary with an empty object label = %+v, want none", got)
	}
	if got := summarize(map[string]any{"links": trackerAttribution()["links"]}, "issue.created"); got != nil {
		t.Errorf("summary without actor and object = %+v, want none", got)
	}
}

func TestIngest_GenericAttribution(t *testing.T) {
	svc, deps := newTestService(t)
	ctx := context.Background()
	src, err := createGeneric(t, deps.sources, withAttribution(methodConfig(genericBearer), trackerAttribution()), "")
	if err != nil {
		t.Fatal(err)
	}
	genericTrigger(t, deps, src.ID, "any-delivery", genericEventTypeDefault)
	body := []byte(trackerPayload)

	if out, err := svc.Ingest(ctx, src.ID, http.Header{"Authorization": {"Bearer " + src.Secret}}, body); err != nil || out.Outcome != IngestAccepted {
		t.Fatalf("Ingest = %v, %v; want accepted", out.Outcome, err)
	}
	drainPending(t, svc, deps.repo)
	call, ok := deps.agents.lastCall()
	if !ok {
		t.Fatal("no run started")
	}
	if len(call.sources) != 2 || call.sources[1].URL != "https://tracker.example.com/i/185" {
		t.Errorf("run sources = %+v, want the two rendered links", call.sources)
	}
	if call.event == nil || call.event.Verb != genericEventTypeDefault || call.event.Object.Label != "#185 Rotate the build cache" {
		t.Errorf("run event = %+v, want the rendered summary", call.event)
	}
}

func TestUpdateSource_GenericAttribution(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	raw := func(v any) *json.RawMessage {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		m := json.RawMessage(b)
		return &m
	}
	src, err := deps.sources.CreateSource(ctx, testTenant, CreateSourceRequest{Name: "tracker", Provider: ProviderGeneric, Config: methodConfig(genericBearer)})
	if err != nil {
		t.Fatal(err)
	}
	// Attribution puts links on the runs of the source's agents, so it needs run access to them.
	mustWebhookTrigger(t, deps.triggers, src.ID, "on-source", true)
	deps.agents.atNoRun = true
	if _, err := deps.sources.UpdateSource(ctx, testTenant, src.ID, UpdateSourceRequest{Attribution: raw(trackerAttribution())}); !errors.Is(err, ErrBindForbidden) {
		t.Fatalf("caller without run access: got %v, want ErrBindForbidden", err)
	}
	if deps.repo.sources[src.ID].Config["attribution"] != nil {
		t.Fatal("a refused attribution change was stored")
	}
	deps.agents.atNoRun = false

	resp, err := deps.sources.UpdateSource(ctx, testTenant, src.ID, UpdateSourceRequest{Attribution: raw(trackerAttribution())})
	if err != nil {
		t.Fatalf("set attribution: %v", err)
	}
	stored := deps.repo.sources[src.ID].Config
	if stored["attribution"] == nil || stored["verification"] == nil || resp.Config["attribution"] == nil {
		t.Errorf("config = %+v, want the attribution next to the verification", stored)
	}
	if links := sourceLinks(mustForSource(t, deps, src.ID), "issue.updated", decodeBody([]byte(trackerPayload))); len(links) != 2 {
		t.Errorf("links after update = %+v, want two", links)
	}

	invalid := map[string]any{"actor": map[string]any{"name": "n", "kind": "robot"}, "object": map[string]any{"label": "o"}}
	for name, a := range map[string]*json.RawMessage{"invalid block": raw(invalid), "not an object": raw([]string{"x"})} {
		if _, err := deps.sources.UpdateSource(ctx, testTenant, src.ID, UpdateSourceRequest{Attribution: a}); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: got %v, want ErrValidation", name, err)
		}
	}
	if !reflect.DeepEqual(deps.repo.sources[src.ID].Config["attribution"], stored["attribution"]) {
		t.Error("a refused update changed the stored attribution")
	}

	if _, err := deps.sources.UpdateSource(ctx, testTenant, src.ID, UpdateSourceRequest{Attribution: raw(map[string]any{})}); err != nil {
		t.Fatalf("remove attribution: %v", err)
	}
	if cfg := deps.repo.sources[src.ID].Config; cfg["attribution"] != nil || cfg["verification"] == nil {
		t.Errorf("config after removal = %+v, want verification and no attribution", cfg)
	}

	github := mustSource(t, deps.sources)
	if _, err := deps.sources.UpdateSource(ctx, testTenant, github.ID, UpdateSourceRequest{Attribution: raw(trackerAttribution())}); !errors.Is(err, ErrValidation) {
		t.Errorf("non-generic source: got %v, want ErrValidation", err)
	}

	declared := mustApply(t, deps.sources, genericSourceItem(t, withAttribution(methodConfig(genericBearer), trackerAttribution()), "token"), declarative.OutcomeProjected)
	if _, err := deps.sources.UpdateSource(ctx, testTenant, declared.ID, UpdateSourceRequest{Attribution: raw(map[string]any{})}); !errors.Is(err, declarative.ErrManagedByResource) {
		t.Errorf("declared source: got %v, want ErrManagedByResource", err)
	}
	if links := sourceLinks(mustForSource(t, deps, declared.ID), "issue.updated", decodeBody([]byte(trackerPayload))); len(links) != 2 {
		t.Errorf("declared links = %+v, want two", links)
	}

	deps.sources.tenants = openTenants{locked: map[string]bool{testTenant: true}}
	if _, err := deps.sources.UpdateSource(ctx, testTenant, src.ID, UpdateSourceRequest{Attribution: raw(trackerAttribution())}); !errors.Is(err, declarative.ErrAPIAuthoringDisabled) {
		t.Errorf("locked tenant: got %v, want ErrAPIAuthoringDisabled", err)
	}
}

// mustForSource builds the adapter a stored source runs with.
func mustForSource(t *testing.T, deps *testDeps, id string) SourceAdapter {
	t.Helper()
	src, err := deps.repo.GetSource(context.Background(), testTenant, id)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := deps.sources.registry.ForSource(*src)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}
