import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"sync"

	"golang.org/x/sync/errgroup"

	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
	logger logging.Logger
}

// githubTreeClient is the one GitHub client method the crawler uses.
// githubTreeClient has the GitHub client methods the crawler uses.
type githubTreeClient interface {
	GetTree(ctx context.Context, token, org, repo, ref string) ([]ghclient.TreeEntry, error)
	GetRawContent(ctx context.Context, token, org, repo, ref, path string) ([]byte, error)
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
	}
	return "", false
}

// Crawl finds the skills in the marketplace repository and downloads their
// files. It is the entry point the service layer calls on a cache miss.
//
// Skills are fetched in parallel. Crawl returns an error if the tree walk or
// the token fails, if ctx ends, or if GitHub is unavailable or rate-limits a
// download (ghclient.ErrUnavailable). For these errors the backend ADR
// (docs/adr/2026-08-28-marketplace-adr-backend.md) expects a 503 and no cache
// write, so a GitHub outage never leaves a partial list in the cache. Any
// other failure of one skill — bad frontmatter, or a file that fails to
// download — skips only that skill, with a warning.
//
// The result is sorted by Slug.
func (c *Crawler) Crawl(ctx context.Context, source MarketplaceSource) ([]Skill, error) {
	skillPaths, err := c.FetchTree(ctx, source)
	if err != nil {
		return nil, err
	}

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get github token: %w", err)
	}

	var mu sync.Mutex
	skills := make([]Skill, 0, len(skillPaths))

	g, gctx := errgroup.WithContext(ctx)
	for skillName, paths := range skillPaths {
		g.Go(func() error {
			skill, err := c.fetchSkill(gctx, token, source, skillName, paths)
			if err == nil {
				mu.Lock()
				skills = append(skills, skill)
				mu.Unlock()
				return nil
			}
			if errors.Is(err, ghclient.ErrUnavailable) {
				// A GitHub outage or rate limit: stop the whole crawl.
				return err
			}
			if gctx.Err() != nil {
				// The crawl stopped: ctx ended, or another skill hit an outage.
				return gctx.Err()
			}
			// A problem with this skill only.
			c.logger.Warn(gctx, "skipping skill that failed to load — the other skills are kept",
				logging.Field{Key: "marketplace", Value: source.Name},
				logging.Field{Key: "skillName", Value: skillName},
				logging.Field{Key: "skillPath", Value: paths.SkillPath},
				logging.Field{Key: "error", Value: err.Error()})
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	sort.Slice(skills, func(i, j int) bool { return skills[i].Slug < skills[j].Slug })
	return skills, nil
}

// fetchSkill downloads the SKILL.md of one skill, parses its frontmatter,
// and then downloads the documents in parallel. SKILL.md comes first, so a
// skill with bad frontmatter costs one download, not one per document.
func (c *Crawler) fetchSkill(ctx context.Context, token string, source MarketplaceSource, skillName string, paths SkillFilePaths) (Skill, error) {
	skillFile, err := c.fetchFile(ctx, token, source, paths.SkillPath)
	if err != nil {
		return Skill{}, err
	}

	frontmatter, err := ParseSkillFrontmatter(skillFile.Content)
	if err != nil {
		return Skill{}, fmt.Errorf("parse frontmatter of %s: %w", paths.SkillPath, err)
	}

	// documents has a fixed length, so it keeps the tree order and is never
	// nil: a skill without documents marshals as "documents": [].
	documents := make([]SkillFile, len(paths.DocumentPaths))
	g, gctx := errgroup.WithContext(ctx)
	for i, documentPath := range paths.DocumentPaths {
		g.Go(func() error {
			document, err := c.fetchFile(gctx, token, source, documentPath)
			if err != nil {
				return err
			}
			documents[i] = document
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Skill{}, err
	}

	return Skill{
		Slug:        source.Name + "/" + skillName, // ADR "Slug Format": {marketplace-name}/{skill-name}
		Name:        frontmatter.Name,
		Description: frontmatter.Description,
		Skill:       skillFile,
		Documents:   documents,
	}, nil
}

// fetchFile downloads one file of the repository. Every download goes
// through here, so the concurrency caps can wrap this one call.
func (c *Crawler) fetchFile(ctx context.Context, token string, source MarketplaceSource, filePath string) (SkillFile, error) {
	content, err := c.github.GetRawContent(ctx, token, source.Org, source.Repo, source.Branch, filePath)
	if err != nil {
		return SkillFile{}, fmt.Errorf("download %s: %w", filePath, err)
	}
	return SkillFile{Path: filePath, Name: path.Base(filePath), Content: string(content)}, nil
}