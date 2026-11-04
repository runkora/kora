package skills

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

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
	// Title is the first "# " heading after the frontmatter, else Name.
	Title      string
	Version    *string
	Tags       []string
	Tools      []string
	ManualOnly bool
}

// rawFrontmatter is the YAML shape. Lists may be YAML lists or strings.
type rawFrontmatter struct {
	Name                   string `json:"name"`
	Description            string `json:"description"`
	Version                any    `json:"version"`
	Tags                   any    `json:"tags"`
	AllowedTools           any    `json:"allowed-tools"`
	DisableModelInvocation bool   `json:"disable-model-invocation"`
	Metadata               struct {
		Version any `json:"version"`
		Tags    any `json:"tags"`
	} `json:"metadata"`
}

// parseSkillFrontmatter parses the YAML frontmatter of a SKILL.md: the block
// between an opening "---" line and the next "---" line. It is parsed as YAML,
// so folded (>) and literal (|) block scalars, quoted values and values
// containing colons all keep their full text.
func parseSkillFrontmatter(content string) (skillFrontmatter, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
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

	var raw rawFrontmatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &raw); err != nil {
		// Hand-written frontmatter is often not strict YAML, for example an
		// unquoted "description: Use when: ..." line. Fall back to reading
		// single-line "key: value" pairs so such skills are not dropped.
		raw = parseFrontmatterLines(lines[1:end])
	}
	fm := skillFrontmatter{
		Name:        strings.TrimSpace(raw.Name),
		Description: strings.TrimSpace(raw.Description),
		Version:     version(raw.Version, raw.Metadata.Version),
		Tags:        listValue(raw.Tags, raw.Metadata.Tags),
		Tools:       listValue(raw.AllowedTools, nil),
		ManualOnly:  raw.DisableModelInvocation,
	}
	if fm.Name == "" {
		return skillFrontmatter{}, errMissingName
	}
	if fm.Description == "" {
		return skillFrontmatter{}, errMissingDescription
	}
	fm.Title = fm.Name
	for _, line := range lines[end+1:] {
		if heading, ok := strings.CutPrefix(line, "# "); ok && strings.TrimSpace(heading) != "" {
			fm.Title = strings.TrimSpace(heading)
			break
		}
	}
	return fm, nil
}

// version returns the first set value without a leading "v", or nil.
func version(values ...any) *string {
	for _, v := range values {
		if v == nil {
			continue
		}
		s := strings.TrimPrefix(strings.TrimSpace(fmt.Sprint(v)), "v")
		if s != "" {
			return &s
		}
	}
	return nil
}

// listValue reads the first set value as a YAML list, or as a string split on
// commas and spaces. It never returns nil.
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
			out = append(out, strings.FieldsFunc(v, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })...)
		}
		if len(out) > 0 {
			return out
		}
	}
	return out
}

// parseFrontmatterLines reads top-level single-line "key: value" pairs,
// splitting on the first colon. Indented lines are continuation text and are
// skipped, so a colon inside them never creates a key.
func parseFrontmatterLines(lines []string) rawFrontmatter {
	var fm rawFrontmatter
	for _, line := range lines {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			fm.Name = unquote(strings.TrimSpace(val))
		case "description":
			fm.Description = unquote(strings.TrimSpace(val))
		}
	}
	return fm
}

// unquote strips one pair of matching surrounding quotes.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
