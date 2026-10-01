package inferencegw

import (
	"context"
	"io"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

type recordingPriceStore struct{ entries []ModelPriceEntry }

func (s *recordingPriceStore) UpsertSynced(_ context.Context, entries []ModelPriceEntry) error {
	s.entries = append(s.entries, entries...)
	return nil
}

type countingRefresher struct{ calls int }

func (c *countingRefresher) Refresh(context.Context) error {
	c.calls++
	return nil
}

func TestDiscoveredPricesStoresPublishedPricesUnderTheProviderName(t *testing.T) {
	store, cache := &recordingPriceStore{}, &countingRefresher{}
	d := NewDiscoveredPrices(store, cache, logging.NewLoggerWithOutput(io.Discard))

	d.Record(context.Background(), []ProviderModel{
		{Provider: "compatible:openrouter", ModelID: "openai/gpt-oss-20b", Price: &ModelPriceEntry{InputCostPer1M: 0.03, OutputCostPer1M: 0.14}},
		{Provider: "compatible:openrouter", ModelID: "meta/llama:free", Price: &ModelPriceEntry{}},
		{Provider: "compatible:ollama", ModelID: "llama3"},
	})

	if len(store.entries) != 2 {
		t.Fatalf("stored %d entries, want 2: %+v", len(store.entries), store.entries)
	}
	got := store.entries[0]
	if got.Provider != "compatible:openrouter" || got.ModelID != "openai/gpt-oss-20b" || got.InputCostPer1M != 0.03 || got.OutputCostPer1M != 0.14 {
		t.Errorf("entry = %+v", got)
	}
	if cache.calls != 1 {
		t.Errorf("cache refreshed %d times, want 1", cache.calls)
	}
}

func TestDiscoveredPricesSkipsUnpricedModels(t *testing.T) {
	store, cache := &recordingPriceStore{}, &countingRefresher{}
	d := NewDiscoveredPrices(store, cache, logging.NewLoggerWithOutput(io.Discard))

	d.Record(context.Background(), []ProviderModel{{Provider: "compatible:ollama", ModelID: "llama3"}})

	if len(store.entries) != 0 || cache.calls != 0 {
		t.Errorf("stored %d entries and refreshed %d times, want none", len(store.entries), cache.calls)
	}
}

func TestDiscoveryHandsCurrentModelsToTheCallback(t *testing.T) {
	var got []ProviderModel
	c := &stubConnector{name: "compatible:openrouter", models: []ProviderModel{{Provider: "compatible:openrouter", ModelID: "m"}}}
	r := NewRoutingEngine(map[ProviderName]ProviderConnector{c.name: c}, []ProviderInfo{{Name: c.name}},
		func(_ context.Context, models []ProviderModel) { got = models })

	if _, err := r.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ModelID != "m" {
		t.Errorf("callback got %+v", got)
	}
}
