// Package plugins implements the extension system: plugin manifests that add
// UI panels, dashboards, themes, keyboard shortcuts and custom AI tools; a
// marketplace index; and installation into the plugins directory.
package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Manifest is plugin.json.
type Manifest struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description"`
	Author      string      `json:"author"`
	Homepage    string      `json:"homepage,omitempty"`
	Entry       string      `json:"entry,omitempty"` // ES module loaded by the UI (panels, dashboards)
	Panels      []Panel     `json:"panels,omitempty"`
	Dashboards  []Dashboard `json:"dashboards,omitempty"`
	Themes      []Theme     `json:"themes,omitempty"`
	Shortcuts   []Shortcut  `json:"shortcuts,omitempty"`
	AITools     []AITool    `json:"aiTools,omitempty"`
	Permissions []string    `json:"permissions,omitempty"`
	Enabled     bool        `json:"enabled"`
	Dir         string      `json:"dir,omitempty"`
}

// Panel is a custom UI panel contributed by a plugin.
type Panel struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Location string   `json:"location"`        // sidebar | resource-tab | dashboard
	Kinds    []string `json:"kinds,omitempty"` // for resource tabs
	Icon     string   `json:"icon,omitempty"`
}

// Dashboard is a declarative PromQL dashboard.
type Dashboard struct {
	ID     string           `json:"id"`
	Title  string           `json:"title"`
	Panels []DashboardPanel `json:"panels"`
}

// DashboardPanel is a dashboard chart.
type DashboardPanel struct {
	Title string `json:"title"`
	Type  string `json:"type"` // line | stat | table
	Query string `json:"query"`
	Unit  string `json:"unit,omitempty"`
}

// Theme is a colour theme.
type Theme struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Dark   bool              `json:"dark"`
	Colors map[string]string `json:"colors"`
}

// Shortcut binds a key combination to a command.
type Shortcut struct {
	Keys    string `json:"keys"`
	Command string `json:"command"`
}

// AITool is a custom tool exposed to the AI assistant. The command receives
// the JSON input on stdin and must print the result on stdout. HTTP tools
// receive the input as a JSON POST body.
type AITool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Properties  map[string]any `json:"properties"`
	Required    []string       `json:"required,omitempty"`
	Command     []string       `json:"command,omitempty"`
	URL         string         `json:"url,omitempty"`
	Write       bool           `json:"write,omitempty"`
}

// Manager manages installed plugins.
type Manager struct {
	Dir            string
	MarketplaceURL string
	AirGapped      func() bool
}

// List returns installed plugins.
func (m *Manager) List() ([]Manifest, error) {
	entries, err := os.ReadDir(m.Dir)
	if os.IsNotExist(err) {
		return []Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Manifest{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(m.Dir, e.Name(), "plugin.json"))
		if err != nil {
			continue
		}
		var mf Manifest
		if json.Unmarshal(b, &mf) != nil {
			continue
		}
		mf.Dir = filepath.Join(m.Dir, e.Name())
		if _, err := os.Stat(filepath.Join(mf.Dir, ".disabled")); err == nil {
			mf.Enabled = false
		} else {
			mf.Enabled = true
		}
		out = append(out, mf)
	}
	return out, nil
}

// SetEnabled enables or disables a plugin.
func (m *Manager) SetEnabled(id string, enabled bool) error {
	p := filepath.Join(m.Dir, id, ".disabled")
	if enabled {
		err := os.Remove(p)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.WriteFile(p, nil, 0o600)
}

// Uninstall removes a plugin.
func (m *Manager) Uninstall(id string) error {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return fmt.Errorf("invalid plugin id")
	}
	return os.RemoveAll(filepath.Join(m.Dir, id))
}

// InstallZip installs a plugin from a zip archive (from the marketplace or a file).
func (m *Manager) InstallZip(data []byte) (*Manifest, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var mf Manifest
	prefix := ""
	for _, f := range zr.File {
		if filepath.Base(f.Name) == "plugin.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			if err := json.Unmarshal(b, &mf); err != nil {
				return nil, err
			}
			prefix = strings.TrimSuffix(f.Name, "plugin.json")
			break
		}
	}
	if mf.ID == "" || strings.ContainsAny(mf.ID, `/\.`) {
		return nil, fmt.Errorf("plugin.json missing or has invalid id")
	}
	dest := filepath.Join(m.Dir, mf.ID)
	_ = os.RemoveAll(dest)
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) || f.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, dest+string(os.PathSeparator)) {
			return nil, fmt.Errorf("illegal path in archive: %s", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 50<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return nil, err
		}
	}
	mf.Dir = dest
	mf.Enabled = true
	return &mf, nil
}

// MarketplaceEntry is a listing in the marketplace index.
type MarketplaceEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	Download    string   `json:"download"`
	Tags        []string `json:"tags"`
	Stars       int      `json:"stars"`
}

// Marketplace fetches the index (disabled in air-gapped mode, where plugins
// are installed from files).
func (m *Manager) Marketplace(ctx context.Context) ([]MarketplaceEntry, error) {
	if m.AirGapped != nil && m.AirGapped() {
		return nil, fmt.Errorf("marketplace is unavailable in air-gapped mode; install plugins from a .zip file")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", m.MarketplaceURL, nil)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out []MarketplaceEntry
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// InstallFromMarketplace downloads and installs an entry.
func (m *Manager) InstallFromMarketplace(ctx context.Context, url string) (*Manifest, error) {
	if m.AirGapped != nil && m.AirGapped() {
		return nil, fmt.Errorf("air-gapped mode")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
	if err != nil {
		return nil, err
	}
	return m.InstallZip(b)
}

// RunAITool executes a plugin AI tool.
func RunAITool(ctx context.Context, t AITool, dir, kubeconfig string, input []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if t.URL != "" {
		req, _ := http.NewRequestWithContext(ctx, "POST", t.URL, bytes.NewReader(input))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return string(b), err
	}
	if len(t.Command) == 0 {
		return "", fmt.Errorf("tool %s has no command or url", t.Name)
	}
	cmd := exec.CommandContext(ctx, t.Command[0], t.Command[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
