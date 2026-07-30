package storage

import "testing"

func TestNullIfZero(t *testing.T) {
	if got := NullIfZero(""); got != nil {
		t.Errorf("empty string = %v, want nil so the column stores NULL", got)
	}
	if got := NullIfZero("x"); got == nil || *got != "x" {
		t.Errorf("non-empty string = %v, want a pointer to \"x\"", got)
	}
	if got := NullIfZero(0); got != nil {
		t.Errorf("zero int = %v, want nil", got)
	}
	if got := NullIfZero(7); got == nil || *got != 7 {
		t.Errorf("non-zero int = %v, want a pointer to 7", got)
	}
}

func TestDeref(t *testing.T) {
	if got := Deref[string](nil); got != "" {
		t.Errorf("nil *string = %q, want the zero value", got)
	}
	if got := Deref[int](nil); got != 0 {
		t.Errorf("nil *int = %d, want 0", got)
	}
	v := "recorded"
	if got := Deref(&v); got != "recorded" {
		t.Errorf("Deref = %q, want %q", got, v)
	}
}

// The pair has to round-trip, because that is how it is used: written through
// NullIfZero and read back through Deref.
func TestNullIfZeroDerefRoundTrip(t *testing.T) {
	for _, in := range []string{"", "a value"} {
		if got := Deref(NullIfZero(in)); got != in {
			t.Errorf("round-trip of %q = %q", in, got)
		}
	}
}
