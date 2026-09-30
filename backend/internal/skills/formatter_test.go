package skills_test

import (
	"testing"

	"github.com/clear-route/orbit/internal/skills"
)

func TestParseSkillFrontmatter(t *testing.T) {
	context := `---
	name: test skill
	description: test description
	---`

	parsed, err := skills.ParseSkillFrontmatter(context)
	if err != nil {
		t.Fatalf("Failed to Parse the skills %q", err)
	}
	if parsed.Name != "test skill" {
		t.Fatalf("Failed to parse the name correctly %q", parsed.Name)
	}
	if parsed.Description != "test description" {
		t.Fatalf("Failed to parse the description correctly %q", parsed.Description)
	}

	if parsed.Name == "" || parsed.Description == "" {
		t.Fatalf("Parsed skill has empty fields: name=%q, description=%q", parsed.Name, parsed.Description)
	}

}
