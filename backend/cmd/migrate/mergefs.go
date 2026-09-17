package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"time"
)

var (
	// errDuplicateMigration reports the same filename or version prefix in two
	// services. goose rejects such a set; failing here names both services.
	errDuplicateMigration = errors.New("duplicate migration across services")

	// errMalformedMigration reports a filename that is not <version>_<slug>.sql.
	errMalformedMigration = errors.New("migration filename is not <version>_<slug>.sql")

	migrationName = regexp.MustCompile(`^(\d+)_[a-z0-9_]+\.sql$`)
)

// mergedFS serves every service's migration files from one root directory,
// which is how goose reads a migration set.
type mergedFS struct {
	files  map[string]fs.FS  // filename to the embedded FS that holds it
	owners map[string]string // filename to service
	names  []string          // sorted filenames
}

// mergeMigrationFS merges the named service filesystems, failing on a
// duplicate filename or version prefix.
func mergeMigrationFS(sources map[string]fs.FS) (*mergedFS, error) {
	m := &mergedFS{files: map[string]fs.FS{}, owners: map[string]string{}}
	versions := map[int64]string{}

	services := make([]string, 0, len(sources))
	for name := range sources {
		services = append(services, name)
	}
	sort.Strings(services)

	for _, service := range services {
		entries, err := fs.ReadDir(sources[service], ".")
		if err != nil {
			return nil, fmt.Errorf("read %s migrations: %w", service, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || name == "embed.go" {
				continue
			}
			match := migrationName.FindStringSubmatch(name)
			if match == nil {
				return nil, fmt.Errorf("%s/%s: %w", service, name, errMalformedMigration)
			}
			version, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", service, name, errMalformedMigration)
			}
			if other, ok := m.owners[name]; ok {
				return nil, fmt.Errorf("%s and %s both carry %s: %w", other, service, name, errDuplicateMigration)
			}
			if other, ok := versions[version]; ok {
				return nil, fmt.Errorf("%s and %s both carry version %d: %w", other, service, version, errDuplicateMigration)
			}
			m.owners[name] = service
			versions[version] = service
			m.files[name] = sources[service]
			m.names = append(m.names, name)
		}
	}
	sort.Strings(m.names)
	return m, nil
}

// Open serves the root directory or a migration file from the FS that owns it.
func (m *mergedFS) Open(name string) (fs.File, error) {
	if name == "." {
		return &rootDir{fs: m}, nil
	}
	source, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return source.Open(name)
}

// ReadDir lists the root: every merged migration in filename order.
func (m *mergedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries := make([]fs.DirEntry, 0, len(m.names))
	for _, file := range m.names {
		info, err := fs.Stat(m.files[file], file)
		if err != nil {
			return nil, err
		}
		entries = append(entries, fs.FileInfoToDirEntry(info))
	}
	return entries, nil
}

// rootDir is the open root directory; offset tracks successive ReadDir calls.
type rootDir struct {
	fs     *mergedFS
	offset int
}

func (d *rootDir) Stat() (fs.FileInfo, error) { return rootInfo{}, nil }
func (d *rootDir) Read([]byte) (int, error)   { return 0, errors.New("is a directory") }
func (d *rootDir) Close() error               { return nil }

func (d *rootDir) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := d.fs.ReadDir(".")
	if err != nil {
		return nil, err
	}
	rest := entries[d.offset:]
	if n <= 0 {
		d.offset = len(entries)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	rest = rest[:min(n, len(rest))]
	d.offset += len(rest)
	return rest, nil
}

type rootInfo struct{}

func (rootInfo) Name() string       { return "." }
func (rootInfo) Size() int64        { return 0 }
func (rootInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (rootInfo) ModTime() time.Time { return time.Time{} }
func (rootInfo) IsDir() bool        { return true }
func (rootInfo) Sys() any           { return nil }
