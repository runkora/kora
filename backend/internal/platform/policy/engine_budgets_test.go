package policy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// A bundle that cannot be loaded is an error to a display and an empty listing
// to enforcement, which fails open.
func TestListBudgets_BundleUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	e, ok := NewEvaluator(srv.URL, 0, logging.NewLoggerWithOutput(discard{})).(*engine)
	if !ok {
		t.Fatal("NewEvaluator does not return the engine")
	}

	if _, _, err := e.ListBudgetsStrict(context.Background(), "t1"); err == nil {
		t.Error("ListBudgetsStrict: want an error for a bundle that cannot be loaded")
	}
	principals, roles, err := e.ListBudgets(context.Background(), "t1")
	if err != nil || len(principals) != 0 || len(roles) != 0 {
		t.Errorf("ListBudgets = %v, %v, %v; want empty maps and no error", principals, roles, err)
	}
	var _ StrictBudgetLister = e
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
