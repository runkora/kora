package inferencegw

import (
	"context"
	"testing"
)

func TestSetProvidersSwapsConnectorsAndDropsStaleModels(t *testing.T) {
	a := &stubConnector{name: "compatible:a", models: []ProviderModel{{Provider: "compatible:a", ModelID: "m1"}}}
	b := &stubConnector{name: "bedrock:b", models: []ProviderModel{{Provider: "bedrock:b", ModelID: "m2"}}}
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{a.name: a, b.name: b}, nil)
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
	ok := &stubConnector{name: "compatible:ok", models: []ProviderModel{{Provider: "compatible:ok", ModelID: "m"}}}
	r := NewRoutingEngine(nil, nil)
	r.SetProviders(map[ProviderName]ProviderConnector{ok.name: ok}, []ProviderInfo{
		{Name: "compatible:ok"},
		{Name: "azure:broken", BuildError: "Secret \"az\" not found"},
	})
	if _, err := r.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := r.ProviderStatuses()
	if len(st) != 2 {
		t.Fatalf("statuses = %+v", st)
	}
	if st[0].Name != "azure:broken" || st[0].Status != ProviderStatusError || st[0].Error == "" {
		t.Errorf("broken = %+v", st[0])
	}
	if st[1].Name != "compatible:ok" || st[1].Status != ProviderStatusReady || st[1].ModelCount != 1 || st[1].LastDiscoveredAt == nil {
		t.Errorf("ok = %+v", st[1])
	}
}
