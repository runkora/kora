package inferencegw

import (
	"context"
	"testing"
)

func TestSetProvidersSwapsConnectorsAndDropsStaleModels(t *testing.T) {
	a := &stubConnector{name: "openai:a", models: []ProviderModel{{Provider: "openai:a", ModelID: "m1"}}}
	b := &stubConnector{name: "bedrock:b", models: []ProviderModel{{Provider: "bedrock:b", ModelID: "m2"}}}
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{a.name: a, b.name: b})
	if _, err := r.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}

	r.SetProviders(map[ProviderName]ProviderConnector{b.name: b}, []ProviderInfo{{Name: b.name}})
	if _, ok := r.Connector(a.name); ok {
		t.Error("removed provider still routable")
	}
	for _, m := range r.ProviderModels() {
		if m.Provider == a.name {
			t.Errorf("removed provider's model %q still listed", m.ModelID)
		}
	}
}

func TestProviderStatuses(t *testing.T) {
	ok := &stubConnector{name: "openai:ok", models: []ProviderModel{{Provider: "openai:ok", ModelID: "m"}}}
	r := NewRoutingEngine(nil)
	r.SetProviders(map[ProviderName]ProviderConnector{ok.name: ok}, []ProviderInfo{
		{Name: "openai:ok", Source: ProviderSourceResource},
		{Name: "anthropic:broken", Source: ProviderSourceResource, BuildError: "anthropicapi: base URL and API key are required"},
	})
	if _, err := r.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := r.ProviderStatuses()
	if len(st) != 2 {
		t.Fatalf("statuses = %+v", st)
	}
	if st[0].Name != "anthropic:broken" || st[0].Status != ProviderStatusError || st[0].Error == "" {
		t.Errorf("broken = %+v", st[0])
	}
	if st[1].Name != "openai:ok" || st[1].Status != ProviderStatusReady || st[1].ModelCount != 1 || st[1].LastDiscoveredAt == nil {
		t.Errorf("ok = %+v", st[1])
	}
}
