package skills_test

import (
	"encoding/json"
	"testing"

	"github.com/clear-route/orbit/internal/skills"
)

func TestSkillDocumentsMarshalAsEmptyArray(t *testing.T) {
	s := skills.Skill{
		Slug:        "clear-route-skills/react-expert",
		Name:        "React Expert",
		Description: "Senior React developer",
		Skill: skills.SkillFile{
			Path:    "skills/react-expert/SKILL.md",
			Name:    "SKILL.md",
			Content: "# React Expert",
		},
		Documents: []skills.SkillFile{},
	}

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	docs, ok := got["documents"]
	if !ok {
		t.Fatal("documents field missing from JSON")
	}
	arr, ok := docs.([]any)
	if !ok {
		t.Fatalf("documents is not an array, got %T", docs)
	}
	if len(arr) != 0 {
		t.Fatalf("expected empty array, got %d items", len(arr))
	}
}

func TestSkillRoundTrip(t *testing.T) {
	original := skills.Skill{
		Slug:        "mkt/skill-name",
		Name:        "My Skill",
		Description: "Does things",
		Skill:       skills.SkillFile{Path: "skills/skill-name/SKILL.md", Name: "SKILL.md", Content: "body"},
		Documents: []skills.SkillFile{
			{Path: "skills/skill-name/resources/guide.md", Name: "guide.md", Content: "guide"},
		},
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded skills.Skill
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Slug != original.Slug {
		t.Errorf("Slug: got %q want %q", decoded.Slug, original.Slug)
	}
	if len(decoded.Documents) != 1 {
		t.Errorf("Documents: got %d items want 1", len(decoded.Documents))
	}
	if decoded.Documents[0].Name != "guide.md" {
		t.Errorf("Documents[0].Name: got %q want %q", decoded.Documents[0].Name, "guide.md")
	}
}
