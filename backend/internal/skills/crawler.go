package skills

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

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
// All marketplaces share the same GitHub App credentials.
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

	// Second pass: collect resource documents — any .md file (not SKILL.md)
	// inside a skill folder or its subfolders. When multiple skill folders
	// nest, the file is assigned to the most specific (longest) match.
	for _, file := range files {
		if file.Type != "blob" || path.Base(file.Path) == "SKILL.md" {
			continue
		}
		if !strings.HasSuffix(file.Path, ".md") {
			continue
		}
		fileDir := path.Dir(file.Path)
		bestFolder := ""
		for folder := range skills {
			if strings.HasPrefix(fileDir+"/", folder+"/") && len(folder) > len(bestFolder) {
				bestFolder = folder
			}
		}
		if bestFolder != "" {
			fp := skills[bestFolder]
			fp.docPaths = append(fp.docPaths, file.Path)
			skills[bestFolder] = fp
		}
	}

	return skills, nil
}

// FetchSkills downloads the SKILL.md of every skill that FetchTree found and
// returns a []SkillEntry sorted by SkillSlug.
//
// Skills are fetched in parallel. A GitHub outage or rate limit aborts the
// whole crawl. Other per-skill failures (bad frontmatter, fetch error) skip
// that skill with a log line; the rest are kept.
//
// If two SKILL.md files produce the same skillSlug, the one whose path sorts
// first is kept; the other is skipped.
func (c *Crawler) FetchSkills(ctx context.Context, source MarketplaceSource, skillPaths map[string]skillFilePath) ([]SkillEntry, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get github token for marketplace %q: %w", source.Name, err)
	}

	var mu sync.Mutex
	entries := make([]SkillEntry, 0, len(skillPaths))
	perRepoSem := semaphore.NewWeighted(perRepoCap)

	g, gctx := errgroup.WithContext(ctx)
	for _, fp := range skillPaths {
		g.Go(func() error {
			entry, err := c.fetchSkill(gctx, token, source, fp, perRepoSem)
			if err == nil {
				mu.Lock()
				entries = append(entries, entry)
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

	sort.Slice(entries, func(i, j int) bool { return entries[i].SkillSlug < entries[j].SkillSlug })

	// Deduplicate: two skills that produce the same slug after slugify — keep the
	// one whose Path sorts first (path order is deterministic within a sorted slice).
	deduped := entries[:0]
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if _, dup := seen[e.SkillSlug]; dup {
			c.logger.Warn(ctx, "skipping skill with duplicate slug — the first path is kept",
				logging.Field{Key: "marketplace", Value: source.Name},
				logging.Field{Key: "skillSlug", Value: e.SkillSlug},
				logging.Field{Key: "skipped", Value: e.Path},
			)
			continue
		}
		seen[e.SkillSlug] = struct{}{}
		deduped = append(deduped, e)
	}

	return deduped, nil
}

// FetchSkillContent downloads the SKILL.md and all resource documents for the
// given entry and returns the full Skill record for the detail endpoint.
func (c *Crawler) FetchSkillContent(ctx context.Context, source MarketplaceSource, entry SkillEntry) (Skill, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return Skill{}, fmt.Errorf("get github token for marketplace %q: %w", source.Name, err)
	}

	all := make([]string, 0, 1+len(entry.DocPaths))
	all = append(all, entry.Path)
	all = append(all, entry.DocPaths...)

	capacity := int64(len(all))
	if capacity > perRepoCap {
		capacity = perRepoCap
	}
	perCallSem := semaphore.NewWeighted(capacity)

	contents := make([]string, len(all))
	g, gctx := errgroup.WithContext(ctx)
	for i, filePath := range all {
		i, filePath := i, filePath
		g.Go(func() error {
			raw, err := c.fetchRaw(gctx, token, source, filePath, perCallSem)
			if err != nil {
				return err
			}
			contents[i] = raw
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Skill{}, err
	}

	docs := make([]SkillFile, 0, len(entry.DocPaths))
	for i, docPath := range entry.DocPaths {
		docs = append(docs, SkillFile{
			Path:    docPath,
			Name:    path.Base(docPath),
			Content: contents[1+i],
		})
	}

	return Skill{
		Name:        entry.Name,
		Description: entry.Description,
		SkillSlug:   entry.SkillSlug,
		File: SkillFile{
			Path:    entry.Path,
			Name:    path.Base(entry.Path),
			Content: contents[0],
		},
		Documents: docs,
	}, nil
}

// fetchSkill downloads one SKILL.md, parses its frontmatter, and returns a
// SkillEntry. The skillSlug is built from the marketplace slug and the
// slugified frontmatter name.
func (c *Crawler) fetchSkill(ctx context.Context, token string, source MarketplaceSource, fp skillFilePath, perRepoSem *semaphore.Weighted) (SkillEntry, error) {
	raw, err := c.fetchRaw(ctx, token, source, fp.skillPath, perRepoSem)
	if err != nil {
		return SkillEntry{}, err
	}

	fm, err := ParseSkillFrontmatter(raw)
	if err != nil {
		return SkillEntry{}, fmt.Errorf("parse frontmatter of %s: %w", fp.skillPath, err)
	}

	nameSlug := skillSlugify(fm.Name)
	if nameSlug == "" {
		return SkillEntry{}, fmt.Errorf("skill name %q in %s slugifies to empty string", fm.Name, fp.skillPath)
	}

	return SkillEntry{
		Name:        fm.Name,
		Description: fm.Description,
		SkillSlug:   source.MarketplaceSlug + "__" + nameSlug,
		Path:        fp.skillPath,
		DocPaths:    fp.docPaths,
	}, nil
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
