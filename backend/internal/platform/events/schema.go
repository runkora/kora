package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Schema is a JSON Schema document.
type Schema = map[string]any

// SchemaBaseDraft is the JSON Schema dialect of every type schema.
const SchemaBaseDraft = "https://json-schema.org/draft/2020-12/schema"

// TypeSchema returns the JSON Schema of an event of type t: the envelope with
// data.entity as the type's payload. It reports false for a type outside the catalog.
func TypeSchema(t Type) (Schema, bool) {
	e, ok := catalog[t]
	if !ok {
		return nil, false
	}
	s := schemaOf(reflect.TypeFor[Envelope]())
	s["$schema"], s["title"], s["description"] = SchemaBaseDraft, string(t), e.description
	props := s["properties"].(Schema)
	props["type"] = Schema{"const": string(t), "description": "The event type."}
	data := props["data"].(Schema)["properties"].(Schema)
	data["entity"] = withDescription(schemaOf(e.payload), "The resource after the change, or before it for a delete.")
	data["changed"].(Schema)["items"] = Schema{"type": "string", "enum": changeable(e.payload)}
	return s, true
}

// Field is one dotted path an event of a type carries.
type Field struct {
	Path        string   `json:"path"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// Fields lists every path in the schema of type t, objects included, in path
// order. It reports false for a type outside the catalog.
func Fields(t Type) ([]Field, bool) {
	s, ok := TypeSchema(t)
	if !ok {
		return nil, false
	}
	var out []Field
	collectFields(s, "", &out)
	slices.SortFunc(out, func(a, b Field) int { return strings.Compare(a.Path, b.Path) })
	return out, true
}

func collectFields(s Schema, prefix string, out *[]Field) {
	props, _ := s["properties"].(Schema)
	for name, raw := range props {
		p := raw.(Schema)
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		f := Field{Path: path, Description: stringOf(p["description"])}
		switch {
		case p["const"] != nil:
			f.Type, f.Enum = "string", []string{fmt.Sprint(p["const"])}
		case p["type"] == "array":
			// An array's enum lists the values its items may take.
			items, _ := p["items"].(Schema)
			f.Type, f.Enum = "array", enumStrings(items["enum"])
		default:
			f.Type = stringOf(p["type"])
			f.Enum = enumStrings(p["enum"])
		}
		*out = append(*out, f)
		if f.Type == "object" {
			collectFields(p, path, out)
		}
	}
}

// changeable lists the field names an update of payload can change: its own
// json names and the hidden fields its producers compare.
func changeable(payload reflect.Type) []string {
	names := slices.Clone(hiddenChanges[payload])
	for field := range payload.Fields() {
		if name, _ := jsonName(field); name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

var timeType = reflect.TypeFor[time.Time]()

// schemaOf derives the schema of Go type t from its json, doc and enum tags.
func schemaOf(t reflect.Type) Schema {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == timeType:
		return Schema{"type": "string", "format": "date-time"}
	case t.Kind() == reflect.Struct:
		return structSchema(t)
	case t.Kind() == reflect.Slice:
		return Schema{"type": "array", "items": schemaOf(t.Elem())}
	case t.Kind() == reflect.Bool:
		return Schema{"type": "boolean"}
	case t.Kind() >= reflect.Int && t.Kind() <= reflect.Uint64:
		return Schema{"type": "integer"}
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		return Schema{"type": "number"}
	case t.Kind() == reflect.String:
		return Schema{"type": "string"}
	default:
		// The entity field is typed any. TypeSchema replaces it.
		return Schema{}
	}
}

// structSchema is an object schema with a property per json field. A field
// that is neither a pointer nor omitempty is required.
func structSchema(t reflect.Type) Schema {
	props, required := Schema{}, []string{}
	for f := range t.Fields() {
		name, optional := jsonName(f)
		if name == "" {
			continue
		}
		p := withDescription(schemaOf(f.Type), f.Tag.Get("doc"))
		if set := f.Tag.Get("enum"); set != "" {
			p["enum"] = enumValues(set, p["type"])
		}
		if format := f.Tag.Get("format"); format != "" {
			p["format"] = format
		}
		props[name] = p
		if !optional && f.Type.Kind() != reflect.Pointer {
			required = append(required, name)
		}
	}
	return Schema{"type": "object", "properties": props, "required": required}
}

// enumValues is the named set, typed for a schema of jsonType.
func enumValues(set string, jsonType any) []any {
	values, ok := enums[set]
	if !ok {
		panic(fmt.Sprintf("events: unknown enum set %q", set))
	}
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
		if jsonType == "integer" {
			n, err := strconv.Atoi(v)
			if err != nil {
				panic(fmt.Sprintf("events: enum set %q holds %q for an integer", set, v))
			}
			out[i] = n
		}
	}
	return out
}

// jsonName is a field's json name, empty for a field json skips, and whether it is omitempty.
func jsonName(f reflect.StructField) (string, bool) {
	if !f.IsExported() {
		return "", false
	}
	name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
	if name == "-" {
		return "", false
	}
	if name == "" {
		name = f.Name
	}
	return name, slices.Contains(strings.Split(opts, ","), "omitempty")
}

func withDescription(s Schema, doc string) Schema {
	if doc != "" {
		s["description"] = doc
	}
	return s
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func enumStrings(v any) []string {
	switch values := v.(type) {
	case []any:
		out := make([]string, len(values))
		for i, x := range values {
			out[i] = fmt.Sprint(x)
		}
		return out
	case []string:
		return values
	}
	return nil
}

// ValidatePayload checks entity against the data.entity schema of type t: no
// field outside the schema, every required field present and every enum value
// allowed. Producers' tests run it over their sample payloads.
func ValidatePayload(t Type, entity any) error {
	s, ok := TypeSchema(t)
	if !ok {
		return fmt.Errorf("%w: unknown type %q", ErrInvalidEvent, t)
	}
	body, err := json.Marshal(entity)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	entitySchema := s["properties"].(Schema)["data"].(Schema)["properties"].(Schema)["entity"].(Schema)
	return validateValue(entitySchema, doc, "data.entity")
}

func validateValue(s Schema, v any, path string) error {
	if v == nil {
		return nil
	}
	if values := enumStrings(s["enum"]); values != nil && !slices.Contains(values, fmt.Sprint(v)) {
		return fmt.Errorf("%w: %s: %v is not one of %v", errEnumValue, path, v, values)
	}
	switch s["type"] {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want an object", path)
		}
		props := s["properties"].(Schema)
		for k, x := range obj {
			p, ok := props[k]
			if !ok {
				return fmt.Errorf("%s.%s: not in the schema", path, k)
			}
			if err := validateValue(p.(Schema), x, path+"."+k); err != nil {
				return err
			}
		}
		for _, k := range s["required"].([]string) {
			if _, ok := obj[k]; !ok {
				return fmt.Errorf("%s.%s: required", path, k)
			}
		}
	case "array":
		items, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s: want an array", path)
		}
		for i, x := range items {
			if err := validateValue(s["items"].(Schema), x, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// errEnumValue reports a value outside its field's enum.
var errEnumValue = errors.New("value outside the enum")

// Changeable lists the field names an update of type t may list in data.changed.
func Changeable(t Type) []string {
	e, ok := catalog[t]
	if !ok {
		return nil
	}
	return changeable(e.payload)
}
