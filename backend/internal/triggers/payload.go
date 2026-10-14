package triggers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// fieldKeyPattern is the grammar for a field key in templates and match conditions.
var fieldKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)*$`)

// decodeBody decodes a delivery body for field resolution. It returns nil
// unless the body is exactly one JSON value.
func decodeBody(body []byte) any {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil
	}
	return doc
}

// referencedKeys lists the field keys a trigger's prompt, coalescing key and match
// condition name.
func referencedKeys(t Trigger) []string {
	keys := placeholderKeys(t.PromptTemplate)
	for _, k := range placeholderKeys(t.Coalesce.keyTemplate()) {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	for _, m := range []map[string][]string{t.MatchCondition, t.MatchContains} {
		for k := range m {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// placeholderKeys lists the distinct keys a template's placeholders name.
func placeholderKeys(tmpl string) []string {
	var keys []string
	for _, m := range templateVarPattern.FindAllStringSubmatch(tmpl, -1) {
		if !slices.Contains(keys, m[1]) {
			keys = append(keys, m[1])
		}
	}
	return keys
}

// triggerFields builds one trigger's field map: the meta fields plus every
// delivery path the trigger references, resolved against the decoded body.
func triggerFields(t Trigger, doc any, meta map[string]string) map[string]string {
	fields := make(map[string]string, len(meta))
	maps.Copy(fields, meta)
	for _, key := range referencedKeys(t) {
		if !strings.HasPrefix(key, metaFieldsPrefix) {
			fields[key] = resolvePath(doc, key)
		}
	}
	return fields
}

// resolvePath returns the value at a dotted path as a string. A missing path resolves empty.
func resolvePath(doc any, key string) string {
	node := doc
	for seg := range strings.SplitSeq(key, ".") {
		switch n := node.(type) {
		case map[string]any:
			node = n[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(n) {
				return ""
			}
			node = n[i]
		default:
			return ""
		}
	}
	return strings.ReplaceAll(stringValue(node), "\x00", "")
}

// stringValue converts a decoded JSON value to the string a field resolves to.
func stringValue(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// validateFieldKeys checks the keys a webhook trigger's prompt and match
// condition name against the key grammar and the known meta fields.
func validateFieldKeys(tmpl string, condition map[string][]string) error {
	for _, k := range referencedKeys(Trigger{PromptTemplate: tmpl, MatchCondition: condition}) {
		if !fieldKeyPattern.MatchString(k) {
			return fmt.Errorf("%w: %q is not a valid field name", ErrValidation, k)
		}
		if strings.HasPrefix(k, metaFieldsPrefix) && !slices.ContainsFunc(metaFields, func(f Field) bool { return f.Path == k }) {
			return fmt.Errorf("%w: %q is not a meta field", ErrValidation, k)
		}
	}
	return nil
}
