package inferenceproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Provider is an entry plus the directory its credential keys resolve in.
type Provider struct {
	Entry
	secretDir string
}

// Secret reads one credential value by its aggregate key. An empty key returns
// an empty value; the entry declared no credential for that field.
func (p Provider) Secret(key string) (string, error) {
	if key == "" {
		return "", nil
	}
	if strings.ContainsAny(key, "/\\") || strings.Contains(key, "..") {
		return "", fmt.Errorf("provider %q: invalid secret key %q", p.Name, key)
	}
	raw, err := os.ReadFile(filepath.Join(p.secretDir, key))
	if err != nil {
		return "", fmt.Errorf("provider %q: read secret %q: %w", p.Name, key, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// Load reads providers.json from configDir. An absent file or directory yields
// no providers; the aggregate is mounted optional and may not exist yet.
func Load(configDir, secretDir string) ([]Provider, error) {
	if configDir == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(filepath.Join(configDir, ConfigMapKey))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ConfigMapKey, err)
	}
	var entries []Entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ConfigMapKey, err)
	}
	out := make([]Provider, 0, len(entries))
	for _, e := range entries {
		out = append(out, Provider{Entry: e, secretDir: secretDir})
	}
	return out, nil
}

// Watch calls onChange after any change in dirs, debounced by 200ms. Kubernetes
// swaps a mounted volume's ..data symlink atomically. One update arrives as a
// burst of events. It returns when ctx ends.
func Watch(ctx context.Context, logger logging.Logger, dirs []string, onChange func(context.Context)) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Error(ctx, "inference provider watch disabled", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	defer func() { _ = watcher.Close() }()

	watched := 0
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := watcher.Add(dir); err != nil {
			logger.Warn(ctx, "inference provider directory not watched",
				logging.Field{Key: "path", Value: dir},
				logging.Field{Key: "error", Value: err.Error()},
			)
			continue
		}
		watched++
	}
	if watched == 0 {
		return
	}

	const debounceDelay = 200 * time.Millisecond
	var debounce *time.Timer
	for {
		select {
		case <-ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return
		case _, ok := <-watcher.Events:
			if !ok {
				return
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(debounceDelay, func() { onChange(ctx) })
		case werr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Error(ctx, "inference provider watcher error", logging.Field{Key: "error", Value: werr.Error()})
		}
	}
}
