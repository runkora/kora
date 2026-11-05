package skillsconfig_test

import (
	"strings"
	"testing"

	sc "github.com/runkora/kora/backend/cmd/skills-service/config"
)

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		cache, failure int
		wantErr        string
	}{
		{43200, 60, ""},
		{0, 60, "cacheTTLSecs"},
		{-1, 60, "cacheTTLSecs"},
		{43200, 0, "failureTTLSecs"},
		{43200, -5, "failureTTLSecs"},
	} {
		var c sc.Config
		c.Marketplace.CacheTTLSecs, c.Marketplace.FailureTTLSecs = tc.cache, tc.failure
		err := c.Validate()
		if (err == nil) != (tc.wantErr == "") || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("Validate(%d, %d) = %v, want %q", tc.cache, tc.failure, err, tc.wantErr)
		}
	}
	if err := sc.Current.Validate(); err != nil {
		t.Errorf("base.yaml does not validate: %v", err)
	}
}
