package events

import (
	"slices"
	"testing"
)

func TestChanged(t *testing.T) {
	type entity struct {
		Name    string            `json:"name"`
		Scopes  []string          `json:"scopes"`
		Labels  map[string]string `json:"labels,omitempty"`
		Enabled bool              `json:"enabled"`
	}
	before := entity{Name: "a", Scopes: []string{"x"}, Labels: map[string]string{"k": "v"}, Enabled: true}
	after := entity{Name: "a", Scopes: []string{"x", "y"}, Enabled: true}
	if got, want := Changed(before, after), []string{"labels", "scopes"}; !slices.Equal(got, want) {
		t.Errorf("Changed = %v, want %v", got, want)
	}
	if got := Changed(before, before); len(got) != 0 {
		t.Errorf("Changed of an unchanged entity = %v", got)
	}
}
