package marketplace_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/clear-route/orbit/internal/platform/marketplace"
)

func TestSlug(t *testing.T) {
	cases := []struct{ name, want string }{
		{"Clear Route Skills", "clear-route-skills"},
		// Names that differ only in case and punctuation clash: they share a URL segment.
		{"clear-route skills", "clear-route-skills"},
		{"  Acme -- Internal!! ", "acme-internal"},
		{"v2.0 Tools", "v2-0-tools"},
		{"Café", "caf"},
		{"---", ""},
	}
	for _, tc := range cases {
		if got := marketplace.Slug(tc.name); got != tc.want {
			t.Errorf("Slug(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSourcesFileWireShape(t *testing.T) {
	in := marketplace.SourcesFile{Tenants: map[string]marketplace.TenantSources{
		"acme": {Sources: []marketplace.Source{
			{Name: "Acme Internal Skills", Org: "acme-corp", Repo: "internal-skills", Branch: "main"},
		}},
	}}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"tenants":{"acme":{"sources":[{"name":"Acme Internal Skills","org":"acme-corp","repo":"internal-skills","branch":"main"}]}}}`
	if string(raw) != want {
		t.Errorf("wire shape = %s, want %s", raw, want)
	}

	var out marketplace.SourcesFile
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}
