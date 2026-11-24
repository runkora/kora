package sessionmanager

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	identityproxyclient "github.com/runkora/kora/backend/internal/platform/clients/identityproxy"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

type tokenStub struct{ err error }

func (s tokenStub) TokenReady(context.Context) error { return s.err }

func TestHandleBound(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"token held":          {nil, http.StatusOK},
		"no token":            {identityproxyclient.ErrNoToken, http.StatusServiceUnavailable},
		"sidecar unreachable": {errors.New("dial tcp 127.0.0.1:9090: connection refused"), http.StatusServiceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			c := &Controller{logger: logging.NewLoggerWithOutput(io.Discard), cfg: ControllerConfig{Token: tokenStub{tc.err}}}
			rec := httptest.NewRecorder()
			c.handleBound(rec, httptest.NewRequest(http.MethodGet, "/health/bound", nil))
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}
