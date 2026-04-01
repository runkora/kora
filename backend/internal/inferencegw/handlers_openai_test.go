package inferencegw

import (
	"strings"
	"testing"
)

// ─── buildCapabilityFilter ────────────────────────────────────────────────────

func TestBuildCapabilityFilter_Default(t *testing.T) {
	t.Parallel()
	// No param → only models with an active handler (CapabilityChat) are included.
	f := buildCapabilityFilter("")
	if !f([]ModelCapability{CapabilityChat}) {
		t.Error("chat model should pass default filter")
	}
	if f([]ModelCapability{CapabilityEmbedding}) {
		t.Error("embedding model should not pass default filter")
	}
	if f(nil) {
		t.Error("empty capabilities should not pass default filter")
	}
}

func TestBuildCapabilityFilter_All(t *testing.T) {
	t.Parallel()
	f := buildCapabilityFilter("all")
	if !f([]ModelCapability{CapabilityEmbedding}) {
		t.Error("embedding should pass 'all' filter")
	}
	if !f(nil) {
		t.Error("empty capabilities should pass 'all' filter")
	}
}

func TestBuildCapabilityFilter_ExplicitList(t *testing.T) {
	t.Parallel()
	f := buildCapabilityFilter("chat,embedding")
	if !f([]ModelCapability{CapabilityChat}) {
		t.Error("chat should pass chat,embedding filter")
	}
	if !f([]ModelCapability{CapabilityEmbedding}) {
		t.Error("embedding should pass chat,embedding filter")
	}
	if f([]ModelCapability{CapabilityImageGeneration}) {
		t.Error("image_generation should not pass chat,embedding filter")
	}
}

func TestBuildCapabilityFilter_WhitespaceTrimmed(t *testing.T) {
	t.Parallel()
	f := buildCapabilityFilter(" chat , embedding ")
	if !f([]ModelCapability{CapabilityChat}) {
		t.Error("chat should pass filter with whitespace in param")
	}
}

// ─── hasActiveCapability ──────────────────────────────────────────────────────

func TestHasActiveCapability(t *testing.T) {
	t.Parallel()
	if !hasActiveCapability([]ModelCapability{CapabilityChat}) {
		t.Error("chat should be active")
	}
	if hasActiveCapability([]ModelCapability{CapabilityEmbedding}) {
		t.Error("embedding should not be active")
	}
	if hasActiveCapability(nil) {
		t.Error("empty slice should return false")
	}
}

// ─── buildFilter ─────────────────────────────────────────────────────────────

func TestBuildFilter_NoFilters(t *testing.T) {
	t.Parallel()
	// A filter with no fields set (including empty TenantID) produces no WHERE clause.
	// In practice, List() rejects an empty TenantID before calling buildFilter — this
	// test covers the helper in isolation.
	where, args := buildFilter(InferenceUsageFilter{})
	if where != "" {
		t.Errorf("expected empty WHERE clause, got %q", where)
	}
	if len(args) != 0 {
		t.Errorf("expected 0 args, got %d", len(args))
	}
}

func TestBuildFilter_TenantID(t *testing.T) {
	t.Parallel()
	tid := "tenant-abc"
	where, args := buildFilter(InferenceUsageFilter{TenantID: tid})
	if !strings.Contains(where, "tenant_id = $1") {
		t.Errorf("WHERE clause %q should contain 'tenant_id = $1'", where)
	}
	if len(args) != 1 || args[0] != "tenant-abc" {
		t.Errorf("args = %v, want [tenant-abc]", args)
	}
}

func TestBuildFilter_MultipleFilters(t *testing.T) {
	t.Parallel()
	tid := "t1"
	pid := "p1"
	where, args := buildFilter(InferenceUsageFilter{TenantID: tid, PrincipalID: &pid})
	if !strings.Contains(where, "WHERE") {
		t.Errorf("expected WHERE keyword, got %q", where)
	}
	if !strings.Contains(where, "AND") {
		t.Errorf("expected AND between filters, got %q", where)
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

func TestBuildFilter_StatusFilter(t *testing.T) {
	t.Parallel()
	s := UsageStatusSuccess
	where, args := buildFilter(InferenceUsageFilter{Status: &s})
	if !strings.Contains(where, "status = $1") {
		t.Errorf("WHERE clause %q should contain 'status = $1'", where)
	}
	if len(args) != 1 || args[0] != "success" {
		t.Errorf("args = %v, want [success]", args)
	}
}
