package skills

import (
	"errors"
	"reflect"
	"strings"
	"testing"
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
			parsed, err := parseSkillFrontmatter(tt.content)
			if err != nil {
				t.Fatalf("parseSkillFrontmatter: %v", err)
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
		{"no frontmatter", "# Just a heading\n", errNoFrontmatter},
		{"unclosed frontmatter", "---\nname: x\ndescription: y\n", errNoFrontmatter},
		{"missing name", "---\ndescription: y\n---\n", errMissingName},
		{"missing description", "---\nname: x\n---\n", errMissingDescription},
		{"empty folded description", "---\nname: x\ndescription: >\n---\n", errMissingDescription},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSkillFrontmatter(tt.content)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseSkillFrontmatter_Metadata(t *testing.T) {
	v := func(s string) *string { return &s }
	for _, tc := range []struct {
		name    string
		content string
		want    skillFrontmatter
	}{
		{
			name:    "top-level fields and first heading",
			content: "---\nname: pdf\ndescription: PDFs.\nversion: v2.1.0\ntags: [documents, forms]\nallowed-tools: Bash, Read\ndisable-model-invocation: true\n---\nIntro\n## Not this\n# PDF Processing Guide \n# Second\n",
			want:    skillFrontmatter{Name: "pdf", Description: "PDFs.", Title: "PDF Processing Guide", Version: v("2.1.0"), Tags: []string{"documents", "forms"}, Tools: []string{"Bash", "Read"}, ManualOnly: true},
		},
		{
			name:    "metadata fallbacks and a space separated string",
			content: "---\nname: docx\ndescription: Word.\nmetadata:\n  version: \"1.4\"\n  tags: office docs\n---\nNo heading here.\n",
			want:    skillFrontmatter{Name: "docx", Description: "Word.", Title: "docx", Version: v("1.4"), Tags: []string{"office", "docs"}, Tools: []string{}},
		},
		{
			name:    "numeric versions keep their text and string flags parse",
			content: "---\nname: n\ndescription: N.\nversion: 2.10\ndisable-model-invocation: \"true\"\nallowed-tools: Bash(git status:*), Bash(git add:*), Read\n---\n```bash\n# install deps\n```\n# Real Title #\n",
			want:    skillFrontmatter{Name: "n", Description: "N.", Title: "Real Title", Version: v("2.10"), Tags: []string{}, Tools: []string{"Bash(git status:*)", "Bash(git add:*)", "Read"}, ManualOnly: true},
		},
		{
			name:    "non-strict YAML keeps single-line metadata",
			content: "---\nname: loose\ndescription: Use when: things\ntags: [x, y]\nversion: 3\n---\n",
			want:    skillFrontmatter{Name: "loose", Description: "Use when: things", Title: "loose", Version: v("3"), Tags: []string{"x", "y"}, Tools: []string{}},
		},
		{
			name:    "a large integer version",
			content: "---\nname: big\ndescription: B.\nmetadata:\n  version: 20241105\n---\n",
			want:    skillFrontmatter{Name: "big", Description: "B.", Title: "big", Version: v("20241105"), Tags: []string{}, Tools: []string{}},
		},
		{
			name:    "versions as written, comments and nulls",
			content: "---\nname: w\ndescription: W.\nversion: 1.10 # bump\nmetadata:\n  version: 9.90\n---\n",
			want:    skillFrontmatter{Name: "w", Description: "W.", Title: "w", Version: v("1.10"), Tags: []string{}, Tools: []string{}},
		},
		{
			name:    "nested numeric version keeps trailing zeros",
			content: "---\nname: m\ndescription: M.\nmetadata:\n  version: 1.10\n---\n",
			want:    skillFrontmatter{Name: "m", Description: "M.", Title: "m", Version: v("1.10"), Tags: []string{}, Tools: []string{}},
		},
		{
			name:    "tilde is absent",
			content: "---\nname: t\ndescription: T.\nversion: ~\n---\n",
			want:    skillFrontmatter{Name: "t", Description: "T.", Title: "t", Tags: []string{}, Tools: []string{}},
		},
		{
			name:    "null is absent",
			content: "---\nname: u\ndescription: U.\nversion: null\nmetadata:\n  version: null\n---\n",
			want:    skillFrontmatter{Name: "u", Description: "U.", Title: "u", Tags: []string{}, Tools: []string{}},
		},
		{
			name:    "quoted string version is taken as decoded",
			content: "---\nname: q\ndescription: Q.\nversion: \"v2.0 # not a comment\"\n---\n",
			want:    skillFrontmatter{Name: "q", Description: "Q.", Title: "q", Version: v("2.0 # not a comment"), Tags: []string{}, Tools: []string{}},
		},
		{
			name:    "nothing declared",
			content: "---\nname: bare\ndescription: Bare.\n---\n",
			want:    skillFrontmatter{Name: "bare", Description: "Bare.", Title: "bare", Tags: []string{}, Tools: []string{}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSkillFrontmatter(tc.content)
			if err != nil {
				t.Fatal(err)
			}
			if body := tc.content[got.BodyStart:]; strings.HasPrefix(body, "---") || !strings.HasSuffix(tc.content, body) {
				t.Errorf("BodyStart %d points at %q", got.BodyStart, body)
			}
			got.BodyStart = 0
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v (version %v), want %+v (version %v)", got, deref(got.Version), tc.want, deref(tc.want.Version))
			}
		})
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
