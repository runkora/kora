package main

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func file(body string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(body)} }

const cleanUp = `-- +goose Up
-- +goose StatementBegin
-- Rows are inserted from the api, never from here.
CREATE TABLE widgets (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL
);
CREATE INDEX idx_widgets_kind ON widgets (kind);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS widgets;
-- +goose StatementEnd
`

func TestLintMigrations_AcceptsCleanFiles(t *testing.T) {
	t.Parallel()

	err := lintMigrations(map[string]fs.FS{
		"a": fstest.MapFS{
			"20260101000000_create_widgets.sql": file(cleanUp),
			"20260102000000_add_widget_size.sql": file(`-- +goose Up
ALTER TABLE widgets ADD COLUMN size INTEGER;
UPDATE widgets SET size = 1 WHERE size IS NULL;
-- +goose Down
ALTER TABLE widgets DROP COLUMN size;
`),
			"20260103000000_index_widgets_size.sql": file(`-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY idx_widgets_size ON widgets (size);
-- +goose Down
DROP INDEX CONCURRENTLY idx_widgets_size;
`),
		},
	})
	if err != nil {
		t.Fatalf("clean files rejected: %v", err)
	}
}

func TestLintMigrations_RejectsEachClause(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		want string
	}{
		"insert in up": {
			body: "-- +goose Up\nINSERT INTO widgets (id) VALUES ('x');\n-- +goose Down\n",
			want: "INSERT in Up",
		},
		"two concurrently statements in one goose block": {
			// goose sends a block as one Exec, and two statements in one Exec are
			// an implicit transaction, which CONCURRENTLY refuses. NO TRANSACTION
			// does not save this file; it fails in the pre-upgrade hook instead.
			body: "-- +goose NO TRANSACTION\n-- +goose Up\n-- +goose StatementBegin\nDROP INDEX CONCURRENTLY IF EXISTS idx_widgets_size;\nCREATE INDEX CONCURRENTLY IF NOT EXISTS idx_widgets_size ON widgets (size);\n-- +goose StatementEnd\n-- +goose Down\n",
			want: "shares one goose block",
		},
		"delete in up": {
			body: "-- +goose Up\nDELETE FROM widgets;\n-- +goose Down\n",
			want: "DELETE in Up",
		},
		"update without alter": {
			body: "-- +goose Up\nUPDATE widgets SET kind = 'a';\n-- +goose Down\n",
			want: "UPDATE widgets in Up without an ALTER TABLE widgets",
		},
		"dml in down": {
			body: "-- +goose Up\nALTER TABLE widgets ADD COLUMN size INTEGER;\n-- +goose Down\nUPDATE widgets SET size = NULL;\n",
			want: "UPDATE in Down",
		},
		"foreign table": {
			body: "-- +goose Up\nALTER TABLE gadgets ADD COLUMN size INTEGER;\n-- +goose Down\n",
			want: "references table gadgets, owned by b",
		},
		"unknown table": {
			body: "-- +goose Up\nDROP TABLE IF EXISTS ghosts;\n-- +goose Down\n",
			want: "references table ghosts, which no service creates",
		},
		"foreign key": {
			body: "-- +goose Up\nALTER TABLE widgets ADD COLUMN gadget_id TEXT REFERENCES gadgets(id);\n-- +goose Down\n",
			want: "references table gadgets, owned by b",
		},
		"rename": {
			body: "-- +goose Up\nALTER TABLE widgets RENAME TO gizmos;\n-- +goose Down\n",
			want: "renames a table",
		},
		"select placeholder": {
			body: "-- +goose Up\nSELECT 'up SQL query';\n-- +goose Down\n",
			want: "SELECT in Up",
		},
		"empty up": {
			body: "-- +goose Up\n-- +goose StatementBegin\n-- +goose StatementEnd\n-- +goose Down\n",
			want: "Up has no statements",
		},
		"cte insert": {
			body: "-- +goose Up\nWITH s AS (SELECT 1) INSERT INTO widgets (id) SELECT 'x' FROM s;\n-- +goose Down\n",
			want: "INSERT in Up",
		},
		"quoted update target": {
			body: "-- +goose Up\nUPDATE \"widgets\" SET kind = 'a';\n-- +goose Down\n",
			want: "UPDATE widgets in Up without an ALTER TABLE widgets",
		},
		"schema-qualified foreign table": {
			body: "-- +goose Up\nALTER TABLE public.gadgets ADD COLUMN size INTEGER;\n-- +goose Down\n",
			want: "references table gadgets, owned by b",
		},
		"unnamed index on foreign table": {
			body: "-- +goose Up\nCREATE INDEX ON gadgets (id);\n-- +goose Down\n",
			want: "references table gadgets, owned by b",
		},
		"drop table list": {
			body: "-- +goose Up\nALTER TABLE widgets ADD COLUMN size INTEGER;\n-- +goose Down\nDROP TABLE widgets, gadgets;\n",
			want: "references table gadgets, owned by b",
		},
		"concurrently in transaction": {
			body: "-- +goose Up\nCREATE INDEX CONCURRENTLY idx_widgets_kind ON widgets (kind);\n-- +goose Down\n",
			want: "CONCURRENTLY without -- +goose NO TRANSACTION",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := lintMigrations(map[string]fs.FS{
				"a": fstest.MapFS{
					"20260101000000_create_widgets.sql": file(cleanUp),
					"20260102000000_change.sql":         file(tc.body),
				},
				"b": fstest.MapFS{
					"20260103000000_create_gadgets.sql": file("-- +goose Up\nCREATE TABLE gadgets (id TEXT PRIMARY KEY);\n-- +goose Down\nDROP TABLE gadgets;\n"),
				},
			})
			if !errors.Is(err, errMigrationContent) {
				t.Fatalf("err = %v, want errMigrationContent", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLintMigrations_IgnoresNonTableFrom(t *testing.T) {
	t.Parallel()

	err := lintMigrations(map[string]fs.FS{
		"a": fstest.MapFS{
			"20260101000000_create_widgets.sql": file(cleanUp),
			"20260102000000_add_year.sql":       file("-- +goose Up\nALTER TABLE widgets ADD COLUMN year INTEGER GENERATED ALWAYS AS (EXTRACT(YEAR FROM created_at)) STORED;\nALTER TABLE widgets ADD CONSTRAINT widgets_kind_size CHECK (kind IS DISTINCT FROM size);\n-- +goose Down\nALTER TABLE widgets DROP COLUMN year;\n"),
		},
	})
	if err != nil {
		t.Fatalf("FROM inside an expression is not a table reference: %v", err)
	}
}

func TestLintMigrations_RejectsDuplicateVersion(t *testing.T) {
	t.Parallel()

	err := lintMigrations(map[string]fs.FS{
		"a": fstest.MapFS{"20260101000000_create_widgets.sql": file(cleanUp)},
		"b": fstest.MapFS{"20260101000000_create_gadgets.sql": file("-- +goose Up\nCREATE TABLE gadgets (id TEXT PRIMARY KEY);\n-- +goose Down\nDROP TABLE gadgets;\n")},
	})
	if !errors.Is(err, errDuplicateMigration) {
		t.Fatalf("err = %v, want errDuplicateMigration", err)
	}
}

func TestLintMigrations_RejectsMalformedName(t *testing.T) {
	t.Parallel()

	err := lintMigrations(map[string]fs.FS{
		"a": fstest.MapFS{"create_widgets.sql": file(cleanUp)},
	})
	if err == nil || !strings.Contains(err.Error(), "filename is not") {
		t.Fatalf("err = %v, want a filename violation", err)
	}
}

func TestSources_PassLint(t *testing.T) {
	t.Parallel()

	if err := lintMigrations(sources); err != nil {
		t.Fatalf("embedded migrations violate the content rule: %v", err)
	}
}

// A CONCURRENTLY migration that splits its statements correctly must pass. The
// rule is about what shares one Exec, so one statement per block is the shape
// the pre-upgrade hook can actually run.
func TestLintMigrations_AcceptsConcurrentlyInSeparateBlocks(t *testing.T) {
	t.Parallel()

	err := lintMigrations(map[string]fs.FS{
		"a": fstest.MapFS{
			"20260101000000_create_widgets.sql": file(cleanUp),
			"20260102000000_add_index.sql": file(
				"-- +goose NO TRANSACTION\n" +
					"-- +goose Up\n" +
					"-- +goose StatementBegin\n" +
					"DROP INDEX CONCURRENTLY IF EXISTS idx_widgets_size;\n" +
					"-- +goose StatementEnd\n" +
					"-- +goose StatementBegin\n" +
					"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_widgets_size ON widgets (size);\n" +
					"-- +goose StatementEnd\n" +
					"-- +goose Down\n" +
					"-- +goose StatementBegin\n" +
					"DROP INDEX CONCURRENTLY IF EXISTS idx_widgets_size;\n" +
					"-- +goose StatementEnd\n",
			),
		},
	})
	if err != nil {
		t.Fatalf("correctly split CONCURRENTLY migration rejected: %v", err)
	}
}
