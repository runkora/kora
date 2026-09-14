package main

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

var (
	// ErrDuplicateMigration reports the same filename or version prefix in two
	// services. goose keys its version table by prefix, so a collision would
	// silently drop one service's migration.
	ErrDuplicateMigration = errors.New("duplicate migration across services")

	// ErrMalformedMigration reports a filename that is not <version>_<slug>.sql.
	ErrMalformedMigration = errors.New("migration filename is not <version>_<slug>.sql")

	migrationName = regexp.MustCompile(`^(\d+)_[a-z0-9_]+\.sql$`)
)

// mergedFS serves every service's migration files from one root directory,
// which is how goose reads a migration set. Each name maps to the embedded FS
// that owns it.
type mergedFS struct {
	files map[string]fs.FS
	names []string
}

// mergeMigrationFS merges the named service filesystems, failing on a
// duplicate filename or version prefix.
func mergeMigrationFS(sources map[string]fs.FS) (*mergedFS, error) {
	m := &mergedFS{files: map[string]fs.FS{}}
	owners := map[string]string{}
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
				return nil, fmt.Errorf("%s/%s: %w", service, name, ErrMalformedMigration)
			}
			version, _ := strconv.ParseInt(match[1], 10, 64)
			if other, ok := owners[name]; ok {
				return nil, fmt.Errorf("%s and %s both carry %s: %w", other, service, name, ErrDuplicateMigration)
			}
			if other, ok := versions[version]; ok {
				return nil, fmt.Errorf("%s and %s both carry version %d: %w", other, service, version, ErrDuplicateMigration)
			}
			owners[name] = service
			versions[version] = service
			m.files[name] = sources[service]
			m.names = append(m.names, name)
		}
	}
	sort.Strings(m.names)
	return m, nil
}

// Open serves a migration file from the service FS that owns it.
func (m *mergedFS) Open(name string) (fs.File, error) {
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
