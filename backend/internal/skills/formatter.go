package skills

import (
	"errors"
	"strings"
)

var (
	ErrNoFrontmatter = errors.New("SKILL.md missing frontmatter block")
	ErrMissingName = errors.New("SKILL.md frontmatter missing required field: name")
	ErrMissingDescription = errors.New("SKILL.md frontmatter missing required field: description")
)

type skillFrontmatter struct {
	Name string
	Description string
}

func ParseSkillFrontmatter(context string) (skillFrontmatter,error) {
	if !strings.HasPrefix(context, "---") {
		return skillFrontmatter{}, ErrNoFrontmatter
	}

	rest := context[len("---"):]
	end := strings.Index(rest, "---")

	if end < 0 {
		return skillFrontmatter{}, ErrNoFrontmatter
	}

	block := rest[:end]
	lines := make(map[string]string)
	for _, line := range strings.Split(block, "\n")  {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		lines[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}

	name := lines["name"]
	description := lines["description"]

	if name == "" {
		return skillFrontmatter{}, ErrMissingName 
	}

	if description == "" {
		return skillFrontmatter{}, ErrMissingDescription
	}

	return skillFrontmatter{Name: name, Description: description}, nil

	
}