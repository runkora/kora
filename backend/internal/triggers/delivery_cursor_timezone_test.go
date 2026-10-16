package triggers

import (
	"net/http"
	"testing"
	"time"
)

// pgx decodes timestamptz into the process's local zone, so with TZ set the cursor
// would carry a "+05:30" offset — and a query string decodes "+" as a space, so
// every page after the first would 400. Both tests fail unless encode() forces UTC.
func TestCursorEncodingIsTimezoneIndependent(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Skipf("no tzdata for Asia/Kolkata: %v", err)
	}
	at := time.Date(2026, 10, 7, 12, 0, 0, 123456000, ist)
	c := deliveryCursor{ReceivedAt: at, ID: "d1"}
	enc := c.encode()
	t.Logf("encoded cursor = %q", enc)

	for _, r := range enc {
		if r == '+' {
			t.Fatalf("cursor %q contains '+', which r.URL.Query() decodes as a space", enc)
		}
	}
	if _, err := parseDeliveryCursor(enc); err != nil {
		t.Fatalf("own cursor does not parse back: %v", err)
	}
}

// End to end: the unit test above would still pass if the handler mangled the value.
func TestHandleListDeliveries_CursorRoundTripsFromANonUTCZone(t *testing.T) {
	c, deps, _ := newTestController(t)
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Skipf("no tzdata for Asia/Kolkata: %v", err)
	}
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, ist)
	seedDeliveries(t, deps.repo, testTenant, []string{"a", "b", "c", "d"}, at, StatusVerified)

	first := decode[deliveryPageBody](t, call(t, c.handleListDeliveries, http.MethodGet,
		"/trigger-deliveries?limit=2", nil, nil))
	if first.NextCursor == nil {
		t.Fatal("want a cursor")
	}
	t.Logf("cursor handed to the client = %q", *first.NextCursor)

	w := call(t, c.handleListDeliveries, http.MethodGet,
		"/trigger-deliveries?limit=2&cursor="+*first.NextCursor, nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("second page: want 200, got %d (%s)", w.Code, w.Body.String())
	}
}
