package policy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// grantServer serves total grants, at most serverCap per page, and counts requests.
func grantServer(t *testing.T, total, serverCap int) (*Client, *int) {
	t.Helper()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		if q.Get("principal_type") != "AGENT" || q.Get("principal_id") != "" || q.Get("limit") != strconv.Itoa(grantPageSize) {
			t.Errorf("query = %v", q)
		}
		offset, _ := strconv.Atoi(q.Get("offset"))
		n := min(serverCap, max(total-offset, 0))
		_ = json.NewEncoder(w).Encode(map[string]any{"grants": make([]Grant, n), "total": total})
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, func(context.Context) (string, error) { return "tok", nil }, logging.NewLoggerWithOutput(io.Discard))
	return c, &requests
}

func TestListGrantsByType(t *testing.T) {
	cases := []struct {
		name         string
		total        int
		serverCap    int
		wantRequests int
	}{
		{"no grants", 0, grantPageSize, 1},
		{"fewer than a page", 3, grantPageSize, 1},
		{"exactly one full page", grantPageSize, grantPageSize, 1},
		{"a page and a few", grantPageSize + 3, grantPageSize, 2},
		{"exactly two full pages", 2 * grantPageSize, grantPageSize, 2},
		// A server that caps pages below what was asked for still returns every grant.
		{"server cap below the page size", 1200, 500, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, requests := grantServer(t, tc.total, tc.serverCap)
			got, err := c.ListGrantsByType(context.Background(), "t1", "AGENT")
			if err != nil {
				t.Fatalf("ListGrantsByType: %v", err)
			}
			if len(got) != tc.total || *requests != tc.wantRequests {
				t.Errorf("got %d grants in %d requests, want %d in %d", len(got), *requests, tc.total, tc.wantRequests)
			}
		})
	}
}
