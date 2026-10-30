package skills

import (
	"errors"
	"strings"

	"sigs.k8s.io/yaml"
)

var (
	ErrNoFrontmatter      = errors.New("SKILL.md missing frontmatter block")
	ErrMissingName        = errors.New("SKILL.md frontmatter missing required field: name")
	ErrMissingDescription = errors.New("SKILL.md frontmatter missing required field: description")
)

type skillFrontmatter struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// skillBody omits frontmatter so metadata is not indexed again as instructions.
func skillBody(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return content
}

// ParseSkillFrontmatter parses the YAML frontmatter of a SKILL.md: the block
// between an opening "---" line and the next "---" line. It is parsed as YAML,
// so folded (>) and literal (|) block scalars, quoted values and values
// containing colons all keep their full text.
func ParseSkillFrontmatter(content string) (skillFrontmatter, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return skillFrontmatter{}, ErrNoFrontmatter
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return skillFrontmatter{}, ErrNoFrontmatter
	}

	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &fm); err != nil {
		// Hand-written frontmatter is often not strict YAML, for example an
		// unquoted "description: Use when: ..." line. Fall back to reading
		// single-line "key: value" pairs so such skills are not dropped.
		fm = parseFrontmatterLines(lines[1:end])
	}
	fm.Name = strings.TrimSpace(fm.Name)
	fm.Description = strings.TrimSpace(fm.Description)

	if fm.Name == "" {
		return skillFrontmatter{}, ErrMissingName
	}
	if fm.Description == "" {
		return skillFrontmatter{}, ErrMissingDescription
	}

	return fm, nil
}

// parseFrontmatterLines reads top-level single-line "key: value" pairs,
// splitting on the first colon. Indented lines are continuation text and are
// skipped, so a colon inside them never creates a key.
func parseFrontmatterLines(lines []string) skillFrontmatter {
	var fm skillFrontmatter
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
