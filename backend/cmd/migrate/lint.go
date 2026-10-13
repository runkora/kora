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
	// gooseBlock captures the body between a StatementBegin/StatementEnd pair.
	// goose hands one block to the driver as a single Exec.
	gooseBlock = regexp.MustCompile(`(?is)--\s*\+goose\s+StatementBegin(.*?)--\s*\+goose\s+StatementEnd`)

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
	// blocks is the body of each StatementBegin/StatementEnd pair, kept whole
	// because the rule below is about what shares one Exec, not one statement.
	blocks []string
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
	for _, b := range gooseBlock.FindAllStringSubmatch(body, -1) {
		m.blocks = append(m.blocks, b[1])
	}

	upBody, downBody, _ := strings.Cut(body, "-- +goose Down")
	if _, after, ok := strings.Cut(upBody, "-- +goose Up"); ok {
		upBody = after
	}
	m.up = statements(upBody)
	m.down = statements(downBody)
	return m
}

// statements splits SQL into statements on top-level semicolons.
//
// Comments and quoted runs are recognised in the same pass, because either can
// contain the other: a `--` inside a string literal does not open a comment, and
// a semicolon inside either is data rather than a terminator. Stripping comments
// first with a regex gets that backwards, and concurrentBlockViolations counts
// what this returns, so a miscount accepts a block that cannot run.
func statements(sql string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}

	for i := 0; i < len(sql); {
		switch {
		case strings.HasPrefix(sql[i:], "--"):
			if j := strings.IndexByte(sql[i:], '\n'); j >= 0 {
				i += j // keep the newline, it still separates tokens
			} else {
				i = len(sql)
			}
		case strings.HasPrefix(sql[i:], "/*"):
			i = skipBlockComment(sql, i)
		case sql[i] == '\'' || sql[i] == '"':
			end := scanQuoted(sql, i, sql[i])
			cur.WriteString(sql[i:end])
			i = end
		case sql[i] == '$':
			if end := scanDollar(sql, i); end > i {
				cur.WriteString(sql[i:end])
				i = end
				continue
			}
			cur.WriteByte(sql[i])
			i++
		case sql[i] == ';':
			flush()
			i++
		default:
			cur.WriteByte(sql[i])
			i++
		}
	}
	flush()
	return out
}

// skipBlockComment returns the index just past the /* */ run starting at i.
// Postgres block comments nest, so this tracks depth rather than stopping at the
// first */.
func skipBlockComment(s string, i int) int {
	depth := 0
	for j := i; j < len(s); {
		switch {
		case strings.HasPrefix(s[j:], "/*"):
			depth++
			j += 2
		case strings.HasPrefix(s[j:], "*/"):
			depth--
			j += 2
			if depth == 0 {
				return j
			}
		default:
			j++
		}
	}
	return len(s)
}

// scanQuoted returns the index just past the '...' or "..." run starting at i,
// treating a doubled quote as an escape. An unterminated run consumes the rest
// of the input, so a malformed file cannot split into surprising pieces.
func scanQuoted(s string, i int, quote byte) int {
	for j := i + 1; j < len(s); j++ {
		if s[j] != quote {
			continue
		}
		if j+1 < len(s) && s[j+1] == quote {
			j++
			continue
		}
		return j + 1
	}
	return len(s)
}

// scanDollar returns the index just past a $tag$...$tag$ body starting at i, or
// i when this is not a dollar quote at all, such as a bare $1 placeholder.
func scanDollar(s string, i int) int {
	close := strings.IndexByte(s[i+1:], '$')
	if close < 0 {
		return i
	}
	tag := s[i : i+close+2]
	for k := 1; k < len(tag)-1; k++ {
		if c := tag[k]; c != '_' && !isAlnum(c) {
			return i
		}
	}
	rest := s[i+len(tag):]
	if k := strings.Index(rest, tag); k >= 0 {
		return i + len(tag) + k + len(tag)
	}
	return len(s)
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// violations lists every clause of the content rule the file breaks.
func (m migration) violations(owners map[string]string) []string {
	out := m.dmlViolations()
	out = append(out, m.tableViolations(owners)...)
	out = append(out, m.concurrentBlockViolations()...)
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

// concurrentBlockViolations covers the block-splitting rule. goose sends one
// StatementBegin/StatementEnd block to the driver as a single Exec, and two
// statements in one Exec are an implicit transaction, which CREATE INDEX
// CONCURRENTLY and DROP INDEX CONCURRENTLY both refuse outright with "cannot
// run inside a transaction block". NO TRANSACTION alone does not save a file
// that packs them together, so the failure lands at deploy time, in the
// chart's pre-upgrade hook, where it blocks the release.
func (m migration) concurrentBlockViolations() []string {
	var out []string
	for _, b := range m.blocks {
		if !concurrently.MatchString(b) {
			continue
		}
		if n := len(statements(b)); n > 1 {
			out = append(out, fmt.Sprintf(
				"a CONCURRENTLY statement shares one goose block with %d other statement(s); give each its own StatementBegin/StatementEnd",
				n-1,
			))
		}
	}
	return out
}
