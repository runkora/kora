package triggers

import (
	"net/http"
	"testing"
	"time"
)

// deliveryPageBody pins the wire field names the console decodes.
type deliveryPageBody struct {
	Deliveries  []map[string]any `json:"deliveries"`
	HasNextPage bool             `json:"has_next_page"`
	NextCursor  *string          `json:"next_cursor"`
}

func seedForHandler(t *testing.T, deps *testDeps, n int, status string) {
	t.Helper()
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, string(rune('a'+i)))
	}
	seedDeliveries(t, deps.repo, testTenant, ids, at, status)
}

func TestHandleListDeliveries_ResponseFieldNames(t *testing.T) {
	c, deps, _ := newTestController(t)
	seedForHandler(t, deps, 3, StatusVerified)

	w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?limit=2", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := decode[deliveryPageBody](t, w)
	if len(body.Deliveries) != 2 {
		t.Fatalf("deliveries: want 2, got %d", len(body.Deliveries))
	}
	if !body.HasNextPage {
		t.Error("has_next_page: want true with a third delivery waiting")
	}
	if body.NextCursor == nil || *body.NextCursor == "" {
		t.Error("next_cursor: want a cursor alongside has_next_page")
	}
}

// An empty string is not reliably falsy once it has been through a typed client,
// so the last page must report next_cursor as JSON null.
func TestHandleListDeliveries_LastPageNullsTheCursor(t *testing.T) {
	c, deps, _ := newTestController(t)
	seedForHandler(t, deps, 2, StatusVerified)

	w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?limit=10", nil, nil)
	body := decode[deliveryPageBody](t, w)
	if body.HasNextPage {
		t.Error("has_next_page: want false when the page holds everything")
	}
	if body.NextCursor != nil {
		t.Errorf("next_cursor: want null on the last page, got %q", *body.NextCursor)
	}
}

// Ignoring a malformed cursor would silently restart from the newest arrival, which
// reads to the user as "Load more gave me the first page again".
func TestHandleListDeliveries_MalformedCursorIs400(t *testing.T) {
	c, deps, _ := newTestController(t)
	seedForHandler(t, deps, 2, StatusVerified)

	for _, cursor := range []string{"not-a-cursor", "2026-10-07T12:00:00Z", "|", "nope|d1"} {
		t.Run(cursor, func(t *testing.T) {
			w := call(t, c.handleListDeliveries, http.MethodGet,
				"/trigger-deliveries?cursor="+cursor, nil, nil)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status: want 400 for cursor %q, got %d (%s)", cursor, w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleListDeliveries_UnknownStatusIs400(t *testing.T) {
	c, deps, _ := newTestController(t)
	seedForHandler(t, deps, 2, StatusVerified)

	w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?status=exploded", nil, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (%s)", w.Code, w.Body.String())
	}
}

// status repeats rather than taking a comma list, because that is what a multi-select sends.
func TestHandleListDeliveries_RepeatedStatusParams(t *testing.T) {
	c, deps, _ := newTestController(t)
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	seedDeliveries(t, deps.repo, testTenant, []string{"v1"}, at, StatusVerified)
	seedDeliveries(t, deps.repo, testTenant, []string{"f1"}, at, StatusFailed)
	seedDeliveries(t, deps.repo, testTenant, []string{"s1"}, at, StatusSkipped)

	w := call(t, c.handleListDeliveries, http.MethodGet,
		"/trigger-deliveries?status=verified&status=failed", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := decode[deliveryPageBody](t, w)
	if len(body.Deliveries) != 2 {
		t.Fatalf("deliveries: want both selected statuses, got %d", len(body.Deliveries))
	}
	for _, d := range body.Deliveries {
		if got := d["status"]; got == StatusSkipped {
			t.Errorf("deliveries: %q leaked past the filter", got)
		}
	}
}

func TestHandleListDeliveries_BlankStatusMeansNoFilter(t *testing.T) {
	c, deps, _ := newTestController(t)
	seedForHandler(t, deps, 3, StatusVerified)

	w := call(t, c.handleListDeliveries, http.MethodGet,
		"/trigger-deliveries?status=&outcome=", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	if body := decode[deliveryPageBody](t, w); len(body.Deliveries) != 3 {
		t.Fatalf("deliveries: want all 3 unfiltered, got %d", len(body.Deliveries))
	}
}

func TestHandleListDeliveries_CursorRoundTripsThroughTheHandler(t *testing.T) {
	c, deps, _ := newTestController(t)
	seedForHandler(t, deps, 4, StatusVerified)

	first := decode[deliveryPageBody](t, call(t, c.handleListDeliveries, http.MethodGet,
		"/trigger-deliveries?limit=2", nil, nil))
	if first.NextCursor == nil {
		t.Fatal("first page: want a cursor to follow")
	}

	w := call(t, c.handleListDeliveries, http.MethodGet,
		"/trigger-deliveries?limit=2&cursor="+*first.NextCursor, nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("second page: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	second := decode[deliveryPageBody](t, w)

	seen := map[any]bool{}
	for _, d := range first.Deliveries {
		seen[d["id"]] = true
	}
	for _, d := range second.Deliveries {
		if seen[d["id"]] {
			t.Errorf("delivery %v appeared on both pages", d["id"])
		}
	}
	if len(second.Deliveries) != 2 {
		t.Fatalf("second page: want 2 deliveries, got %d", len(second.Deliveries))
	}
}
