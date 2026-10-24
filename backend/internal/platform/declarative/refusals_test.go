package declarative

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestWriteRefusal(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{fmt.Errorf("update agent: %w", ErrManagedByResource), http.StatusConflict, api.ErrCodeManagedByResource},
		{fmt.Errorf("create trigger: %w", ErrAPIAuthoringDisabled), http.StatusForbidden, api.ErrCodeAPIAuthoringDisabled},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		if !WriteRefusal(context.Background(), w, logger, tc.err) {
			t.Fatalf("%v: not written", tc.err)
		}
		var body struct{ Code string }
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != tc.wantStatus || body.Code != tc.wantCode {
			t.Errorf("%v: status %d code %q", tc.err, w.Code, body.Code)
		}
	}
	if WriteRefusal(context.Background(), httptest.NewRecorder(), logger, errors.New("other")) {
		t.Error("an unrelated error was answered")
	}
}

func TestRefKeys(t *testing.T) {
	if RefEnvironment("go") != "environment/go" || RefAgent("a") != "agent/a" || RefSource("s") != "source/s" {
		t.Error("Refs keys changed; they are a wire contract with the services")
	}
}
