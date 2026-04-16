package inferencegw

import (
	"context"
	"sync"
)

// Compile-time interface satisfaction check.
var _ InferenceUsageRepository = (*mockInferenceUsageRepository)(nil)

// ── mockInferenceUsageRepository ──────────────────────────────────────────────

type mockInferenceUsageRepository struct {
	mu          sync.Mutex
	records     []InferenceUsageRecord
	hasNextPage bool
	createErr   error
	listErr     error
	getErr      error
	lastFilter  InferenceUsageFilter // captured on each List call
}

func newMockUsageRepo() *mockInferenceUsageRepository {
	return &mockInferenceUsageRepository{}
}

func (m *mockInferenceUsageRepository) Create(_ context.Context, record InferenceUsageRecord) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, record)
	return nil
}

func (m *mockInferenceUsageRepository) List(_ context.Context, filter InferenceUsageFilter) ([]InferenceUsageRecord, bool, error) {
	if m.listErr != nil {
		return nil, false, m.listErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastFilter = filter
	return m.records, m.hasNextPage, nil
}

// LastFilter returns a copy of the filter captured by the most recent List call.
// Reading via this accessor keeps the access race-detector-clean.
func (m *mockInferenceUsageRepository) LastFilter() InferenceUsageFilter {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastFilter
}

func (m *mockInferenceUsageRepository) Get(_ context.Context, tenantID, id string) (InferenceUsageRecord, error) {
	if m.getErr != nil {
		return InferenceUsageRecord{}, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.TenantID == tenantID && r.ID == id {
			return r, nil
		}
	}
	return InferenceUsageRecord{}, ErrUsageRecordNotFound
}

// ── shared test helpers ────────────────────────────────────────────────────────

// strPtr returns a pointer to s. Used to populate *string filter fields.
//
//go:fix inline
func strPtr(s string) *string { return new(s) }
