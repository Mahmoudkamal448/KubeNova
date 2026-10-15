package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/clusters"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/resources"
)

func (s *Server) routes(mux *http.ServeMux) {
	s.clusterRoutes(mux)
	s.resourceRoutes(mux)
	s.podRoutes(mux)
	s.monitoringRoutes(mux)
	s.helmGitopsRoutes(mux)
	s.securityRoutes(mux)
	s.aiRoutes(mux)
	s.rancherCloudRoutes(mux)
	s.enterpriseRoutes(mux)
	s.toolRoutes(mux)
}

func (s *Server) clusterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { ok(w, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]any{"version": Version, "license": s.License.Current(), "user": s.Identity.Current(), "dataDir": s.Store.Dir(), "tools": s.Tools.Status(r.Context())})
	})
	mux.HandleFunc("GET /api/clusters", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]any{"clusters": s.Clusters.List(q(r, "q")), "groups": s.Clusters.Groups(), "recent": s.Clusters.Recent(10)})
	})
	mux.HandleFunc("POST /api/clusters/reload", func(w http.ResponseWriter, r *http.Request) {
		s.Clusters.Reload()
		ok(w, s.Clusters.List(""))
	})
	mux.HandleFunc("POST /api/clusters/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		s.Clusters.CheckAll(ctx, 32)
		ok(w, s.Clusters.List(""))
	})
	mux.HandleFunc("PATCH /api/clusters/{id}", func(w http.ResponseWriter, r *http.Request) {
		var m clusters.Meta
		if err := decode(r, &m); err != nil {
			fail(w, err)
			return
		}
		if err := s.Clusters.UpdateMeta(r.PathValue("id"), m); err != nil {
			fail(w, err)
			return
		}
		c, _ := s.Clusters.Get(r.PathValue("id"))
		ok(w, c)
	})
	mux.HandleFunc("POST /api/clusters/{id}/connect", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		s.Clusters.Touch(id)
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		h, err := s.Clusters.CheckHealth(ctx, id, true)
		if err != nil {
			// Offline: return the cached status so the UI can work from cache.
			if h != nil {
				h.Cached = true
			}
		}
		ok(w, h)
	})
	mux.HandleFunc("GET /api/clusters/{id}/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		h, _ := s.Clusters.CheckHealth(ctx, r.PathValue("id"), qb(r, "summary"))
		ok(w, h)
	})
	mux.HandleFunc("POST /api/clusters/{id}/switch", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Clusters.SwitchContext(r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/clusters/{id}/kubeconfig", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Clusters.KubeconfigFor(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		b, _ := os.ReadFile(p)
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(b)
	})
	// kubeconfig sources
	mux.HandleFunc("GET /api/kubeconfigs", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]any{"sources": s.Clusters.Sources(), "importDir": s.Clusters.ImportDir()})
	})
	mux.HandleFunc("POST /api/kubeconfigs", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Path string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.Clusters.AddSource(b.Path); err != nil {
			fail(w, err)
			return
		}
		ok(w, s.Clusters.Sources())
	})
	mux.HandleFunc("DELETE /api/kubeconfigs", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Clusters.RemoveSource(q(r, "path")); err != nil {
			fail(w, err)
			return
		}
		ok(w, s.Clusters.Sources())
	})
	mux.HandleFunc("POST /api/kubeconfigs/import", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Name, Content string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		p, err := s.Clusters.ImportKubeconfig(b.Name, []byte(b.Content))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"path": p})
	})
	mux.HandleFunc("GET /api/kubeconfigs/merged", func(w http.ResponseWriter, r *http.Request) {
		b, err := s.Clusters.MergedYAML()
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(b)
	})
}

func gvrFrom(r *http.Request) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: group(r.PathValue("group")), Version: r.PathValue("version"), Resource: r.PathValue("resource")}
}

type listResponse struct {
	Rows     []resources.Row `json:"rows"`
	Continue string          `json:"continue,omitempty"`
	RV       string          `json:"resourceVersion"`
	Cached   bool            `json:"cached,omitempty"`
	CachedAt *time.Time      `json:"cachedAt,omitempty"`
	Error    string          `json:"error,omitempty"`
}

func cacheKey(cid string, gvr schema.GroupVersionResource, ns string) string {
	g := gvr.Group
	if g == "" {
		g = "core"
	}
	return "cache/" + cid + "/" + strings.ReplaceAll(g+"_"+gvr.Version+"_"+gvr.Resource+"_"+ns, "/", "_")
}

func (s *Server) resourceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/clusters/{id}/api-resources", func(w http.ResponseWriter, r *http.Request) {
		cid := r.PathValue("id")
		if qb(r, "refresh") {
			s.Clusters.InvalidateDiscovery(cid)
		}
		rs, err := s.Res.Discover(r.Context(), cid)
		if err != nil {
			var cached []resources.APIResource
			if s.Store.Load("cache/"+cid+"/api-resources", &cached) == nil && len(cached) > 0 {
				ok(w, cached)
				return
			}
			fail(w, err)
			return
		}
		_ = s.Store.Save("cache/"+cid+"/api-resources", rs)
		ok(w, rs)
	})
	base := "/api/clusters/{id}/r/{group}/{version}/{resource}"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		cid, gvr := r.PathValue("id"), gvrFrom(r)
		o := resources.ListOptions{Namespace: q(r, "namespace"), LabelSelector: q(r, "labelSelector"), FieldSelector: q(r, "fieldSelector"), Limit: qi(r, "limit", 0), Continue: q(r, "continue")}
		key := cacheKey(cid, gvr, o.Namespace)
		l, err := s.Res.List(r.Context(), cid, gvr, o)
		if err != nil {
			var cached listResponse
			if s.Store.Load(key, &cached) == nil && cached.Rows != nil {
				cached.Cached, cached.Error = true, err.Error()
				ok(w, cached)
				return
			}
			fail(w, err)
			return
		}
		resp := listResponse{Rows: make([]resources.Row, 0, len(l.Items)), Continue: l.GetContinue(), RV: l.GetResourceVersion()}
		for i := range l.Items {
			resp.Rows = append(resp.Rows, resources.Summarize(&l.Items[i]))
		}
		if o.Continue == "" && o.LabelSelector == "" && o.FieldSelector == "" {
			go func() {
				c := resp
				now := time.Now()
				c.CachedAt = &now
				_ = s.Store.Save(key, c)
			}()
		}
		ok(w, resp)
	})
	mux.HandleFunc("GET "+base+"/{name}", func(w http.ResponseWriter, r *http.Request) {
		obj, err := s.Res.Get(r.Context(), r.PathValue("id"), gvrFrom(r), q(r, "namespace"), r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		if q(r, "format") == "yaml" {
			o := obj.DeepCopy()
			if !qb(r, "managedFields") {
				o.SetManagedFields(nil)
			}
			y, _ := resources.ToYAML(o.Object)
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(y))
			return
		}
		ok(w, obj.Object)
	})
	mux.HandleFunc("GET "+base+"/{name}/export", func(w http.ResponseWriter, r *http.Request) {
		y, err := s.Res.Export(r.Context(), r.PathValue("id"), gvrFrom(r), q(r, "namespace"), r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Content-Disposition", "attachment; filename="+r.PathValue("name")+".yaml")
		_, _ = w.Write([]byte(y))
	})
	mux.HandleFunc("DELETE "+base+"/{name}", func(w http.ResponseWriter, r *http.Request) {
		var grace *int64
		if v := q(r, "grace"); v != "" {
			g := qi(r, "grace", 0)
			grace = &g
		}
		if err := s.Res.Delete(r.Context(), r.PathValue("id"), gvrFrom(r), q(r, "namespace"), r.PathValue("name"), q(r, "propagation"), grace); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"deleted": true})
	})
	mux.HandleFunc("PATCH "+base+"/{name}", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Type  string          `json:"type"`
			Patch json.RawMessage `json:"patch"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		obj, err := s.Res.Patch(r.Context(), r.PathValue("id"), gvrFrom(r), q(r, "namespace"), r.PathValue("name"), b.Type, b.Patch)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, obj.Object)
	})
	mux.HandleFunc("POST "+base+"/{name}/scale", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Replicas int32 }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.Res.Scale(r.Context(), r.PathValue("id"), gvrFrom(r), q(r, "namespace"), r.PathValue("name"), b.Replicas); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST "+base+"/{name}/restart", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Res.Restart(r.Context(), r.PathValue("id"), gvrFrom(r), q(r, "namespace"), r.PathValue("name")); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET "+base+"/{name}/history", func(w http.ResponseWriter, r *http.Request) {
		h, err := s.Res.History(r.Context(), r.PathValue("id"), r.PathValue("resource"), q(r, "namespace"), r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, h)
	})
	mux.HandleFunc("POST "+base+"/{name}/rollback", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Revision int64 }
		_ = decode(r, &b)
		if err := s.Res.Rollback(r.Context(), r.PathValue("id"), r.PathValue("resource"), q(r, "namespace"), r.PathValue("name"), b.Revision); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST "+base+"/{name}/clone", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ TargetCluster, TargetNamespace, NewName string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		obj, err := s.Res.Clone(r.Context(), r.PathValue("id"), gvrFrom(r), q(r, "namespace"), r.PathValue("name"), b.TargetCluster, b.TargetNamespace, b.NewName)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"name": obj.GetName(), "namespace": obj.GetNamespace()})
	})
	mux.HandleFunc("POST /api/clusters/{id}/apply", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			YAML string `json:"yaml"`
			resources.ApplyOptions
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		res, err := s.Res.Apply(r.Context(), r.PathValue("id"), b.YAML, b.ApplyOptions)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, res)
	})
	mux.HandleFunc("POST /api/clusters/{id}/create", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			YAML string `json:"yaml"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		res, err := s.Res.Create(r.Context(), r.PathValue("id"), b.YAML)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, res)
	})
	mux.HandleFunc("POST /api/clusters/{id}/diff", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			YAML string `json:"yaml"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		res, err := s.Res.Diff(r.Context(), r.PathValue("id"), b.YAML)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, res)
	})
	mux.HandleFunc("GET /api/clusters/{id}/watch", s.handleWatch)
	mux.HandleFunc("GET /api/clusters/{id}/topology", func(w http.ResponseWriter, r *http.Request) {
		g, err := s.Res.Topology(r.Context(), r.PathValue("id"), q(r, "namespace"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, g)
	})
	mux.HandleFunc("GET /api/clusters/{id}/timeline", func(w http.ResponseWriter, r *http.Request) {
		t, err := s.Res.Timeline(r.Context(), r.PathValue("id"), q(r, "namespace"), q(r, "kind"), q(r, "name"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, t)
	})
	mux.HandleFunc("POST /api/search", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Query    string   `json:"query"`
			Clusters []string `json:"clusters"`
			Kinds    []string `json:"kinds"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if len(b.Clusters) == 0 {
			for _, c := range s.Clusters.List("") {
				if c.Health != nil && c.Health.Status != "unreachable" {
					b.Clusters = append(b.Clusters, c.ID)
				}
			}
		}
		ok(w, s.Res.SearchAcross(r.Context(), b.Clusters, b.Query, b.Kinds))
	})
	mux.HandleFunc("POST /api/compare", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ A, B, Resource, Namespace string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		res, err := s.Res.Compare(r.Context(), b.A, b.B, b.Resource, b.Namespace)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, res)
	})
	mux.HandleFunc("POST /api/diff", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ A, B string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"diff": resources.DiffObjects(b.A, b.B)})
	})
}

type watchEvent struct {
	Type string         `json:"type"`
	Row  *resources.Row `json:"row,omitempty"`
	RV   string         `json:"rv,omitempty"`
	Err  string         `json:"error,omitempty"`
}

// handleWatch streams row-level watch events over a WebSocket. It re-establishes
// the watch from the last seen resourceVersion when the API server closes it.
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	cid := r.PathValue("id")
	gvr := schema.GroupVersionResource{Group: group(q(r, "group")), Version: q(r, "version"), Resource: q(r, "resource")}
	o := resources.ListOptions{Namespace: q(r, "namespace"), LabelSelector: q(r, "labelSelector")}
	rv := q(r, "resourceVersion")
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()
	for ctx.Err() == nil {
		wi, err := s.Res.Watch(ctx, cid, gvr, o, rv)
		if err != nil {
			_ = conn.WriteJSON(watchEvent{Type: "ERROR", Err: err.Error()})
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		for ev := range wi.ResultChan() {
			if ev.Type == watch.Error {
				// Usually "resource version too old": ask the client to relist.
				_ = conn.WriteJSON(watchEvent{Type: "RELIST"})
				rv = ""
				break
			}
			u, okU := ev.Object.(*unstructured.Unstructured)
			if !okU {
				continue
			}
			rv = u.GetResourceVersion()
			if ev.Type == watch.Bookmark {
				continue
			}
			row := resources.Summarize(u)
			if err := conn.WriteJSON(watchEvent{Type: string(ev.Type), Row: &row, RV: rv}); err != nil {
				wi.Stop()
				return
			}
		}
		wi.Stop()
	}
}

var _ = sort.Strings
