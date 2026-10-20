package triggers

import (
	"errors"
	"slices"
	"testing"
)

func TestResolvePath(t *testing.T) {
	doc := decodeBody([]byte(`{
		"s": "text",
		"n": 12345678901234567890,
		"f": 1.50,
		"b": true,
		"z": null,
		"nul": "a\u0000b",
		"obj": {"k": "v", "n": 2},
		"list": [{"name": "first"}, {"name": "second"}],
		"scalars": ["x", "y"]
	}`))
	tests := map[string]string{
		"s":              "text",
		"n":              "12345678901234567890",
		"f":              "1.50",
		"b":              "true",
		"z":              "",
		"nul":            "ab",
		"obj":            `{"k":"v","n":2}`,
		"obj.k":          "v",
		"list":           `[{"name":"first"},{"name":"second"}]`,
		"list.1.name":    "second",
		"scalars.0":      "x",
		"missing":        "",
		"s.deeper":       "",
		"list.2.name":    "",
		"list.name":      "",
		"obj.k.too_deep": "",
	}
	for path, want := range tests {
		if got := resolvePath(doc, path); got != want {
			t.Errorf("resolvePath(%s) = %q, want %q", path, got, want)
		}
	}
}

func TestDecodeBody(t *testing.T) {
	tests := map[string]bool{
		`{"a": 1}`:         true,
		` {"a": 1} `:       true,
		`{"a": 1} garbage`: false,
		`{"a": 1}{"b": 2}`: false,
		`not json`:         false,
		``:                 false,
		`{"a": 1, "b": {}`: false,
		`["top", "level"]`: true,
	}
	for body, ok := range tests {
		if got := decodeBody([]byte(body)) != nil; got != ok {
			t.Errorf("decodeBody(%q) decoded = %v, want %v", body, got, ok)
		}
	}
}

func TestTriggerFields(t *testing.T) {
	tr := Trigger{
		PromptTemplate: "Reply to {{ comment.body }} on {{ meta.source }}",
		MatchCondition: map[string][]string{"action": {"created"}, "comment.user.login": {"alice"}},
	}
	doc := decodeBody([]byte(`{"action":"created","comment":{"body":"hi","user":{"login":"alice"}},"unreferenced":"x"}`))
	got := triggerFields(tr, doc, map[string]string{metaEventType: "issue_comment", metaDeliveryID: "gh-1", metaSource: "gh-source"}, nil)
	want := map[string]string{
		"comment.body":       "hi",
		"action":             "created",
		"comment.user.login": "alice",
		metaEventType:        "issue_comment",
		metaDeliveryID:       "gh-1",
		metaSource:           "gh-source",
	}
	if len(got) != len(want) {
		t.Errorf("fields = %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

// A body that did not decode, such as a truncated one, resolves no delivery fields.
func TestTriggerFields_NoDocument(t *testing.T) {
	tr := Trigger{PromptTemplate: "{{ comment.body }}"}
	got := triggerFields(tr, nil, map[string]string{metaEventType: "issue_comment", metaDeliveryID: "gh-1", metaSource: "src"}, nil)
	if got["comment.body"] != "" || got[metaDeliveryID] != "gh-1" {
		t.Errorf("fields = %v, want empty delivery fields and the meta fields", got)
	}
}

func TestReferencedKeys(t *testing.T) {
	tr := Trigger{
		PromptTemplate: "{{ a }} {{a}} {{ b.c }}",
		MatchCondition: map[string][]string{"b.c": {"x"}, "d": {"y"}},
	}
	got := referencedKeys(tr)
	slices.Sort(got)
	if !slices.Equal(got, []string{"a", "b.c", "d"}) {
		t.Errorf("referencedKeys = %v", got)
	}
}

func TestValidateFieldKeys(t *testing.T) {
	tests := []struct {
		name  string
		tmpl  string
		cond  map[string][]string
		valid bool
	}{
		{"schema paths", "{{ comment.body }} {{ alerts.0.status }}", map[string][]string{"action": {"x"}}, true},
		{"meta field", "{{ meta.event_type }}", map[string][]string{"meta.source": {"x"}}, true},
		{"unknown meta field", "{{ meta.nope }}", nil, false},
		{"unknown meta filter", "p", map[string][]string{"meta.nope": {"x"}}, false},
		{"empty segment in template", "{{ comment..body }}", nil, false},
		{"leading dot", "{{ .body }}", nil, false},
		{"bad filter key", "p", map[string][]string{"reactions.+1": {"1"}}, false},
		{"no keys", "plain prompt", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFieldKeys(tt.tmpl, tt.cond)
			if tt.valid && err != nil {
				t.Errorf("err = %v, want valid", err)
			}
			if !tt.valid && !errors.Is(err, ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}
