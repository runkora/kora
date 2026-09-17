package main

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestMergeMigrationFS_MergesAndOrders(t *testing.T) {
	t.Parallel()

	merged, err := mergeMigrationFS(map[string]fs.FS{
		"b": fstest.MapFS{
			"20260102000000_create_b.sql": {Data: []byte("b")},
			"embed.go":                    {Data: []byte("package migrations")},
		},
		"a": fstest.MapFS{"20260101000000_create_a.sql": {Data: []byte("a")}},
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	entries, err := fs.ReadDir(merged, ".")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	want := []string{"20260101000000_create_a.sql", "20260102000000_create_b.sql"}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, name := range want {
		if entries[i].Name() != name {
			t.Errorf("entry %d = %s, want %s", i, entries[i].Name(), name)
		}
	}

	body, err := fs.ReadFile(merged, "20260102000000_create_b.sql")
	if err != nil {
		t.Fatalf("readfile: %v", err)
	}
	if string(body) != "b" {
		t.Errorf("read %q, want b", body)
	}
	if err := fstest.TestFS(merged, want...); err != nil {
		t.Errorf("fs contract: %v", err)
	}
}

func TestMergeMigrationFS_RejectsCollisions(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]fs.FS{
		"same filename": {
			"a": fstest.MapFS{"20260101000000_create_x.sql": {}},
			"b": fstest.MapFS{"20260101000000_create_x.sql": {}},
		},
		"same version": {
			"a": fstest.MapFS{"20260101000000_create_a.sql": {}},
			"b": fstest.MapFS{"20260101000000_create_b.sql": {}},
		},
	}
	for name, sources := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := mergeMigrationFS(sources); !errors.Is(err, errDuplicateMigration) {
				t.Fatalf("err = %v, want errDuplicateMigration", err)
			}
		})
	}
}

func TestMergeMigrationFS_RejectsMalformedName(t *testing.T) {
	t.Parallel()

	_, err := mergeMigrationFS(map[string]fs.FS{
		"a": fstest.MapFS{"create_a.sql": {}},
	})
	if !errors.Is(err, errMalformedMigration) {
		t.Fatalf("err = %v, want errMalformedMigration", err)
	}
}

func TestSources_MergeCleanly(t *testing.T) {
	t.Parallel()

	merged, err := mergeMigrationFS(sources)
	if err != nil {
		t.Fatalf("embedded migrations do not merge: %v", err)
	}
	if len(merged.names) == 0 {
		t.Fatal("no embedded migrations found")
	}
}
