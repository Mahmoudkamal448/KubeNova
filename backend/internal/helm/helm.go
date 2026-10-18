// Package helm wraps the Helm CLI to provide repository management (including
// OCI registries), install, upgrade, rollback, diff, dry-run, history and
// uninstall for releases.
package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mahmoudkamal448/kubenova/backend/internal/clusters"
	"github.com/mahmoudkamal448/kubenova/backend/internal/textdiff"
	"github.com/mahmoudkamal448/kubenova/backend/internal/tools"
)

// Service runs helm against a cluster.
type Service struct {
	Clusters *clusters.Manager
	Tools    *tools.Runner
}

func (s *Service) run(ctx context.Context, clusterID string, args []string, stdin []byte) (string, error) {
	kc := ""
	if clusterID != "" {
		var err error
		kc, err = s.Clusters.KubeconfigFor(clusterID)
		if err != nil {
			return "", err
		}
	}
	res, err := s.Tools.Run(ctx, kc, "helm", args, stdin)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

func (s *Service) runJSON(ctx context.Context, clusterID string, args []string, v any) error {
	out, err := s.run(ctx, clusterID, append(args, "-o", "json"), nil)
	if err != nil {
		return err
	}
	if out == "" {
		return nil
	}
	return json.Unmarshal([]byte(out), v)
}

// Release is a helm release.
type Release struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Revision   string `json:"revision"`
	Updated    string `json:"updated"`
	Status     string `json:"status"`
	Chart      string `json:"chart"`
	AppVersion string `json:"app_version"`
}

// Releases lists releases in all namespaces (or one). Read directly from
// the cluster, so it works without the helm CLI.
func (s *Service) Releases(ctx context.Context, clusterID, ns string) ([]Release, error) {
	return s.nativeReleases(ctx, clusterID, ns)
}

// HistoryEntry is a release revision.
type HistoryEntry struct {
	Revision    int    `json:"revision"`
	Updated     string `json:"updated"`
	Status      string `json:"status"`
	Chart       string `json:"chart"`
	AppVersion  string `json:"app_version"`
	Description string `json:"description"`
}

// History returns release history.
func (s *Service) History(ctx context.Context, clusterID, ns, name string) ([]HistoryEntry, error) {
	return s.nativeHistory(ctx, clusterID, ns, name)
}

// Get returns values, manifest, notes or hooks of a release revision.
func (s *Service) Get(ctx context.Context, clusterID, ns, name, what string, revision int) (string, error) {
	return s.nativeGet(ctx, clusterID, ns, name, what, revision)
}

// InstallRequest describes an install or upgrade.
type InstallRequest struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Chart       string `json:"chart"` // repo/chart, oci://..., or path
	Version     string `json:"version"`
	Values      string `json:"values"`
	DryRun      bool   `json:"dryRun"`
	CreateNS    bool   `json:"createNamespace"`
	Wait        bool   `json:"wait"`
	Atomic      bool   `json:"atomic"`
	ResetValues bool   `json:"resetValues"`
}

func (s *Service) valuesFile(values string) (string, func(), error) {
	if values == "" {
		return "", func() {}, nil
	}
	f, err := os.CreateTemp("", "kubenova-values-*.yaml")
	if err != nil {
		return "", nil, err
	}
	if _, err := f.WriteString(values); err != nil {
		return "", nil, err
	}
	f.Close()
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

// Upgrade installs or upgrades a release (helm upgrade --install).
func (s *Service) Upgrade(ctx context.Context, clusterID string, r InstallRequest) (string, error) {
	args := []string{"upgrade", "--install", r.Name, r.Chart, "-n", r.Namespace}
	if r.Version != "" {
		args = append(args, "--version", r.Version)
	}
	vf, cleanup, err := s.valuesFile(r.Values)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if vf != "" {
		args = append(args, "-f", vf)
	}
	if r.DryRun {
		args = append(args, "--dry-run=server")
	}
	if r.CreateNS {
		args = append(args, "--create-namespace")
	}
	if r.Wait {
		args = append(args, "--wait")
	}
	if r.Atomic {
		args = append(args, "--atomic")
	}
	if r.ResetValues {
		args = append(args, "--reset-values")
	}
	return s.run(ctx, clusterID, args, nil)
}

// Diff renders the chart with new values and diffs it against the deployed manifest.
func (s *Service) Diff(ctx context.Context, clusterID string, r InstallRequest) (string, error) {
	current, err := s.Get(ctx, clusterID, r.Namespace, r.Name, "manifest", 0)
	if err != nil {
		current = ""
	}
	args := []string{"template", r.Name, r.Chart, "-n", r.Namespace, "--is-upgrade"}
	if r.Version != "" {
		args = append(args, "--version", r.Version)
	}
	vf, cleanup, err := s.valuesFile(r.Values)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if vf != "" {
		args = append(args, "-f", vf)
	}
	next, err := s.run(ctx, clusterID, args, nil)
	if err != nil {
		return "", err
	}
	return textdiff.Unified("deployed", "proposed", current, next, 3), nil
}

// Rollback rolls back a release.
func (s *Service) Rollback(ctx context.Context, clusterID, ns, name string, revision int) (string, error) {
	args := []string{"rollback", name, "-n", ns, "--wait"}
	if revision > 0 {
		args = []string{"rollback", name, strconv.Itoa(revision), "-n", ns, "--wait"}
	}
	return s.run(ctx, clusterID, args, nil)
}

// Uninstall deletes a release.
func (s *Service) Uninstall(ctx context.Context, clusterID, ns, name string) (string, error) {
	return s.run(ctx, clusterID, []string{"uninstall", name, "-n", ns}, nil)
}

// Repo is a chart repository.
type Repo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Repos lists configured repositories.
func (s *Service) Repos(ctx context.Context) ([]Repo, error) {
	var out []Repo
	err := s.runJSON(ctx, "", []string{"repo", "list"}, &out)
	if err != nil {
		return []Repo{}, nil // "no repositories to show" is an error in helm
	}
	return out, nil
}

// AddRepo adds an HTTP repository.
func (s *Service) AddRepo(ctx context.Context, name, url, user, pass string) error {
	args := []string{"repo", "add", name, url, "--force-update"}
	if user != "" {
		args = append(args, "--username", user, "--password-stdin")
	}
	var stdin []byte
	if pass != "" {
		stdin = []byte(pass)
	}
	_, err := s.run(ctx, "", args, stdin)
	return err
}

// RemoveRepo removes a repository.
func (s *Service) RemoveRepo(ctx context.Context, name string) error {
	_, err := s.run(ctx, "", []string{"repo", "remove", name}, nil)
	return err
}

// UpdateRepos refreshes repository indexes.
func (s *Service) UpdateRepos(ctx context.Context) error {
	_, err := s.run(ctx, "", []string{"repo", "update"}, nil)
	return err
}

// RegistryLogin logs in to an OCI registry.
func (s *Service) RegistryLogin(ctx context.Context, host, user, pass string) error {
	_, err := s.run(ctx, "", []string{"registry", "login", host, "--username", user, "--password-stdin"}, []byte(pass))
	return err
}

// Chart is a search result.
type Chart struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	AppVersion  string `json:"app_version"`
	Description string `json:"description"`
}

// Search searches configured repositories.
func (s *Service) Search(ctx context.Context, q string, versions bool) ([]Chart, error) {
	args := []string{"search", "repo", q}
	if versions {
		args = append(args, "--versions")
	}
	var out []Chart
	err := s.runJSON(ctx, "", args, &out)
	return out, err
}

// ShowValues returns default values for a chart (works for OCI too).
func (s *Service) ShowValues(ctx context.Context, chart, version string) (string, error) {
	args := []string{"show", "values", chart}
	if version != "" {
		args = append(args, "--version", version)
	}
	return s.run(ctx, "", args, nil)
}

// ShowChart returns Chart.yaml + README for AI explanation.
func (s *Service) ShowChart(ctx context.Context, chart, version string) (string, error) {
	args := []string{"show", "all", chart}
	if version != "" {
		args = append(args, "--version", version)
	}
	return s.run(ctx, "", args, nil)
}

// CacheDir returns helm's cache dir (exposed for air-gapped chart import).
func CacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "helm")
	}
	return ""
}

var _ = fmt.Sprintf
