// Package clusters manages an unlimited number of Kubernetes clusters sourced
// from any number of kubeconfig files. It handles merging, context switching,
// grouping, favourites, recents, health checks and an offline cache.
package clusters

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	// Register every in-tree auth provider (OIDC etc). Exec plugins such as
	// aws-iam-authenticator, gke-gcloud-auth-plugin and kubelogin are supported
	// natively by client-go.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/store"
)

// Cluster describes a single kube context discovered from a kubeconfig source.
type Cluster struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Context    string    `json:"context"`
	Source     string    `json:"source"`
	Server     string    `json:"server"`
	User       string    `json:"user"`
	Namespace  string    `json:"namespace"`
	AuthType   string    `json:"authType"`
	Group      string    `json:"group"`
	Favorite   bool      `json:"favorite"`
	Color      string    `json:"color,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	LastOpened time.Time `json:"lastOpened,omitempty"`
	Health     *Health   `json:"health,omitempty"`
}

// Meta is user editable metadata persisted per cluster.
type Meta struct {
	Alias      string    `json:"alias,omitempty"`
	Group      string    `json:"group,omitempty"`
	Favorite   bool      `json:"favorite,omitempty"`
	Color      string    `json:"color,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	LastOpened time.Time `json:"lastOpened,omitempty"`
}

type persisted struct {
	Sources []string           `json:"sources"`
	Meta    map[string]*Meta   `json:"meta"`
	Health  map[string]*Health `json:"health"`
}

type clientSet struct {
	config    *rest.Config
	typed     *kubernetes.Clientset
	dynamic   dynamic.Interface
	discovery discovery.CachedDiscoveryInterface
	mapper    *restmapper.DeferredDiscoveryRESTMapper
}

// Manager is the central registry of clusters.
type Manager struct {
	st       *store.Store
	mu       sync.RWMutex
	state    persisted
	clusters map[string]*Cluster
	raw      map[string]*clientcmdapi.Config // source path -> parsed kubeconfig
	clients  map[string]*clientSet
	Proxy    string
}

// NewManager loads persisted state and scans all kubeconfig sources.
func NewManager(st *store.Store) (*Manager, error) {
	m := &Manager{
		st:       st,
		clusters: map[string]*Cluster{},
		raw:      map[string]*clientcmdapi.Config{},
		clients:  map[string]*clientSet{},
	}
	if err := st.Load("clusters", &m.state); err != nil {
		return nil, err
	}
	if m.state.Meta == nil {
		m.state.Meta = map[string]*Meta{}
	}
	if m.state.Health == nil {
		m.state.Health = map[string]*Health{}
	}
	if err := os.MkdirAll(m.ImportDir(), 0o700); err != nil {
		return nil, err
	}
	m.Reload()
	return m, nil
}

// ImportDir is where kubeconfigs imported from Rancher, cloud providers or
// pasted by the user are stored.
func (m *Manager) ImportDir() string { return filepath.Join(m.st.Dir(), "kubeconfigs") }

// DefaultSources returns kubeconfig files discovered from KUBECONFIG, ~/.kube
// and the import directory.
func (m *Manager) DefaultSources() []string {
	var out []string
	if env := os.Getenv("KUBECONFIG"); env != "" {
		out = append(out, filepath.SplitList(env)...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".kube", "config"))
		if entries, err := os.ReadDir(filepath.Join(home, ".kube", "configs")); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					out = append(out, filepath.Join(home, ".kube", "configs", e.Name()))
				}
			}
		}
	}
	if entries, err := os.ReadDir(m.ImportDir()); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				out = append(out, filepath.Join(m.ImportDir(), e.Name()))
			}
		}
	}
	return out
}

// Sources returns every kubeconfig source (default and user-added).
func (m *Manager) Sources() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.allSourcesLocked()
}

func (m *Manager) allSourcesLocked() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(m.DefaultSources(), m.state.Sources...) {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// AddSource registers an additional kubeconfig file or directory.
func (m *Manager) AddSource(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if info.IsDir() {
		entries, _ := os.ReadDir(path)
		for _, e := range entries {
			if !e.IsDir() {
				m.state.Sources = append(m.state.Sources, filepath.Join(path, e.Name()))
			}
		}
	} else {
		m.state.Sources = append(m.state.Sources, path)
	}
	m.mu.Unlock()
	if err := m.save(); err != nil {
		return err
	}
	m.Reload()
	return nil
}

// RemoveSource unregisters a kubeconfig source. Imported files are deleted.
func (m *Manager) RemoveSource(path string) error {
	m.mu.Lock()
	var kept []string
	for _, s := range m.state.Sources {
		if s != path {
			kept = append(kept, s)
		}
	}
	m.state.Sources = kept
	m.mu.Unlock()
	if strings.HasPrefix(path, m.ImportDir()) {
		_ = os.Remove(path)
	}
	if err := m.save(); err != nil {
		return err
	}
	m.Reload()
	return nil
}

// ImportKubeconfig stores raw kubeconfig content (from Rancher, a cloud
// provider or pasted by the user) and registers it.
func (m *Manager) ImportKubeconfig(name string, data []byte) (string, error) {
	if _, err := clientcmd.Load(data); err != nil {
		return "", fmt.Errorf("invalid kubeconfig: %w", err)
	}
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '-'
		}
		return r
	}, name)
	if safe == "" {
		safe = fmt.Sprintf("import-%d", time.Now().Unix())
	}
	p := filepath.Join(m.ImportDir(), safe+".yaml")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return "", err
	}
	m.Reload()
	return p, nil
}

// Reload rescans every kubeconfig source.
func (m *Manager) Reload() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusters = map[string]*Cluster{}
	m.raw = map[string]*clientcmdapi.Config{}
	for _, src := range m.allSourcesLocked() {
		cfg, err := clientcmd.LoadFromFile(src)
		if err != nil {
			continue
		}
		m.raw[src] = cfg
		for ctxName, ctx := range cfg.Contexts {
			id := ClusterID(src, ctxName)
			c := &Cluster{ID: id, Name: ctxName, Context: ctxName, Source: src, User: ctx.AuthInfo, Namespace: ctx.Namespace}
			if cl, ok := cfg.Clusters[ctx.Cluster]; ok {
				c.Server = cl.Server
			}
			if ai, ok := cfg.AuthInfos[ctx.AuthInfo]; ok {
				c.AuthType = authType(ai)
			}
			if meta, ok := m.state.Meta[id]; ok {
				if meta.Alias != "" {
					c.Name = meta.Alias
				}
				c.Group, c.Favorite, c.Color, c.Tags, c.LastOpened = meta.Group, meta.Favorite, meta.Color, meta.Tags, meta.LastOpened
			}
			c.Health = m.state.Health[id]
			m.clusters[id] = c
		}
	}
	// Drop cached clients for clusters which no longer exist.
	for id := range m.clients {
		if _, ok := m.clusters[id]; !ok {
			delete(m.clients, id)
		}
	}
}

func authType(ai *clientcmdapi.AuthInfo) string {
	switch {
	case ai.Exec != nil:
		cmd := filepath.Base(ai.Exec.Command)
		switch {
		case strings.Contains(cmd, "aws"):
			return "aws-iam"
		case strings.Contains(cmd, "gke-gcloud"), strings.Contains(cmd, "gcloud"):
			return "gcp"
		case strings.Contains(cmd, "kubelogin"):
			return "azure-ad"
		case strings.Contains(cmd, "oidc"):
			return "oidc"
		}
		return "exec:" + cmd
	case ai.AuthProvider != nil:
		return ai.AuthProvider.Name
	case ai.Token != "" || ai.TokenFile != "":
		return "token"
	case len(ai.ClientCertificateData) > 0 || ai.ClientCertificate != "":
		return "client-certificate"
	case ai.Username != "":
		return "basic"
	}
	return "unknown"
}

// ClusterID derives a stable identifier from a kubeconfig source and context.
func ClusterID(source, ctx string) string {
	h := sha1.Sum([]byte(source + "\x00" + ctx))
	return hex.EncodeToString(h[:])[:16]
}

// List returns all clusters sorted by favourite, group then name. The query
// filters by name, server, group or tag.
func (m *Manager) List(query string) []*Cluster {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q := strings.ToLower(query)
	out := make([]*Cluster, 0, len(m.clusters))
	for _, c := range m.clusters {
		if q != "" {
			hay := strings.ToLower(c.Name + " " + c.Server + " " + c.Group + " " + strings.Join(c.Tags, " ") + " " + c.AuthType)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Favorite != out[j].Favorite {
			return out[i].Favorite
		}
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Recent returns the most recently opened clusters.
func (m *Manager) Recent(n int) []*Cluster {
	all := m.List("")
	var out []*Cluster
	for _, c := range all {
		if !c.LastOpened.IsZero() {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastOpened.After(out[j].LastOpened) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Groups returns the distinct cluster groups.
func (m *Manager) Groups() []string {
	set := map[string]bool{}
	for _, c := range m.List("") {
		if c.Group != "" {
			set[c.Group] = true
		}
	}
	var out []string
	for g := range set {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// Get returns a cluster by id.
func (m *Manager) Get(id string) (*Cluster, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.clusters[id]
	if !ok {
		return nil, fmt.Errorf("cluster %q not found", id)
	}
	return c, nil
}

// UpdateMeta changes user metadata (alias, group, favourite, colour, tags).
func (m *Manager) UpdateMeta(id string, meta Meta) error {
	m.mu.Lock()
	if _, ok := m.clusters[id]; !ok {
		m.mu.Unlock()
		return fmt.Errorf("cluster %q not found", id)
	}
	prev := m.state.Meta[id]
	if prev != nil && meta.LastOpened.IsZero() {
		meta.LastOpened = prev.LastOpened
	}
	m.state.Meta[id] = &meta
	m.mu.Unlock()
	if err := m.save(); err != nil {
		return err
	}
	m.Reload()
	return nil
}

// Touch records that a cluster was opened (for the recents list).
func (m *Manager) Touch(id string) {
	m.mu.Lock()
	meta := m.state.Meta[id]
	if meta == nil {
		meta = &Meta{}
		m.state.Meta[id] = meta
	}
	meta.LastOpened = time.Now()
	if c, ok := m.clusters[id]; ok {
		c.LastOpened = meta.LastOpened
	}
	m.mu.Unlock()
	_ = m.save()
}

func (m *Manager) save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.st.Save("clusters", m.state)
}

// RawConfig returns the kubeconfig API object containing the cluster context.
func (m *Manager) RawConfig(id string) (*clientcmdapi.Config, *Cluster, error) {
	c, err := m.Get(id)
	if err != nil {
		return nil, nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	raw, ok := m.raw[c.Source]
	if !ok {
		return nil, nil, fmt.Errorf("kubeconfig %s not loaded", c.Source)
	}
	return raw, c, nil
}

// RESTConfig builds a client configuration for the cluster.
func (m *Manager) RESTConfig(id string) (*rest.Config, error) {
	raw, c, err := m.RawConfig(id)
	if err != nil {
		return nil, err
	}
	cc := clientcmd.NewNonInteractiveClientConfig(*raw, c.Context, &clientcmd.ConfigOverrides{}, &clientcmd.ClientConfigLoadingRules{ExplicitPath: c.Source})
	cfg, err := cc.ClientConfig()
	if err != nil {
		return nil, err
	}
	cfg.QPS = 100
	cfg.Burst = 200
	cfg.UserAgent = "KubeNova"
	if m.Proxy != "" {
		applyProxy(cfg, m.Proxy)
	}
	return cfg, nil
}

func (m *Manager) clientsFor(id string) (*clientSet, error) {
	m.mu.RLock()
	cs, ok := m.clients[id]
	m.mu.RUnlock()
	if ok {
		return cs, nil
	}
	cfg, err := m.RESTConfig(id)
	if err != nil {
		return nil, err
	}
	typed, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	disc := memory.NewMemCacheClient(typed.Discovery())
	cs = &clientSet{config: cfg, typed: typed, dynamic: dyn, discovery: disc, mapper: restmapper.NewDeferredDiscoveryRESTMapper(disc)}
	m.mu.Lock()
	m.clients[id] = cs
	m.mu.Unlock()
	return cs, nil
}

// Typed returns the typed clientset for a cluster.
func (m *Manager) Typed(id string) (*kubernetes.Clientset, error) {
	cs, err := m.clientsFor(id)
	if err != nil {
		return nil, err
	}
	return cs.typed, nil
}

// Dynamic returns the dynamic client for a cluster.
func (m *Manager) Dynamic(id string) (dynamic.Interface, error) {
	cs, err := m.clientsFor(id)
	if err != nil {
		return nil, err
	}
	return cs.dynamic, nil
}

// Discovery returns the cached discovery client for a cluster.
func (m *Manager) Discovery(id string) (discovery.CachedDiscoveryInterface, error) {
	cs, err := m.clientsFor(id)
	if err != nil {
		return nil, err
	}
	return cs.discovery, nil
}

// Mapper returns the REST mapper for a cluster.
func (m *Manager) Mapper(id string) (*restmapper.DeferredDiscoveryRESTMapper, error) {
	cs, err := m.clientsFor(id)
	if err != nil {
		return nil, err
	}
	return cs.mapper, nil
}

// Config returns the REST config used by cached clients.
func (m *Manager) Config(id string) (*rest.Config, error) {
	cs, err := m.clientsFor(id)
	if err != nil {
		return nil, err
	}
	return cs.config, nil
}

// InvalidateDiscovery resets cached API discovery (e.g. after a CRD install).
func (m *Manager) InvalidateDiscovery(id string) {
	if cs, err := m.clientsFor(id); err == nil {
		cs.discovery.Invalidate()
		cs.mapper.Reset()
	}
}

// Merged produces a single kubeconfig with every context from every source.
// Name collisions are resolved by suffixing the source file name.
func (m *Manager) Merged() *clientcmdapi.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := clientcmdapi.NewConfig()
	for src, cfg := range m.raw {
		suffix := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		rename := func(existing func(string) bool, name string) string {
			if !existing(name) {
				return name
			}
			return name + "@" + suffix
		}
		for name, cl := range cfg.Clusters {
			n := rename(func(s string) bool { _, ok := out.Clusters[s]; return ok }, name)
			out.Clusters[n] = cl
		}
		for name, ai := range cfg.AuthInfos {
			n := rename(func(s string) bool { _, ok := out.AuthInfos[s]; return ok }, name)
			out.AuthInfos[n] = ai
		}
		for name, ctx := range cfg.Contexts {
			n := rename(func(s string) bool { _, ok := out.Contexts[s]; return ok }, name)
			cp := *ctx
			if _, ok := out.Clusters[cp.Cluster+"@"+suffix]; ok {
				cp.Cluster += "@" + suffix
			}
			if _, ok := out.AuthInfos[cp.AuthInfo+"@"+suffix]; ok {
				cp.AuthInfo += "@" + suffix
			}
			out.Contexts[n] = &cp
		}
		if out.CurrentContext == "" {
			out.CurrentContext = cfg.CurrentContext
		}
	}
	return out
}

// MergedYAML serialises the merged kubeconfig.
func (m *Manager) MergedYAML() ([]byte, error) {
	return clientcmd.Write(*m.Merged())
}

// SwitchContext sets current-context in the source kubeconfig, so external
// tools (kubectl, helm) follow the IDE selection.
func (m *Manager) SwitchContext(id string) error {
	c, err := m.Get(id)
	if err != nil {
		return err
	}
	cfg, err := clientcmd.LoadFromFile(c.Source)
	if err != nil {
		return err
	}
	cfg.CurrentContext = c.Context
	if err := clientcmd.WriteToFile(*cfg, c.Source); err != nil {
		return err
	}
	m.Touch(id)
	return nil
}

// KubeconfigFor returns a minimal single-context kubeconfig for the cluster.
// It is written to a temp file and used for CLI tools (kubectl/helm/stern).
func (m *Manager) KubeconfigFor(id string) (string, error) {
	raw, c, err := m.RawConfig(id)
	if err != nil {
		return "", err
	}
	cfg := raw.DeepCopy()
	cfg.CurrentContext = c.Context
	dir := filepath.Join(m.st.Dir(), "run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := clientcmdapi.FlattenConfig(cfg); err != nil {
		// Relative paths stay valid if flattening fails because the source sits
		// next to the files; fall back to the unflattened config.
		cfg = raw.DeepCopy()
		cfg.CurrentContext = c.Context
	}
	p := filepath.Join(dir, id+".kubeconfig")
	if err := clientcmd.WriteToFile(*cfg, p); err != nil {
		return "", err
	}
	return p, nil
}

// StartBackgroundHealth periodically checks health of all clusters with a
// bounded worker pool so that 500+ clusters do not overwhelm the host.
func (m *Manager) StartBackgroundHealth(ctx context.Context, every time.Duration, workers int) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			m.CheckAll(ctx, workers)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// CheckAll checks every cluster concurrently.
func (m *Manager) CheckAll(ctx context.Context, workers int) {
	ids := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range ids {
				cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				_, _ = m.CheckHealth(cctx, id, false)
				cancel()
			}
		}()
	}
	for _, c := range m.List("") {
		select {
		case ids <- c.ID:
		case <-ctx.Done():
		}
	}
	close(ids)
	wg.Wait()
	_ = m.save()
}
