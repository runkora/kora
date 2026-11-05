package skills

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	skillFileName = "SKILL.md"
	// perCrawlFetches bounds concurrent SKILL.md fetches of one crawl.
	perCrawlFetches = 10
	// globalFetches bounds concurrent file fetches across the process.
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
// commit and fetches every SKILL.md. Skills come back in path order.
func (c *Crawler) Crawl(ctx context.Context, m *Marketplace) (skillList, error) {
	token, err := m.Token(ctx)
	if err != nil {
		return skillList{}, err
	}
	sha, err := c.resolve(ctx, token, m)
	if err != nil {
		return skillList{}, err
	}
	tree, err := c.github.GetTree(ctx, token, m.Owner, m.Repo, sha)
	if err != nil {
		return skillList{}, fmt.Errorf("read tree of %s/%s@%s: %w", m.Owner, m.Repo, sha, err)
	}

	folders := c.skillFolders(ctx, m, tree)
	skills, unread, err := c.readSkills(ctx, token, m, sha, folders)
	if err != nil {
		return skillList{}, fmt.Errorf("fetch skills of %s/%s: %w", m.Owner, m.Repo, err)
	}
	// Skills were found but none could be read: a failed crawl, not an empty
	// Marketplace.
	if len(folders) > 0 && len(skills) == 0 {
		return skillList{}, fmt.Errorf("%s/%s: %w (%d found)", m.Owner, m.Repo, errNoSkillsRead, len(folders))
	}
	return skillList{
		Source: m.source(), SHA: sha, SyncedAt: time.Now().UTC().Truncate(time.Second),
		Limits: c.limits, Partial: unread, Skills: skills,
	}, nil
}

// Reusable reports whether a list of an unchanged commit can be kept: it is
// complete and was crawled with the current limits.
func (c *Crawler) Reusable(list skillList) bool {
	return !list.Partial && list.Limits == c.limits
}

// readSkills fetches and parses every folder's SKILL.md and returns the
// skills in path order. A skill that cannot be read is skipped and logged, and
// unread reports a SKILL.md whose download failed in a way that may pass. An
// outage aborts.
func (c *Crawler) readSkills(ctx context.Context, token string, m *Marketplace, sha string, folders []skillFolder) (skills []skillRecord, unread bool, err error) {
	var failed atomic.Bool
	records := make([]skillRecord, len(folders))
	found := make([]bool, len(folders))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(perCrawlFetches)
	for i, f := range folders {
		g.Go(func() error {
			raw, err := c.fetch(gctx, token, m, sha, path.Join(f.path, skillFileName), f.size)
			if err != nil {
				// An outage or a refused credential fails the whole crawl.
				if errors.Is(err, ghclient.ErrUnavailable) || errors.Is(err, ghclient.ErrUnauthorized) || gctx.Err() != nil {
					return err
				}
				// Non-text or oversized content is the same on every crawl.
				if !errors.Is(err, errNotText) && !errors.Is(err, ghclient.ErrTooLarge) {
					failed.Store(true)
				}
				c.skip(gctx, m, f.path, err.Error())
				return nil
			}
			fm, err := parseSkillFrontmatter(raw)
			if err != nil {
				c.skip(gctx, m, f.path, err.Error())
				return nil
			}
			records[i] = skillRecord{
				Path: f.path, Name: fm.Name, Title: fm.Title, Description: fm.Description,
				Version: fm.Version, Tags: fm.Tags, Tools: fm.Tools, ManualOnly: fm.ManualOnly,
				Content: raw, BodyStart: fm.BodyStart, Files: f.files,
			}
			found[i] = true
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, false, err
	}
	for i, r := range records {
		if found[i] {
			skills = append(skills, r)
		}
	}
	return skills, failed.Load(), nil
}

// errNoSkillsRead is a crawl that found skills and could read none of them.
var errNoSkillsRead = errors.New("no skill could be read")

// ResolveCommit returns the commit the Marketplace ref points at.
func (c *Crawler) ResolveCommit(ctx context.Context, m *Marketplace) (string, error) {
	token, err := m.Token(ctx)
	if err != nil {
		return "", err
	}
	return c.resolve(ctx, token, m)
}

func (c *Crawler) resolve(ctx context.Context, token string, m *Marketplace) (string, error) {
	ref := m.Ref
	if ref == "" {
		ref = "HEAD"
	}
	sha, err := c.github.ResolveRef(ctx, token, m.Owner, m.Repo, ref)
	if err != nil {
		return "", fmt.Errorf("resolve %s/%s@%s: %w", m.Owner, m.Repo, ref, err)
	}
	return sha, nil
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
	kept := make(map[string]*skillFolder, len(folders))
	for _, f := range folders {
		kept[f.path] = f
	}
	// Every folder with a SKILL.md owns the files below it, kept or not, so a
	// skipped nested skill's files never reach its parent.
	skillDirs := map[string]bool{}
	for _, e := range tree {
		if e.Type == "blob" && path.Base(e.Path) == skillFileName {
			skillDirs[path.Dir(e.Path)] = true
		}
	}
	for _, e := range tree {
		if e.Type != "blob" || e.Mode == ghclient.SymlinkMode || path.Base(e.Path) == skillFileName || !isTextPath(e.Path) {
			continue
		}
		owner := kept[deepestSkill(e.Path, skillDirs)]
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
		case e.Mode == ghclient.SymlinkMode:
			c.skip(ctx, m, e.Path, "SKILL.md is a symbolic link")
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

// deepestSkill returns the nearest folder above p that holds a SKILL.md, or
// "".
func deepestSkill(p string, skillDirs map[string]bool) string {
	for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
		if skillDirs[dir] {
			return dir
		}
	}
	return ""
}

// FetchFile fetches one supporting file of skill at sha. A file that turns
// out not to be text, or longer than its tree size, returns ErrFileNotFound.
func (c *Crawler) FetchFile(ctx context.Context, m *Marketplace, sha string, skill skillRecord, file SkillFileInfo) (string, error) {
	token, err := m.Token(ctx)
	if err != nil {
		return "", err
	}
	content, err := c.fetch(ctx, token, m, sha, path.Join(skill.Path, file.Path), file.Size)
	if errors.Is(err, errNotText) || errors.Is(err, ghclient.ErrTooLarge) {
		return "", fmt.Errorf("%s: %w", file.Path, ErrFileNotFound)
	}
	return content, err
}

// retryDelay is the pause before the one retry of a file fetch.
const retryDelay = 250 * time.Millisecond

func retryable(err error) bool {
	for _, final := range []error{ghclient.ErrNotFound, ghclient.ErrTooLarge, ghclient.ErrRateLimited, ghclient.ErrUnauthorized} {
		if errors.Is(err, final) {
			return false
		}
	}
	return true
}

// errNotText is a fetched file that is not UTF-8 text.
var errNotText = errors.New("file is not UTF-8 text")

// fetch downloads one text file.
func (c *Crawler) fetch(ctx context.Context, token string, m *Marketplace, sha, p string, size int64) (string, error) {
	if err := c.global.Acquire(ctx, 1); err != nil {
		return "", err
	}
	defer c.global.Release(1)

	// A read never exceeds the tree's size, so the SKILL.md budget counts what
	// is read, and never exceeds the configured cap.
	limit := size
	if c.limits.BytesPerFile > 0 {
		limit = min(size, c.limits.BytesPerFile)
	}
	raw, err := c.github.GetRawContent(ctx, token, m.Owner, m.Repo, sha, p, limit)
	// One retry for a failure that may pass. A missing file, a file over its
	// bound, a refused credential or a rate limit gives the same answer again.
	if err != nil && retryable(err) && ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-time.After(retryDelay):
			raw, err = c.github.GetRawContent(ctx, token, m.Owner, m.Repo, sha, p, limit)
		}
	}
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
