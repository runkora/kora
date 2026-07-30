package storage

// Helpers for the pointer/SQL-NULL boundary.
//
// Every repository that stores an optional column reinvented these: the same
// empty-to-NULL mapping appeared as nullableStr, nullableText, nullableInt,
// nullableRunStr and nullableTime across five packages, each with its own name
// and each having to remember which zero value meant "not recorded". Six
// spellings of one idea is five chances to get it subtly different, so they live
// here once.

// NullIfZero returns nil for the zero value and a pointer to v otherwise, so a
// column reads as SQL NULL when nothing was recorded.
//
// The distinction matters on read as much as write: a nullable column that
// stores "" is indistinguishable from one that stores nothing, and callers that
// need to tell an empty string from an absent one lose that once it round-trips.
func NullIfZero[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// Deref returns the value p points at, or the zero value when p is nil, for
// scanning a nullable column into a plain field.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
