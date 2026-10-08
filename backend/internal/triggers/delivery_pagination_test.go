package triggers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// seedDeliveries writes n deliveries that all share receivedAt, so the only
// thing separating them is the id. That is the case a received_at-only cursor
// gets wrong, and it is common: a webhook burst lands many rows on one
// timestamp.
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

func TestListDeliveriesCursorSeparatesRowsSharingATimestamp(t *testing.T) {
	svc, deps := newTestService(t)
	repo := deps.repo
	ctx := context.Background()
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	seedDeliveries(t, repo, testTenant, []string{"d1", "d2", "d3", "d4"}, at, StatusVerified)

	first, next, err := svc.ListDeliveries(ctx, testTenant, DeliveryFilter{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("first page: want 2 deliveries, got %d", len(first))
	}
	if next == "" {
		t.Fatal("first page: want a next cursor, got none")
	}

	cursor, err := parseDeliveryCursor(next)
	if err != nil {
		t.Fatalf("parse cursor %q: %v", next, err)
	}
	second, _, err := svc.ListDeliveries(ctx, testTenant, DeliveryFilter{Limit: 2, Cursor: cursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}

	// The real assertion: no delivery appears on both pages, and between them
	// they cover every row. A received_at-only cursor would either skip or
	// repeat here, because all four share a timestamp.
	seen := map[string]int{}
	for _, d := range append(append([]Delivery{}, first...), second...) {
		seen[d.ID]++
	}
	if len(seen) != 4 {
		t.Fatalf("want 4 distinct deliveries across both pages, got %d: %v", len(seen), seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("delivery %s appeared %d times across pages, want exactly 1", id, n)
		}
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

func TestListDeliveriesFiltersByStatusAcrossPages(t *testing.T) {
	svc, deps := newTestService(t)
	repo := deps.repo
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	seedDeliveries(t, repo, testTenant, []string{"ok1", "ok2"}, at, StatusDispatched)
	seedDeliveries(t, repo, testTenant, []string{"bad1", "bad2", "bad3"}, at, StatusFailed)

	var got []string
	var cursor *deliveryCursor
	for {
		page, next, err := svc.ListDeliveries(context.Background(), testTenant,
			DeliveryFilter{Status: []string{StatusFailed}, Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, d := range page {
			if d.Status != StatusFailed {
				t.Fatalf("status filter leaked %q", d.Status)
			}
			got = append(got, d.ID)
		}
		if next == "" {
			break
		}
		if cursor, err = parseDeliveryCursor(next); err != nil {
			t.Fatalf("parse cursor: %v", err)
		}
	}
	// Paging applies to the filtered set, so every failed delivery is reachable
	// and no dispatched one appears.
	if len(got) != 3 {
		t.Fatalf("want all 3 failed deliveries across pages, got %d: %v", len(got), got)
	}
}

func TestListDeliveriesRejectsUnknownFilterValues(t *testing.T) {
	svc, _ := newTestService(t)

	for name, filter := range map[string]DeliveryFilter{
		"status":  {Status: []string{"nonsense"}},
		"outcome": {Outcome: []string{"nonsense"}},
	} {
		t.Run(name, func(t *testing.T) {
			// Ignoring a typo would return an unfiltered page, which reads as a
			// bug rather than as the mistake it is.
			if _, _, err := svc.ListDeliveries(context.Background(), testTenant, filter); !errors.Is(err, ErrValidation) {
				t.Fatalf("want ErrValidation for an unknown %s, got %v", name, err)
			}
		})
	}
}

func TestListDeliveriesAcceptsEveryDeclaredStatusAndOutcome(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Guards the issue's own mistake: it listed four of the seven outcomes.
	// Deriving the sets from the constants means the filter can never accept
	// fewer values than the pipeline can write.
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
	// Nanoseconds must survive: two deliveries in the same burst can be a few
	// hundred nanoseconds apart, and a truncating format would collapse them.
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
	// An absent cursor is "first page", not a malformed one.
	c, err := parseDeliveryCursor("")
	if err != nil || c != nil {
		t.Errorf("empty cursor: want (nil, nil), got (%v, %v)", c, err)
	}
}

func TestNonEmptyDropsBlankFilterValues(t *testing.T) {
	// url.Values reports a present-but-empty ?status= as []string{""}, which
	// would otherwise fail validation and 400 a request whose plain meaning is
	// "no filter".
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

	// Asking for more than the ceiling returns the ceiling, not the default. The
	// old behaviour handed back 100 rows for limit=501 and 500 for limit=500,
	// so asking for more returned five times fewer.
	page, _, err := svc.ListDeliveries(context.Background(), testTenant,
		DeliveryFilter{Limit: maxDeliveryPageSize + 1})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page) != maxDeliveryPageSize {
		t.Fatalf("over-ceiling limit returned %d rows, want the ceiling %d", len(page), maxDeliveryPageSize)
	}
}

// The two reads behind a page can each fail, and the service has to say so
// rather than hand back a half-built page. A caller that gets rows plus a nil
// error believes it saw everything that matched.
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
		// Wrapped, not replaced: the caller needs to know which read failed.
		if !strings.Contains(err.Error(), "attach delivery runs") {
			t.Errorf("want the error to name the step, got %q", err)
		}
		if next != "" {
			t.Errorf("want no cursor alongside an error, got %q", next)
		}
	})
}
