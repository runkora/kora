package triggers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// seedDeliveries writes every delivery at the same receivedAt, the webhook-burst
// case a received_at-only cursor gets wrong.
func seedDeliveries(t *testing.T, repo *mockRepository, tenant string, ids []string, receivedAt time.Time, status string) {
	t.Helper()
	for _, id := range ids {
		repo.deliveries = append(repo.deliveries, &Delivery{
			ID:         id,
			TenantID:   tenant,
			Status:     status,
			Kind:       KindWebhook,
			ReceivedAt: receivedAt,
		})
	}
}

func TestListDeliveriesLastPageReportsNoCursor(t *testing.T) {
	svc, deps := newTestService(t)
	repo := deps.repo
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	seedDeliveries(t, repo, testTenant, []string{"d1", "d2"}, at, StatusVerified)

	page, next, err := svc.ListDeliveries(context.Background(), testTenant, DeliveryFilter{Limit: 5})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("want 2 deliveries, got %d", len(page))
	}
	if next != "" {
		t.Errorf("want no cursor on the last page, got %q", next)
	}
}

func TestListDeliveriesRejectsUnknownFilterValues(t *testing.T) {
	svc, _ := newTestService(t)

	for name, filter := range map[string]DeliveryFilter{
		"status":  {Status: []string{"nonsense"}},
		"outcome": {Outcome: []string{"nonsense"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := svc.ListDeliveries(context.Background(), testTenant, filter); !errors.Is(err, ErrValidation) {
				t.Fatalf("want ErrValidation for an unknown %s, got %v", name, err)
			}
		})
	}
}

func TestListDeliveriesAcceptsEveryDeclaredStatusAndOutcome(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// This can only prove the filter accepts whatever the constants declare: drop a
	// value and the validator drops it too, so this stays green while ?status=<it>
	// 400s. TestPostgres_FilterableStatusesMatchTheDatabase is the real guard.
	if _, _, err := svc.ListDeliveries(ctx, testTenant, DeliveryFilter{Status: AllDeliveryStatuses()}); err != nil {
		t.Errorf("every declared delivery status should be filterable: %v", err)
	}
	if _, _, err := svc.ListDeliveries(ctx, testTenant, DeliveryFilter{Outcome: AllRunStatuses()}); err != nil {
		t.Errorf("every declared run outcome should be filterable: %v", err)
	}
}

func TestDeliveryCursorRoundTrips(t *testing.T) {
	want := deliveryCursor{
		ReceivedAt: time.Date(2026, 10, 7, 12, 0, 0, 123456789, time.UTC),
		ID:         "d-1",
	}
	got, err := parseDeliveryCursor(want.encode())
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	// Nanoseconds must survive: a truncating format would collapse two rows from one burst.
	if !got.ReceivedAt.Equal(want.ReceivedAt) || got.ID != want.ID {
		t.Fatalf("round trip lost data: got %+v, want %+v", *got, want)
	}
}

func TestParseDeliveryCursorRejectsMalformedValues(t *testing.T) {
	for _, raw := range []string{"no-separator", "|d1", "2026-10-07T12:00:00Z|", "not-a-time|d1"} {
		if _, err := parseDeliveryCursor(raw); err == nil {
			t.Errorf("cursor %q: want an error, got none", raw)
		}
	}
	c, err := parseDeliveryCursor("")
	if err != nil || c != nil {
		t.Errorf("empty cursor: want (nil, nil), got (%v, %v)", c, err)
	}
}

func TestNonEmptyDropsBlankFilterValues(t *testing.T) {
	// url.Values reports a present-but-empty ?status= as []string{""}, which would
	// otherwise fail validation and 400 a request meaning "no filter".
	for name, tc := range map[string]struct {
		in   []string
		want int
	}{
		"present but blank": {in: []string{""}, want: 0},
		"absent":            {in: nil, want: 0},
		"blank among real":  {in: []string{"", StatusFailed, ""}, want: 1},
		"all real":          {in: []string{StatusFailed, StatusMissed}, want: 2},
	} {
		t.Run(name, func(t *testing.T) {
			if got := len(nonEmpty(tc.in)); got != tc.want {
				t.Fatalf("nonEmpty(%q) kept %d values, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestListDeliveriesClampsAnOverCeilingLimitToTheCeiling(t *testing.T) {
	svc, deps := newTestService(t)
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 0, maxDeliveryPageSize+10)
	for i := range maxDeliveryPageSize + 10 {
		ids = append(ids, fmt.Sprintf("d-%04d", i))
	}
	seedDeliveries(t, deps.repo, testTenant, ids, at, StatusVerified)

	page, _, err := svc.ListDeliveries(context.Background(), testTenant,
		DeliveryFilter{Limit: maxDeliveryPageSize + 1})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page) != maxDeliveryPageSize {
		t.Fatalf("over-ceiling limit returned %d rows, want the ceiling %d", len(page), maxDeliveryPageSize)
	}
}

func TestListDeliveriesSurfacesRepositoryFailures(t *testing.T) {
	boom := errors.New("connection reset")

	t.Run("the page query failing", func(t *testing.T) {
		svc, deps := newTestService(t)
		deps.repo.listDeliveriesErr = boom
		_, next, err := svc.ListDeliveries(context.Background(), testTenant, DeliveryFilter{Limit: 10})
		if !errors.Is(err, boom) {
			t.Fatalf("want the repository error through, got %v", err)
		}
		if next != "" {
			t.Errorf("want no cursor alongside an error, got %q", next)
		}
	})

	t.Run("attaching the fan-out failing", func(t *testing.T) {
		svc, deps := newTestService(t)
		at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
		seedDeliveries(t, deps.repo, testTenant, []string{"d1"}, at, StatusVerified)
		deps.repo.deliveryRunsErr = boom

		_, next, err := svc.ListDeliveries(context.Background(), testTenant, DeliveryFilter{Limit: 10})
		if !errors.Is(err, boom) {
			t.Fatalf("want the wrapped repository error, got %v", err)
		}
		if !strings.Contains(err.Error(), "attach delivery runs") {
			t.Errorf("want the error to name the step, got %q", err)
		}
		if next != "" {
			t.Errorf("want no cursor alongside an error, got %q", next)
		}
	})

	t.Run("attaching the coalescing keys failing", func(t *testing.T) {
		svc, deps := newTestService(t)
		at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
		seedDeliveries(t, deps.repo, testTenant, []string{"d1"}, at, StatusVerified)
		deps.repo.deliveryKeysErr = boom

		_, next, err := svc.ListDeliveries(context.Background(), testTenant, DeliveryFilter{Limit: 10})
		if !errors.Is(err, boom) {
			t.Fatalf("want the wrapped repository error, got %v", err)
		}
		if !strings.Contains(err.Error(), "attach delivery keys") {
			t.Errorf("want the error to name the step, got %q", err)
		}
		if next != "" {
			t.Errorf("want no cursor alongside an error, got %q", next)
		}
	})
}
