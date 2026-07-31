package triggers

import (
	"errors"
	"slices"
	"testing"
)

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get(ProviderGitHub); !ok {
		t.Fatal("github adapter must be registered")
	}
	if _, ok := r.Get(ProviderAlertmanager); !ok {
		t.Fatal("alertmanager adapter must be registered")
	}
	if _, ok := r.Get("gitlab"); ok {
		t.Fatal("gitlab must not be registered in v1")
	}
	if err := r.validateProvider(ProviderGitHub); err != nil {
		t.Errorf("validateProvider(github) = %v", err)
	}
	if err := r.validateProvider(ProviderAlertmanager); err != nil {
		t.Errorf("validateProvider(alertmanager) = %v", err)
	}
	if err := r.validateProvider("nope"); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("validateProvider(nope) = %v, want ErrUnknownProvider", err)
	}
}

// Every event type GitHub declares must be one Fields actually has a case for —
// i.e. one that yields more than the common set every event gets.
//
// This is what stops the catalog becoming a wish list: declaring an event type
// without teaching Fields about it fails here, and so does deleting a case from
// Fields while leaving its declaration behind.
func TestGitHubAdapter_DeclaredEventTypesAreModeled(t *testing.T) {
	a := &githubAdapter{}
	body := []byte(`{"action":"opened","ref":"refs/heads/main",
		"pull_request":{"number":1,"base":{"ref":"main"},"head":{"ref":"f"},"user":{"login":"a"}},
		"issue":{"number":2,"title":"t","user":{"login":"a"}},
		"repository":{"full_name":"acme/w"},"sender":{"login":"b"}}`)

	declared := a.EventTypes()
	if len(declared) == 0 {
		t.Fatal("github declares no event types")
	}
	// The fields every event gets regardless of type. A declared type has to
	// earn more than these.
	common := a.Fields("release", body)
	for _, et := range declared {
		if got := a.Fields(et, body); len(got) <= len(common) {
			t.Errorf("github declares %q but Fields extracts nothing specific for it", et)
		}
	}
}

// Alertmanager's catalog is a different shape: Fields does not branch on the
// event type, so what the declaration must match is the closed set of values
// EventType can return. A catalog entry no EventType call produces documents
// a value that can never fire.
func TestAlertmanagerAdapter_DeclaredTypesMatchEventType(t *testing.T) {
	a := &alertmanagerAdapter{}
	declared := a.EventTypes()

	produced := []string{}
	for _, status := range []string{"firing", "resolved"} {
		got := a.EventType(nil, []byte(`{"groupKey":"g","status":"`+status+`"}`))
		if !slices.Contains(declared, got) {
			t.Errorf("EventType(%s) = %q, which is not in the declared catalog %v", status, got, declared)
		}
		produced = append(produced, got)
	}
	for _, d := range declared {
		if !slices.Contains(produced, d) {
			t.Errorf("catalog declares %q, which EventType never produces", d)
		}
	}
}

func TestMatchesCondition(t *testing.T) {
	fields := map[string]string{"action": "opened", "pr.base_ref": "main"}

	tests := []struct {
		name      string
		condition map[string][]string
		want      bool
	}{
		{"empty condition matches all", map[string][]string{}, true},
		{"single clause match", map[string][]string{"action": {"opened"}}, true},
		{"single clause no match", map[string][]string{"action": {"closed"}}, false},
		{"membership match", map[string][]string{"action": {"closed", "opened"}}, true},
		{"multi clause all match", map[string][]string{"action": {"opened"}, "pr.base_ref": {"main"}}, true},
		{"multi clause one fails", map[string][]string{"action": {"opened"}, "pr.base_ref": {"release"}}, false},
		{"empty allowed list is ignored", map[string][]string{"action": {}}, true},
		{"missing field no match", map[string][]string{"missing": {"x"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesCondition(tt.condition, fields); got != tt.want {
				t.Errorf("matchesCondition = %v, want %v", got, tt.want)
			}
		})
	}
}
