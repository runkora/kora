package skills

import (
	"context"
	"fmt"
	"path"

	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// SkillFilePaths holds the file paths of one skill in the repository.
// SkillPath is the SKILL.md file.
// DocumentPaths are the other files in the skill folder, at any depth.
type SkillFilePaths struct {
	SkillPath     string
	DocumentPaths []string
}

// Crawler finds the skills in a marketplace repository.
type Crawler struct {
	github githubTreeClient
	tokens githubTokenProvider
	logger logging.Logger
}

// githubTreeClient is the one GitHub client method the crawler uses.
type githubTreeClient interface {
	GetTree(ctx context.Context, token, org, repo, ref string) ([]ghclient.TreeEntry, error)
}

// githubTokenProvider gives the crawler a GitHub token. *TokenProvider satisfies it.
type githubTokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// NewCrawler creates a Crawler.
func NewCrawler(github githubTreeClient, tokens githubTokenProvider, logger logging.Logger) *Crawler {
	return &Crawler{github: github, tokens: tokens, logger: logger}
}

// FetchTree lists all files in the marketplace repository with a GitHub call.
// It returns the file paths of each skill by skillName.
//
// A skill is any folder that contains a SKILL.md, at any depth, so both of
// these layouts work:
//
//	skills/{skillName}/SKILL.md
//	plugins/{pluginName}/skills/{skillName}/SKILL.md
//
// skillName is the name of that folder. Every other file in the folder, at
// any depth, is a document of the skill. A file that belongs to a nested
// skill folder belongs to the nested skill only. Files outside every skill
// folder are ignored.
//
// Two skill folders with the same name: the first one in the tree is kept
// and the other is skipped with a warning. A SKILL.md at the repository root
// has no folder name and is skipped with a warning. An empty repository gives
// an empty map.
func (c *Crawler) FetchTree(ctx context.Context, source MarketplaceSource) (map[string]SkillFilePaths, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get github token: %w", err)
	}

	files, err := c.github.GetTree(ctx, token, source.Org, source.Repo, source.Branch)
	if err != nil {
		return nil, fmt.Errorf("list files of %s/%s: %w", source.Org, source.Repo, err)
	}

	// Step 1: find the skill folders. skillFolders maps each folder path to
	// its skillName, for example "plugins/decks/skills/deck-builder" → "deck-builder".
	skills := map[string]SkillFilePaths{}
	skillFolders := map[string]string{}
	for _, file := range files {
		if file.Type != "blob" || path.Base(file.Path) != "SKILL.md" {
			continue
		}

		folder := path.Dir(file.Path)
		if folder == "." {
			c.logger.Warn(ctx, "skipping SKILL.md at the repository root — a skill needs its own folder",
				logging.Field{Key: "marketplace", Value: source.Name})
			continue
		}

		skillName := path.Base(folder)
		if existing, ok := skills[skillName]; ok {
			c.logger.Warn(ctx, "skipping skill with a duplicate name — the first one is kept",
				logging.Field{Key: "marketplace", Value: source.Name},
				logging.Field{Key: "skillName", Value: skillName},
				logging.Field{Key: "kept", Value: existing.SkillPath},
				logging.Field{Key: "skipped", Value: file.Path})
			continue
		}

		skills[skillName] = SkillFilePaths{SkillPath: file.Path}
		skillFolders[folder] = skillName
	}

	// Step 2: give every other file to the nearest skill folder above it.
	for _, file := range files {
		if file.Type != "blob" {
			continue // a folder, not a file
		}

		skillName, ok := nearestSkill(file.Path, skillFolders)
		if !ok {
			continue // not inside a skill folder
		}

		paths := skills[skillName]
		if file.Path == paths.SkillPath {
			continue
		}
		paths.DocumentPaths = append(paths.DocumentPaths, file.Path)
		skills[skillName] = paths
	}

	return skills, nil
}

// nearestSkill reports the skillName of the nearest skill folder that
// contains filePath. It returns false if no skill folder contains it.
func nearestSkill(filePath string, skillFolders map[string]string) (string, bool) {
	for folder := path.Dir(filePath); folder != "."; folder = path.Dir(folder) {
		if skillName, ok := skillFolders[folder]; ok {
			return skillName, true
		}
	}
	return "", false
}
