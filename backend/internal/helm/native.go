package helm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// Helm 3 stores every release revision in the cluster as a Secret (default
// driver) or ConfigMap labelled owner=helm, holding base64(gzip(JSON)). Reading
// them directly lists releases, history, values and manifests without the
// helm CLI, and only needs permission to read those Secrets.

type storedRelease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	Info      struct {
		Status       string    `json:"status"`
		LastDeployed time.Time `json:"last_deployed"`
		Description  string    `json:"description"`
		Notes        string    `json:"notes"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
		Values map[string]any `json:"values"`
	} `json:"chart"`
	Config   map[string]any `json:"config"`
	Manifest string         `json:"manifest"`
	Hooks    []struct {
		Path     string `json:"path"`
		Manifest string `json:"manifest"`
	} `json:"hooks"`
}

func decodeRelease(data []byte) (*storedRelease, error) {
	b, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, err
	}
	if len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		if b, err = io.ReadAll(zr); err != nil {
			return nil, err
		}
	}
	var r storedRelease
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// stored returns every stored revision matching the label selector.
func (s *Service) stored(ctx context.Context, clusterID, ns, selector string) ([]*storedRelease, error) {
	cs, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	opts := metav1.ListOptions{LabelSelector: "owner=helm" + selector}
	var out []*storedRelease
	secrets, err := cs.CoreV1().Secrets(ns).List(ctx, opts)
	if err != nil {
		return nil, err
	}
	for _, sec := range secrets.Items {
		if r, err := decodeRelease(sec.Data["release"]); err == nil {
			out = append(out, r)
		}
	}
	// Releases installed with HELM_DRIVER=configmap.
	if cms, err := cs.CoreV1().ConfigMaps(ns).List(ctx, opts); err == nil {
		for _, cm := range cms.Items {
			if r, err := decodeRelease([]byte(cm.Data["release"])); err == nil {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func helmTime(t time.Time) string { return t.Format("2006-01-02 15:04:05.999999999 -0700 MST") }

func chartName(r *storedRelease) string {
	return r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
}

func (s *Service) nativeReleases(ctx context.Context, clusterID, ns string) ([]Release, error) {
	all, err := s.stored(ctx, clusterID, ns, "")
	if err != nil {
		return nil, err
	}
	latest := map[string]*storedRelease{}
	for _, r := range all {
		k := r.Namespace + "/" + r.Name
		if cur, ok := latest[k]; !ok || r.Version > cur.Version {
			latest[k] = r
		}
	}
	out := []Release{}
	for _, r := range latest {
		out = append(out, Release{Name: r.Name, Namespace: r.Namespace, Revision: strconv.Itoa(r.Version), Updated: helmTime(r.Info.LastDeployed),
			Status: r.Info.Status, Chart: chartName(r), AppVersion: r.Chart.Metadata.AppVersion})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *Service) revisions(ctx context.Context, clusterID, ns, name string) ([]*storedRelease, error) {
	all, err := s.stored(ctx, clusterID, ns, ",name="+name)
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Version < all[j].Version })
	return all, nil
}

func (s *Service) nativeHistory(ctx context.Context, clusterID, ns, name string) ([]HistoryEntry, error) {
	revs, err := s.revisions(ctx, clusterID, ns, name)
	if err != nil {
		return nil, err
	}
	out := []HistoryEntry{}
	for _, r := range revs {
		out = append(out, HistoryEntry{Revision: r.Version, Updated: r.Info.LastDeployed.Format(time.RFC3339), Status: r.Info.Status,
			Chart: chartName(r), AppVersion: r.Chart.Metadata.AppVersion, Description: r.Info.Description})
	}
	if len(out) > 50 {
		out = out[len(out)-50:]
	}
	return out, nil
}

func (s *Service) nativeGet(ctx context.Context, clusterID, ns, name, what string, revision int) (string, error) {
	revs, err := s.revisions(ctx, clusterID, ns, name)
	if err != nil {
		return "", err
	}
	if len(revs) == 0 {
		return "", fmt.Errorf("release %s not found in namespace %s", name, ns)
	}
	r := revs[len(revs)-1]
	if revision > 0 {
		r = nil
		for _, x := range revs {
			if x.Version == revision {
				r = x
			}
		}
		if r == nil {
			return "", fmt.Errorf("release %s has no revision %d", name, revision)
		}
	}
	switch what {
	case "values":
		// Same as `helm get values --all`: chart defaults overridden by the
		// user-supplied values.
		b, err := yaml.Marshal(mergeValues(r.Chart.Values, r.Config))
		return string(b), err
	case "manifest":
		return r.Manifest, nil
	case "notes":
		return r.Info.Notes, nil
	case "hooks":
		var b strings.Builder
		for _, h := range r.Hooks {
			fmt.Fprintf(&b, "---\n# Source: %s\n%s\n", h.Path, h.Manifest)
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("unknown release section %q", what)
}

func mergeValues(base, over map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if bm, ok := out[k].(map[string]any); ok {
			if om, ok := v.(map[string]any); ok {
				out[k] = mergeValues(bm, om)
				continue
			}
		}
		out[k] = v
	}
	return out
}
