package triggers

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func mustCatalog(t *testing.T, schema string) *Catalog {
	t.Helper()
	c, err := BuildCatalog([]byte(schema))
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	return c
}

func fieldsByPath(c *Catalog, event string) map[string]Field {
	out := map[string]Field{}
	for _, f := range c.Fields(event) {
		out[f.Path] = f
	}
	return out
}

func TestBuildCatalog_Walk(t *testing.T) {
	c := mustCatalog(t, `{
		"events": {
			"comment": {"oneOf": [
				{"$ref": "#/definitions/created"},
				{"$ref": "#/definitions/deleted"}
			]}
		},
		"definitions": {
			"created": {"allOf": [{"$ref": "#/definitions/base"}, {"properties": {
				"action": {"type": "string", "enum": ["created"]}
			}}]},
			"deleted": {"allOf": [{"$ref": "#/definitions/base"}, {"properties": {
				"action": {"type": "string", "enum": ["deleted", null, true]}
			}}]},
			"base": {"type": "object", "properties": {
				"comment": {"type": "object", "properties": {
					"body": {"type": "string", "description": "Comment text"},
					"labels": {"type": "array", "items": {"type": "object", "properties": {"name": {"type": "string"}}}},
					"reactions": {"type": "object", "properties": {"+1": {"type": "integer"}, "total": {"type": "integer"}}}
				}},
				"user": {"$ref": "#/definitions/user", "description": "The acting user"},
				"number": {"type": "integer"}
			}},
			"user": {"type": "object", "description": "A user", "properties": {"login": {"type": "string"}}}
		}
	}`)

	if got := c.EventTypes(); !slices.Equal(got, []string{"comment"}) {
		t.Fatalf("event types = %v", got)
	}
	f := fieldsByPath(c, "comment")

	checks := []struct {
		path  string
		group string
	}{
		{"action", eventGroup},
		{"number", eventGroup},
		{"comment", "comment"},
		{"comment.body", "comment"},
		{"comment.labels", "comment"},
		{"comment.labels.0.name", "comment"},
		{"comment.reactions.total", "comment"},
		{"user", "user"},
		{"user.login", "user"},
	}
	for _, want := range checks {
		got, ok := f[want.path]
		if !ok {
			t.Errorf("missing field %s", want.path)
			continue
		}
		if got.Group != want.group {
			t.Errorf("%s group = %q, want %q", want.path, got.Group, want.group)
		}
	}
	if _, ok := f["comment.reactions.+1"]; ok {
		t.Error("a segment outside the key grammar must be skipped")
	}
	if !slices.Equal(f["action"].Enum, []string{"created", "deleted", "true"}) {
		t.Errorf("action enum = %v, want variants merged, null dropped, bool as text", f["action"].Enum)
	}
	if f["comment.body"].Description != "Comment text" {
		t.Errorf("description = %q", f["comment.body"].Description)
	}
	if f["user"].Description != "The acting user" {
		t.Errorf("a $ref sibling description must win, got %q", f["user"].Description)
	}
}

func TestBuildCatalog_PatternFields(t *testing.T) {
	c := mustCatalog(t, `{
		"events": {"e": {"type": "object", "properties": {
			"labels": {"type": "object", "additionalProperties": {"type": "string"}},
			"open": {"type": "object", "additionalProperties": true},
			"closed": {"type": "object", "additionalProperties": false},
			"mixed": {"type": "object", "properties": {"a": {"type": "string"}}, "additionalProperties": true}
		}}},
		"definitions": {}
	}`)
	f := fieldsByPath(c, "e")
	for _, p := range []string{"labels.<name>", "open.<name>"} {
		if !f[p].Pattern || f[p].Group != strings.Split(p, ".")[0] {
			t.Errorf("%s = %+v, want a pattern field in its group", p, f[p])
		}
	}
	for _, p := range []string{"closed.<name>", "mixed.<name>"} {
		if _, ok := f[p]; ok {
			t.Errorf("%s must not be a pattern: the object is closed or has its own properties", p)
		}
	}
	if f["labels"].Group != "labels" {
		t.Errorf("a map is not a top-level scalar, got group %q", f["labels"].Group)
	}
}

func TestBuildCatalog_RecursiveSchema(t *testing.T) {
	c := mustCatalog(t, `{
		"events": {"e": {"$ref": "#/definitions/node"}},
		"definitions": {"node": {"type": "object", "properties": {
			"name": {"type": "string"},
			"parent": {"$ref": "#/definitions/node"}
		}}}
	}`)
	f := fieldsByPath(c, "e")
	if _, ok := f["parent"]; !ok {
		t.Error("the recursive property itself is a field")
	}
	if _, ok := f["parent.name"]; ok {
		t.Error("a definition already on the branch must not be descended again")
	}
}

func TestBuildCatalog_DepthCap(t *testing.T) {
	nested := `{"type": "string"}`
	for i := 20; i > 0; i-- {
		nested = fmt.Sprintf(`{"type": "object", "properties": {"l%d": %s}}`, i, nested)
	}
	c := mustCatalog(t, `{"events": {"e": `+nested+`}, "definitions": {}}`)
	deepest := 0
	for _, fl := range c.Fields("e") {
		if fl.Group != metaGroup {
			deepest = max(deepest, strings.Count(fl.Path, ".")+1)
		}
	}
	if deepest != maxCatalogDepth {
		t.Errorf("deepest path = %d segments, want the cap of %d", deepest, maxCatalogDepth)
	}
}

func TestBuildCatalog_Errors(t *testing.T) {
	tests := map[string]string{
		"not json":       `{`,
		"no events":      `{"events": {}, "definitions": {}}`,
		"unknown ref":    `{"events": {"e": {"$ref": "#/definitions/missing"}}, "definitions": {}}`,
		"reserved meta":  `{"events": {"e": {"properties": {"meta": {"type": "string"}}}}, "definitions": {}}`,
		"nested unknown": `{"events": {"e": {"properties": {"a": {"$ref": "#/definitions/missing"}}}}, "definitions": {}}`,
	}
	for name, schema := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildCatalog([]byte(schema)); !errors.Is(err, ErrInvalidSchema) {
				t.Errorf("err = %v, want ErrInvalidSchema", err)
			}
		})
	}
}

// An event type the schema does not declare still gets the meta fields.
func TestCatalog_FieldsOfUndeclaredEvent(t *testing.T) {
	c := mustCatalog(t, `{"events": {"e": {"properties": {"a": {"type": "string"}}}}, "definitions": {}}`)
	got := c.Fields("unknown")
	if len(got) != len(metaFields) {
		t.Fatalf("fields = %+v, want only the meta fields", got)
	}
	for _, f := range got {
		if f.Group != metaGroup {
			t.Errorf("%s group = %q, want meta", f.Path, f.Group)
		}
	}
}

// Every registered provider's schema builds into a catalog at construction.
func TestNewRegistry_BuildsEveryCatalog(t *testing.T) {
	r, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	for _, p := range r.Providers() {
		c, ok := r.Catalog(p)
		if !ok || len(c.EventTypes()) == 0 {
			t.Errorf("%s has no catalog or no event types", p)
		}
	}
	if _, ok := r.Catalog("gitlab"); ok {
		t.Error("an unknown provider has no catalog")
	}
}
