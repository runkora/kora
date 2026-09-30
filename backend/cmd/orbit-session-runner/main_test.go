package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A missing path is dropped rather than handed to Landlock, which would fail the run.
func TestExisting(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")

	for _, tc := range []struct {
		name string
		dirs bool
		want []string
	}{
		{"directories", true, []string{dir}},
		{"files", false, []string{file}},
	} {
		if got := existing(tc.dirs, dir, file, missing); !slices.Equal(got, tc.want) {
			t.Errorf("%s: existing = %v, want %v", tc.name, got, tc.want)
		}
	}
}
