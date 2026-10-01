package inferencegw

import (
	"context"
	"errors"
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

// blockingConnector lists its models only once release is closed.
type blockingConnector struct {
	stubConnector
	release chan struct{}
}

func (b *blockingConnector) ListModels(ctx context.Context) ([]ProviderModel, error) {
	<-b.release
	return b.models, nil
}

func TestDiscoveryFromAReplacedProviderSetIsNotPublished(t *testing.T) {
	old := &blockingConnector{
		stubConnector: stubConnector{name: "compatible:old", models: []ProviderModel{{Provider: "compatible:old", ModelID: "stale"}}},
		release:       make(chan struct{}),
	}
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{old.name: old}, []ProviderInfo{{Name: old.name}})
	done := make(chan struct{})
	go func() {
		_, _ = r.ListModels(context.Background())
		close(done)
	}()

	current := &stubConnector{name: "compatible:new", models: []ProviderModel{{Provider: "compatible:new", ModelID: "fresh"}}}
	r.SetProviders(map[ProviderName]ProviderConnector{current.name: current}, []ProviderInfo{{Name: current.name}})
	if _, err := r.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(old.release)
	<-done

	models := r.ProviderModels()
	if len(models) != 1 || models[0].ModelID != "fresh" {
		t.Errorf("catalog = %+v, want only the current provider's model", models)
	}
	if st := r.ProviderStatuses(); len(st) != 1 || st[0].ModelCount != 1 {
		t.Errorf("statuses = %+v", st)
	}
}

// failingConnector returns its declared models with a discovery error.
type failingConnector struct{ stubConnector }

func (f *failingConnector) ListModels(context.Context) ([]ProviderModel, error) {
	return f.models, errors.New("list models: 401")
}

func TestDiscoveryErrorKeepsModelsAndMarksTheProvider(t *testing.T) {
	f := &failingConnector{stubConnector{name: "compatible:x", models: []ProviderModel{{Provider: "compatible:x", ModelID: "declared"}}}}
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{f.name: f}, []ProviderInfo{{Name: f.name}})
	_, _ = r.ListModels(context.Background())
	if models := r.ProviderModels(); len(models) != 1 {
		t.Errorf("declared models dropped: %+v", models)
	}
	st := r.ProviderStatuses()
	if st[0].Status != ProviderStatusError || st[0].ModelCount != 1 || st[0].Error == "" {
		t.Errorf("status = %+v", st[0])
	}
}
