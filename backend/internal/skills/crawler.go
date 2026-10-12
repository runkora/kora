
package skills

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

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

const (
	// perRepoCap bounds the concurrent GitHub fetches of a single FetchSkills call.
	perRepoCap = 20
	// globalCap bounds the concurrent GitHub fetches across every FetchSkills call
	// in the process, protecting the shared GitHub rate limit.
	globalCap = 100
)

// Crawler finds the skills in a marketplace repository.
type Crawler struct {
	github githubTreeClient
	tokens githubTokenProviderResolver
	logger logging.Logger
	// globalSem is shared by every FetchSkills call on this Crawler.
	// Crawler is constructed once per process, so this is the process-wide cap.
	globalSem *semaphore.Weighted
}

// githubTreeClient has the GitHub client methods the crawler uses.
type githubTreeClient interface {
	GetTree(ctx context.Context, token, org, repo, ref string) ([]ghclient.TreeEntry, error)
	GetRawContent(ctx context.Context, token, org, repo, ref, path string) ([]byte, error)
}

// githubTokenProvider gives the crawler a GitHub token.
type githubTokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// githubTokenProviderResolver selects a token provider for a marketplace source.
//
// For an empty credentialsRef, the pool should return the default provider.
// The pool should return an error if the requested credentials cannot be loaded.
type githubTokenProviderResolver interface {
	For(credentialsRef string) (githubTokenProvider, error)
}

// NewCrawler creates a Crawler using a token provider pool.
func NewCrawler(
	github githubTreeClient,
	tokens githubTokenProviderResolver,
	logger logging.Logger,
) *Crawler {
	return &Crawler{
		github:    github,
		tokens:    tokens,
		logger:    logger,
		globalSem: semaphore.NewWeighted(globalCap),
	}
}

// tokenForSource resolves the provider configured for a marketplace source
// and obtains its GitHub access token.
func (c *Crawler) tokenForSource(ctx context.Context,source MarketplaceSource) (string, error) {
	provider, err := c.tokens.For(source.CredentialsRef)
	if err != nil {
		return "", fmt.Errorf("resolve github credentials for marketplace %q: %w", source.Name, err)
	}

	token, err := provider.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("get github token for marketplace %q: %w", source.Name, err)
	}

	return token, nil
}

// FetchTree lists all files in the marketplace repository with a GitHub call.
// It returns the file paths of each skill by skillName
// A skill is any folder that contains a SKILL.md, at any depth
// Two skill folders with the same name: the first one in the tree is kept
// and the other is skipped with a warning. A SKILL.md at the repository root
// has no folder name and is skipped with a warning. An empty repository gives
// an empty map.
func (c *Crawler) FetchTree(ctx context.Context, source MarketplaceSource) (map[string]SkillFilePaths, error) {
	token, err := c.tokenForSource(ctx, source)
	if err != nil {
		return nil, err
	}

	files, err := c.github.GetTree(ctx, token, source.Org, source.Repo, source.Branch)
	if err != nil {
		return nil, fmt.Errorf("list files of %s/%s: %w", source.Org, source.Repo, err)
	}

	skills := map[string]SkillFilePaths{}
	skillFolders := map[string]string{}
	for _, file := range files {
		if file.Type != "blob" || path.Base(file.Path) != "SKILL.md" {
			continue
		}

		folder := path.Dir(file.Path)
		if folder == "." {
			c.logger.Warn(
				ctx,
				"skipping SKILL.md at the repository root — a skill needs its own folder",
				logging.Field{Key: "marketplace", Value: source.Name},
			)
			continue
		}

		skillName := path.Base(folder)
		if existing, ok := skills[skillName]; ok {
			c.logger.Warn(
				ctx,
				"skipping skill with a duplicate name — the first one is kept",
				logging.Field{Key: "marketplace", Value: source.Name},
				logging.Field{Key: "skillName", Value: skillName},
				logging.Field{Key: "kept", Value: existing.SkillPath},
				logging.Field{Key: "skipped", Value: file.Path},
			)
			continue
		}

		skills[skillName] = SkillFilePaths{
			SkillPath: file.Path,
		}
		skillFolders[folder] = skillName
	}

	// Step 2: Give every other file to the nearest skill folder above it.
	for _, file := range files {
		if file.Type != "blob" {
			continue
		}

		skillName, ok := nearestSkill(file.Path, skillFolders)
		if !ok {
			continue
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

// FetchSkills downloads the files of the skills that FetchTree found.
//
// Skills are fetched in parallel. A GitHub outage or rate limit aborts the
// whole crawl. Other failures skip only the affected skill.
//
// The result is sorted by Slug.
func (c *Crawler) FetchSkills(ctx context.Context,_ string, source MarketplaceSource, skillPaths map[string]SkillFilePaths) ([]Skill, error) {
	token, err := c.tokenForSource(ctx, source)
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	skills := make([]Skill, 0, len(skillPaths))
	perRepoSem := semaphore.NewWeighted(perRepoCap)

	g, gctx := errgroup.WithContext(ctx)
	for skillName, paths := range skillPaths {
		g.Go(func() error {
			skill, err := c.fetchSkill(gctx, token, source, skillName, paths, perRepoSem)
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
				return gctx.Err()
			}
			// A problem with this skill only.
			c.logger.Warn(gctx,"skipping skill that failed to load — the other skills are kept",
				logging.Field{Key: "marketplace", Value: source.Name},
				logging.Field{Key: "skillName", Value: skillName},
				logging.Field{Key: "skillPath", Value: paths.SkillPath},
				logging.Field{Key: "error", Value: err.Error()},
			)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	sort.Slice(skills, func(i, j int) bool { return skills[i].Slug < skills[j].Slug })
	return skills, nil
}

// Crawl walks the repository tree and downloads the skills. It calls
// FetchTree, then FetchSkills, so it returns the same errors and result.
func (c *Crawler) Crawl(ctx context.Context, source MarketplaceSource) ([]Skill, error) {
	skillPaths, err := c.FetchTree(ctx, source)
	if err != nil {
		return nil, err
	}
	return c.FetchSkills(ctx, source.Name, source, skillPaths)
}

// fetchSkill downloads the SKILL.md of one skill, parses its frontmatter,
// and then downloads the documents in parallel. SKILL.md comes first, so a
// skill with bad frontmatter costs one download, not one per document.
func (c *Crawler) fetchSkill(ctx context.Context, token string, source MarketplaceSource, skillName string, paths SkillFilePaths, perRepoSem *semaphore.Weighted) (Skill, error) {
	skillFile, err := c.fetchFile(ctx, token, source, paths.SkillPath, perRepoSem)
	if err != nil {
		return Skill{}, err
	}

	frontmatter, err := ParseSkillFrontmatter(skillFile.Content)
	if err != nil {
		return Skill{}, fmt.Errorf("parse frontmatter of %s: %w", paths.SkillPath, err)
	}
	
	// A skill without documents marshals as "documents": [] rather than null.
	documents := make([]SkillFile, len(paths.DocumentPaths))
	g, gctx := errgroup.WithContext(ctx)
	for i, documentPath := range paths.DocumentPaths {
		g.Go(func() error {
			document, err := c.fetchFile(gctx, token, source, documentPath, perRepoSem)
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
		Slug:        source.Org + ":" + source.Repo + ":" + skillName,
		Name:        frontmatter.Name,
		Description: frontmatter.Description,
		Skill:       skillFile,
		Documents:   documents,
	}, nil
}

// fetchFile downloads one file of the repository. Every download goes
// through here, so the concurrency caps wrap this one call. Acquiring
// through semaphore.Weighted (rather than a plain channel) means a goroutine
// still waiting for a permit returns immediately with ctx.Err() if ctx ends,
// instead of blocking until a permit happens to free up.
func (c *Crawler) fetchFile(ctx context.Context, token string, source MarketplaceSource, filePath string, perRepoSem *semaphore.Weighted) (SkillFile, error) {
	if err := c.globalSem.Acquire(ctx, 1); err != nil {
		return SkillFile{}, err
	}
	defer c.globalSem.Release(1)

	if err := perRepoSem.Acquire(ctx, 1); err != nil {
		return SkillFile{}, err
	}
	defer perRepoSem.Release(1)

	content, err := c.github.GetRawContent(ctx, token, source.Org, source.Repo, source.Branch, filePath)
	if err != nil {
		return SkillFile{}, fmt.Errorf("download %s: %w", filePath, err)
	}
	return SkillFile{Path: filePath, Name: path.Base(filePath), Content: string(content)}, nil
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
