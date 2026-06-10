package inferencegw

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/clear-route/orbit/internal/platform/policy"
)

// errTestInternal is a sentinel used in tests to simulate unexpected repository errors.
var errTestInternal = errors.New("internal test error")

// Compile-time interface satisfaction check.
var _ InferenceUsageRepository = (*mockInferenceUsageRepository)(nil)

// ── mockInferenceUsageRepository ──────────────────────────────────────────────

type mockInferenceUsageRepository struct {
	mu             sync.Mutex
	records        []InferenceUsageRecord
	hasNextPage    bool
	createErr      error
	listErr        error
	getErr         error
	statsResult    []TokenUsageStat
	statsErr       error
	analyticsResp  AnalyticsResponse
	analyticsErr   error
	principalsResp AnalyticsPrincipalsResponse
	principalsErr  error
	lastFilter     InferenceUsageFilter // captured on each List call
	lastAnalytics  AnalyticsFilter      // captured on each GetAnalytics call
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

func (m *mockInferenceUsageRepository) UsageStats(_ context.Context, tenantID, principalID string) ([]TokenUsageStat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastFilter = InferenceUsageFilter{TenantID: tenantID, PrincipalID: &principalID}
	if m.statsErr != nil {
		return nil, m.statsErr
	}
	return m.statsResult, nil
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

func (m *mockInferenceUsageRepository) GetAnalytics(_ context.Context, filter AnalyticsFilter) (AnalyticsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastAnalytics = filter
	if m.analyticsErr != nil {
		return AnalyticsResponse{}, m.analyticsErr
	}
	return m.analyticsResp, nil
}

func (m *mockInferenceUsageRepository) ListPrincipalUsage(_ context.Context, _ AnalyticsPrincipalsFilter) (AnalyticsPrincipalsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.principalsErr != nil {
		return AnalyticsPrincipalsResponse{}, m.principalsErr
	}
	return m.principalsResp, nil
}

func (m *mockInferenceUsageRepository) GetMonthlySpend(_ context.Context, _, _ string, _ time.Time) (float64, error) {
	return 0, nil
}

// ── shared test helpers ────────────────────────────────────────────────────────

// f64Ptr returns a pointer to f. Used to populate *float64 pricing override fields.
//
//go:fix inline
func f64Ptr(f float64) *float64 { return new(f) }

// ── mockPricingCatalogRepository ─────────────────────────────────────────────

var _ PricingCatalogRepository = (*mockPricingCatalogRepository)(nil)

type mockPricingCatalogRepository struct {
	setOverrideErr   error
	clearOverrideErr error
	loadAllResult    []PricingCatalogEntry
	loadAllErr       error
	upsertSyncedErr  error

	// captured args
	lastSetProvider   ProviderName
	lastSetModelID    string
	lastSetInput      *float64
	lastSetOutput     *float64
	lastSetCacheRead  *float64
	lastSetCacheWrite *float64
	lastClearProvider ProviderName
	lastClearModelID  string
}

func (m *mockPricingCatalogRepository) UpsertSynced(_ context.Context, _ []ModelPriceEntry) error {
	return m.upsertSyncedErr
}

func (m *mockPricingCatalogRepository) SetOverride(_ context.Context, provider ProviderName, modelID string, input, output, cacheRead, cacheWrite *float64) error {
	m.lastSetProvider = provider
	m.lastSetModelID = modelID
	m.lastSetInput = input
	m.lastSetOutput = output
	m.lastSetCacheRead = cacheRead
	m.lastSetCacheWrite = cacheWrite
	return m.setOverrideErr
}

func (m *mockPricingCatalogRepository) ClearOverride(_ context.Context, provider ProviderName, modelID string) error {
	m.lastClearProvider = provider
	m.lastClearModelID = modelID
	return m.clearOverrideErr
}

func (m *mockPricingCatalogRepository) LoadAll(_ context.Context) ([]PricingCatalogEntry, error) {
	return m.loadAllResult, m.loadAllErr
}

// ── mockSpendCounter ──────────────────────────────────────────────────────────

var _ SpendCounter = (*mockSpendCounter)(nil)

type mockSpendCounter struct {
	spend       float64
	getSpendErr error
	incrErr     error
}

func (m *mockSpendCounter) GetSpend(_ context.Context, _, _ string, _ []policy.BudgetCategory) (float64, error) {
	return m.spend, m.getSpendErr
}

func (m *mockSpendCounter) IncrementSpend(_ context.Context, _, _ string, _ []policy.BudgetCategory, amount float64) error {
	if m.incrErr != nil {
		return m.incrErr
	}
	m.spend += amount
	return nil
}
