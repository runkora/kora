package inferencegw

import (
	"testing"
)

// ─── derefFloat64 ─────────────────────────────────────────────────────────────

func TestDerefFloat64_Nil(t *testing.T) {
	t.Parallel()
	if got := derefFloat64(nil); got != 0 {
		t.Errorf("derefFloat64(nil) = %v, want 0", got)
	}
}

func TestDerefFloat64_NonNil(t *testing.T) {
	t.Parallel()
	v := 3.14
	if got := derefFloat64(&v); got != 3.14 {
		t.Errorf("derefFloat64(&3.14) = %v, want 3.14", got)
	}
}

func TestDerefFloat64_Zero(t *testing.T) {
	t.Parallel()
	v := 0.0
	if got := derefFloat64(&v); got != 0 {
		t.Errorf("derefFloat64(&0.0) = %v, want 0", got)
	}
}

// ─── applyNonTokenCosts ───────────────────────────────────────────────────────

func TestApplyNonTokenCosts_AllNil(t *testing.T) {
	t.Parallel()
	var e ModelPriceEntry
	applyNonTokenCosts(&e, liteLLMEntry{})
	if e.ImageInputCost != 0 || e.ImageOutputCost != 0 ||
		e.RequestInputCost != 0 || e.RequestOutputCost != 0 ||
		e.SecondInputCost != 0 || e.SecondOutputCost != 0 ||
		e.AudioInputCostPer1M != 0 || e.AudioOutputCostPer1M != 0 {
		t.Errorf("expected all zero for nil source, got %+v", e)
	}
}

func TestApplyNonTokenCosts_ImageNotScaled(t *testing.T) {
	t.Parallel()
	v := 0.001 // $0.001 per image
	var e ModelPriceEntry
	applyNonTokenCosts(&e, liteLLMEntry{
		InputCostPerImage:  &v,
		OutputCostPerImage: &v,
	})
	if e.ImageInputCost != 0.001 {
		t.Errorf("ImageInputCost = %v, want 0.001 (should not be scaled)", e.ImageInputCost)
	}
	if e.ImageOutputCost != 0.001 {
		t.Errorf("ImageOutputCost = %v, want 0.001 (should not be scaled)", e.ImageOutputCost)
	}
}

func TestApplyNonTokenCosts_AudioScaledToPerMillion(t *testing.T) {
	t.Parallel()
	// LiteLLM stores per-token: $0.000003 per audio token = $3.00 per million
	perToken := 0.000003
	var e ModelPriceEntry
	applyNonTokenCosts(&e, liteLLMEntry{
		InputCostPerAudioToken:  &perToken,
		OutputCostPerAudioToken: &perToken,
	})
	want := perToken * 1_000_000 // 3.0
	if e.AudioInputCostPer1M != want {
		t.Errorf("AudioInputCostPer1M = %v, want %v", e.AudioInputCostPer1M, want)
	}
	if e.AudioOutputCostPer1M != want {
		t.Errorf("AudioOutputCostPer1M = %v, want %v", e.AudioOutputCostPer1M, want)
	}
}

func TestApplyNonTokenCosts_RequestAndSecondNotScaled(t *testing.T) {
	t.Parallel()
	reqCost := 0.005
	secCost := 0.001
	var e ModelPriceEntry
	applyNonTokenCosts(&e, liteLLMEntry{
		InputCostPerRequest: &reqCost,
		InputCostPerSecond:  &secCost,
	})
	if e.RequestInputCost != 0.005 {
		t.Errorf("RequestInputCost = %v, want 0.005 (should not be scaled)", e.RequestInputCost)
	}
	if e.SecondInputCost != 0.001 {
		t.Errorf("SecondInputCost = %v, want 0.001 (should not be scaled)", e.SecondInputCost)
	}
}

func TestApplyNonTokenCosts_MixedNilAndNonNil(t *testing.T) {
	t.Parallel()
	imgCost := 0.01
	var e ModelPriceEntry
	applyNonTokenCosts(&e, liteLLMEntry{
		InputCostPerImage: &imgCost,
		// OutputCostPerImage left nil
	})
	if e.ImageInputCost != 0.01 {
		t.Errorf("ImageInputCost = %v, want 0.01", e.ImageInputCost)
	}
	if e.ImageOutputCost != 0 {
		t.Errorf("ImageOutputCost = %v, want 0 (nil input should produce 0)", e.ImageOutputCost)
	}
}
