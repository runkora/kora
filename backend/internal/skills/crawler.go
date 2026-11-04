package skills

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	skillFileName = "SKILL.md"
	// perCrawlFetches bounds concurrent file fetches of one crawl.
	perCrawlFetches = 10
	// globalFetches bounds concurrent file fetches across the process, which
	// share GitHub rate limits.
	globalFetches = 50
)

// Limits caps what one Marketplace may cost a crawl. A zero limit is no cap.
type Limits struct {
	SkillsPerMarketplace       int
	FilesPerSkill              int
	BytesPerFile               int64
	SkillMdBytesPerMarketplace int64
}

// over reports whether n passes limit. A zero limit is no cap.
func over[T int | int64](n, limit T) bool {
	return limit > 0 && n > limit
}

// textExtensions are the supporting files fetched. Anything else is treated as
// binary and never downloaded.
var textExtensions = map[string]bool{
	".md": true, ".markdown": true, ".txt": true, ".rst": true, ".adoc": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".cfg": true, ".conf": true,
	".xml": true, ".csv": true, ".tsv": true, ".html": true, ".htm": true, ".css": true, ".scss": true,
	".sh": true, ".bash": true, ".zsh": true, ".ps1": true,
	".py": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true, ".jsx": true,
	".go": true, ".rb": true, ".rs": true, ".java": true, ".kt": true, ".swift": true, ".c": true, ".h": true,
	".cpp": true, ".hpp": true, ".cs": true, ".php": true, ".pl": true, ".lua": true, ".r": true, ".sql": true,
	".tf": true, ".hcl": true, ".j2": true, ".tmpl": true, ".tpl": true,
}

// textNames are extensionless files that are text.
var textNames = map[string]bool{"LICENSE": true, "Makefile": true, "Dockerfile": true, "README": true}

func isTextPath(p string) bool {
	base := path.Base(p)
	return textNames[base] || textExtensions[strings.ToLower(path.Ext(base))]
}

// githubReader is the GitHub client surface the crawler uses.
type githubReader interface {
	ResolveRef(ctx context.Context, token, owner, repo, ref string) (string, error)
	GetTree(ctx context.Context, token, owner, repo, sha string) ([]ghclient.TreeEntry, error)
	GetRawContent(ctx context.Context, token, owner, repo, ref, path string, maxBytes int64) ([]byte, error)
}

// Crawler reads skills from a Marketplace repository.
type Crawler struct {
	github githubReader
	limits Limits
	logger logging.Logger
	global *semaphore.Weighted
}

// NewCrawler returns a Crawler bounded by limits.
func NewCrawler(github githubReader, limits Limits, logger logging.Logger) *Crawler {
	return &Crawler{github: github, limits: limits, logger: logger, global: semaphore.NewWeighted(globalFetches)}
}

// Crawl resolves the Marketplace ref to one commit, reads the tree at that
// commit and fetches every SKILL.md. Skills come back in name order.
func (c *Crawler) Crawl(ctx context.Context, m *Marketplace) (skillList, error) {
	token, err := m.Token(ctx)
	if err != nil {
		return skillList{}, err
	}
	ref := m.Ref
	if ref == "" {
		ref = "HEAD"
	}
	sha, err := c.github.ResolveRef(ctx, token, m.Owner, m.Repo, ref)
	if err != nil {
		return skillList{}, fmt.Errorf("resolve %s/%s@%s: %w", m.Owner, m.Repo, ref, err)
	}
	tree, err := c.github.GetTree(ctx, token, m.Owner, m.Repo, sha)
	if err != nil {
		return skillList{}, fmt.Errorf("read tree of %s/%s@%s: %w", m.Owner, m.Repo, sha, err)
	}

	folders := c.skillFolders(ctx, m, tree)
	records := make([]skillRecord, len(folders))
	found := make([]bool, len(folders))
	perCrawl := semaphore.NewWeighted(perCrawlFetches)
	g, gctx := errgroup.WithContext(ctx)
	for i, f := range folders {
		g.Go(func() error {
			raw, err := c.fetch(gctx, perCrawl, token, m, sha, path.Join(f.path, skillFileName), f.size)
			if err != nil {
				if errors.Is(err, ghclient.ErrUnavailable) || gctx.Err() != nil {
					return err
				}
				c.skip(gctx, m, f.path, err.Error())
				return nil
			}
			fm, err := parseSkillFrontmatter(raw)
			if err != nil {
				c.skip(gctx, m, f.path, err.Error())
				return nil
			}
			records[i] = skillRecord{Path: f.path, Name: fm.Name, Description: fm.Description, Content: raw, Files: f.files}
			found[i] = true
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return skillList{}, fmt.Errorf("fetch skills of %s/%s: %w", m.Owner, m.Repo, err)
	}

	list := skillList{Source: m.source(), SHA: sha, Skills: make([]skillRecord, 0, len(records))}
	for i, r := range records {
		if found[i] {
			list.Skills = append(list.Skills, r)
		}
	}
	slices.SortFunc(list.Skills, func(a, b skillRecord) int {
		if n := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); n != 0 {
			return n
		}
		return strings.Compare(a.Path, b.Path)
	})
	return list, nil
}

// skillFolder is a folder holding a SKILL.md of size bytes and its supporting
// files.
type skillFolder struct {
	path  string
	size  int64
	files []SkillFileInfo
}

// skillFolders finds every folder with a SKILL.md and assigns each other text
// blob to its deepest enclosing skill. Skills and files over the limits are
// skipped and logged.
func (c *Crawler) skillFolders(ctx context.Context, m *Marketplace, tree []ghclient.TreeEntry) []skillFolder {
	folders := c.findSkills(ctx, m, tree)
	byPath := make(map[string]*skillFolder, len(folders))
	for _, f := range folders {
		byPath[f.path] = f
	}
	for _, e := range tree {
		if e.Type != "blob" || path.Base(e.Path) == skillFileName || !isTextPath(e.Path) {
			continue
		}
		owner := deepestSkill(e.Path, byPath)
		switch {
		case owner == nil:
		case over(e.Size, c.limits.BytesPerFile):
			c.skip(ctx, m, e.Path, fmt.Sprintf("over bytesPerFile (%d)", c.limits.BytesPerFile))
		default:
			owner.files = append(owner.files, SkillFileInfo{Path: strings.TrimPrefix(e.Path, owner.path+"/"), Size: e.Size})
		}
	}

	out := make([]skillFolder, len(folders))
	for i, f := range folders {
		slices.SortFunc(f.files, func(a, b SkillFileInfo) int { return strings.Compare(a.Path, b.Path) })
		if over(len(f.files), c.limits.FilesPerSkill) {
			for _, file := range f.files[c.limits.FilesPerSkill:] {
				c.skip(ctx, m, path.Join(f.path, file.Path), fmt.Sprintf("over filesPerSkill (%d)", c.limits.FilesPerSkill))
			}
			f.files = f.files[:c.limits.FilesPerSkill]
		}
		out[i] = *f
	}
	return out
}

// findSkills returns the folders holding a SKILL.md in path order, up to
// skillsPerMarketplace and skillMdBytesPerMarketplace.
func (c *Crawler) findSkills(ctx context.Context, m *Marketplace, tree []ghclient.TreeEntry) []*skillFolder {
	var found []*skillFolder
	for _, e := range tree {
		if e.Type != "blob" || path.Base(e.Path) != skillFileName {
			continue
		}
		dir := path.Dir(e.Path)
		switch {
		case dir == ".":
			c.skip(ctx, m, e.Path, "a SKILL.md at the repository root is not in a skill folder")
		case over(e.Size, c.limits.BytesPerFile):
			c.skip(ctx, m, dir, fmt.Sprintf("SKILL.md is over bytesPerFile (%d)", c.limits.BytesPerFile))
		default:
			found = append(found, &skillFolder{path: dir, size: e.Size})
		}
	}
	slices.SortFunc(found, func(a, b *skillFolder) int { return strings.Compare(a.path, b.path) })

	folders := make([]*skillFolder, 0, len(found))
	var budget int64
	for _, f := range found {
		switch {
		case over(len(folders)+1, c.limits.SkillsPerMarketplace):
			c.skip(ctx, m, f.path, fmt.Sprintf("over skillsPerMarketplace (%d)", c.limits.SkillsPerMarketplace))
		case over(budget+f.size, c.limits.SkillMdBytesPerMarketplace):
			c.skip(ctx, m, f.path, fmt.Sprintf("over skillMdBytesPerMarketplace (%d)", c.limits.SkillMdBytesPerMarketplace))
		default:
			budget += f.size
			folders = append(folders, f)
		}
	}
	return folders
}

// deepestSkill returns the nearest skill folder above p, or nil.
func deepestSkill(p string, byPath map[string]*skillFolder) *skillFolder {
	for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
		if f, ok := byPath[dir]; ok {
			return f
		}
	}
	return nil
}

// FetchFiles fetches a skill's supporting files at sha. A file that turns out
// not to be text is left out.
func (c *Crawler) FetchFiles(ctx context.Context, m *Marketplace, sha string, skill skillRecord) ([]SkillFile, error) {
	token, err := m.Token(ctx)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	files := make([]SkillFile, 0, len(skill.Files))
	perSkill := semaphore.NewWeighted(perCrawlFetches)
	g, gctx := errgroup.WithContext(ctx)
	for _, file := range skill.Files {
		name := file.Path
		g.Go(func() error {
			content, err := c.fetch(gctx, perSkill, token, m, sha, path.Join(skill.Path, name), file.Size)
			if err != nil {
				if errors.Is(err, ghclient.ErrUnavailable) || gctx.Err() != nil {
					return err
				}
				c.skip(gctx, m, path.Join(skill.Path, name), err.Error())
				return nil
			}
			mu.Lock()
			files = append(files, SkillFile{Path: name, Content: content})
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("fetch files of %s: %w", skill.Path, err)
	}
	slices.SortFunc(files, func(a, b SkillFile) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// errNotText is a fetched file that is not UTF-8 text.
var errNotText = errors.New("file is not UTF-8 text")

// fetch downloads one text file of the size the tree reported.
func (c *Crawler) fetch(ctx context.Context, local *semaphore.Weighted, token string, m *Marketplace, sha, p string, size int64) (string, error) {
	// The per-crawl slot comes first, so a waiting crawl holds no global slot
	// and cannot starve other tenants.
	if err := local.Acquire(ctx, 1); err != nil {
		return "", err
	}
	defer local.Release(1)
	if err := c.global.Acquire(ctx, 1); err != nil {
		return "", err
	}
	defer c.global.Release(1)

	raw, err := c.github.GetRawContent(ctx, token, m.Owner, m.Repo, sha, p, size)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", p, err)
	}
	if !utf8.Valid(raw) || slices.Contains(raw, 0) {
		return "", errNotText
	}
	return string(raw), nil
}

// skip logs one skipped skill or file with the reason.
func (c *Crawler) skip(ctx context.Context, m *Marketplace, p, reason string) {
	c.logger.Warn(ctx, "skipping skill marketplace item",
		logging.Field{Key: "tenant", Value: m.TenantID},
		logging.Field{Key: "marketplace", Value: m.Slug},
		logging.Field{Key: "path", Value: p},
		logging.Field{Key: "reason", Value: reason},
	)
}
