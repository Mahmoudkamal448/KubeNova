package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/ai"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/gitops"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/helm"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/metrics"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/plugins"
)

func (s *Server) monitoringRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/clusters/{id}/metrics/overview", func(w http.ResponseWriter, r *http.Request) {
		o, err := s.Metrics.ClusterOverview(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, o)
	})
	mux.HandleFunc("GET /api/clusters/{id}/metrics/pods", func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Metrics.PodUsage(r.Context(), r.PathValue("id"), q(r, "namespace"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, u)
	})
	mux.HandleFunc("GET /api/clusters/{id}/metrics/nodes", func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Metrics.NodeUsage(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, u)
	})
	mux.HandleFunc("GET /api/metrics/queries", func(w http.ResponseWriter, r *http.Request) { ok(w, metrics.Queries) })
	mux.HandleFunc("POST /api/clusters/{id}/metrics/query", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Query string `json:"query"`
			Range int64  `json:"rangeSeconds"`
			Step  int64  `json:"stepSeconds"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		var start, end time.Time
		step := time.Duration(b.Step) * time.Second
		if b.Range > 0 {
			end = time.Now()
			start = end.Add(-time.Duration(b.Range) * time.Second)
			if step == 0 {
				step = time.Duration(max(b.Range/120, 15)) * time.Second
			}
		}
		res, err := s.Metrics.PromQuery(r.Context(), r.PathValue("id"), b.Query, start, end, step)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(res)
	})
	mux.HandleFunc("POST /api/clusters/{id}/loki/query", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Query string `json:"query"`
			Range int64  `json:"rangeSeconds"`
			Limit int    `json:"limit"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if b.Range == 0 {
			b.Range = 3600
		}
		if b.Limit == 0 {
			b.Limit = 500
		}
		res, err := s.Metrics.LokiQuery(r.Context(), r.PathValue("id"), b.Query, time.Now().Add(-time.Duration(b.Range)*time.Second), time.Now(), b.Limit)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(res)
	})
	mux.HandleFunc("GET /api/clusters/{id}/alerts", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.Metrics.Alerts(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(res)
	})
	mux.HandleFunc("GET /api/clusters/{id}/traces/{trace}", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.Metrics.Trace(r.Context(), r.PathValue("id"), r.PathValue("trace"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(res)
	})
	mux.HandleFunc("GET /api/clusters/{id}/observability", func(w http.ResponseWriter, r *http.Request) { ok(w, s.Metrics.Endpoints(r.PathValue("id"))) })
	mux.HandleFunc("PUT /api/clusters/{id}/observability", func(w http.ResponseWriter, r *http.Request) {
		var e metrics.Endpoint
		if err := decode(r, &e); err != nil {
			fail(w, err)
			return
		}
		if err := s.Metrics.SetEndpoint(r.PathValue("id"), e); err != nil {
			fail(w, err)
			return
		}
		ok(w, s.Metrics.Endpoints(r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/clusters/{id}/observability/detect", func(w http.ResponseWriter, r *http.Request) {
		m, err := s.Metrics.AutoDetect(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, m)
	})
	// Grafana: reverse proxy through the API server so dashboards can be embedded.
	mux.HandleFunc("GET /api/clusters/{id}/grafana/{path...}", func(w http.ResponseWriter, r *http.Request) {
		b, err := s.Metrics.Do(r.Context(), r.PathValue("id"), metrics.Grafana, "/"+r.PathValue("path"), r.URL.Query())
		if err != nil {
			fail(w, err)
			return
		}
		_, _ = w.Write(b)
	})
	mux.HandleFunc("GET /api/dashboards", func(w http.ResponseWriter, r *http.Request) {
		out := append([]plugins.Dashboard{}, s.Settings.Get().Dashboards...)
		if ps, err := s.Plugins.List(); err == nil {
			for _, p := range ps {
				if p.Enabled {
					out = append(out, p.Dashboards...)
				}
			}
		}
		ok(w, out)
	})
}

func (s *Server) helmGitopsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/clusters/{id}/helm/releases", func(w http.ResponseWriter, r *http.Request) {
		rs, err := s.Helm.Releases(r.Context(), r.PathValue("id"), q(r, "namespace"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, rs)
	})
	mux.HandleFunc("GET /api/clusters/{id}/helm/releases/{ns}/{name}/history", func(w http.ResponseWriter, r *http.Request) {
		h, err := s.Helm.History(r.Context(), r.PathValue("id"), r.PathValue("ns"), r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, h)
	})
	mux.HandleFunc("GET /api/clusters/{id}/helm/releases/{ns}/{name}/{what}", func(w http.ResponseWriter, r *http.Request) {
		what := r.PathValue("what")
		if what != "values" && what != "manifest" && what != "notes" && what != "hooks" {
			fail(w, errInvalid("what"))
			return
		}
		rev, _ := strconv.Atoi(q(r, "revision"))
		out, err := s.Helm.Get(r.Context(), r.PathValue("id"), r.PathValue("ns"), r.PathValue("name"), what, rev)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"content": out})
	})
	mux.HandleFunc("POST /api/clusters/{id}/helm/upgrade", func(w http.ResponseWriter, r *http.Request) {
		var b helm.InstallRequest
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()
		out, err := s.Helm.Upgrade(ctx, r.PathValue("id"), b)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"output": out})
	})
	mux.HandleFunc("POST /api/clusters/{id}/helm/diff", func(w http.ResponseWriter, r *http.Request) {
		var b helm.InstallRequest
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		out, err := s.Helm.Diff(r.Context(), r.PathValue("id"), b)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"diff": out})
	})
	mux.HandleFunc("POST /api/clusters/{id}/helm/releases/{ns}/{name}/rollback", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Revision int }
		_ = decode(r, &b)
		out, err := s.Helm.Rollback(r.Context(), r.PathValue("id"), r.PathValue("ns"), r.PathValue("name"), b.Revision)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"output": out})
	})
	mux.HandleFunc("DELETE /api/clusters/{id}/helm/releases/{ns}/{name}", func(w http.ResponseWriter, r *http.Request) {
		out, err := s.Helm.Uninstall(r.Context(), r.PathValue("id"), r.PathValue("ns"), r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"output": out})
	})
	mux.HandleFunc("GET /api/helm/repos", func(w http.ResponseWriter, r *http.Request) {
		rs, _ := s.Helm.Repos(r.Context())
		ok(w, rs)
	})
	mux.HandleFunc("POST /api/helm/repos", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Name, URL, Username, Password string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.Helm.AddRepo(r.Context(), b.Name, b.URL, b.Username, b.Password); err != nil {
			fail(w, err)
			return
		}
		rs, _ := s.Helm.Repos(r.Context())
		ok(w, rs)
	})
	mux.HandleFunc("DELETE /api/helm/repos/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Helm.RemoveRepo(r.Context(), r.PathValue("name")); err != nil {
			fail(w, err)
			return
		}
		rs, _ := s.Helm.Repos(r.Context())
		ok(w, rs)
	})
	mux.HandleFunc("POST /api/helm/repos/update", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Helm.UpdateRepos(r.Context()); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/helm/registry/login", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Host, Username, Password string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.Helm.RegistryLogin(r.Context(), b.Host, b.Username, b.Password); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/helm/search", func(w http.ResponseWriter, r *http.Request) {
		cs, err := s.Helm.Search(r.Context(), q(r, "q"), qb(r, "versions"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, cs)
	})
	mux.HandleFunc("GET /api/helm/chart", func(w http.ResponseWriter, r *http.Request) {
		var out string
		var err error
		if q(r, "what") == "all" {
			out, err = s.Helm.ShowChart(r.Context(), q(r, "chart"), q(r, "version"))
		} else {
			out, err = s.Helm.ShowValues(r.Context(), q(r, "chart"), q(r, "version"))
		}
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"content": out})
	})
	mux.HandleFunc("GET /api/clusters/{id}/gitops/apps", func(w http.ResponseWriter, r *http.Request) {
		apps, err := s.GitOps.Apps(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		if apps == nil {
			apps = []gitops.App{}
		}
		ok(w, apps)
	})
	mux.HandleFunc("POST /api/clusters/{id}/gitops/action", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			App    gitops.App `json:"app"`
			Action string     `json:"action"`
			Arg    string     `json:"arg"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.GitOps.Action(r.Context(), r.PathValue("id"), b.App, b.Action, b.Arg); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
}

type errInvalid string

func (e errInvalid) Error() string { return "invalid " + string(e) }

func (s *Server) securityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/clusters/{id}/security/rbac", func(w http.ResponseWriter, r *http.Request) {
		g, err := s.Security.RBACMatrix(r.Context(), r.PathValue("id"), q(r, "subject"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, g)
	})
	mux.HandleFunc("GET /api/clusters/{id}/security/access", func(w http.ResponseWriter, r *http.Request) {
		m, err := s.Security.AccessMatrix(r.Context(), r.PathValue("id"), q(r, "namespace"), q(r, "user"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, m)
	})
	mux.HandleFunc("POST /api/clusters/{id}/security/can-i", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Verb, Group, Resource, Namespace, User string
			Groups                                 []string
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		allowed, reason, err := s.Security.CanI(r.Context(), r.PathValue("id"), b.Verb, b.Group, b.Resource, b.Namespace, b.User, b.Groups)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]any{"allowed": allowed, "reason": reason})
	})
	mux.HandleFunc("GET /api/clusters/{id}/secrets/{ns}/{name}", func(w http.ResponseWriter, r *http.Request) {
		d, err := s.Security.DecodeSecret(r.Context(), r.PathValue("id"), r.PathValue("ns"), r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, d)
	})
	mux.HandleFunc("PUT /api/clusters/{id}/secrets/{ns}/{name}", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Data   map[string]string `json:"data"`
			Binary map[string]bool   `json:"binary"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.Security.UpdateSecret(r.Context(), r.PathValue("id"), r.PathValue("ns"), r.PathValue("name"), b.Data, b.Binary); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/clusters/{id}/security/certificates", func(w http.ResponseWriter, r *http.Request) {
		c, err := s.Security.Certificates(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, c)
	})
	mux.HandleFunc("GET /api/clusters/{id}/security/policies", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Security.Policies(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, p)
	})
	mux.HandleFunc("GET /api/clusters/{id}/security/images", func(w http.ResponseWriter, r *http.Request) {
		m, err := s.Security.Images(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, m)
	})
	mux.HandleFunc("POST /api/security/scan-image", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Image string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		res, err := s.Security.ScanImage(ctx, b.Image)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(res)
	})
	mux.HandleFunc("POST /api/clusters/{id}/security/scan", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Scanner, Framework string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()
		res, err := s.Security.ScanCluster(ctx, r.PathValue("id"), b.Scanner, b.Framework)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(res)
	})
}

// aiTools assembles built-in, MCP and plugin tools for a cluster.
func (s *Server) aiTools(clusterID, ns string) []ai.Tool {
	tools := ai.KubernetesTools(ai.Services{Resources: s.Res, Pods: s.Pods, Metrics: s.Metrics, Helm: s.Helm, GitOps: s.GitOps}, clusterID, ns)
	for _, c := range s.MCP.Clients() {
		for _, t := range c.Tools {
			client, tool := c, t
			var schema struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			}
			_ = json.Unmarshal(tool.InputSchema, &schema)
			if schema.Properties == nil {
				schema.Properties = map[string]any{}
			}
			tools = append(tools, ai.Tool{
				Name: sanitizeToolName("mcp__" + client.Config.Name + "__" + tool.Name), Description: tool.Description,
				Properties: schema.Properties, Required: schema.Required,
				Run: func(ctx context.Context, in json.RawMessage) (string, error) {
					out, _, err := client.CallTool(ctx, tool.Name, in)
					return out, err
				},
			})
		}
	}
	if ps, err := s.Plugins.List(); err == nil {
		kc, _ := s.Clusters.KubeconfigFor(clusterID)
		for _, p := range ps {
			if !p.Enabled {
				continue
			}
			for _, t := range p.AITools {
				tool, dir := t, p.Dir
				tools = append(tools, ai.Tool{Name: sanitizeToolName(p.ID + "__" + tool.Name), Description: tool.Description, Properties: tool.Properties, Required: tool.Required, Write: tool.Write,
					Run: func(ctx context.Context, in json.RawMessage) (string, error) {
						return plugins.RunAITool(ctx, tool, dir, kc, in)
					}})
			}
		}
	}
	return tools
}

func sanitizeToolName(n string) string {
	n = strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, n)
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

func (s *Server) aiRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ai/tasks", func(w http.ResponseWriter, r *http.Request) { ok(w, ai.Tasks) })
	mux.HandleFunc("POST /api/ai/ask", func(w http.ResponseWriter, r *http.Request) {
		var b ai.Request
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()
		a, err := s.AI.Ask(ctx, b, nil)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, a)
	})
	// Streaming variant: progress (tool calls) then the final answer.
	mux.HandleFunc("GET /api/ai/stream", func(w http.ResponseWriter, r *http.Request) {
		conn, err := s.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var b ai.Request
		if err := conn.ReadJSON(&b); err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()
		a, err := s.AI.Ask(ctx, b, func(st ai.Step) { _ = conn.WriteJSON(map[string]any{"step": st}) })
		if err != nil {
			_ = conn.WriteJSON(map[string]string{"error": err.Error()})
			return
		}
		_ = conn.WriteJSON(map[string]any{"answer": a})
	})
	mux.HandleFunc("GET /api/mcp", func(w http.ResponseWriter, r *http.Request) { ok(w, s.MCP.Status()) })
	mux.HandleFunc("POST /api/mcp/sync", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		s.MCP.Sync(ctx, s.Settings.Get().MCPServers)
		ok(w, s.MCP.Status())
	})
}
