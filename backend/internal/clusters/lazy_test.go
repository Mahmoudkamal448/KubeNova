package clusters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAPIServer counts every request a cluster receives.
func fakeAPIServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"gitVersion":"v1.31.0"}`))
		case "/api/v1/nodes":
			_, _ = w.Write([]byte(`{"kind":"NodeList","apiVersion":"v1","items":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestOnlyOpenedClustersAreContacted(t *testing.T) {
	for _, mode := range []string{HealthOpened, HealthAll, HealthOff} {
		t.Run(mode, func(t *testing.T) {
			m, dir := newTestManager(t)
			m.HealthMode = mode
			a, hitsA := fakeAPIServer(t)
			b, hitsB := fakeAPIServer(t)
			root := filepath.Join(dir, "kc")
			_ = os.MkdirAll(root, 0o700)
			_ = os.WriteFile(filepath.Join(root, "a.yaml"), kubeconfig("a", a.URL), 0o600)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m.SyncInterval = 30 * time.Millisecond
			m.StartSync(ctx)
			m.StartBackgroundHealth(ctx, 50*time.Millisecond, 4)
			if err := m.AddSyncDir(SyncDir{Path: root}); err != nil {
				t.Fatal(err)
			}
			// A file appearing later must not trigger a connection either.
			_ = os.WriteFile(filepath.Join(root, "b.yaml"), kubeconfig("b", b.URL), 0o600)
			time.Sleep(400 * time.Millisecond)
			if len(m.List("")) != 2 {
				t.Fatalf("want 2 clusters, got %d", len(m.List("")))
			}
			if mode != HealthAll && (hitsA.Load() != 0 || hitsB.Load() != 0) {
				t.Fatalf("unopened clusters were contacted: a=%d b=%d", hitsA.Load(), hitsB.Load())
			}
			if mode == HealthAll && (hitsA.Load() == 0 || hitsB.Load() == 0) {
				t.Fatalf("mode all should check every cluster: a=%d b=%d", hitsA.Load(), hitsB.Load())
			}
			if mode == HealthAll {
				cancel()
				time.Sleep(300 * time.Millisecond) // let in-flight checks finish writing state
				return
			}
			// Opening A: only A is contacted (by the open itself and, in
			// "opened" mode, by the periodic check); B stays untouched.
			idA := m.List("a")[0].ID
			m.Touch(idA)
			if _, err := m.CheckHealth(ctx, idA, false); err != nil {
				t.Fatal(err)
			}
			before := hitsA.Load()
			time.Sleep(300 * time.Millisecond)
			if hitsB.Load() != 0 {
				t.Fatalf("cluster b was contacted without being opened: %d", hitsB.Load())
			}
			if !m.List("a")[0].Connected || m.List("b")[0].Connected {
				t.Fatal("connected flags wrong")
			}
			if mode == HealthOpened && hitsA.Load() == before {
				t.Fatal("opened cluster is not health-checked in the background")
			}
			if mode == HealthOff && hitsA.Load() != before {
				t.Fatal("health mode off still checks in the background")
			}
			m.Disconnect(idA)
			if m.List("a")[0].Connected {
				t.Fatal("disconnect did not clear the connected flag")
			}
		})
	}
}

func TestUnchangedFilesAreNotReparsed(t *testing.T) {
	m, dir := newTestManager(t)
	root := filepath.Join(dir, "kc")
	_ = os.MkdirAll(root, 0o700)
	for _, n := range []string{"a", "b", "c"} {
		_ = os.WriteFile(filepath.Join(root, n+".yaml"), kubeconfig(n, "https://"+n+":6443"), 0o600)
	}
	if err := m.AddSyncDir(SyncDir{Path: root}); err != nil {
		t.Fatal(err)
	}
	cfgBefore := m.raw[filepath.Join(root, "a.yaml")]
	_ = os.WriteFile(filepath.Join(root, "b.yaml"), kubeconfig("b", "https://b2:6443"), 0o600)
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(filepath.Join(root, "b.yaml"), future, future)
	m.Reload()
	if m.raw[filepath.Join(root, "a.yaml")] != cfgBefore {
		t.Fatal("unchanged file was parsed again")
	}
	if m.List("b")[0].Server != "https://b2:6443" {
		t.Fatal("changed file was not reparsed")
	}
}
