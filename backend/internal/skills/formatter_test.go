package skills_test

import (
	"errors"
	"testing"

	"github.com/clear-route/orbit/internal/skills"
)

func TestParseSkillFrontmatter(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		wantName        string
		wantDescription string
	}{
		{
			name:            "plain values",
			content:         "---\nname: test skill\ndescription: test description\n---\n# Body\n",
			wantName:        "test skill",
			wantDescription: "test description",
		},
		{
			name: "folded block description",
			content: "---\nname: academy-guide\ndescription: >\n" +
				"  Stop and check this skill. Trigger on: \"how do I\",\n" +
				"  \"getting started with\".\n---\n",
			wantName:        "academy-guide",
			wantDescription: "Stop and check this skill. Trigger on: \"how do I\", \"getting started with\".",
		},
		{
			name:            "literal block description",
			content:         "---\nname: lit\ndescription: |\n  line one\n  line two\n---\n",
			wantName:        "lit",
			wantDescription: "line one\nline two",
		},
		{
			name:            "quoted values with colons",
			content:         "---\nname: \"quoted\"\ndescription: 'Use when: the user asks'\n---\n",
			wantName:        "quoted",
			wantDescription: "Use when: the user asks",
		},
		{
			name:            "extra fields and CRLF line endings",
			content:         "---\r\nname: extra\r\nlicense: MIT\r\ndescription: has extras\r\n---\r\n",
			wantName:        "extra",
			wantDescription: "has extras",
		},
		{
			name:            "invalid YAML falls back to single-line values",
			content:         "---\nname: loose\ndescription: Use when: the user asks\n---\n",
			wantName:        "loose",
			wantDescription: "Use when: the user asks",
		},
		{
			name:            "body containing a horizontal rule",
			content:         "---\nname: rule\ndescription: before the rule\n---\n# Body\n---\nmore\n",
			wantName:        "rule",
			wantDescription: "before the rule",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := skills.ParseSkillFrontmatter(tt.content)
			if err != nil {
				t.Fatalf("ParseSkillFrontmatter: %v", err)
			}
			if parsed.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", parsed.Name, tt.wantName)
			}
			if parsed.Description != tt.wantDescription {
				t.Errorf("Description = %q, want %q", parsed.Description, tt.wantDescription)
			}
		})
	}
}

func TestParseSkillFrontmatterErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr error
	}{
		{"no frontmatter", "# Just a heading\n", skills.ErrNoFrontmatter},
		{"unclosed frontmatter", "---\nname: x\ndescription: y\n", skills.ErrNoFrontmatter},
		{"missing name", "---\ndescription: y\n---\n", skills.ErrMissingName},
		{"missing description", "---\nname: x\n---\n", skills.ErrMissingDescription},
		{"empty folded description", "---\nname: x\ndescription: >\n---\n", skills.ErrMissingDescription},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := skills.ParseSkillFrontmatter(tt.content)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
