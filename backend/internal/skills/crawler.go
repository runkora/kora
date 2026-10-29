package skills

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// skillFilePath holds the SKILL.md path and any resource document paths for
// one skill found during FetchTree.
type skillFilePath struct {
	skillPath string
	docPaths  []string
}

const (
	// perRepoCap bounds the concurrent GitHub fetches of a single FetchSkills call.
	perRepoCap = 20
	// globalCap bounds the concurrent GitHub fetches across every FetchSkills call
	// in the process, protecting the shared GitHub rate limit.
	globalCap = 100

	// defaultBranch is passed as the Git ref when no branch is configured,
	// causing GitHub to use the repository's default branch.
	defaultBranch = "HEAD"
)

// Crawler finds and downloads skills in a marketplace repository.
type Crawler struct {
	github    githubTreeClient
	tokens    githubTokenProvider
	logger    logging.Logger
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

// NewCrawler creates a Crawler using the given single GitHub token provider.
func NewCrawler(
	github githubTreeClient,
	tokens githubTokenProvider,
	logger logging.Logger,
) *Crawler {
	return &Crawler{
		github:    github,
		tokens:    tokens,
		logger:    logger,
		globalSem: semaphore.NewWeighted(globalCap),
	}
}

// FetchTree lists all files in the marketplace repository with a GitHub call.
// It returns the SKILL.md path of each skill, keyed by the skill's folder path.
// A skill is any folder that contains a SKILL.md, at any depth.
// A SKILL.md at the repository root is skipped with a warning.
// An empty repository gives an empty map.
// collects and stores the paths of valid SKILL.md files in the skills map.
func (c *Crawler) FetchTree(ctx context.Context, source MarketplaceSource) (map[string]skillFilePath, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get github token for marketplace %q: %w", source.Name, err)
	}

	files, err := c.github.GetTree(ctx, token, source.Org, source.RepoName, defaultBranch)
	if err != nil {
		return nil, fmt.Errorf("list files of %s/%s: %w", source.Org, source.RepoName, err)
	}

	skills := map[string]skillFilePath{}
	for _, file := range files {
		if file.Type != "blob" || path.Base(file.Path) != "SKILL.md" {
			continue
		}

		folder := path.Dir(file.Path)
		if folder == "." {
			c.logger.Warn(ctx,
				"skipping SKILL.md at the repository root — a skill needs its own folder",
				logging.Field{Key: "marketplace", Value: source.Name},
			)
			continue
		}

		if existing, ok := skills[folder]; ok {
			c.logger.Warn(ctx,
				"skipping duplicate SKILL.md — the earlier path is kept",
				logging.Field{Key: "marketplace", Value: source.Name},
				logging.Field{Key: "kept", Value: existing.skillPath},
				logging.Field{Key: "skipped", Value: file.Path},
			)
			continue
		}

		skills[folder] = skillFilePath{skillPath: file.Path}
	}

	// Second pass: collect resource documents — any file (not SKILL.md)
	// inside a skill folder or its subfolders. When multiple skill folders
	// nest, the file is assigned to the most specific (longest) match.
	collectResourceDocs(files, skills)

	return skills, nil
}

func collectResourceDocs(
	files []ghclient.TreeEntry,
	skills map[string]skillFilePath,
) {
	for _, file := range files {
		if file.Type != "blob" || path.Base(file.Path) == "SKILL.md" {
			continue
		}

		fileDir := path.Dir(file.Path)
		bestFolder := ""
		for folder := range skills {
			if strings.HasPrefix(fileDir+"/", folder+"/") &&
				len(folder) > len(bestFolder) {
				bestFolder = folder
			}
		}

		if bestFolder != "" {
			fp := skills[bestFolder]
			fp.docPaths = append(fp.docPaths, file.Path)
			skills[bestFolder] = fp
		}
	}
}

// FetchSkills downloads the SKILL.md and all resource documents of every skill
// that FetchTree found and returns a []Skill sorted by SkillSlug.
//
// Skills are fetched in parallel. A GitHub outage or rate limit aborts the
// whole crawl. Other per-skill failures (bad frontmatter, fetch error) skip
// that skill with a log line; the rest are kept.
//
// If two SKILL.md files produce the same skillSlug, the one whose path sorts
// first is kept; the other is skipped.
func (c *Crawler) FetchSkills(ctx context.Context, source MarketplaceSource, skillPaths map[string]skillFilePath) ([]Skill, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get github token for marketplace %q: %w", source.Name, err)
	}

	var mu sync.Mutex
	skills := make([]Skill, 0, len(skillPaths))
	perRepoSem := semaphore.NewWeighted(perRepoCap)

	g, gctx := errgroup.WithContext(ctx)
	for _, fp := range skillPaths {
		g.Go(func() error {
			skill, err := c.fetchSkillFull(gctx, token, source, fp, perRepoSem)
			if err == nil {
				mu.Lock()
				skills = append(skills, skill)
				mu.Unlock()
				return nil
			}
			if errors.Is(err, ghclient.ErrUnavailable) {
				return err
			}
			if gctx.Err() != nil {
				return gctx.Err()
			}
			c.logger.Warn(gctx, "skipping skill that failed to load — the other skills are kept",
				logging.Field{Key: "marketplace", Value: source.Name},
				logging.Field{Key: "skillPath", Value: fp.skillPath},
				logging.Field{Key: "error", Value: err.Error()},
			)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	sort.Slice(skills, func(i, j int) bool { return skills[i].SkillSlug < skills[j].SkillSlug })

	return skills, nil
}

// fetchSkillFull downloads SKILL.md and all resource documents for one skill,
// parses its frontmatter, and returns the full Skill record.
func (c *Crawler) fetchSkillFull(ctx context.Context, token string, source MarketplaceSource, fp skillFilePath, perRepoSem *semaphore.Weighted) (Skill, error) {
	raw, err := c.fetchRaw(ctx, token, source, fp.skillPath, perRepoSem)
	if err != nil {
		return Skill{}, err
	}

	fm, err := ParseSkillFrontmatter(raw)
	if err != nil {
		return Skill{}, fmt.Errorf("parse frontmatter of %s: %w", fp.skillPath, err)
	}

	nameSlug := skillSlugify(fm.Name)
	if nameSlug == "" {
		return Skill{}, fmt.Errorf("skill name %q in %s slugifies to empty string", fm.Name, fp.skillPath)
	}

	fetched := make([]*SkillFile, len(fp.docPaths))
	g, gctx := errgroup.WithContext(ctx)
	for i, docPath := range fp.docPaths {
		g.Go(func() error {
			content, err := c.fetchRaw(gctx, token, source, docPath, perRepoSem)
			if err != nil {
				return err
			}
			// Binary files (fonts, images, archives) cannot be carried in a
			// text Content field, so they are left out of the skill.
			if !isText(content) {
				c.logger.Debug(gctx, "skipping binary resource document",
					logging.Field{Key: "marketplace", Value: source.Name},
					logging.Field{Key: "path", Value: docPath},
				)
				return nil
			}
			fetched[i] = &SkillFile{
				Path:    docPath,
				Name:    path.Base(docPath),
				Content: content,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Skill{}, err
	}

	docs := make([]SkillFile, 0, len(fetched))
	for _, doc := range fetched {
		if doc != nil {
			docs = append(docs, *doc)
		}
	}

	return Skill{
		Name:        fm.Name,
		Description: fm.Description,
		SkillSlug:   source.MarketplaceSlug + "__" + nameSlug,
		File: SkillFile{
			Path:    fp.skillPath,
			Name:    path.Base(fp.skillPath),
			Content: raw,
		},
		Documents: docs,
	}, nil
}

// isText reports whether content is UTF-8 text with no NUL bytes.
func isText(content string) bool {
	return utf8.ValidString(content) && !strings.ContainsRune(content, 0)
}

// fetchRaw downloads one file, acquiring both the global and per-repo semaphores.
func (c *Crawler) fetchRaw(ctx context.Context, token string, source MarketplaceSource, filePath string, perRepoSem *semaphore.Weighted) (string, error) {
	if err := c.globalSem.Acquire(ctx, 1); err != nil {
		return "", err
	}
	defer c.globalSem.Release(1)

	if err := perRepoSem.Acquire(ctx, 1); err != nil {
		return "", err
	}
	defer perRepoSem.Release(1)

	content, err := c.github.GetRawContent(ctx, token, source.Org, source.RepoName, defaultBranch, filePath)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", filePath, err)
	}
	return string(content), nil
}
