package triggers

import (
	"net/http"
	"testing"
	"time"
)

// pgx decodes timestamptz into the process's local zone, so on a pod with TZ
// set the cursor would render a "+05:30" offset. A query string decodes "+" as
// a space, so such a cursor cannot survive the round trip the API documents and
// every page after the first would 400. These two tests are the reason encode()
// forces UTC; both fail without it.
func TestCursorEncodingIsTimezoneIndependent(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		// A stripped image without tzdata cannot exercise the bug; skipping is
		// honest where failing would blame the code for the image.
		t.Skipf("no tzdata for Asia/Kolkata: %v", err)
	}
	at := time.Date(2026, 10, 7, 12, 0, 0, 123456000, ist)
	c := deliveryCursor{ReceivedAt: at, ID: "d1"}
	enc := c.encode()
	t.Logf("encoded cursor = %q", enc)

	// A query string decodes "+" as a space, so a cursor containing "+" cannot
	// survive the round trip the API documents.
	for _, r := range enc {
		if r == '+' {
			t.Fatalf("cursor %q contains '+', which r.URL.Query() decodes as a space", enc)
		}
	}
	if _, err := parseDeliveryCursor(enc); err != nil {
		t.Fatalf("own cursor does not parse back: %v", err)
	}
}

// The same thing end to end, because the unit test above would still pass if
// the handler mangled the value on the way out.
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

	// Echoed back verbatim, as curl and the documented contract would.
	w := call(t, c.handleListDeliveries, http.MethodGet,
		"/trigger-deliveries?limit=2&cursor="+*first.NextCursor, nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("second page: want 200, got %d (%s)", w.Code, w.Body.String())
	}
}
