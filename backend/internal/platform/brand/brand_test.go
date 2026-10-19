package brand

import (
	"errors"
	"testing"
)

// New accepts any name a string can carry.
func TestNew(t *testing.T) {
	cases := map[string]struct {
		name    string
		wantErr error
	}{
		"plain":            {"kora", nil},
		"with spaces":      {"Acme Agents", nil},
		"unicode":          {"Ökosystem", nil},
		"empty":            {"", ErrProductNameEmpty},
		"whitespace only":  {"  \t", ErrProductNameEmpty},
		"newline":          {"Acme\nAgents", ErrProductNameControl},
		"nul":              {"Acme\x00", ErrProductNameControl},
		"escape sequence":  {"\x1b[31mAcme", ErrProductNameControl},
		"long is accepted": {"a very long product name that the theme contract, not this package, bounds", nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := New(c.name)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("New(%q) error = %v, want %v", c.name, err, c.wantErr)
			}
			if err == nil && b.ProductName() != c.name {
				t.Errorf("ProductName() = %q, want %q", b.ProductName(), c.name)
			}
		})
	}
}

// Load returns the default, lets the env var replace it, and rejects a bad value.
func TestLoad(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}

	b, err := load(env(nil))
	if err != nil {
		t.Fatalf("load() error: %v", err)
	}
	if b.ProductName() != "kora" {
		t.Errorf("default ProductName() = %q, want kora", b.ProductName())
	}

	b, err = load(env(map[string]string{EnvProductName: "Acme Agents"}))
	if err != nil {
		t.Fatalf("load() with env error: %v", err)
	}
	if b.ProductName() != "Acme Agents" {
		t.Errorf("env ProductName() = %q, want Acme Agents", b.ProductName())
	}

	if _, err := load(env(map[string]string{EnvProductName: "Acme\nAgents"})); !errors.Is(err, ErrProductNameControl) {
		t.Errorf("load() with a control character = %v, want ErrProductNameControl", err)
	}
}

func TestMustNewPanicsOnEmpty(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustNew(\"\") did not panic")
		}
	}()
	MustNew("")
}
