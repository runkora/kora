//go:build linux

package mount

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
)

// ledger records the mounts the node agent made, by mount point and NFS source.
// It lives in a kora-owned file under the kubelet's plugin directory, which no
// pod can write, and survives agent restarts. The agent unmounts only what the
// ledger lists and what mountinfo still shows with the same source.
type ledger struct {
	file    string
	entries map[string]string
}

// loadLedger reads the ledger file. A missing file is an empty ledger.
func loadLedger(file string) (*ledger, error) {
	l := &ledger{file: file, entries: map[string]string{}}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read mount ledger: %w", err)
	}
	if err := json.Unmarshal(data, &l.entries); err != nil {
		return nil, fmt.Errorf("parse mount ledger: %w", err)
	}
	return l, nil
}

// save writes the ledger through a temporary file and a rename.
func (l *ledger) save() error {
	if err := os.MkdirAll(filepath.Dir(l.file), 0o700); err != nil {
		return fmt.Errorf("create mount ledger directory: %w", err)
	}
	data, err := json.Marshal(l.entries)
	if err != nil {
		return err
	}
	tmp := l.file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write mount ledger: %w", err)
	}
	return os.Rename(tmp, l.file)
}

// sources returns the NFS sources of the recorded mounts.
func (l *ledger) sources() map[string]bool {
	out := make(map[string]bool, len(l.entries))
	for _, s := range l.entries {
		out[s] = true
	}
	return out
}

// snapshot returns a copy of the recorded mounts.
func (l *ledger) snapshot() map[string]string { return maps.Clone(l.entries) }
