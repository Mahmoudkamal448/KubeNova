// Package settings holds application wide preferences.
package settings

import (
	"sync"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/ai"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/mcp"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/plugins"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/store"
)

// Settings are user preferences.
type Settings struct {
	Theme            string              `json:"theme"` // dark | light | system | plugin theme id
	AI               ai.Settings         `json:"ai"`
	Proxy            string              `json:"proxy"`
	AirGapped        bool                `json:"airGapped"`
	AppRBAC          bool                `json:"appRbac"`
	HealthInterval   int                 `json:"healthIntervalSeconds"`
	HealthWorkers    int                 `json:"healthWorkers"`
	MCPServers       []mcp.ServerConfig  `json:"mcpServers"`
	Shell            string              `json:"shell"`
	Workdir          string              `json:"workdir"`
	Shortcuts        map[string]string   `json:"shortcuts"`
	Dashboards       []plugins.Dashboard `json:"dashboards"`
	PageSize         int                 `json:"pageSize"`
	MarketplaceURL   string              `json:"marketplaceUrl"`
	DefaultNamespace string              `json:"defaultNamespace"`
	KubeconfigSync   int                 `json:"kubeconfigSyncSeconds"`
}

// Defaults returns default settings.
func Defaults() Settings {
	return Settings{
		Theme:          "dark",
		AI:             ai.Settings{Provider: "anthropic", Model: ai.DefaultModel, Effort: "high", MaxSteps: 20},
		HealthInterval: 120,
		HealthWorkers:  16,
		PageSize:       500,
		KubeconfigSync: 3,
		MarketplaceURL: "https://raw.githubusercontent.com/mahmoudkamal448/infrastructue-rebo/main/marketplace/index.json",
		Shortcuts: map[string]string{
			"palette": "Mod+K", "search": "Mod+Shift+F", "terminal": "Mod+`", "ai": "Mod+I",
			"clusters": "Mod+1", "workloads": "Mod+2", "k9s": "Mod+Shift+K",
		},
	}
}

// Service loads and saves settings.
type Service struct {
	Store *store.Store
	mu    sync.RWMutex
	cur   *Settings
}

// Get returns current settings.
func (s *Service) Get() Settings {
	s.mu.RLock()
	if s.cur != nil {
		defer s.mu.RUnlock()
		return *s.cur
	}
	s.mu.RUnlock()
	st := Defaults()
	_ = s.Store.Load("settings", &st)
	if st.AI.MaxSteps == 0 {
		st.AI.MaxSteps = 20
	}
	s.mu.Lock()
	s.cur = &st
	s.mu.Unlock()
	return st
}

// Put saves settings. A redacted API key keeps the stored value.
func (s *Service) Put(st Settings) error {
	prev := s.Get()
	if st.AI.APIKey == "••••" {
		st.AI.APIKey = prev.AI.APIKey
	}
	s.mu.Lock()
	s.cur = &st
	s.mu.Unlock()
	return s.Store.Save("settings", st)
}

// Redacted returns settings safe to send to the UI.
func (s *Service) Redacted() Settings {
	st := s.Get()
	if st.AI.APIKey != "" {
		st.AI.APIKey = "••••"
	}
	return st
}
