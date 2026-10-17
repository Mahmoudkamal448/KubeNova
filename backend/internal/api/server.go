// Package api exposes every backend capability over a local HTTP + WebSocket
// API consumed by the Electron/React UI (and by team server deployments).
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/ai"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/cloud"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/clusters"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/enterprise"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/gitops"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/helm"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/integrations"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/mcp"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/metrics"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/plugins"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/pods"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/rancher"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/resources"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/security"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/settings"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/store"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/tools"
)

// Version is the application version.
var Version = "1.0.5"

// Server wires services to HTTP handlers.
type Server struct {
	Token    string
	BaseURL  string
	Store    *store.Store
	Settings *settings.Service
	Clusters *clusters.Manager
	Res      *resources.Service
	Pods     *pods.Service
	Metrics  *metrics.Service
	Helm     *helm.Service
	GitOps   *gitops.Service
	Security *security.Service
	AI       *ai.Assistant
	MCP      *mcp.Manager
	Rancher  *rancher.Service
	Cloud    *cloud.Service
	Tools    *tools.Runner
	Plugins  *plugins.Manager
	Integr   *integrations.Service
	License  *enterprise.LicenseService
	Spaces   *enterprise.Workspaces
	Audit    *enterprise.Audit
	Identity *enterprise.Identity
	SCIM     *enterprise.SCIM
	upgrader websocket.Upgrader
}

// New builds a server and all services from a data directory.
func New(dataDir, token, baseURL string) (*Server, error) {
	st, err := store.New(dataDir)
	if err != nil {
		return nil, err
	}
	s := &Server{Token: token, BaseURL: baseURL, Store: st}
	s.Settings = &settings.Service{Store: st}
	cfg := s.Settings.Get()
	s.Clusters, err = clusters.NewManager(st)
	if err != nil {
		return nil, err
	}
	s.Clusters.Proxy = cfg.Proxy
	s.Tools = &tools.Runner{BundleDir: filepath.Join(dataDir, "bin"), Proxy: cfg.Proxy}
	if d := os.Getenv("KUBENOVA_TOOLS_DIR"); d != "" {
		s.Tools.BundleDir = d
	}
	s.Res = &resources.Service{Clusters: s.Clusters}
	s.Pods = &pods.Service{Clusters: s.Clusters}
	s.Metrics = &metrics.Service{Clusters: s.Clusters, Store: st}
	s.Helm = &helm.Service{Clusters: s.Clusters, Tools: s.Tools}
	s.GitOps = &gitops.Service{Clusters: s.Clusters}
	s.Security = &security.Service{Clusters: s.Clusters, Tools: s.Tools}
	s.Rancher = &rancher.Service{Store: st, Kube: s.Clusters}
	s.Cloud = &cloud.Service{Tools: s.Tools, Clusters: s.Clusters}
	s.Integr = &integrations.Service{Store: st, Tools: s.Tools, Clusters: s.Clusters}
	s.Plugins = &plugins.Manager{Dir: filepath.Join(dataDir, "plugins"), MarketplaceURL: cfg.MarketplaceURL, AirGapped: func() bool { return s.Settings.Get().AirGapped }}
	s.License = &enterprise.LicenseService{Store: st}
	s.Spaces = &enterprise.Workspaces{Store: st}
	s.Audit, err = enterprise.NewAudit(filepath.Join(dataDir, "audit"))
	if err != nil {
		return nil, err
	}
	s.Identity = &enterprise.Identity{Store: st, BaseURL: func() string { return s.BaseURL }}
	s.SCIM = &enterprise.SCIM{Store: st, Token: func() string { return s.Identity.Config().SCIMToken }}
	s.MCP = mcp.NewManager()
	s.AI = &ai.Assistant{Settings: func() ai.Settings {
		a := s.Settings.Get().AI
		if s.Settings.Get().AirGapped {
			a.Provider = "local"
		}
		return a
	}, ToolsFor: s.aiTools}
	s.upgrader = websocket.Upgrader{ReadBufferSize: 32 * 1024, WriteBufferSize: 32 * 1024, CheckOrigin: func(r *http.Request) bool { return true }}
	return s, nil
}

// Start launches background work (health checks, MCP servers).
func (s *Server) Start(ctx context.Context) {
	cfg := s.Settings.Get()
	interval := time.Duration(cfg.HealthInterval) * time.Second
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	workers := cfg.HealthWorkers
	if workers <= 0 {
		workers = 16
	}
	s.Clusters.HealthMode = healthMode(cfg.HealthMode)
	s.Clusters.StartBackgroundHealth(ctx, interval, workers)
	s.Clusters.SyncInterval = syncInterval(cfg.KubeconfigSync)
	s.Clusters.StartSync(ctx)
	go s.MCP.Sync(ctx, cfg.MCPServers)
}

func healthMode(m string) string {
	switch m {
	case clusters.HealthAll, clusters.HealthOff:
		return m
	}
	return clusters.HealthOpened
}

func syncInterval(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = 3
	}
	return time.Duration(seconds) * time.Second
}

// ---------- helpers ----------

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

func fail(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		code = http.StatusNotFound
	case strings.Contains(msg, "forbidden"), strings.Contains(msg, "Forbidden"):
		code = http.StatusForbidden
	case strings.Contains(msg, "invalid"), strings.Contains(msg, "required"):
		code = http.StatusBadRequest
	case errors.Is(err, context.DeadlineExceeded):
		code = http.StatusGatewayTimeout
	}
	writeJSON(w, code, apiError{Error: msg})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func q(r *http.Request, k string) string { return r.URL.Query().Get(k) }

func qi(r *http.Request, k string, d int64) int64 {
	if v, err := strconv.ParseInt(q(r, k), 10, 64); err == nil {
		return v
	}
	return d
}

func qb(r *http.Request, k string) bool { b, _ := strconv.ParseBool(q(r, k)); return b }

// group path segment: "core" denotes the legacy core API group.
func group(g string) string {
	if g == "core" || g == "_" {
		return ""
	}
	return g
}

// ---------- middleware ----------

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController and websocket hijacking reach the writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) authorized(r *http.Request) bool {
	if s.Token == "" {
		return true
	}
	tok := r.Header.Get("X-KubeNova-Token")
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) == 1
}

func clusterFromPath(p string) string {
	const pre = "/api/clusters/"
	if !strings.HasPrefix(p, pre) {
		return ""
	}
	rest := strings.TrimPrefix(p, pre)
	if i := strings.Index(rest, "/"); i > 0 {
		return rest[:i]
	}
	return ""
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-KubeNova-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		public := strings.HasPrefix(r.URL.Path, "/api/auth/oidc/callback") || strings.HasPrefix(r.URL.Path, "/api/auth/saml/") ||
			strings.HasPrefix(r.URL.Path, "/scim/") || r.URL.Path == "/api/health"
		if !public && !s.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized"})
			return
		}
		user := s.Identity.Current()
		cid := clusterFromPath(r.URL.Path)
		required := enterprise.RoleViewer
		if r.Method != http.MethodGet {
			required = enterprise.RoleEditor
		}
		if err := s.Spaces.Authorize(s.Settings.Get().AppRBAC, user, cid, required); err != nil {
			writeJSON(w, http.StatusForbidden, apiError{Error: err.Error()})
			return
		}
		if r.Method == http.MethodGet || strings.HasPrefix(r.URL.Path, "/api/ai/") || websocket.IsWebSocketUpgrade(r) {
			if websocket.IsWebSocketUpgrade(r) && (strings.Contains(r.URL.Path, "/exec") || strings.HasSuffix(r.URL.Path, "/terminal")) {
				s.Audit.Record(enterprise.AuditEntry{User: user.Name, Cluster: cid, Action: "session", Method: "WS", Path: r.URL.Path, Detail: r.URL.Query().Get("pod") + r.URL.Query().Get("cmd")})
			}
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		s.Audit.Record(enterprise.AuditEntry{User: user.Name, Cluster: cid, Action: actionName(r), Method: r.Method, Path: r.URL.Path, Status: rec.status, RemoteIP: r.RemoteAddr})
	})
}

func actionName(r *http.Request) string {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) > 0 {
		return strings.ToLower(r.Method) + ":" + parts[len(parts)-1]
	}
	return strings.ToLower(r.Method)
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	return s.middleware(mux)
}

func logErr(err error) {
	if err != nil {
		log.Println(err)
	}
}
