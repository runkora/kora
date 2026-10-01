package inferencegw

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

type recordingPriceStore struct {
	provider ProviderName
	entries  []ModelPriceEntry
	calls    int
	err      error
}

func (s *recordingPriceStore) ReplaceSynced(_ context.Context, provider ProviderName, entries []ModelPriceEntry) error {
	s.calls++
	s.provider, s.entries = provider, entries
	return s.err
}

type countingRefresher struct {
	calls int
	err   error
}

func (c *countingRefresher) Refresh(context.Context) error {
	c.calls++
	return c.err
}

func TestDiscoveredPricesReplaceTheProvidersPrices(t *testing.T) {
	store, cache := &recordingPriceStore{}, &countingRefresher{}
	d := NewDiscoveredPrices(store, cache, logging.NewLoggerWithOutput(io.Discard))

	d.Record(context.Background(), "compatible:openrouter", []ProviderModel{
		{Provider: "compatible:openrouter", ModelID: "openai/gpt-oss-20b", Price: &ModelPriceEntry{InputCostPer1M: 0.03, OutputCostPer1M: 0.14}},
		{Provider: "compatible:openrouter", ModelID: "meta/llama:free", Price: &ModelPriceEntry{}},
		{Provider: "compatible:openrouter", ModelID: "openrouter/auto"},
	})

	if store.provider != "compatible:openrouter" || len(store.entries) != 2 {
		t.Fatalf("replaced %q with %+v, want 2 entries for compatible:openrouter", store.provider, store.entries)
	}
	got := store.entries[0]
	if got.Provider != "compatible:openrouter" || got.ModelID != "openai/gpt-oss-20b" || got.InputCostPer1M != 0.03 || got.OutputCostPer1M != 0.14 {
		t.Errorf("entry = %+v", got)
	}
	if cache.calls != 1 {
		t.Errorf("cache refreshed %d times, want 1", cache.calls)
	}
}

func TestDiscoveredPricesClearAProviderThatPublishesNone(t *testing.T) {
	store, cache := &recordingPriceStore{}, &countingRefresher{}
	d := NewDiscoveredPrices(store, cache, logging.NewLoggerWithOutput(io.Discard))

	d.Record(context.Background(), "compatible:ollama", []ProviderModel{{Provider: "compatible:ollama", ModelID: "llama3"}})

	if store.calls != 1 || len(store.entries) != 0 {
		t.Errorf("replace calls = %d with %d entries, want one call with none", store.calls, len(store.entries))
	}
}

func TestDiscoveredPricesIgnoreProvidersPricedByType(t *testing.T) {
	store, cache := &recordingPriceStore{}, &countingRefresher{}
	d := NewDiscoveredPrices(store, cache, logging.NewLoggerWithOutput(io.Discard))

	d.Record(context.Background(), "bedrock:us", []ProviderModel{{Provider: "bedrock:us", ModelID: "anthropic.claude"}})

	if store.calls != 0 || cache.calls != 0 {
		t.Errorf("replace calls = %d, refreshes = %d, want none", store.calls, cache.calls)
	}
}

func TestDiscoveredPricesStoreFailureSkipsTheRefresh(t *testing.T) {
	var logs bytes.Buffer
	store, cache := &recordingPriceStore{err: errors.New("db down")}, &countingRefresher{}
	d := NewDiscoveredPrices(store, cache, logging.NewLoggerWithOutput(&logs))

	d.Record(context.Background(), "compatible:openrouter", []ProviderModel{{ModelID: "m", Price: &ModelPriceEntry{}}})

	if cache.calls != 0 {
		t.Errorf("cache refreshed %d times after a store failure, want 0", cache.calls)
	}
	if !strings.Contains(logs.String(), "discovered prices not stored") || !strings.Contains(logs.String(), "db down") {
		t.Errorf("log = %q, want the store failure", logs.String())
	}
}

func TestDiscoveredPricesLogARefreshFailure(t *testing.T) {
	var logs bytes.Buffer
	store, cache := &recordingPriceStore{}, &countingRefresher{err: errors.New("load failed")}
	d := NewDiscoveredPrices(store, cache, logging.NewLoggerWithOutput(&logs))

	d.Record(context.Background(), "compatible:openrouter", []ProviderModel{{ModelID: "m", Price: &ModelPriceEntry{}}})

	if !strings.Contains(logs.String(), "price cache not refreshed") || strings.Contains(logs.String(), "discovered prices stored") {
		t.Errorf("log = %q, want only the refresh failure", logs.String())
	}
}

func TestDiscoveryHandsEachSucceededProviderToTheCallback(t *testing.T) {
	got := map[ProviderName][]ProviderModel{}
	ok := &stubConnector{name: "compatible:openrouter", models: []ProviderModel{{Provider: "compatible:openrouter", ModelID: "m"}}}
	failed := &failingConnector{stubConnector{name: "compatible:down", models: []ProviderModel{{Provider: "compatible:down", ModelID: "declared"}}}}
	r := NewRoutingEngine(
		map[ProviderName]ProviderConnector{ok.name: ok, failed.name: failed},
		[]ProviderInfo{{Name: ok.name}, {Name: failed.name}},
		func(_ context.Context, provider ProviderName, models []ProviderModel) { got[provider] = models })

	if _, err := r.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[ok.name]) != 1 || got[ok.name][0].ModelID != "m" {
		t.Errorf("callback got %+v, want only the provider that listed its models", got)
	}
}
