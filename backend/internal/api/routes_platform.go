package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/cloud"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/enterprise"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/integrations"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/rancher"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/resources"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/settings"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/terminal"
)

func jsonOut(w http.ResponseWriter, raw json.RawMessage, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	_, _ = w.Write(raw)
}

func (s *Server) rancherCloudRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/rancher/servers", func(w http.ResponseWriter, r *http.Request) { ok(w, s.Rancher.Servers()) })
	mux.HandleFunc("POST /api/rancher/servers", func(w http.ResponseWriter, r *http.Request) {
		var b rancher.Server
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		sv, err := s.Rancher.Upsert(b)
		if err != nil {
			fail(w, err)
			return
		}
		sv.Token = ""
		ok(w, sv)
	})
	mux.HandleFunc("DELETE /api/rancher/servers/{sid}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Rancher.Remove(r.PathValue("sid")); err != nil {
			fail(w, err)
			return
		}
		ok(w, s.Rancher.Servers())
	})
	mux.HandleFunc("POST /api/rancher/servers/{sid}/login", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Provider, Username, Password string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.Rancher.Login(r.Context(), r.PathValue("sid"), b.Provider, b.Username, b.Password); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/rancher/servers/{sid}/sso", func(w http.ResponseWriter, r *http.Request) {
		rid, url, err := s.Rancher.SSOStart(r.PathValue("sid"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"requestId": rid, "url": url})
	})
	mux.HandleFunc("GET /api/rancher/servers/{sid}/sso/{rid}", func(w http.ResponseWriter, r *http.Request) {
		done, err := s.Rancher.SSOPoll(r.Context(), r.PathValue("sid"), r.PathValue("rid"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"done": done})
	})
	mux.HandleFunc("GET /api/rancher/servers/{sid}/clusters", func(w http.ResponseWriter, r *http.Request) {
		cs, err := s.Rancher.Clusters(r.Context(), r.PathValue("sid"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, cs)
	})
	mux.HandleFunc("GET /api/rancher/servers/{sid}/clusters/{cid}/projects", func(w http.ResponseWriter, r *http.Request) {
		ps, err := s.Rancher.Projects(r.Context(), r.PathValue("sid"), r.PathValue("cid"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, ps)
	})
	mux.HandleFunc("GET /api/rancher/servers/{sid}/clusters/{cid}/projects/{pid}/namespaces", func(w http.ResponseWriter, r *http.Request) {
		ns, err := s.Rancher.ProjectNamespaces(r.Context(), r.PathValue("sid"), r.PathValue("cid"), r.PathValue("pid"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, ns)
	})
	mux.HandleFunc("POST /api/rancher/servers/{sid}/import", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Clusters []string }
		_ = decode(r, &b)
		paths, err := s.Rancher.ImportKubeconfigs(r.Context(), r.PathValue("sid"), b.Clusters)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]any{"imported": paths})
	})
	mux.HandleFunc("GET /api/rancher/servers/{sid}/clusters/{cid}/nodes", func(w http.ResponseWriter, r *http.Request) {
		raw, err := s.Rancher.Nodes(r.Context(), r.PathValue("sid"), r.PathValue("cid"))
		jsonOut(w, raw, err)
	})
	mux.HandleFunc("POST /api/rancher/servers/{sid}/nodes/{nid}/{action}", func(w http.ResponseWriter, r *http.Request) {
		a := r.PathValue("action")
		if a != "cordon" && a != "uncordon" && a != "drain" && a != "stopDrain" {
			fail(w, errInvalid("action"))
			return
		}
		if err := s.Rancher.NodeAction(r.Context(), r.PathValue("sid"), r.PathValue("nid"), a); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/rancher/servers/{sid}/clusters/{cid}/apps", func(w http.ResponseWriter, r *http.Request) {
		raw, err := s.Rancher.Apps(r.Context(), r.PathValue("sid"), r.PathValue("cid"))
		jsonOut(w, raw, err)
	})
	mux.HandleFunc("GET /api/rancher/servers/{sid}/fleet", func(w http.ResponseWriter, r *http.Request) {
		repos, err := s.Rancher.FleetRepos(r.Context(), r.PathValue("sid"))
		if err != nil {
			fail(w, err)
			return
		}
		bundles, _ := s.Rancher.FleetBundles(r.Context(), r.PathValue("sid"))
		ok(w, map[string]json.RawMessage{"gitrepos": repos, "bundles": bundles})
	})
	mux.HandleFunc("GET /api/rancher/servers/{sid}/provisioning", func(w http.ResponseWriter, r *http.Request) {
		raw, err := s.Rancher.ProvisioningClusters(r.Context(), r.PathValue("sid"))
		jsonOut(w, raw, err)
	})
	mux.HandleFunc("POST /api/rancher/servers/{sid}/provisioning/{ns}/{name}/upgrade", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Version string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.Rancher.UpgradeCluster(r.Context(), r.PathValue("sid"), r.PathValue("ns"), r.PathValue("name"), b.Version); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/rancher/servers/{sid}/provisioning/{ns}/{name}/pools/{pool}", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Quantity int }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.Rancher.ScaleMachinePool(r.Context(), r.PathValue("sid"), r.PathValue("ns"), r.PathValue("name"), r.PathValue("pool"), b.Quantity); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/rancher/servers/{sid}/register", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Name string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		res, err := s.Rancher.Register(r.Context(), r.PathValue("sid"), b.Name)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, res)
	})
	mux.HandleFunc("POST /api/rancher/servers/{sid}/raw", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Method, Path string
			Body         any
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		raw, err := s.Rancher.Raw(r.Context(), r.PathValue("sid"), strings.ToUpper(b.Method), b.Path, b.Body)
		jsonOut(w, raw, err)
	})

	// Cloud providers.
	mux.HandleFunc("GET /api/cloud/kinds", func(w http.ResponseWriter, r *http.Request) { ok(w, cloud.ResourceKinds()) })
	mux.HandleFunc("POST /api/cloud/{provider}/discover", func(w http.ResponseWriter, r *http.Request) {
		var o cloud.Options
		_ = decode(r, &o)
		cs, err := s.Cloud.Discover(r.Context(), r.PathValue("provider"), o)
		if err != nil {
			fail(w, err)
			return
		}
		if cs == nil {
			cs = []cloud.ManagedCluster{}
		}
		ok(w, cs)
	})
	mux.HandleFunc("POST /api/cloud/{provider}/import", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Cluster cloud.ManagedCluster `json:"cluster"`
			Options cloud.Options        `json:"options"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		p, err := s.Cloud.Import(r.Context(), b.Cluster, b.Options)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"path": p})
	})
	mux.HandleFunc("POST /api/cloud/{provider}/login", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			cloud.Options
			Method string `json:"method"`
		}
		_ = decode(r, &b)
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		out, err := s.Cloud.Login(ctx, r.PathValue("provider"), b.Options, b.Method)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"output": out})
	})
	mux.HandleFunc("GET /api/cloud/{provider}/profiles", func(w http.ResponseWriter, r *http.Request) {
		ps, err := s.Cloud.Profiles(r.Context(), r.PathValue("provider"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, ps)
	})
	mux.HandleFunc("POST /api/cloud/{provider}/resources/{kind}", func(w http.ResponseWriter, r *http.Request) {
		var o cloud.Options
		_ = decode(r, &o)
		raw, err := s.Cloud.Resources(r.Context(), r.PathValue("provider"), r.PathValue("kind"), o)
		jsonOut(w, raw, err)
	})
}

func (s *Server) enterpriseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) { ok(w, s.Settings.Redacted()) })
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var st settings.Settings
		if err := decode(r, &st); err != nil {
			fail(w, err)
			return
		}
		if err := s.Settings.Put(st); err != nil {
			fail(w, err)
			return
		}
		cur := s.Settings.Get()
		s.Clusters.Proxy = cur.Proxy
		s.Clusters.SyncInterval = syncInterval(cur.KubeconfigSync)
		s.Tools.Proxy = cur.Proxy
		s.Plugins.MarketplaceURL = cur.MarketplaceURL
		go s.MCP.Sync(context.Background(), cur.MCPServers)
		ok(w, s.Settings.Redacted())
	})
	mux.HandleFunc("GET /api/license", func(w http.ResponseWriter, r *http.Request) { ok(w, s.License.Current()) })
	mux.HandleFunc("POST /api/license", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Licensee, Key string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.License.Register(b.Licensee, b.Key); err != nil {
			fail(w, err)
			return
		}
		ok(w, s.License.Current())
	})
	mux.HandleFunc("GET /api/workspaces", func(w http.ResponseWriter, r *http.Request) { ok(w, s.Spaces.List()) })
	mux.HandleFunc("POST /api/workspaces", func(w http.ResponseWriter, r *http.Request) {
		var b enterprise.Workspace
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if b.ID == "" && len(b.Members) == 0 {
			u := s.Identity.Current()
			subj := u.Email
			if subj == "" {
				subj = u.Name
			}
			b.Members = []enterprise.Member{{Subject: subj, Role: enterprise.RoleOwner}}
		}
		ws, err := s.Spaces.Save(b)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, ws)
	})
	mux.HandleFunc("DELETE /api/workspaces/{wid}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Spaces.Delete(r.PathValue("wid")); err != nil {
			fail(w, err)
			return
		}
		ok(w, s.Spaces.List())
	})
	mux.HandleFunc("GET /api/audit", func(w http.ResponseWriter, r *http.Request) {
		es, err := s.Audit.Query(q(r, "q"), int(qi(r, "limit", 500)))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, es)
	})
	mux.HandleFunc("GET /api/audit/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", "attachment; filename=kubenova-audit.jsonl")
		http.ServeFile(w, r, s.Audit.Path())
	})
	// Identity / SSO.
	mux.HandleFunc("GET /api/auth/me", func(w http.ResponseWriter, r *http.Request) { ok(w, s.Identity.Current()) })
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		s.Identity.Logout()
		ok(w, s.Identity.Current())
	})
	mux.HandleFunc("GET /api/auth/config", func(w http.ResponseWriter, r *http.Request) {
		c := s.Identity.Config()
		if c.OIDC.ClientSecret != "" {
			c.OIDC.ClientSecret = "••••"
		}
		if c.LDAP.BindPassword != "" {
			c.LDAP.BindPassword = "••••"
		}
		if c.SCIMToken != "" {
			c.SCIMToken = "••••"
		}
		ok(w, c)
	})
	mux.HandleFunc("PUT /api/auth/config", func(w http.ResponseWriter, r *http.Request) {
		var c enterprise.IdentityConfig
		if err := decode(r, &c); err != nil {
			fail(w, err)
			return
		}
		prev := s.Identity.Config()
		if c.OIDC.ClientSecret == "••••" {
			c.OIDC.ClientSecret = prev.OIDC.ClientSecret
		}
		if c.LDAP.BindPassword == "••••" {
			c.LDAP.BindPassword = prev.LDAP.BindPassword
		}
		if c.SCIMToken == "••••" {
			c.SCIMToken = prev.SCIMToken
		}
		if err := s.Identity.SetConfig(c); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/auth/oidc/start", func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Identity.OIDCStart(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"url": u})
	})
	mux.HandleFunc("GET /api/auth/oidc/callback", func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Identity.OIDCCallback(r.Context(), q(r, "state"), q(r, "code"))
		w.Header().Set("Content-Type", "text/html")
		if err != nil {
			fmt.Fprintf(w, "<h3>Sign-in failed</h3><p>%s</p>", htmlEscape(err.Error()))
			return
		}
		fmt.Fprintf(w, "<h3>Signed in to KubeNova as %s</h3><p>You can close this window.</p>", htmlEscape(u.Name))
	})
	mux.HandleFunc("POST /api/auth/ldap", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Username, Password string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		u, err := s.Identity.LDAPLogin(b.Username, b.Password)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, u)
	})
	mux.HandleFunc("/api/auth/saml/", func(w http.ResponseWriter, r *http.Request) {
		m, err := s.Identity.SAML(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/login"):
			m.RequireAccount(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				u, err := s.Identity.SAMLComplete(r)
				w.Header().Set("Content-Type", "text/html")
				if err != nil {
					fmt.Fprintf(w, "<h3>Sign-in failed</h3><p>%s</p>", htmlEscape(err.Error()))
					return
				}
				fmt.Fprintf(w, "<h3>Signed in to KubeNova as %s</h3><p>You can close this window.</p>", htmlEscape(u.Name))
			})).ServeHTTP(w, r)
		default:
			m.ServeHTTP(w, r) // /saml/acs and /saml/metadata
		}
	})
	mux.Handle("/scim/v2/", s.SCIM)
	mux.HandleFunc("GET /api/scim/users", func(w http.ResponseWriter, r *http.Request) { ok(w, s.SCIM.Users()) })
}

func htmlEscape(s string) string {
	return strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;", `"`, "&quot;").Replace(s)
}

func (s *Server) toolRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tools", func(w http.ResponseWriter, r *http.Request) { ok(w, s.Tools.Status(r.Context())) })
	// Built-in terminal: local shell, kubectl/helm preconfigured, k9s, stern,
	// SSH and database sessions.
	mux.HandleFunc("GET /api/terminal", func(w http.ResponseWriter, r *http.Request) {
		cid := q(r, "clusterId")
		kc := ""
		if cid != "" {
			p, err := s.Clusters.KubeconfigFor(cid)
			if err == nil {
				kc = p
			}
		}
		env := s.Tools.Env(kc)
		if ns := q(r, "namespace"); ns != "" {
			env = append(env, "KUBENOVA_NAMESPACE="+ns)
		}
		cmd, args := s.Settings.Get().Shell, []string(nil)
		if cmd == "" {
			cmd = terminal.DefaultShell()
		}
		switch mode := q(r, "cmd"); {
		case mode == "k9s":
			cmd, args = "k9s", nil
			if ns := q(r, "namespace"); ns != "" {
				args = []string{"-n", ns}
			}
		case mode == "stern":
			cmd, args = "stern", strings.Fields(q(r, "args"))
		case strings.HasPrefix(mode, "conn:"):
			c, err := s.Integr.Get(strings.TrimPrefix(mode, "conn:"))
			if err != nil {
				fail(w, err)
				return
			}
			local := 0
			if c.ClusterID != "" && c.Service != "" {
				f, err := s.Pods.StartForward(context.Background(), c.ClusterID, c.Namespace, "service", c.Service, 0, c.Port)
				if err != nil {
					fail(w, err)
					return
				}
				defer s.Pods.StopForward(f.ID)
				local = f.LocalPort
			}
			var extra []string
			cmd, args, extra, err = s.Integr.TerminalCommand(c, local)
			if err != nil {
				fail(w, err)
				return
			}
			env = append(env, extra...)
		}
		if p, err := s.Tools.Find(cmd); err == nil {
			cmd = p
		}
		conn, err := s.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		dir := s.Settings.Get().Workdir
		if dir == "" {
			dir, _ = os.UserHomeDir()
		}
		if err := terminal.Run(conn, cmd, args, env, dir); err != nil {
			_ = conn.WriteMessage(1, []byte("failed to start "+cmd+": "+err.Error()+"\r\n"))
		}
	})
	mux.HandleFunc("POST /api/yaml/format", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			YAML string `json:"yaml"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		objs, err := resources.DecodeYAML(b.YAML)
		if err != nil {
			ok(w, map[string]any{"valid": false, "error": err.Error()})
			return
		}
		var docs []string
		for _, o := range objs {
			y, _ := yaml.Marshal(o.Object)
			docs = append(docs, string(y))
		}
		var problems []string
		for i, o := range objs {
			if o.GetAPIVersion() == "" {
				problems = append(problems, fmt.Sprintf("document %d: apiVersion missing", i+1))
			}
			if o.GetKind() == "" {
				problems = append(problems, fmt.Sprintf("document %d: kind missing", i+1))
			}
			if o.GetName() == "" && o.GetGenerateName() == "" {
				problems = append(problems, fmt.Sprintf("document %d: metadata.name missing", i+1))
			}
		}
		ok(w, map[string]any{"valid": len(problems) == 0, "formatted": strings.Join(docs, "---\n"), "problems": problems})
	})
	// Local file explorer for manifests (scoped to the user's home / workdir).
	mux.HandleFunc("GET /api/fs/list", func(w http.ResponseWriter, r *http.Request) {
		p := q(r, "path")
		if p == "" {
			p = s.Settings.Get().Workdir
			if p == "" {
				p, _ = os.UserHomeDir()
			}
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			fail(w, err)
			return
		}
		type ent struct {
			Name  string    `json:"name"`
			Path  string    `json:"path"`
			Dir   bool      `json:"dir"`
			Size  int64     `json:"size"`
			MTime time.Time `json:"mtime"`
		}
		out := []ent{}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") && !qb(r, "hidden") {
				continue
			}
			info, _ := e.Info()
			en := ent{Name: e.Name(), Path: filepath.Join(p, e.Name()), Dir: e.IsDir()}
			if info != nil {
				en.Size, en.MTime = info.Size(), info.ModTime()
			}
			out = append(out, en)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Dir != out[j].Dir {
				return out[i].Dir
			}
			return out[i].Name < out[j].Name
		})
		ok(w, map[string]any{"path": p, "parent": filepath.Dir(p), "entries": out})
	})
	mux.HandleFunc("GET /api/fs/read", func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(q(r, "path"))
		if err != nil {
			fail(w, err)
			return
		}
		if len(b) > 10<<20 {
			fail(w, fmt.Errorf("file too large to edit"))
			return
		}
		ok(w, map[string]string{"content": string(b)})
	})
	mux.HandleFunc("PUT /api/fs/write", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Path, Content string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := os.WriteFile(b.Path, []byte(b.Content), 0o644); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/git", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Dir  string   `json:"dir"`
			Args []string `json:"args"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		allowed := map[string]bool{"status": true, "log": true, "diff": true, "add": true, "commit": true, "pull": true, "push": true, "branch": true, "checkout": true, "fetch": true, "show": true, "restore": true, "stash": true, "clone": true}
		if len(b.Args) == 0 || !allowed[b.Args[0]] {
			fail(w, errInvalid("git subcommand"))
			return
		}
		out, err := s.Integr.Git(r.Context(), b.Dir, b.Args...)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"output": out, "error": err.Error()})
			return
		}
		ok(w, map[string]string{"output": out})
	})
	// Plugins & marketplace.
	mux.HandleFunc("GET /api/plugins", func(w http.ResponseWriter, r *http.Request) {
		ps, err := s.Plugins.List()
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, ps)
	})
	mux.HandleFunc("POST /api/plugins/upload", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(io.LimitReader(r.Body, 100<<20))
		if err != nil {
			fail(w, err)
			return
		}
		m, err := s.Plugins.InstallZip(b)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, m)
	})
	mux.HandleFunc("GET /api/marketplace", func(w http.ResponseWriter, r *http.Request) {
		es, err := s.Plugins.Marketplace(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, es)
	})
	mux.HandleFunc("POST /api/marketplace/install", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Download string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		m, err := s.Plugins.InstallFromMarketplace(r.Context(), b.Download)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, m)
	})
	mux.HandleFunc("POST /api/plugins/{pid}/{action}", func(w http.ResponseWriter, r *http.Request) {
		var err error
		switch r.PathValue("action") {
		case "enable":
			err = s.Plugins.SetEnabled(r.PathValue("pid"), true)
		case "disable":
			err = s.Plugins.SetEnabled(r.PathValue("pid"), false)
		default:
			err = errInvalid("action")
		}
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("DELETE /api/plugins/{pid}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Plugins.Uninstall(r.PathValue("pid")); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/plugins/{pid}/files/{path...}", func(w http.ResponseWriter, r *http.Request) {
		base := filepath.Join(s.Plugins.Dir, filepath.Base(r.PathValue("pid")))
		p := filepath.Join(base, filepath.FromSlash(r.PathValue("path")))
		if !strings.HasPrefix(p, base+string(os.PathSeparator)) {
			fail(w, errInvalid("path"))
			return
		}
		http.ServeFile(w, r, p)
	})
	s.integrationRoutes(mux)
}

func (s *Server) integrationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/connections", func(w http.ResponseWriter, r *http.Request) { ok(w, s.Integr.Connections(q(r, "kind"))) })
	mux.HandleFunc("POST /api/connections", func(w http.ResponseWriter, r *http.Request) {
		var c integrations.Connection
		if err := decode(r, &c); err != nil {
			fail(w, err)
			return
		}
		c, err := s.Integr.Save(c)
		if err != nil {
			fail(w, err)
			return
		}
		c.Secret = ""
		ok(w, c)
	})
	mux.HandleFunc("DELETE /api/connections/{cid}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Integr.Delete(r.PathValue("cid")); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/connections/{cid}/query", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Query string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		c, err := s.Integr.Get(r.PathValue("cid"))
		if err != nil {
			fail(w, err)
			return
		}
		local := 0
		if c.ClusterID != "" && c.Service != "" {
			f, err := s.Pods.StartForward(context.Background(), c.ClusterID, c.Namespace, "service", c.Service, 0, c.Port)
			if err != nil {
				fail(w, err)
				return
			}
			defer s.Pods.StopForward(f.ID)
			local = f.LocalPort
		}
		out, err := s.Integr.Query(r.Context(), c, b.Query, local)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"output": out, "error": err.Error()})
			return
		}
		ok(w, map[string]string{"output": out})
	})
	mux.HandleFunc("GET /api/connections/{cid}/s3", func(w http.ResponseWriter, r *http.Request) {
		c, err := s.Integr.Get(r.PathValue("cid"))
		if err != nil {
			fail(w, err)
			return
		}
		raw, err := s.Integr.S3List(r.Context(), c, q(r, "bucket"), q(r, "prefix"))
		jsonOut(w, raw, err)
	})
	mux.HandleFunc("GET /api/connections/{cid}/s3/object", func(w http.ResponseWriter, r *http.Request) {
		c, err := s.Integr.Get(r.PathValue("cid"))
		if err != nil {
			fail(w, err)
			return
		}
		b, err := s.Integr.S3Get(r.Context(), c, q(r, "bucket"), q(r, "key"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(q(r, "key")))
		_, _ = w.Write(b)
	})
	mux.HandleFunc("GET /api/integrations/presets", func(w http.ResponseWriter, r *http.Request) { ok(w, integrations.APIPresets) })
	mux.HandleFunc("POST /api/connections/{cid}/api", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Endpoint string            `json:"endpoint"`
			Params   map[string]string `json:"params"`
			Method   string            `json:"method"`
			Body     json.RawMessage   `json:"body"`
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		c, err := s.Integr.Get(r.PathValue("cid"))
		if err != nil {
			fail(w, err)
			return
		}
		var body []byte
		if len(b.Body) > 0 && string(b.Body) != "null" {
			body = b.Body
		}
		raw, err := s.Integr.CallAPI(r.Context(), c, b.Endpoint, b.Params, b.Method, body)
		jsonOut(w, raw, err)
	})
	mux.HandleFunc("POST /api/terraform/state", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Source string }
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		res, meta, err := s.Integr.TerraformState(r.Context(), b.Source)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]any{"resources": res, "meta": meta})
	})
	mux.HandleFunc("GET /api/ansible/playbooks", func(w http.ResponseWriter, r *http.Request) {
		ps, err := integrations.Playbooks(q(r, "dir"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, ps)
	})
	mux.HandleFunc("POST /api/ansible/run", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Playbook, Inventory, ExtraVars string
			Check                          bool
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()
		out, err := s.Integr.RunPlaybook(ctx, b.Playbook, b.Inventory, b.Check, b.ExtraVars)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"output": out, "error": err.Error()})
			return
		}
		ok(w, map[string]string{"output": out})
	})
	mux.HandleFunc("GET /api/clusters/{id}/cost", func(w http.ResponseWriter, r *http.Request) {
		rep, err := s.Integr.Cost(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, rep)
	})
	mux.HandleFunc("PUT /api/cost/pricing", func(w http.ResponseWriter, r *http.Request) {
		var p integrations.Pricing
		if err := decode(r, &p); err != nil {
			fail(w, err)
			return
		}
		if err := s.Integr.SetPricing(p); err != nil {
			fail(w, err)
			return
		}
		ok(w, p)
	})
	mux.HandleFunc("GET /api/clusters/{id}/velero", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Integr.Velero(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, v)
	})
	mux.HandleFunc("POST /api/clusters/{id}/velero", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Kind, Name, FromBackup string
			Namespaces             []string
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		if err := s.Integr.VeleroCreate(r.Context(), r.PathValue("id"), b.Kind, b.Name, b.Namespaces, b.FromBackup); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	})
}
