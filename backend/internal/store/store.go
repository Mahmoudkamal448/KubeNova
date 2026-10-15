// Package store provides a small, concurrency-safe JSON document store that
// persists application state (settings, cluster metadata, workspaces, audit
// logs, offline cache) under the user's data directory.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Store persists named JSON documents as individual files in a directory.
type Store struct {
	dir string
	mu  sync.Mutex
}

// DefaultDir returns the platform specific application data directory.
func DefaultDir() string {
	if d := os.Getenv("KUBENOVA_DATA_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "KubeNova")
}

// New creates a store rooted at dir, creating the directory when needed.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir returns the root directory of the store.
func (s *Store) Dir() string { return s.dir }

// Path resolves a document name (which may contain sub directories) to a file path.
func (s *Store) Path(name string) string {
	return filepath.Join(s.dir, filepath.FromSlash(name)+".json")
}

// Load decodes the named document into v. A missing document leaves v untouched
// and returns nil.
func (s *Store) Load(name string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.Path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Save atomically writes v as the named document.
func (s *Store) Save(name string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.Path(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Delete removes the named document if it exists.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.Path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
