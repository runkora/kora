package inferencegw

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── Refresh ──────────────────────────────────────────────────────────────────

func TestDatabasePricingCacheRefresh_NonTokenFields(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	entries := []PricingCatalogEntry{
		{
			Provider:                      ProviderBedrock,
			ModelID:                       "image-model",
			SyncedInput:                   0,
			SyncedOutput:                  0,
			LastSyncedAt:                  now,
			SyncedImageInputCostPerImage:  0.001,
			SyncedImageOutputCostPerImage: 0.002,
			SyncedRequestInputCost:        0.005,
			SyncedRequestOutputCost:       0.006,
			SyncedSecondInputCost:         0.0001,
			SyncedSecondOutputCost:        0.0002,
			SyncedAudioInputCostPer1M:     1.5,
			SyncedAudioOutputCostPer1M:    3.0,
		},
	}

	repo := &mockPricingCatalogRepository{loadAllResult: entries}
	cache := NewDatabasePricingCache(repo, time.Minute, logging.NewLoggerWithOutput(io.Discard))

	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got, ok := cache.Price(ProviderBedrock, "image-model")
	if !ok {
		t.Fatal("expected pricing entry to be present after refresh")
	}

	if got.ImageInputCost != 0.001 {
		t.Errorf("ImageInputCost = %v, want 0.001", got.ImageInputCost)
	}
	if got.ImageOutputCost != 0.002 {
		t.Errorf("ImageOutputCost = %v, want 0.002", got.ImageOutputCost)
	}
	if got.RequestInputCost != 0.005 {
		t.Errorf("RequestInputCost = %v, want 0.005", got.RequestInputCost)
	}
	if got.RequestOutputCost != 0.006 {
		t.Errorf("RequestOutputCost = %v, want 0.006", got.RequestOutputCost)
	}
	if got.SecondInputCost != 0.0001 {
		t.Errorf("SecondInputCost = %v, want 0.0001", got.SecondInputCost)
	}
	if got.SecondOutputCost != 0.0002 {
		t.Errorf("SecondOutputCost = %v, want 0.0002", got.SecondOutputCost)
	}
	if got.AudioInputCostPer1M != 1.5 {
		t.Errorf("AudioInputCostPer1M = %v, want 1.5", got.AudioInputCostPer1M)
	}
	if got.AudioOutputCostPer1M != 3.0 {
		t.Errorf("AudioOutputCostPer1M = %v, want 3.0", got.AudioOutputCostPer1M)
	}
}

func TestDatabasePricingCacheRefresh_TokenFieldsUsedEffective(t *testing.T) {
	t.Parallel()
	override := 2.0
	entries := []PricingCatalogEntry{
		{
			Provider:      ProviderBedrock,
			ModelID:       "anthropic.claude-sonnet-4-6",
			SyncedInput:   3.0,
			SyncedOutput:  15.0,
			OverrideInput: &override,
			LastSyncedAt:  time.Now().UTC(),
		},
	}

	repo := &mockPricingCatalogRepository{loadAllResult: entries}
	cache := NewDatabasePricingCache(repo, time.Minute, logging.NewLoggerWithOutput(io.Discard))

	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got, ok := cache.Price(ProviderBedrock, "anthropic.claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected pricing entry to be present after refresh")
	}

	if got.InputCostPerMillion != 2.0 {
		t.Errorf("InputCostPerMillion = %v, want 2.0 (override)", got.InputCostPerMillion)
	}
	if got.OutputCostPerMillion != 15.0 {
		t.Errorf("OutputCostPerMillion = %v, want 15.0 (synced fallback)", got.OutputCostPerMillion)
	}
}

func TestDatabasePricingCacheRefresh_NonTokenFieldsAreZeroWhenAbsent(t *testing.T) {
	t.Parallel()
	entries := []PricingCatalogEntry{
		{
			Provider:     ProviderBedrock,
			ModelID:      "text-only-model",
			SyncedInput:  3.0,
			SyncedOutput: 15.0,
			LastSyncedAt: time.Now().UTC(),
		},
	}

	repo := &mockPricingCatalogRepository{loadAllResult: entries}
	cache := NewDatabasePricingCache(repo, time.Minute, logging.NewLoggerWithOutput(io.Discard))

	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got, ok := cache.Price(ProviderBedrock, "text-only-model")
	if !ok {
		t.Fatal("expected pricing entry to be present after refresh")
	}

	if got.ImageInputCost != 0 {
		t.Errorf("ImageInputCost = %v, want 0 for text-only model", got.ImageInputCost)
	}
	if got.AudioInputCostPer1M != 0 {
		t.Errorf("AudioInputCostPer1M = %v, want 0 for text-only model", got.AudioInputCostPer1M)
	}
}
