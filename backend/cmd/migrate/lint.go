package main

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
)

// errMigrationContent reports a violation of the content rule in
// docs/adr/2026-09-10-database-migrations.md, Decision 7.
var errMigrationContent = errors.New("migration content rule violated")

// ident matches a table name with an optional schema prefix and captures the
// bare name.
const ident = `(?:"?\w+"?\.)?"?(\w+)"?`

// cte is the optional WITH prefix a DML statement may carry.
const cte = `(?:WITH\b.*?\)\s*)?`

var (
	lineComment   = regexp.MustCompile(`--[^\n]*`)
	blockComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	createTable   = regexp.MustCompile(`(?i)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?` + ident)
	dmlStart      = regexp.MustCompile(`(?is)^\s*` + cte + `(INSERT|DELETE|UPDATE|SELECT)\b`)
	updateTarget  = regexp.MustCompile(`(?is)^\s*` + cte + `UPDATE\s+(?:ONLY\s+)?` + ident)
	insertTarget  = regexp.MustCompile(`(?is)^\s*` + cte + `INSERT\s+INTO\s+` + ident)
	deleteTarget  = regexp.MustCompile(`(?is)^\s*` + cte + `DELETE\s+FROM\s+` + ident)
	alterTarget   = regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(?:ONLY\s+)?(?:IF\s+EXISTS\s+)?` + ident)
	renameTable   = regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(?:ONLY\s+)?(?:IF\s+EXISTS\s+)?` + ident + `\s+RENAME\s+TO\b`)
	dropTables    = regexp.MustCompile(`(?i)^\s*DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(.+)`)
	dropTarget    = regexp.MustCompile(`^` + ident)
	references    = regexp.MustCompile(`(?i)\bREFERENCES\s+` + ident)
	indexTarget   = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(?:"?\w+"?\s+)?ON\s+(?:ONLY\s+)?` + ident)
	concurrently  = regexp.MustCompile(`(?i)\bCONCURRENTLY\b`)
	noTransaction = regexp.MustCompile(`(?i)--\s*\+goose\s+NO\s+TRANSACTION`)

	// tableReferences lists every form in which a statement names one table.
	tableReferences = []*regexp.Regexp{references, insertTarget, deleteTarget, updateTarget, alterTarget, indexTarget}
)

// migration is one parsed file: its two directions as comment-free statements.
type migration struct {
	service string
	name    string
	up      []string
	down    []string
	noTx    bool
}

// lintMigrations checks every service's files against the content rule and
// returns every violation in one error. Merging first means a duplicate
// filename or version across services fails lint, not only up.
func lintMigrations(sources map[string]fs.FS) error {
	merged, err := mergeMigrationFS(sources)
	if err != nil {
		return err
	}

	var files []migration
	owners := map[string]string{}
	for _, name := range merged.names {
		service := merged.owners[name]
		body, err := fs.ReadFile(merged.files[name], name)
		if err != nil {
			return fmt.Errorf("read %s/%s: %w", service, name, err)
		}
		m := parseMigration(service, name, string(body))
		files = append(files, m)
		for _, stmt := range m.up {
			if match := createTable.FindStringSubmatch(stmt); match != nil {
				owners[strings.ToLower(match[1])] = service
			}
		}
	}

	var violations []string
	for _, m := range files {
		for _, v := range m.violations(owners) {
			violations = append(violations, fmt.Sprintf("%s/%s: %s", m.service, m.name, v))
		}
	}
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("%w:\n  %s", errMigrationContent, strings.Join(violations, "\n  "))
}

// parseMigration splits a goose file into Up and Down statements with comments
// removed. The NO TRANSACTION directive is read before comments are stripped.
func parseMigration(service, name, body string) migration {
	m := migration{service: service, name: name, noTx: noTransaction.MatchString(body)}

	upBody, downBody, _ := strings.Cut(body, "-- +goose Down")
	if _, after, ok := strings.Cut(upBody, "-- +goose Up"); ok {
		upBody = after
	}
	m.up = statements(upBody)
	m.down = statements(downBody)
	return m
}

func statements(sql string) []string {
	sql = blockComment.ReplaceAllString(sql, "")
	sql = lineComment.ReplaceAllString(sql, "")
	var out []string
	for stmt := range strings.SplitSeq(sql, ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// violations lists every clause of the content rule the file breaks.
func (m migration) violations(owners map[string]string) []string {
	out := m.dmlViolations()
	out = append(out, m.tableViolations(owners)...)
	if len(m.up) == 0 {
		out = append(out, "Up has no statements")
	}
	if !migrationName.MatchString(m.name) {
		out = append(out, "filename is not <version>_<slug>.sql")
	}
	return out
}

// dmlViolations covers clauses 1 to 3: no seeds or queries, a backfill only
// beside the ALTER that needs it, and no row changes on the way down.
func (m migration) dmlViolations() []string {
	var out []string

	altered := map[string]bool{}
	for _, stmt := range m.up {
		if match := alterTarget.FindStringSubmatch(stmt); match != nil {
			altered[strings.ToLower(match[1])] = true
		}
	}

	for _, stmt := range m.up {
		switch kind := strings.ToUpper(firstMatch(dmlStart, stmt)); kind {
		case "INSERT", "DELETE":
			out = append(out, kind+" in Up: seeds and reference data are registered at boot by the owning service")
		case "SELECT":
			out = append(out, "SELECT in Up: replace the create template's placeholder with DDL")
		case "UPDATE":
			target := strings.ToLower(firstMatch(updateTarget, stmt))
			switch {
			case target == "":
				out = append(out, "UPDATE in Up with a target the content check cannot read")
			case !altered[target]:
				out = append(out, "UPDATE "+target+" in Up without an ALTER TABLE "+target+" in the same file")
			}
		}
	}
	for _, stmt := range m.down {
		if kind := firstMatch(dmlStart, stmt); kind != "" {
			out = append(out, strings.ToUpper(kind)+" in Down: Down never touches rows")
		}
	}
	return out
}

// tableViolations covers clauses 4 and 5: only the owning service touches a
// table, and CONCURRENTLY needs the NO TRANSACTION directive.
func (m migration) tableViolations(owners map[string]string) []string {
	var out []string
	for _, stmt := range slices.Concat(m.up, m.down) {
		for _, table := range referencedTables(stmt) {
			owner, known := owners[table]
			switch {
			case !known:
				out = append(out, "references table "+table+", which no service creates")
			case owner != m.service:
				out = append(out, "references table "+table+", owned by "+owner)
			}
		}
		if renameTable.MatchString(stmt) {
			out = append(out, "renames a table; create the new table and migrate rows instead")
		}
		if concurrently.MatchString(stmt) && !m.noTx {
			out = append(out, "CONCURRENTLY without -- +goose NO TRANSACTION")
		}
	}
	return out
}

// firstMatch returns the first capture group, or "" when re does not match.
func firstMatch(re *regexp.Regexp, stmt string) string {
	if match := re.FindStringSubmatch(stmt); match != nil {
		return match[1]
	}
	return ""
}

// referencedTables returns every table a statement names, deduplicated.
func referencedTables(stmt string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(table string) {
		table = strings.ToLower(table)
		if !seen[table] {
			seen[table] = true
			out = append(out, table)
		}
	}
	for _, re := range tableReferences {
		for _, match := range re.FindAllStringSubmatch(stmt, -1) {
			add(match[1])
		}
	}
	if match := dropTables.FindStringSubmatch(stmt); match != nil {
		for target := range strings.SplitSeq(match[1], ",") {
			if name := dropTarget.FindStringSubmatch(strings.TrimSpace(target)); name != nil {
				add(name[1])
			}
		}
	}
	return out
}
