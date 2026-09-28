package triggers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Field is one path a trigger can reference, as declared by a provider schema.
type Field struct {
	Path        string   `json:"path"`
	Group       string   `json:"group"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	// Pattern marks a map whose keys are data, such as commonLabels.<name>.
	Pattern bool `json:"pattern,omitempty"`
}

// Catalog is the event types and fields one provider's schema declares.
type Catalog struct {
	events map[string][]Field
}

// ErrInvalidSchema is returned when a provider schema cannot be built into a catalog.
var ErrInvalidSchema = errors.New("invalid provider schema")

const (
	metaGroup        = "meta"
	eventGroup       = "event"
	definitionRef    = "#/definitions/"
	maxCatalogDepth  = 16
	patternSegment   = "<name>"
	metaEventType    = "meta.event_type"
	metaDeliveryID   = "meta.delivery_id"
	metaSource       = "meta.source"
	metaFieldsPrefix = metaGroup + "."
)

// metaFields are supplied by the platform for every provider.
var metaFields = []Field{
	{Path: metaEventType, Group: metaGroup, Description: "The event type of the delivery"},
	{Path: metaDeliveryID, Group: metaGroup, Description: "The provider's ID for the delivery"},
	{Path: metaSource, Group: metaGroup, Description: "The name of the source the delivery arrived on"},
}

// segmentPattern is what one path segment may contain; the template grammar allows nothing else.
var segmentPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type schemaNode = map[string]any

type catalogBuilder struct {
	definitions map[string]schemaNode
	fields      map[string]*Field
}

// BuildCatalog walks every event in a provider schema into its fields.
func BuildCatalog(schema []byte) (*Catalog, error) {
	var doc struct {
		Events      map[string]schemaNode `json:"events"`
		Definitions map[string]schemaNode `json:"definitions"`
	}
	dec := json.NewDecoder(bytes.NewReader(schema))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSchema, err)
	}
	if len(doc.Events) == 0 {
		return nil, fmt.Errorf("%w: no events", ErrInvalidSchema)
	}
	c := &Catalog{events: make(map[string][]Field, len(doc.Events))}
	for event, root := range doc.Events {
		b := &catalogBuilder{definitions: doc.Definitions, fields: map[string]*Field{}}
		if err := b.walk(root, nil, nil); err != nil {
			return nil, fmt.Errorf("%w: event %q: %w", ErrInvalidSchema, event, err)
		}
		if _, reserved := b.fields[metaGroup]; reserved {
			return nil, fmt.Errorf("%w: event %q declares the reserved top-level field %q", ErrInvalidSchema, event, metaGroup)
		}
		c.events[event] = b.list()
	}
	return c, nil
}

// EventTypes returns the declared event types, sorted.
func (c *Catalog) EventTypes() []string {
	out := make([]string, 0, len(c.events))
	for e := range c.events {
		out = append(out, e)
	}
	slices.Sort(out)
	return out
}

// Fields returns an event type's fields followed by the meta fields. An event
// type the schema does not declare has only the meta fields.
func (c *Catalog) Fields(eventType string) []Field {
	return append(slices.Clone(c.events[eventType]), metaFields...)
}

// resolve follows a $ref chain. It returns nil for a ref already on the branch.
func (b *catalogBuilder) resolve(node schemaNode, stack []string) (schemaNode, []string, error) {
	for {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node, stack, nil
		}
		name := strings.TrimPrefix(ref, definitionRef)
		if slices.Contains(stack, name) {
			return nil, stack, nil
		}
		def, ok := b.definitions[name]
		if !ok {
			return nil, stack, fmt.Errorf("unknown reference %q", ref)
		}
		node, stack = def, append(slices.Clone(stack), name)
	}
}

// walk records the enum values and shape of the node at path, then descends
// into its variants, properties and list items. Variants share the path, so
// they are walked even at the depth cap to complete a field's enum.
func (b *catalogBuilder) walk(raw any, path, stack []string) error {
	node, _ := raw.(schemaNode)
	if node == nil {
		return nil
	}
	node, stack, err := b.resolve(node, stack)
	if err != nil || node == nil {
		return err
	}
	b.collect(node, path)
	if err := b.walkVariants(node, path, stack); err != nil {
		return err
	}
	if len(path) >= maxCatalogDepth {
		return nil
	}
	props, _ := node["properties"].(schemaNode)
	if err := b.walkProperties(props, path, stack); err != nil {
		return err
	}
	if items, ok := node["items"]; ok {
		if err := b.walk(items, append(slices.Clone(path), "0"), stack); err != nil {
			return err
		}
	}
	if len(props) == 0 && len(path) > 0 && opensToData(node["additionalProperties"]) {
		b.add(append(slices.Clone(path), patternSegment), "", true)
	}
	return nil
}

// collect adds a node's enum values to the field at path. Null resolves empty
// and is left out.
func (b *catalogBuilder) collect(node schemaNode, path []string) {
	f, ok := b.fields[strings.Join(path, ".")]
	if !ok {
		return
	}
	values, _ := node["enum"].([]any)
	for _, v := range values {
		if s := stringValue(v); s != "" && !slices.Contains(f.Enum, s) {
			f.Enum = append(f.Enum, s)
		}
	}
}

// walkVariants walks every allOf, anyOf and oneOf branch at the same path.
func (b *catalogBuilder) walkVariants(node schemaNode, path, stack []string) error {
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		subs, _ := node[key].([]any)
		for _, sub := range subs {
			if err := b.walk(sub, path, stack); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *catalogBuilder) walkProperties(props schemaNode, path, stack []string) error {
	for name, prop := range props {
		if !segmentPattern.MatchString(name) {
			continue
		}
		child := append(slices.Clone(path), name)
		if err := b.record(prop, child, stack); err != nil {
			return err
		}
		if err := b.walk(prop, child, stack); err != nil {
			return err
		}
	}
	return nil
}

// opensToData reports whether additionalProperties makes an object a map of data keys.
func opensToData(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case schemaNode:
		return true
	}
	return false
}

// record adds a property's field. A $ref's sibling description wins over the
// resolved schema's.
func (b *catalogBuilder) record(raw any, path, stack []string) error {
	prop, _ := raw.(schemaNode)
	desc, _ := prop["description"].(string)
	resolved, rstack, err := b.resolve(prop, stack)
	if err != nil {
		return err
	}
	if desc == "" && resolved != nil {
		desc, _ = resolved["description"].(string)
	}
	f := b.add(path, desc, false)
	if len(path) == 1 && !b.composite(resolved, rstack) {
		f.Group = eventGroup
	} else {
		f.Group = path[0]
	}
	return nil
}

func (b *catalogBuilder) add(path []string, desc string, pattern bool) *Field {
	p := strings.Join(path, ".")
	f, ok := b.fields[p]
	if !ok {
		f = &Field{Path: p, Group: path[0], Pattern: pattern}
		b.fields[p] = f
	}
	if f.Description == "" {
		f.Description = desc
	}
	return f
}

// composite reports whether a schema has fields of its own, through combinators.
func (b *catalogBuilder) composite(node schemaNode, stack []string) bool {
	if node == nil {
		return false
	}
	if _, ok := node["properties"]; ok {
		return true
	}
	if _, ok := node["items"]; ok {
		return true
	}
	if opensToData(node["additionalProperties"]) {
		return true
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		subs, _ := node[key].([]any)
		for _, sub := range subs {
			sn, _ := sub.(schemaNode)
			resolved, rstack, err := b.resolve(sn, stack)
			if err == nil && b.composite(resolved, rstack) {
				return true
			}
		}
	}
	return false
}

func (b *catalogBuilder) list() []Field {
	out := make([]Field, 0, len(b.fields))
	for _, f := range b.fields {
		out = append(out, *f)
	}
	slices.SortFunc(out, func(x, y Field) int { return strings.Compare(x.Path, y.Path) })
	return out
}
