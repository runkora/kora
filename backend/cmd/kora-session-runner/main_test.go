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

// enforce refuses a kernel below the required ABI and an unknown mode; bestEffort
// runs on any kernel.
func TestCheckKernel(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		abi     int
		wantErr bool
	}{
		{"", 0, true},
		{"enforce", 1, true},
		{"enforce", 2, false},
		{"bestEffort", 0, false},
		{"off", 6, true},
	} {
		if err := checkKernel(tc.mode, tc.abi); (err != nil) != tc.wantErr {
			t.Errorf("checkKernel(%q, %d) = %v, want error %v", tc.mode, tc.abi, err, tc.wantErr)
		}
	}
}
