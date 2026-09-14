package main

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// ErrMigrationContent is returned when a migration violates the content rule in
// docs/adr/2026-09-10-database-migrations.md, Decision 7.
var ErrMigrationContent = errors.New("migration content rule violated")

var (
	lineComment   = regexp.MustCompile(`--[^\n]*`)
	blockComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	createTable   = regexp.MustCompile(`(?i)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	dmlStart      = regexp.MustCompile(`(?i)^\s*(INSERT|DELETE|UPDATE)\b`)
	updateTarget  = regexp.MustCompile(`(?i)^\s*UPDATE\s+(?:ONLY\s+)?(\w+)`)
	alterTarget   = regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(?:ONLY\s+)?(?:IF\s+EXISTS\s+)?(\w+)`)
	renameTable   = regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(?:ONLY\s+)?(?:IF\s+EXISTS\s+)?\w+\s+RENAME\s+TO\b`)
	concurrently  = regexp.MustCompile(`(?i)\bCONCURRENTLY\b`)
	noTransaction = regexp.MustCompile(`(?i)--\s*\+goose\s+NO\s+TRANSACTION`)

	// tableReferences lists every form in which a statement names a table.
	tableReferences = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bREFERENCES\s+(\w+)`),
		regexp.MustCompile(`(?i)\bFROM\s+(\w+)`),
		regexp.MustCompile(`(?i)\bJOIN\s+(\w+)`),
		regexp.MustCompile(`(?i)^\s*UPDATE\s+(?:ONLY\s+)?(\w+)`),
		regexp.MustCompile(`(?i)^\s*INSERT\s+INTO\s+(\w+)`),
		regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(?:ONLY\s+)?(?:IF\s+EXISTS\s+)?(\w+)`),
		regexp.MustCompile(`(?i)^\s*DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(\w+)`),
		regexp.MustCompile(`(?i)^\s*CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?\w+\s+ON\s+(?:ONLY\s+)?(\w+)`),
	}
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
// returns one error per violation, joined.
func lintMigrations(sources map[string]fs.FS) error {
	services := make([]string, 0, len(sources))
	for name := range sources {
		services = append(services, name)
	}
	sort.Strings(services)

	var files []migration
	owners := map[string]string{}
	for _, service := range services {
		entries, err := fs.ReadDir(sources[service], ".")
		if err != nil {
			return fmt.Errorf("read %s migrations: %w", service, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}
			body, err := fs.ReadFile(sources[service], entry.Name())
			if err != nil {
				return fmt.Errorf("read %s/%s: %w", service, entry.Name(), err)
			}
			m := parseMigration(service, entry.Name(), string(body))
			files = append(files, m)
			for _, stmt := range m.up {
				if match := createTable.FindStringSubmatch(stmt); match != nil {
					owners[strings.ToLower(match[1])] = service
				}
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
	return fmt.Errorf("%w:\n  %s", ErrMigrationContent, strings.Join(violations, "\n  "))
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
	if !migrationName.MatchString(m.name) {
		out = append(out, "filename is not <version>_<slug>.sql")
	}
	return out
}

// dmlViolations covers clauses 1 to 3: no seeds, backfills only beside the
// ALTER that needs them, and no row changes on the way down.
func (m migration) dmlViolations() []string {
	var out []string

	altered := map[string]bool{}
	for _, stmt := range m.up {
		if match := alterTarget.FindStringSubmatch(stmt); match != nil {
			altered[strings.ToLower(match[1])] = true
		}
	}

	for _, stmt := range m.up {
		switch kind := strings.ToUpper(firstWord(dmlStart, stmt)); kind {
		case "INSERT", "DELETE":
			out = append(out, kind+" in Up: seeds and reference data are registered at boot by the owning service")
		case "UPDATE":
			target := strings.ToLower(updateTarget.FindStringSubmatch(stmt)[1])
			if !altered[target] {
				out = append(out, "UPDATE "+target+" in Up without an ALTER TABLE "+target+" in the same file")
			}
		}
	}
	for _, stmt := range m.down {
		if kind := firstWord(dmlStart, stmt); kind != "" {
			out = append(out, strings.ToUpper(kind)+" in Down: Down never changes rows")
		}
	}
	return out
}

// tableViolations covers clauses 4 and 5: only the owning service touches a
// table, and CONCURRENTLY needs the NO TRANSACTION directive.
func (m migration) tableViolations(owners map[string]string) []string {
	var out []string
	for _, stmt := range append(append([]string{}, m.up...), m.down...) {
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

func firstWord(re *regexp.Regexp, stmt string) string {
	if match := re.FindStringSubmatch(stmt); match != nil {
		return match[1]
	}
	return ""
}

// referencedTables returns every table a statement names, deduplicated.
func referencedTables(stmt string) []string {
	seen := map[string]bool{}
	var out []string
	for _, re := range tableReferences {
		for _, match := range re.FindAllStringSubmatch(stmt, -1) {
			table := strings.ToLower(match[1])
			if !seen[table] {
				seen[table] = true
				out = append(out, table)
			}
		}
	}
	return out
}
