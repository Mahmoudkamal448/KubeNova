package clusters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/store"
)

const kc = `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: "https://%s:6443"}
users:
- name: u
  user: {token: t}
contexts:
- name: %s
  context: {cluster: c, user: u}
current-context: %s
`

func write(t *testing.T, dir, name, ctx string) string {
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(strings.NewReplacer("%s", ctx).Replace(kc)), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMultipleKubeconfigsAndMerge(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KUBECONFIG", write(t, dir, "a.yaml", "alpha")+string(os.PathListSeparator)+write(t, dir, "b.yaml", "beta"))
	t.Setenv("HOME", dir)
	st, _ := store.New(filepath.Join(dir, "data"))
	m, err := NewManager(st)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(m.List("")); n != 2 {
		t.Fatalf("expected 2 clusters, got %d", n)
	}
	if n := len(m.List("beta")); n != 1 {
		t.Fatalf("search failed: %d", n)
	}
	merged := m.Merged()
	if len(merged.Contexts) != 2 {
		t.Fatalf("merge produced %d contexts", len(merged.Contexts))
	}
	// Both files use the same cluster/user names; merge must keep both.
	if len(merged.Clusters) != 2 || len(merged.AuthInfos) != 2 {
		t.Fatalf("name collision not resolved: %d clusters %d users", len(merged.Clusters), len(merged.AuthInfos))
	}
	id := m.List("alpha")[0].ID
	if err := m.UpdateMeta(id, Meta{Group: "prod", Favorite: true, Alias: "Alpha"}); err != nil {
		t.Fatal(err)
	}
	c, _ := m.Get(id)
	if !c.Favorite || c.Group != "prod" || c.Name != "Alpha" {
		t.Fatalf("meta not applied: %+v", c)
	}
	if m.List("")[0].ID != id {
		t.Fatal("favorites should sort first")
	}
	m.Touch(id)
	if len(m.Recent(5)) != 1 {
		t.Fatal("recent not tracked")
	}
	if _, err := m.ImportKubeconfig("pasted", []byte(strings.NewReplacer("%s", "gamma").Replace(kc))); err != nil {
		t.Fatal(err)
	}
	if len(m.List("")) != 3 {
		t.Fatal("import not registered")
	}
	gamma := m.List("gamma")[0]
	if err := m.SetDefaultGroup(gamma.Source, "Rancher: corp"); err != nil {
		t.Fatal(err)
	}
	if g := m.List("gamma")[0].Group; g != "Rancher: corp" {
		t.Fatalf("default group not applied: %q", g)
	}
	// An existing group must not be overwritten.
	_ = m.SetDefaultGroup(m.List("Alpha")[0].Source, "Rancher: corp")
	if g := m.List("Alpha")[0].Group; g != "prod" {
		t.Fatalf("user group overwritten: %q", g)
	}
}
