package harnesses

import "testing"

func TestListIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, h := range All() {
		if h.ID == "" || h.Label == "" || h.Description == "" {
			t.Errorf("harness %+v has an empty field", h)
		}
		if seen[h.ID] {
			t.Errorf("duplicate harness ID %q", h.ID)
		}
		seen[h.ID] = true
	}
	if !Valid(Default) {
		t.Errorf("Default %q is not in the list", Default)
	}
	if Valid("") || Valid("bogus") {
		t.Error("Valid accepted an unsupported ID")
	}
	if len(IDs()) != len(All()) {
		t.Error("IDs and All differ in length")
	}
}
