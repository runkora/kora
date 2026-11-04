package skills

import (
	"errors"
	"fmt"
	"strconv"
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
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &doc); err != nil {
		doc = map[string]any{}
	}
	meta, _ := doc["metadata"].(map[string]any)

	fm := skillFrontmatter{
		Name:        strings.TrimSpace(firstString(doc["name"], text["name"])),
		Description: strings.TrimSpace(firstString(doc["description"], text["description"])),
		Version:     version(text["version"], doc["version"], meta["version"]),
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

// version returns the first set value without a leading "v", or nil. A YAML
// number is formatted without exponent.
func version(values ...any) *string {
	for _, v := range values {
		var s string
		switch v := v.(type) {
		case string:
			s = v
		case float64:
			s = strconv.FormatFloat(v, 'f', -1, 64)
		case nil:
			continue
		default:
			s = fmt.Sprint(v)
		}
		if s = strings.TrimPrefix(strings.TrimSpace(s), "v"); s != "" && !strings.ContainsAny(s[:1], "[{|>") {
			return &s
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

// parseFrontmatterLines reads top-level single-line "key: value" pairs,
// splitting on the first colon. Indented lines are continuation text and are
// skipped, so a colon inside them never creates a key.
func parseFrontmatterLines(lines []string) map[string]any {
	out := map[string]any{}
	for _, line := range lines {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
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
