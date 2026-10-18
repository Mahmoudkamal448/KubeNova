package clusters

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mahmoudkamal448/kubenova/backend/internal/store"
)

func kubeconfig(ctx, server string) []byte {
	return []byte(strings.NewReplacer("CTX", ctx, "SERVER", server).Replace(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: "SERVER"}
users:
- name: u
  user: {token: t}
contexts:
- name: CTX
  context: {cluster: c, user: u}
current-context: CTX
`))
}

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("USERPROFILE", filepath.Join(dir, "home"))
	st, _ := store.New(filepath.Join(dir, "data"))
	m, err := NewManager(st)
	if err != nil {
		t.Fatal(err)
	}
	return m, dir
}

func waitFor(t *testing.T, ch <-chan struct{}, cond func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case <-ch:
		case <-time.After(100 * time.Millisecond):
		case <-deadline:
			t.Fatal("timed out waiting for sync")
		}
	}
}

func TestSyncedFolder(t *testing.T) {
	m, dir := newTestManager(t)
	root := filepath.Join(dir, "kubeconfigs")
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "prod"), 0o700))
	must(os.MkdirAll(filepath.Join(root, ".git"), 0o700))
	must(os.WriteFile(filepath.Join(root, "dev.yaml"), kubeconfig("dev", "https://dev:6443"), 0o600))
	must(os.WriteFile(filepath.Join(root, "prod", "eu.yaml"), kubeconfig("prod-eu", "https://eu:6443"), 0o600))
	must(os.WriteFile(filepath.Join(root, "README.md"), []byte("# notes"), 0o600))
	must(os.WriteFile(filepath.Join(root, "values.yaml"), []byte("replicas: 3\n"), 0o600))
	must(os.WriteFile(filepath.Join(root, "broken.yaml"), []byte("clusters: [\n"), 0o600))
	must(os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[core]"), 0o600))

	// Non-recursive first: only the top-level kubeconfig.
	must(m.AddSyncDir(SyncDir{Path: root}))
	if n := len(m.List("")); n != 1 {
		t.Fatalf("non-recursive: want 1 cluster, got %d", n)
	}
	// Recursive with per-folder groups.
	must(m.AddSyncDir(SyncDir{Path: root, Recursive: true, Group: "@folder"}))
	if n := len(m.List("")); n != 2 {
		t.Fatalf("recursive: want 2 clusters, got %d", n)
	}
	if g := m.List("prod-eu")[0].Group; g != "prod" {
		t.Fatalf("prod-eu group = %q, want prod", g)
	}
	if g := m.List("dev")[0].Group; g != "kubeconfigs" {
		t.Fatalf("dev group = %q, want kubeconfigs", g)
	}
	st := m.Status()
	if len(st.Dirs) != 1 || st.Dirs[0].Clusters != 2 || len(st.Dirs[0].Errors) != 1 || !strings.HasSuffix(st.Dirs[0].Errors[0].Path, "broken.yaml") {
		t.Fatalf("unexpected status: %+v", st.Dirs)
	}
	// Files from a synced folder can't be removed one by one.
	if err := m.RemoveSource(filepath.Join(root, "dev.yaml")); err == nil {
		t.Fatal("expected error removing a synced file")
	}

	m.SyncInterval = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, unsub := m.Subscribe()
	defer unsub()
	m.StartSync(ctx)

	// Added file appears.
	must(os.WriteFile(filepath.Join(root, "prod", "us.yaml"), kubeconfig("prod-us", "https://us:6443"), 0o600))
	waitFor(t, ch, func() bool { return len(m.List("prod-us")) == 1 })

	// Changed file is reloaded (new server address).
	time.Sleep(20 * time.Millisecond) // make sure the mtime moves on coarse filesystems
	must(os.WriteFile(filepath.Join(root, "dev.yaml"), kubeconfig("dev", "https://dev-new:6443"), 0o600))
	future := time.Now().Add(2 * time.Second)
	must(os.Chtimes(filepath.Join(root, "dev.yaml"), future, future))
	waitFor(t, ch, func() bool { c := m.List("dev"); return len(c) == 1 && c[0].Server == "https://dev-new:6443" })

	// Deleted file disappears.
	must(os.Remove(filepath.Join(root, "prod", "eu.yaml")))
	waitFor(t, ch, func() bool { return len(m.List("prod-eu")) == 0 })

	// User-set group beats the folder group and survives reloads.
	id := m.List("prod-us")[0].ID
	must(m.UpdateMeta(id, Meta{Group: "critical"}))
	if g := m.List("prod-us")[0].Group; g != "critical" {
		t.Fatalf("user group not kept: %q", g)
	}

	// Stop syncing: clusters go away, files stay.
	must(m.RemoveSyncDir(root))
	if n := len(m.List("")); n != 0 {
		t.Fatalf("want 0 clusters after unsync, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(root, "dev.yaml")); err != nil {
		t.Fatal("unsync must not delete files")
	}
}

func TestChangedKubeconfigDropsCachedClient(t *testing.T) {
	m, dir := newTestManager(t)
	p := filepath.Join(dir, "one.yaml")
	if err := os.WriteFile(p, kubeconfig("one", "https://a:6443"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.AddSource(p); err != nil {
		t.Fatal(err)
	}
	id := m.List("")[0].ID
	if _, err := m.Typed(id); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.clients[id]; !ok {
		t.Fatal("client not cached")
	}
	m.Reload() // unchanged file keeps the client
	if _, ok := m.clients[id]; !ok {
		t.Fatal("client dropped although kubeconfig is unchanged")
	}
	_ = os.WriteFile(p, kubeconfig("one", "https://b:6443"), 0o600)
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, future, future)
	m.Reload()
	if _, ok := m.clients[id]; ok {
		t.Fatal("stale client kept after kubeconfig changed")
	}
	cfg, _ := m.Config(id)
	if cfg.Host != "https://b:6443" {
		t.Fatalf("new client uses %s", cfg.Host)
	}
}
