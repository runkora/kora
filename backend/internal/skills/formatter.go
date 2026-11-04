package skills

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"sigs.k8s.io/yaml"
)

// Frontmatter errors skip the skill.
var (
	errNoFrontmatter      = errors.New("SKILL.md missing frontmatter block")
	errMissingName        = errors.New("SKILL.md frontmatter missing required field: name")
	errMissingDescription = errors.New("SKILL.md frontmatter missing required field: description")
)

// skillFrontmatter is what a SKILL.md declares, as the API shows it.
type skillFrontmatter struct {
	Name        string
	Description string
	// Title is the first "# " heading after the frontmatter, outside code
	// fences, else Name.
	Title      string
	Version    *string
	Tags       []string
	Tools      []string
	ManualOnly bool
	// BodyStart is the byte offset of the text after the frontmatter.
	BodyStart int
}

// parseSkillFrontmatter parses the YAML frontmatter of a SKILL.md: the block
// between an opening "---" line and the next "---" line. It is parsed as YAML,
// so folded (>) and literal (|) block scalars, quoted values and values
// containing colons all keep their full text. Single-line values are read from
// the text as well, which keeps hand-written frontmatter and numeric versions
// such as 1.0 as written.
func parseSkillFrontmatter(content string) (skillFrontmatter, error) {
	rawLines := strings.SplitAfter(content, "\n")
	lines := make([]string, len(rawLines))
	for i, l := range rawLines {
		lines[i] = strings.TrimRight(l, "\r\n")
	}
	// Editors on Windows often start the file with a UTF-8 byte order mark.
	if len(lines) > 0 {
		lines[0] = strings.TrimPrefix(lines[0], "\ufeff")
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return skillFrontmatter{}, errNoFrontmatter
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return skillFrontmatter{}, errNoFrontmatter
	}

	text := parseFrontmatterLines(lines[1:end])
	// Hand-written frontmatter is often not strict YAML, for example an
	// unquoted "description: Use when: ..." line. Then only the line values
	// above are used.
	doc := map[string]any{}
	decoded := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &doc) == nil
	if !decoded {
		doc = map[string]any{}
	}
	meta, _ := doc["metadata"].(map[string]any)
	textOf := func(key string) string { s, _ := text[key].(string); return s }

	fm := skillFrontmatter{
		Name:        strings.TrimSpace(firstString(doc["name"], text["name"])),
		Description: strings.TrimSpace(firstString(doc["description"], text["description"])),
		Version:     firstVersion(declaredVersion(doc["version"], textOf("version"), decoded), declaredVersion(meta["version"], textOf("metadata.version"), decoded)),
		Tags:        listValue(doc["tags"], meta["tags"], text["tags"]),
		Tools:       listValue(doc["allowed-tools"], text["allowed-tools"]),
		ManualOnly:  doc["disable-model-invocation"] == true || strings.EqualFold(firstString(doc["disable-model-invocation"], text["disable-model-invocation"]), "true"),
	}
	if fm.Name == "" {
		return skillFrontmatter{}, errMissingName
	}
	if fm.Description == "" {
		return skillFrontmatter{}, errMissingDescription
	}
	for _, l := range rawLines[:end+1] {
		fm.BodyStart += len(l)
	}
	fm.Title = fm.Name
	if heading := firstHeading(lines[end+1:]); heading != "" {
		fm.Title = heading
	}
	return fm, nil
}

// firstHeading returns the first "# " heading outside a code fence.
func firstHeading(lines []string) string {
	fenced := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if heading, ok := strings.CutPrefix(line, "# "); ok && !fenced {
			if heading = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(heading), "#")); heading != "" {
				return heading
			}
		}
	}
	return ""
}

// firstString returns the first value that is a non-empty string.
func firstString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// declaredVersion returns a version as written, without a leading "v". A
// YAML string is taken as decoded. A number, or any value when the YAML did
// not decode, is taken from the line text so 1.10 stays 1.10. A trailing
// comment is dropped, and "~", "null" or no value is absent.
func declaredVersion(value any, line string, decoded bool) *string {
	s, isString := value.(string)
	if !isString && (value != nil || !decoded) {
		s = stripComment(line)
	}
	s = strings.TrimPrefix(strings.TrimSpace(unquote(strings.TrimSpace(s))), "v")
	if s == "" || s == "~" || strings.EqualFold(s, "null") || strings.ContainsAny(s[:1], "[{|>") {
		return nil
	}
	return &s
}

// stripComment drops a YAML comment: a "#" at the start or after whitespace.
func stripComment(s string) string {
	for i, r := range s {
		if r == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			return s[:i]
		}
	}
	return s
}

func firstVersion(versions ...*string) *string {
	for _, v := range versions {
		if v != nil {
			return v
		}
	}
	return nil
}

// listValue reads the first set value as a YAML list or as a string. It never
// returns nil.
func listValue(values ...any) []string {
	out := []string{}
	for _, v := range values {
		switch v := v.(type) {
		case []any:
			for _, item := range v {
				if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
					out = append(out, s)
				}
			}
		case string:
			out = splitList(v)
		}
		if len(out) > 0 {
			return out
		}
	}
	return out
}

// splitList splits a string list on commas when it has one, otherwise on
// spaces. A separator inside parentheses, as in "Bash(git add:*)", is kept.
func splitList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	isSep := unicode.IsSpace
	if strings.Contains(s, ",") {
		isSep = func(r rune) bool { return r == ',' }
	}
	out := []string{}
	depth, start := 0, 0
	flush := func(end int) {
		if item := strings.Trim(strings.TrimSpace(s[start:end]), `"'`); item != "" {
			out = append(out, item)
		}
	}
	for i, r := range s {
		switch {
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case depth == 0 && isSep(r):
			flush(i)
			start = i + utf8.RuneLen(r)
		}
	}
	flush(len(s))
	return out
}

// parseFrontmatterLines reads single-line "key: value" pairs, splitting on the
// first colon. An indented pair under a key with no value, such as
// "metadata:", is read as "metadata.key". Other indented lines are
// continuation text and are skipped.
func parseFrontmatterLines(lines []string) map[string]any {
	out := map[string]any{}
	parent := ""
	for _, line := range lines {
		if line == "" {
			continue
		}
		prefix := ""
		if line[0] == ' ' || line[0] == '\t' {
			// One indented level under a block key, as "metadata.version".
			if parent == "" {
				continue
			}
			prefix, line = parent+".", strings.TrimSpace(line)
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if prefix == "" {
			parent = ""
			if strings.TrimSpace(stripComment(val)) == "" {
				parent = strings.TrimSpace(key)
			}
		}
		key = prefix + key
		// A block scalar's text is on the following lines, which only YAML reads.
		if val = unquote(strings.TrimSpace(val)); val != "" && !strings.ContainsAny(val[:1], "|>") {
			out[strings.TrimSpace(key)] = val
		}
	}
	return out
}

// unquote strips one pair of matching surrounding quotes.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
