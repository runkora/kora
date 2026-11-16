package events

import (
	"bytes"
	"encoding/json"
	"slices"
)

// Changed lists the JSON fields whose values differ between two entities of
// the same type, sorted. Producers pass it the before and after of an update.
func Changed(before, after any) []string {
	b, a := fields(before), fields(after)
	var out []string
	for k, v := range a {
		if !bytes.Equal(b[k], v) {
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// fields is the top-level JSON object of v. An entity that does not marshal
// to an object has no fields.
func fields(v any) map[string]json.RawMessage {
	body, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return nil
	}
	return m
}
