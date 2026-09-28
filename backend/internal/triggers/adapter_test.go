package triggers

import (
	"errors"
	"slices"
	"testing"
)

func TestRegistry(t *testing.T) {
	r := mustRegistry()
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

// Alertmanager's declared event types are exactly the values EventType produces.
func TestAlertmanagerAdapter_DeclaredTypesMatchEventType(t *testing.T) {
	c := mustRegistry().Catalog(ProviderAlertmanager)
	if c == nil {
		t.Fatal("alertmanager has no catalog")
	}
	a := &alertmanagerAdapter{}
	var produced []string
	for _, status := range []string{"firing", "resolved"} {
		produced = append(produced, a.EventType(nil, []byte(`{"groupKey":"g","status":"`+status+`"}`)))
	}
	if !slices.Equal(c.EventTypes(), produced) {
		t.Errorf("declared %v, EventType produces %v", c.EventTypes(), produced)
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

func TestMatchesContains(t *testing.T) {
	fields := map[string]string{"comment.body": "Hey @Kora-Agent, please review", "action": "created"}

	tests := []struct {
		name     string
		contains map[string][]string
		want     bool
	}{
		{"no entries match all", nil, true},
		{"substring ignoring case", map[string][]string{"comment.body": {"@kora-agent"}}, true},
		{"absent substring", map[string][]string{"comment.body": {"@other-bot"}}, false},
		{"any listed substring", map[string][]string{"comment.body": {"@other-bot", "PLEASE"}}, true},
		{"every entry must pass", map[string][]string{"comment.body": {"review"}, "action": {"edit"}}, false},
		{"missing field contains nothing", map[string][]string{"comment.missing": {"x"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesContains(tt.contains, fields); got != tt.want {
				t.Errorf("matchesContains = %v, want %v", got, tt.want)
			}
		})
	}
}
