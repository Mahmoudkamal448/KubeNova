package clusters

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SyncDir is a directory whose kubeconfig files are kept in sync: files that
// appear are added, changed files are reloaded and removed files disappear.
type SyncDir struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	// Group is the default group for clusters from this folder. The special
	// value "@folder" uses the name of the folder that contains each file.
	// Groups set by the user on a cluster always win.
	Group string `json:"group,omitempty"`
}

// SourceStatus describes one kubeconfig file that was scanned.
type SourceStatus struct {
	Path     string    `json:"path"`
	Dir      string    `json:"dir,omitempty"` // synced folder it came from
	Origin   string    `json:"origin"`        // kubeconfig-env | default | synced | file | imported
	Contexts int       `json:"contexts"`
	Error    string    `json:"error,omitempty"`
	ModTime  time.Time `json:"modTime"`
}

// SyncStatus is the state of all kubeconfig sources.
type SyncStatus struct {
	Dirs     []SyncDirStatus `json:"dirs"`
	Files    []SourceStatus  `json:"files"`
	LastScan time.Time       `json:"lastScan"`
	Interval time.Duration   `json:"intervalNs"`
}

// SyncDirStatus is a synced folder with its scan results.
type SyncDirStatus struct {
	SyncDir
	Exists   bool           `json:"exists"`
	Files    int            `json:"files"`
	Clusters int            `json:"clusters"`
	Errors   []SourceStatus `json:"errors,omitempty"`
}

type source struct {
	path   string
	origin string
	dir    *SyncDir
}

type stamp struct {
	size int64
	mod  time.Time
}

const (
	maxKubeconfigSize = 5 << 20
	maxWalkDepth      = 8
)

// candidate reports whether a file in a synced folder should be read as a
// kubeconfig. Obvious non-kubeconfig files are skipped so a folder can also
// hold READMEs, scripts and the like.
func candidate(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~") {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case "", ".yaml", ".yml", ".conf", ".config", ".kubeconfig", ".json":
		return true
	}
	return false
}

func walkDir(d *SyncDir, visit func(path string)) {
	root := filepath.Clean(d.Path)
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			if p == root {
				return nil
			}
			if !d.Recursive || strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" ||
				strings.Count(strings.TrimPrefix(p, root), string(os.PathSeparator)) > maxWalkDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !candidate(e.Name()) {
			return nil
		}
		if info, err := e.Info(); err == nil && info.Size() <= maxKubeconfigSize && info.Mode().IsRegular() || e.Type()&fs.ModeSymlink != 0 {
			visit(p)
		}
		return nil
	})
}

// collectSourcesLocked lists every kubeconfig file from all origins, de-duplicated.
func (m *Manager) collectSourcesLocked() []source {
	seen := map[string]bool{}
	var out []source
	add := func(p, origin string, d *SyncDir) {
		if p == "" {
			return
		}
		key := filepath.Clean(p)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, source{path: key, origin: origin, dir: d})
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		for _, p := range filepath.SplitList(env) {
			add(p, "kubeconfig-env", nil)
		}
	}
	for _, p := range m.homeSources() {
		add(p, "default", nil)
	}
	for i := range m.state.Dirs {
		d := &m.state.Dirs[i]
		walkDir(d, func(p string) { add(p, "synced", d) })
	}
	for _, p := range m.state.Sources {
		add(p, "file", nil)
	}
	if entries, err := os.ReadDir(m.ImportDir()); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				add(filepath.Join(m.ImportDir(), e.Name()), "imported", nil)
			}
		}
	}
	return out
}

func (m *Manager) homeSources() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := []string{filepath.Join(home, ".kube", "config")}
	// ~/.kube/configs is always synced (non-recursive).
	walkDir(&SyncDir{Path: filepath.Join(home, ".kube", "configs")}, func(p string) { out = append(out, p) })
	return out
}

// defaultGroup returns the group a synced folder assigns to a file.
func defaultGroup(d *SyncDir, path string) string {
	if d == nil || d.Group == "" {
		return ""
	}
	if d.Group != "@folder" {
		return d.Group
	}
	parent := filepath.Dir(path)
	if filepath.Clean(parent) == filepath.Clean(d.Path) {
		return filepath.Base(filepath.Clean(d.Path))
	}
	return filepath.Base(parent)
}

func stampsOf(srcs []source) map[string]stamp {
	out := make(map[string]stamp, len(srcs))
	for _, s := range srcs {
		if info, err := os.Stat(s.path); err == nil {
			out[s.path] = stamp{size: info.Size(), mod: info.ModTime()}
		}
	}
	return out
}

func sameStamps(a, b map[string]stamp) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// SyncDirs returns the synced folders.
func (m *Manager) SyncDirs() []SyncDir {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]SyncDir(nil), m.state.Dirs...)
}

// AddSyncDir starts syncing a folder (or updates its options).
func (m *Manager) AddSyncDir(d SyncDir) error {
	d.Path = filepath.Clean(d.Path)
	info, err := os.Stat(d.Path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", d.Path)
	}
	m.mu.Lock()
	replaced := false
	for i := range m.state.Dirs {
		if m.state.Dirs[i].Path == d.Path {
			m.state.Dirs[i] = d
			replaced = true
		}
	}
	if !replaced {
		m.state.Dirs = append(m.state.Dirs, d)
	}
	m.mu.Unlock()
	if err := m.save(); err != nil {
		return err
	}
	m.Reload()
	return nil
}

// RemoveSyncDir stops syncing a folder. Its files are not deleted.
func (m *Manager) RemoveSyncDir(path string) error {
	path = filepath.Clean(path)
	m.mu.Lock()
	var kept []SyncDir
	for _, d := range m.state.Dirs {
		if d.Path != path {
			kept = append(kept, d)
		}
	}
	found := len(kept) != len(m.state.Dirs)
	m.state.Dirs = kept
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("synced folder %s not found", path)
	}
	if err := m.save(); err != nil {
		return err
	}
	m.Reload()
	return nil
}

// Status reports every source file and synced folder.
func (m *Manager) Status() SyncStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := SyncStatus{LastScan: m.lastScan, Interval: m.SyncInterval, Files: []SourceStatus{}, Dirs: []SyncDirStatus{}}
	byDir := map[string]*SyncDirStatus{}
	for _, d := range m.state.Dirs {
		_, err := os.Stat(d.Path)
		ds := &SyncDirStatus{SyncDir: d, Exists: err == nil}
		st.Dirs = append(st.Dirs, *ds)
		byDir[d.Path] = &st.Dirs[len(st.Dirs)-1]
	}
	for _, f := range m.sourceStatus {
		st.Files = append(st.Files, f)
		if ds := byDir[f.Dir]; ds != nil {
			ds.Files++
			ds.Clusters += f.Contexts
			if f.Error != "" {
				ds.Errors = append(ds.Errors, f)
			}
		}
	}
	sort.Slice(st.Files, func(i, j int) bool { return st.Files[i].Path < st.Files[j].Path })
	return st
}

// Subscribe returns a channel that receives a value whenever the cluster list
// changes, and a function to unsubscribe.
func (m *Manager) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	m.subMu.Lock()
	m.subs[ch] = struct{}{}
	m.subMu.Unlock()
	return ch, func() {
		m.subMu.Lock()
		delete(m.subs, ch)
		m.subMu.Unlock()
	}
}

func (m *Manager) notify() {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- struct{}{}:
		default: // a notification is already pending
		}
	}
}

// StartSync rescans all kubeconfig sources every SyncInterval and reloads when
// any file was added, changed or removed. Newly discovered clusters get a
// health check right away. Polling (rather than OS file events) works the same
// on every platform and on network or cloud-synced folders.
func (m *Manager) StartSync(ctx context.Context) {
	go func() {
		for {
			interval := m.SyncInterval
			if interval <= 0 {
				interval = 3 * time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
			m.mu.RLock()
			srcs := m.collectSourcesLocked()
			prev := m.stamps
			m.mu.RUnlock()
			if sameStamps(prev, stampsOf(srcs)) {
				m.mu.Lock()
				m.lastScan = time.Now()
				m.mu.Unlock()
				continue
			}
			m.Reload()
		}
	}()
}

func (m *Manager) checkNew(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, _ = m.CheckHealth(cctx, id, false)
			cancel()
		}(id)
	}
	wg.Wait()
	_ = m.save()
	m.notify()
}
