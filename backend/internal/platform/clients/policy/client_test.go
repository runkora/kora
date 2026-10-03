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

func TestListGrantsByType_PagesUntilAShortPage(t *testing.T) {
	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		offsets = append(offsets, q.Get("offset"))
		if q.Get("principal_type") != "AGENT" || q.Get("principal_id") != "" || q.Get("limit") != strconv.Itoa(grantPageSize) {
			t.Errorf("query = %v", q)
		}
		n := grantPageSize
		if q.Get("offset") != "0" {
			n = 3
		}
		grants := make([]Grant, n)
		_ = json.NewEncoder(w).Encode(map[string]any{"grants": grants})
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL, func(context.Context) (string, error) { return "tok", nil }, logging.NewLoggerWithOutput(io.Discard))
	got, err := c.ListGrantsByType(context.Background(), "t1", "AGENT")
	if err != nil {
		t.Fatalf("ListGrantsByType: %v", err)
	}
	if len(got) != grantPageSize+3 || len(offsets) != 2 || offsets[1] != strconv.Itoa(grantPageSize) {
		t.Errorf("got %d grants over offsets %v", len(got), offsets)
	}
}
