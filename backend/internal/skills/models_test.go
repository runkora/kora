package skills_test

import (
	"encoding/json"
	"testing"

	"github.com/clear-route/orbit/internal/skills"
)

func TestSkillMarshalRoundtrip(t *testing.T) {
	original := skills.Skill{
		Name:        "React Expert",
		Description: "Senior React developer",
		SkillSlug:   "org__repo__react-expert",
		File: skills.SkillFile{
			Path:    "skills/react-expert/SKILL.md",
			Name:    "SKILL.md",
			Content: "# React Expert\nSome content",
		},
		Documents: []skills.SkillFile{
			{Path: "skills/react-expert/resources/testing.md", Name: "testing.md", Content: "# Testing"},
		},
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got skills.Skill
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Name != original.Name || got.SkillSlug != original.SkillSlug || got.File.Content != original.File.Content {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", got, original)
	}
	if len(got.Documents) != 1 || got.Documents[0].Content != original.Documents[0].Content {
		t.Errorf("roundtrip documents mismatch: got %+v, want %+v", got.Documents, original.Documents)
	}
}

func TestSkillEntryPathInJSON(t *testing.T) {
	entry := skills.SkillEntry{
		Name:        "React Expert",
		Description: "desc",
		SkillSlug:   "org__repo__react-expert",
		Path:        "skills/react-expert/SKILL.md",
	}

	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Path must be present so Redis-cached entries retain the GitHub path.
	if _, ok := got["path"]; !ok {
		t.Error("SkillEntry.Path missing from JSON — Redis cache would lose the file path")
	}
	if got["skillSlug"] != "org__repo__react-expert" {
		t.Errorf("skillSlug = %v, want org__repo__react-expert", got["skillSlug"])
	}
}
