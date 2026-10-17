// Package rancher integrates directly with the Rancher API: login (local,
// LDAP/AD, SSO via browser), API tokens, multiple servers, cluster and project
// discovery, kubeconfig import (through Rancher's auth proxy), nodes,
// workloads, apps, Fleet, machine pools, registration and upgrades.
package rancher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/clusters"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/store"
)

// Server is a configured Rancher server.
type Server struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Token    string `json:"token,omitempty"`
	Insecure bool   `json:"insecure"`
	User     string `json:"user,omitempty"`
	Version  string `json:"version,omitempty"`
}

// Service manages Rancher servers.
type Service struct {
	Store   *store.Store
	Kube    *clusters.Manager
	mu      sync.Mutex
	pending map[string]ssoRequest // SSO login requests in progress
}

func (s *Service) load() []Server {
	var out []Server
	_ = s.Store.Load("rancher/servers", &out)
	return out
}

func (s *Service) save(servers []Server) error { return s.Store.Save("rancher/servers", servers) }

// Servers lists servers (tokens redacted).
func (s *Service) Servers() []Server {
	out := s.load()
	for i := range out {
		if out[i].Token != "" {
			out[i].Token = "••••"
		}
	}
	return out
}

func (s *Service) server(id string) (Server, error) {
	for _, sv := range s.load() {
		if sv.ID == id {
			return sv, nil
		}
	}
	return Server{}, fmt.Errorf("rancher server %q not found", id)
}

// Upsert adds or updates a server.
func (s *Service) Upsert(sv Server) (Server, error) {
	sv.URL = strings.TrimRight(sv.URL, "/")
	if sv.ID == "" {
		h := sha256.Sum256([]byte(sv.URL))
		sv.ID = hex.EncodeToString(h[:6])
	}
	all := s.load()
	found := false
	for i := range all {
		if all[i].ID == sv.ID {
			if sv.Token == "" || sv.Token == "••••" {
				sv.Token = all[i].Token
			}
			all[i] = sv
			found = true
		}
	}
	if !found {
		all = append(all, sv)
	}
	if sv.Token != "" {
		if v, err := s.version(context.Background(), sv); err == nil {
			sv.Version = v
			for i := range all {
				if all[i].ID == sv.ID {
					all[i].Version = v
				}
			}
		}
	}
	return sv, s.save(all)
}

// Remove deletes a server.
func (s *Service) Remove(id string) error {
	var kept []Server
	for _, sv := range s.load() {
		if sv.ID != id {
			kept = append(kept, sv)
		}
	}
	return s.save(kept)
}

func httpClient(insecure bool) *http.Client {
	return &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure}, //nolint:gosec // user opt-in for self-signed Rancher installs
	}}
}

func (s *Service) do(ctx context.Context, sv Server, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, sv.URL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sv.Token != "" {
		req.Header.Set("Authorization", "Bearer "+sv.Token)
	}
	resp, err := httpClient(sv.Insecure).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 400 {
		var e struct{ Message string }
		_ = json.Unmarshal(b, &e)
		if e.Message == "" {
			e.Message = string(b)
		}
		return fmt.Errorf("rancher %s %s: %s: %s", method, path, resp.Status, e.Message)
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (s *Service) version(ctx context.Context, sv Server) (string, error) {
	var v struct {
		Value string `json:"value"`
	}
	err := s.do(ctx, sv, "GET", "/v3/settings/server-version", nil, &v)
	return v.Value, err
}

// Cluster is a Rancher managed cluster.
type Cluster struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	State         string            `json:"state"`
	Provider      string            `json:"provider"`
	Driver        string            `json:"driver"`
	Version       string            `json:"version"`
	NodeCount     int               `json:"nodeCount"`
	Labels        map[string]string `json:"labels,omitempty"`
	Transitioning string            `json:"transitioningMessage,omitempty"`
}

// Clusters discovers clusters (RKE2, K3s, imported, hosted EKS/AKS/GKE...).
func (s *Service) Clusters(ctx context.Context, id string) ([]Cluster, error) {
	sv, err := s.server(id)
	if err != nil {
		return nil, err
	}
	var res struct {
		Data []struct {
			ID                   string            `json:"id"`
			Name                 string            `json:"name"`
			State                string            `json:"state"`
			Provider             string            `json:"provider"`
			Driver               string            `json:"driver"`
			NodeCount            int               `json:"nodeCount"`
			Labels               map[string]string `json:"labels"`
			TransitioningMessage string            `json:"transitioningMessage"`
			Version              struct {
				GitVersion string `json:"gitVersion"`
			} `json:"version"`
		} `json:"data"`
	}
	if err := s.do(ctx, sv, "GET", "/v3/clusters", nil, &res); err != nil {
		return nil, err
	}
	var out []Cluster
	for _, c := range res.Data {
		out = append(out, Cluster{ID: c.ID, Name: c.Name, State: c.State, Provider: c.Provider, Driver: c.Driver, Version: c.Version.GitVersion, NodeCount: c.NodeCount, Labels: c.Labels, Transitioning: c.TransitioningMessage})
	}
	return out, nil
}

// Project is a Rancher project.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ClusterID   string `json:"clusterId"`
	State       string `json:"state"`
	Description string `json:"description"`
}

// Projects lists projects in a cluster.
func (s *Service) Projects(ctx context.Context, id, clusterID string) ([]Project, error) {
	sv, err := s.server(id)
	if err != nil {
		return nil, err
	}
	var res struct {
		Data []Project `json:"data"`
	}
	err = s.do(ctx, sv, "GET", "/v3/projects?clusterId="+url.QueryEscape(clusterID), nil, &res)
	return res.Data, err
}

// ProjectNamespaces returns namespaces belonging to a project (for filtering).
func (s *Service) ProjectNamespaces(ctx context.Context, id, clusterID, projectID string) ([]string, error) {
	sv, err := s.server(id)
	if err != nil {
		return nil, err
	}
	var res struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
		} `json:"items"`
	}
	pid := projectID
	if i := strings.Index(pid, ":"); i >= 0 {
		pid = pid[i+1:]
	}
	err = s.do(ctx, sv, "GET", "/k8s/clusters/"+clusterID+"/api/v1/namespaces?labelSelector="+url.QueryEscape("field.cattle.io/projectId="+pid), nil, &res)
	out := []string{}
	for _, n := range res.Items {
		out = append(out, n.Metadata.Name)
	}
	return out, err
}

// ImportKubeconfigs generates kubeconfigs through Rancher's authentication
// proxy for the given clusters (all when empty) and registers them.
func (s *Service) ImportKubeconfigs(ctx context.Context, id string, clusterIDs []string) ([]string, error) {
	sv, err := s.server(id)
	if err != nil {
		return nil, err
	}
	if len(clusterIDs) == 0 {
		cs, err := s.Clusters(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			clusterIDs = append(clusterIDs, c.ID)
		}
	}
	var paths []string
	for _, cid := range clusterIDs {
		var res struct {
			Config string `json:"config"`
		}
		if err := s.do(ctx, sv, "POST", "/v3/clusters/"+cid+"?action=generateKubeconfig", nil, &res); err != nil {
			return paths, err
		}
		p, err := s.Kube.ImportKubeconfig("rancher-"+sv.Name+"-"+cid, []byte(res.Config))
		if err != nil {
			return paths, err
		}
		// Keep each Rancher server's clusters together in the cluster list.
		if err := s.Kube.SetDefaultGroup(p, "Rancher: "+sv.Name); err != nil {
			return paths, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// Raw performs an arbitrary Rancher API call (used by node management, apps,
// Fleet, monitoring and the Cluster Explorer views).
func (s *Service) Raw(ctx context.Context, id, method, path string, body any) (json.RawMessage, error) {
	sv, err := s.server(id)
	if err != nil {
		return nil, err
	}
	var out json.RawMessage
	err = s.do(ctx, sv, method, path, body, &out)
	return out, err
}

// Nodes lists nodes of a cluster.
func (s *Service) Nodes(ctx context.Context, id, clusterID string) (json.RawMessage, error) {
	return s.Raw(ctx, id, "GET", "/v3/nodes?clusterId="+url.QueryEscape(clusterID), nil)
}

// NodeAction performs cordon/uncordon/drain on a node (id like c-xxx:m-yyy).
func (s *Service) NodeAction(ctx context.Context, id, nodeID, action string) error {
	var body any
	if action == "drain" {
		body = map[string]any{"deleteLocalData": true, "force": true, "ignoreDaemonSets": true, "gracePeriod": -1, "timeout": 120}
	}
	_, err := s.Raw(ctx, id, "POST", "/v3/nodes/"+nodeID+"?action="+action, body)
	return err
}

// Apps lists installed Rancher apps (catalog.cattle.io) in a cluster.
func (s *Service) Apps(ctx context.Context, id, clusterID string) (json.RawMessage, error) {
	return s.Raw(ctx, id, "GET", "/k8s/clusters/"+clusterID+"/v1/catalog.cattle.io.apps", nil)
}

// FleetRepos lists Fleet GitRepos (managed from the local cluster).
func (s *Service) FleetRepos(ctx context.Context, id string) (json.RawMessage, error) {
	return s.Raw(ctx, id, "GET", "/v1/fleet.cattle.io.gitrepos", nil)
}

// FleetBundles lists Fleet bundles with their deployment status.
func (s *Service) FleetBundles(ctx context.Context, id string) (json.RawMessage, error) {
	return s.Raw(ctx, id, "GET", "/v1/fleet.cattle.io.bundles", nil)
}

// ProvisioningClusters returns v2 provisioning clusters (RKE2/K3s) including
// machine pools and Kubernetes version.
func (s *Service) ProvisioningClusters(ctx context.Context, id string) (json.RawMessage, error) {
	return s.Raw(ctx, id, "GET", "/v1/provisioning.cattle.io.clusters", nil)
}

// UpgradeCluster changes the Kubernetes version of an RKE2/K3s cluster.
func (s *Service) UpgradeCluster(ctx context.Context, id, ns, name, version string) error {
	var obj map[string]any
	raw, err := s.Raw(ctx, id, "GET", "/v1/provisioning.cattle.io.clusters/"+ns+"/"+name, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	spec, _ := obj["spec"].(map[string]any)
	if spec == nil {
		return fmt.Errorf("cluster has no spec")
	}
	spec["kubernetesVersion"] = version
	_, err = s.Raw(ctx, id, "PUT", "/v1/provisioning.cattle.io.clusters/"+ns+"/"+name, obj)
	return err
}

// ScaleMachinePool sets the quantity of a machine pool.
func (s *Service) ScaleMachinePool(ctx context.Context, id, ns, name, pool string, quantity int) error {
	var obj map[string]any
	raw, err := s.Raw(ctx, id, "GET", "/v1/provisioning.cattle.io.clusters/"+ns+"/"+name, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	spec, _ := obj["spec"].(map[string]any)
	rke, _ := spec["rkeConfig"].(map[string]any)
	pools, _ := rke["machinePools"].([]any)
	found := false
	for _, p := range pools {
		m, _ := p.(map[string]any)
		if m["name"] == pool {
			m["quantity"] = quantity
			found = true
		}
	}
	if !found {
		return fmt.Errorf("machine pool %q not found", pool)
	}
	_, err = s.Raw(ctx, id, "PUT", "/v1/provisioning.cattle.io.clusters/"+ns+"/"+name, obj)
	return err
}

// Register creates an imported cluster and returns its registration command.
func (s *Service) Register(ctx context.Context, id, name string) (map[string]string, error) {
	sv, err := s.server(id)
	if err != nil {
		return nil, err
	}
	var c struct {
		ID string `json:"id"`
	}
	if err := s.do(ctx, sv, "POST", "/v3/clusters", map[string]any{"type": "cluster", "name": name}, &c); err != nil {
		return nil, err
	}
	_ = s.do(ctx, sv, "POST", "/v3/clusterregistrationtokens", map[string]any{"type": "clusterRegistrationToken", "clusterId": c.ID}, nil)
	var toks struct {
		Data []struct {
			Command         string `json:"command"`
			InsecureCommand string `json:"insecureCommand"`
			ManifestURL     string `json:"manifestUrl"`
		} `json:"data"`
	}
	for i := 0; i < 10; i++ {
		if err := s.do(ctx, sv, "GET", "/v3/clusterregistrationtokens?clusterId="+c.ID, nil, &toks); err == nil && len(toks.Data) > 0 && toks.Data[0].Command != "" {
			t := toks.Data[0]
			return map[string]string{"clusterId": c.ID, "command": t.Command, "insecureCommand": t.InsecureCommand, "manifestUrl": t.ManifestURL}, nil
		}
		time.Sleep(time.Second)
	}
	return map[string]string{"clusterId": c.ID}, nil
}
