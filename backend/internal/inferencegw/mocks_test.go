package inferencegw

import (
	"context"
	"errors"
	"sync"
	"time"
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
func f64Ptr(f float64) *float64 { return &f }

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

// ── mockQuotaRepository ───────────────────────────────────────────────────────

var _ QuotaRepository = (*mockQuotaRepository)(nil)

type mockQuotaRepository struct {
	mu sync.Mutex

	resolveResult   float64
	resolveHasLimit bool
	resolveErr      error

	groups         []QuotaGroup
	createGroupErr error
	getGroupErr    error
	listGroupsErr  error
	updateGroupErr error
	deleteGroupErr error

	setMembershipErr    error
	removeMembershipErr error

	principalBudget   float64
	principalHasLimit bool
	getPrincipalErr   error
	setPrincipalErr   error
	clearPrincipalErr error
}

func (m *mockQuotaRepository) Resolve(_ context.Context, _, _ string) (float64, bool, error) {
	return m.resolveResult, m.resolveHasLimit, m.resolveErr
}

func (m *mockQuotaRepository) CreateGroup(_ context.Context, g QuotaGroup) error {
	if m.createGroupErr != nil {
		return m.createGroupErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.groups = append(m.groups, g)
	return nil
}

func (m *mockQuotaRepository) GetGroup(_ context.Context, _, id string) (QuotaGroup, error) {
	if m.getGroupErr != nil {
		return QuotaGroup{}, m.getGroupErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range m.groups {
		if g.ID == id {
			return g, nil
		}
	}
	return QuotaGroup{}, ErrQuotaGroupNotFound
}

func (m *mockQuotaRepository) ListGroups(_ context.Context, _ string) ([]QuotaGroup, error) {
	if m.listGroupsErr != nil {
		return nil, m.listGroupsErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.groups == nil {
		return []QuotaGroup{}, nil
	}
	out := make([]QuotaGroup, len(m.groups))
	copy(out, m.groups)
	return out, nil
}

func (m *mockQuotaRepository) UpdateGroupBudget(_ context.Context, _, id string, budget float64) error {
	if m.updateGroupErr != nil {
		return m.updateGroupErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, g := range m.groups {
		if g.ID == id {
			m.groups[i].MonthlyBudgetUSD = budget
			return nil
		}
	}
	return ErrQuotaGroupNotFound
}

func (m *mockQuotaRepository) DeleteGroup(_ context.Context, _, id string) error {
	if m.deleteGroupErr != nil {
		return m.deleteGroupErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, g := range m.groups {
		if g.ID == id {
			m.groups = append(m.groups[:i], m.groups[i+1:]...)
			return nil
		}
	}
	return ErrQuotaGroupNotFound
}

func (m *mockQuotaRepository) SetMembership(_ context.Context, _, _, _ string) error {
	return m.setMembershipErr
}

func (m *mockQuotaRepository) RemoveMembership(_ context.Context, _, _, _ string) error {
	return m.removeMembershipErr
}

func (m *mockQuotaRepository) SetPrincipalQuota(_ context.Context, _, _ string, budget float64) error {
	if m.setPrincipalErr != nil {
		return m.setPrincipalErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.principalBudget = budget
	m.principalHasLimit = true
	return nil
}

func (m *mockQuotaRepository) GetPrincipalQuota(_ context.Context, _, _ string) (float64, bool, error) {
	if m.getPrincipalErr != nil {
		return 0, false, m.getPrincipalErr
	}
	return m.principalBudget, m.principalHasLimit, nil
}

func (m *mockQuotaRepository) ClearPrincipalQuota(_ context.Context, _, _ string) error {
	if m.clearPrincipalErr != nil {
		return m.clearPrincipalErr
	}
	if !m.principalHasLimit {
		return ErrPrincipalQuotaNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.principalBudget = 0
	m.principalHasLimit = false
	return nil
}

// ── mockSpendCounter ──────────────────────────────────────────────────────────

var _ SpendCounter = (*mockSpendCounter)(nil)

type mockSpendCounter struct {
	spend       float64
	getSpendErr error
	incrErr     error
}

func (m *mockSpendCounter) GetSpend(_ context.Context, _, _ string) (float64, error) {
	return m.spend, m.getSpendErr
}

func (m *mockSpendCounter) IncrementSpend(_ context.Context, _, _ string, amount float64) error {
	if m.incrErr != nil {
		return m.incrErr
	}
	m.spend += amount
	return nil
}

// ── mockQuotaResolver ─────────────────────────────────────────────────────────

var _ QuotaResolver = (*mockQuotaResolver)(nil)

type mockQuotaResolver struct {
	budget   float64
	hasLimit bool
	err      error
}

func (m *mockQuotaResolver) Resolve(_ context.Context, _, _ string) (float64, bool, error) {
	return m.budget, m.hasLimit, m.err
}
