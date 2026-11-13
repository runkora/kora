package connections

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseAPIKeyConfig_Defaults(t *testing.T) {
	for _, raw := range []string{"", "null", " {} ", `{"placeholderEnv":"NPM_TOKEN"}`} {
		cfg, err := ParseAPIKeyConfig(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("ParseAPIKeyConfig(%q) = %v", raw, err)
		}
		if len(cfg.Headers) != 1 || cfg.Headers[0].Name != "Authorization" || cfg.Headers[0].Prefix != "Bearer " {
			t.Errorf("ParseAPIKeyConfig(%q) headers = %+v, want the default", raw, cfg.Headers)
		}
	}
}

func TestParseAPIKeyConfig_Invalid(t *testing.T) {
	for _, raw := range []string{
		`{"headers":[]}`,
		`{"headers":[{"name":"Host"}]}`,
		`{"headers":[{"name":"x-envoy-a"}]}`,
		`{"headers":[{"name":"a"},{"name":"A"}]}`,
		`{"headers":[{"name":"` + strings.Repeat("a", MaxHeaderNameLength+1) + `"}]}`,
		`{"headers":[{"name":"a","prefix":"` + strings.Repeat("p", MaxHeaderPrefixLength+1) + `"}]}`,
		`{"headers":[{"name":"a b"}]}`,
		`{"unknown":1}`,
		`{} {}`,
	} {
		if _, err := ParseAPIKeyConfig(json.RawMessage(raw)); !errors.Is(err, ErrInvalidAPIKeyConfig) {
			t.Errorf("ParseAPIKeyConfig(%q) = %v, want ErrInvalidAPIKeyConfig", raw, err)
		}
	}
}

func TestIsReservedHeader(t *testing.T) {
	for name, want := range map[string]bool{
		"Host": true, "X-Forwarded-For": true, "x-envoy-foo": true, "X-Kora-Run-Id": true,
		"x-api-key": false, "Authorization": false,
	} {
		if got := IsReservedHeader(name); got != want {
			t.Errorf("IsReservedHeader(%q) = %v, want %v", name, got, want)
		}
	}
}
