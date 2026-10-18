// Package integrations implements the "toolbox" around Kubernetes: SSH
// manager, database manager, S3 browser, Harbor, Vault, Jenkins, GitLab,
// GitHub, Terraform state viewer, Ansible, Git, cost dashboards and Velero
// backup/restore.
package integrations

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/mahmoudkamal448/kubenova/backend/internal/clusters"
	"github.com/mahmoudkamal448/kubenova/backend/internal/store"
	"github.com/mahmoudkamal448/kubenova/backend/internal/tools"
)

// Connection is a saved connection of any kind (ssh, postgres, mysql, redis,
// mongodb, s3, harbor, vault, jenkins, gitlab, github).
type Connection struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Name     string            `json:"name"`
	Host     string            `json:"host,omitempty"`
	Port     int               `json:"port,omitempty"`
	User     string            `json:"user,omitempty"`
	Secret   string            `json:"secret,omitempty"` // password / token
	Database string            `json:"database,omitempty"`
	URL      string            `json:"url,omitempty"`
	Region   string            `json:"region,omitempty"`
	KeyFile  string            `json:"keyFile,omitempty"`
	Jump     string            `json:"jump,omitempty"`
	Group    string            `json:"group,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Extra    map[string]string `json:"extra,omitempty"`
	// Optional: reach an in-cluster database through port-forwarding.
	ClusterID string `json:"clusterId,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Service   string `json:"service,omitempty"`
}

// Service provides integrations.
type Service struct {
	Store    *store.Store
	Tools    *tools.Runner
	Clusters *clusters.Manager
}

func (s *Service) all() []Connection {
	var out []Connection
	_ = s.Store.Load("connections", &out)
	return out
}

// Connections lists saved connections of a kind ("" = all), secrets redacted.
func (s *Service) Connections(kind string) []Connection {
	out := []Connection{}
	for _, c := range s.all() {
		if kind == "" || c.Kind == kind {
			if c.Secret != "" {
				c.Secret = "••••"
			}
			out = append(out, c)
		}
	}
	return out
}

// Get returns a connection with secrets.
func (s *Service) Get(id string) (Connection, error) {
	for _, c := range s.all() {
		if c.ID == id {
			return c, nil
		}
	}
	return Connection{}, fmt.Errorf("connection %q not found", id)
}

// Save creates or updates a connection.
func (s *Service) Save(c Connection) (Connection, error) {
	all := s.all()
	if c.ID == "" {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		c.ID = hex.EncodeToString(b)
		all = append(all, c)
	} else {
		for i := range all {
			if all[i].ID == c.ID {
				if c.Secret == "••••" {
					c.Secret = all[i].Secret
				}
				all[i] = c
			}
		}
	}
	return c, s.Store.Save("connections", all)
}

// Delete removes a connection.
func (s *Service) Delete(id string) error {
	var kept []Connection
	for _, c := range s.all() {
		if c.ID != id {
			kept = append(kept, c)
		}
	}
	return s.Store.Save("connections", kept)
}

// TerminalCommand returns the command + env used to open an interactive
// session for SSH or database connections in the built-in terminal.
func (s *Service) TerminalCommand(c Connection, localPort int) (string, []string, []string, error) {
	host, port := c.Host, c.Port
	if localPort > 0 {
		host, port = "127.0.0.1", localPort
	}
	var env []string
	switch c.Kind {
	case "ssh":
		args := []string{}
		if c.Port > 0 {
			args = append(args, "-p", fmt.Sprint(c.Port))
		}
		if c.KeyFile != "" {
			args = append(args, "-i", c.KeyFile)
		}
		if c.Jump != "" {
			args = append(args, "-J", c.Jump)
		}
		target := c.Host
		if c.User != "" {
			target = c.User + "@" + c.Host
		}
		return "ssh", append(args, target), env, nil
	case "postgres":
		env = append(env, "PGPASSWORD="+c.Secret)
		return "psql", []string{"-h", host, "-p", fmt.Sprint(orInt(port, 5432)), "-U", c.User, orStr(c.Database, "postgres")}, env, nil
	case "mysql":
		env = append(env, "MYSQL_PWD="+c.Secret)
		return "mysql", []string{"-h", host, "-P", fmt.Sprint(orInt(port, 3306)), "-u", c.User, c.Database}, env, nil
	case "redis":
		args := []string{"-h", host, "-p", fmt.Sprint(orInt(port, 6379))}
		if c.Secret != "" {
			env = append(env, "REDISCLI_AUTH="+c.Secret)
		}
		return "redis-cli", args, env, nil
	case "mongodb":
		uri := c.URL
		if uri == "" {
			uri = fmt.Sprintf("mongodb://%s:%s@%s:%d/%s", url.QueryEscape(c.User), url.QueryEscape(c.Secret), host, orInt(port, 27017), c.Database)
		}
		return "mongosh", []string{uri}, env, nil
	}
	return "", nil, nil, fmt.Errorf("no terminal for %s", c.Kind)
}

// Query runs a single query against a database connection using its CLI.
func (s *Service) Query(ctx context.Context, c Connection, query string, localPort int) (string, error) {
	cmd, args, env, err := s.TerminalCommand(c, localPort)
	if err != nil {
		return "", err
	}
	switch c.Kind {
	case "postgres":
		args = append(args, "-c", query, "--csv")
	case "mysql":
		args = append(args, "-e", query, "--batch")
	case "redis":
		args = append(args, strings.Fields(query)...)
	case "mongodb":
		args = append(args, "--quiet", "--eval", query)
	default:
		return "", fmt.Errorf("queries not supported for %s", c.Kind)
	}
	res, err := s.Tools.Run(ctx, "", cmd, args, nil, env...)
	if err != nil {
		if res != nil {
			return res.Stdout + res.Stderr, err
		}
		return "", err
	}
	return res.Stdout, nil
}

func orInt(v, d int) int {
	if v == 0 {
		return d
	}
	return v
}

func orStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// S3List lists buckets (prefix empty, bucket empty) or objects using the AWS
// CLI; works with AWS S3, MinIO, Ceph RGW, DigitalOcean Spaces and others via
// the endpoint URL.
func (s *Service) S3List(ctx context.Context, c Connection, bucket, prefix string) (json.RawMessage, error) {
	env := []string{"AWS_ACCESS_KEY_ID=" + c.User, "AWS_SECRET_ACCESS_KEY=" + c.Secret}
	if c.Region != "" {
		env = append(env, "AWS_DEFAULT_REGION="+c.Region)
	}
	var args []string
	if bucket == "" {
		args = []string{"s3api", "list-buckets", "--output", "json"}
	} else {
		args = []string{"s3api", "list-objects-v2", "--bucket", bucket, "--delimiter", "/", "--output", "json", "--max-items", "1000"}
		if prefix != "" {
			args = append(args, "--prefix", prefix)
		}
	}
	if c.URL != "" {
		args = append(args, "--endpoint-url", c.URL)
	}
	res, err := s.Tools.Run(ctx, "", "aws", args, nil, env...)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(res.Stdout) == "" {
		return json.RawMessage("{}"), nil
	}
	return json.RawMessage(res.Stdout), nil
}

// S3Get downloads an object.
func (s *Service) S3Get(ctx context.Context, c Connection, bucket, key string) ([]byte, error) {
	env := []string{"AWS_ACCESS_KEY_ID=" + c.User, "AWS_SECRET_ACCESS_KEY=" + c.Secret}
	args := []string{"s3", "cp", "s3://" + bucket + "/" + key, "-"}
	if c.URL != "" {
		args = append(args, "--endpoint-url", c.URL)
	}
	res, err := s.Tools.Run(ctx, "", "aws", args, nil, env...)
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}

// APIPresets defines the REST endpoints used by each HTTP integration.
var APIPresets = map[string]map[string]string{
	"harbor":  {"projects": "/api/v2.0/projects?page_size=100", "repositories": "/api/v2.0/projects/{project}/repositories?page_size=100", "artifacts": "/api/v2.0/projects/{project}/repositories/{repo}/artifacts?with_scan_overview=true&page_size=50", "health": "/api/v2.0/health"},
	"vault":   {"health": "/v1/sys/health", "mounts": "/v1/sys/mounts", "list": "/v1/{mount}/metadata/{path}?list=true", "read": "/v1/{mount}/data/{path}", "k8s-roles": "/v1/auth/kubernetes/role?list=true"},
	"jenkins": {"jobs": "/api/json?tree=jobs[name,url,color,lastBuild[number,result,timestamp,duration]]", "builds": "/job/{job}/api/json?tree=builds[number,result,timestamp,duration,url]", "log": "/job/{job}/{build}/consoleText"},
	"gitlab":  {"projects": "/api/v4/projects?membership=true&per_page=100&order_by=last_activity_at", "pipelines": "/api/v4/projects/{project}/pipelines?per_page=50", "jobs": "/api/v4/projects/{project}/pipelines/{pipeline}/jobs", "clusters": "/api/v4/projects/{project}/cluster_agents"},
	"github":  {"repos": "/user/repos?per_page=100&sort=updated", "runs": "/repos/{repo}/actions/runs?per_page=50", "jobs": "/repos/{repo}/actions/runs/{run}/jobs", "deployments": "/repos/{repo}/deployments"},
}

// CallAPI performs a preset or raw API call against an HTTP integration.
func (s *Service) CallAPI(ctx context.Context, c Connection, endpoint string, params map[string]string, method string, body []byte) (json.RawMessage, error) {
	path := endpoint
	if p, ok := APIPresets[c.Kind][endpoint]; ok {
		path = p
	}
	for k, v := range params {
		val := url.PathEscape(v)
		if k == "repo" || k == "path" || k == "project" {
			val = strings.ReplaceAll(url.PathEscape(v), "%2F", "/")
			if c.Kind == "gitlab" && k == "project" {
				val = url.PathEscape(v)
			}
		}
		path = strings.ReplaceAll(path, "{"+k+"}", val)
	}
	base := strings.TrimRight(c.URL, "/")
	if base == "" && c.Kind == "github" {
		base = "https://api.github.com"
	}
	if method == "" {
		method = "GET"
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	switch c.Kind {
	case "vault":
		req.Header.Set("X-Vault-Token", c.Secret)
		if ns := c.Extra["namespace"]; ns != "" {
			req.Header.Set("X-Vault-Namespace", ns)
		}
	case "gitlab":
		req.Header.Set("PRIVATE-TOKEN", c.Secret)
	case "github":
		req.Header.Set("Authorization", "Bearer "+c.Secret)
		req.Header.Set("Accept", "application/vnd.github+json")
	default: // harbor, jenkins: basic auth
		req.SetBasicAuth(c.User, c.Secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if !json.Valid(b) {
		b, _ = json.Marshal(map[string]string{"text": string(b)})
	}
	return b, nil
}

// TFResource is a resource in a Terraform state.
type TFResource struct {
	Address  string         `json:"address"`
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Provider string         `json:"provider"`
	Module   string         `json:"module,omitempty"`
	Mode     string         `json:"mode"`
	ID       string         `json:"id,omitempty"`
	Attrs    map[string]any `json:"attributes,omitempty"`
}

// TerraformState parses a state from a local file, an s3:// URL or a
// Kubernetes secret backend (k8s://<cluster>/<namespace>/<secret>).
func (s *Service) TerraformState(ctx context.Context, source string) ([]TFResource, map[string]any, error) {
	var raw []byte
	var err error
	switch {
	case strings.HasPrefix(source, "s3://"):
		res, rerr := s.Tools.Run(ctx, "", "aws", []string{"s3", "cp", source, "-"}, nil)
		if rerr != nil {
			return nil, nil, rerr
		}
		raw = []byte(res.Stdout)
	case strings.HasPrefix(source, "k8s://"):
		parts := strings.SplitN(strings.TrimPrefix(source, "k8s://"), "/", 3)
		if len(parts) != 3 {
			return nil, nil, fmt.Errorf("use k8s://<clusterId>/<namespace>/<secret>")
		}
		typed, terr := s.Clusters.Typed(parts[0])
		if terr != nil {
			return nil, nil, terr
		}
		sec, gerr := typed.CoreV1().Secrets(parts[1]).Get(ctx, parts[2], metav1.GetOptions{})
		if gerr != nil {
			return nil, nil, gerr
		}
		raw, err = gunzip(sec.Data["tfstate"])
		if err != nil {
			return nil, nil, err
		}
	default:
		raw, err = os.ReadFile(source)
		if err != nil {
			return nil, nil, err
		}
	}
	var st struct {
		Version          int            `json:"version"`
		TerraformVersion string         `json:"terraform_version"`
		Serial           int            `json:"serial"`
		Outputs          map[string]any `json:"outputs"`
		Resources        []struct {
			Module    string `json:"module"`
			Mode      string `json:"mode"`
			Type      string `json:"type"`
			Name      string `json:"name"`
			Provider  string `json:"provider"`
			Instances []struct {
				IndexKey   any            `json:"index_key"`
				Attributes map[string]any `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, nil, err
	}
	var out []TFResource
	for _, r := range st.Resources {
		for _, in := range r.Instances {
			addr := r.Type + "." + r.Name
			if r.Mode == "data" {
				addr = "data." + addr
			}
			if r.Module != "" {
				addr = r.Module + "." + addr
			}
			if in.IndexKey != nil {
				addr += fmt.Sprintf("[%v]", in.IndexKey)
			}
			id, _ := in.Attributes["id"].(string)
			attrs := in.Attributes
			for k := range attrs {
				lk := strings.ToLower(k)
				if strings.Contains(lk, "password") || strings.Contains(lk, "secret") || strings.Contains(lk, "token") || strings.Contains(lk, "private_key") {
					attrs[k] = "(sensitive)"
				}
			}
			out = append(out, TFResource{Address: addr, Type: r.Type, Name: r.Name, Provider: r.Provider, Module: r.Module, Mode: r.Mode, ID: id, Attrs: attrs})
		}
	}
	meta := map[string]any{"version": st.Version, "terraformVersion": st.TerraformVersion, "serial": st.Serial, "outputs": redactOutputs(st.Outputs)}
	return out, meta, nil
}

func redactOutputs(o map[string]any) map[string]any {
	for k, v := range o {
		if m, ok := v.(map[string]any); ok && m["sensitive"] == true {
			m["value"] = "(sensitive)"
			o[k] = m
		}
	}
	return o
}

// Playbooks lists Ansible playbooks in a directory.
func Playbooks(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "roles" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && (strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml")) {
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), "hosts:") {
				out = append(out, p)
			}
		}
		return nil
	})
	return out, err
}

// RunPlaybook runs ansible-playbook (optionally --check).
func (s *Service) RunPlaybook(ctx context.Context, playbook, inventory string, check bool, extraVars string) (string, error) {
	args := []string{playbook}
	if inventory != "" {
		args = append(args, "-i", inventory)
	}
	if check {
		args = append(args, "--check", "--diff")
	}
	if extraVars != "" {
		args = append(args, "-e", extraVars)
	}
	res, err := s.Tools.Run(ctx, "", "ansible-playbook", args, nil, "ANSIBLE_FORCE_COLOR=0")
	if res != nil {
		return res.Stdout + res.Stderr, err
	}
	return "", err
}

// Git runs a git subcommand in a repository directory.
func (s *Service) Git(ctx context.Context, dir string, args ...string) (string, error) {
	res, err := s.Tools.Run(ctx, "", "git", append([]string{"-C", dir}, args...), nil)
	if res != nil {
		return res.Stdout + res.Stderr, err
	}
	return "", err
}

// Pricing is the hourly price per vCPU, GiB of RAM and GiB of storage.
type Pricing struct {
	CPUHour      float64 `json:"cpuHour"`
	MemGiBHour   float64 `json:"memGiBHour"`
	DiskGiBMonth float64 `json:"diskGiBMonth"`
	LBMonth      float64 `json:"lbMonth"`
	Currency     string  `json:"currency"`
}

// DefaultPricing approximates on-demand public cloud prices.
var DefaultPricing = Pricing{CPUHour: 0.031611, MemGiBHour: 0.004237, DiskGiBMonth: 0.10, LBMonth: 18, Currency: "USD"}

// CostLine is the monthly cost of a namespace or workload.
type CostLine struct {
	Namespace string  `json:"namespace"`
	Workload  string  `json:"workload,omitempty"`
	CPUCores  float64 `json:"cpuCores"`
	MemGiB    float64 `json:"memGiB"`
	DiskGiB   float64 `json:"diskGiB"`
	LBs       int     `json:"loadBalancers"`
	Monthly   float64 `json:"monthly"`
}

// CostReport is a cost dashboard.
type CostReport struct {
	Currency   string     `json:"currency"`
	Total      float64    `json:"total"`
	Namespaces []CostLine `json:"namespaces"`
	Workloads  []CostLine `json:"workloads"`
	Idle       []string   `json:"idle"`
	Pricing    Pricing    `json:"pricing"`
}

// Cost estimates monthly cost from resource requests, PVCs and load balancers.
func (s *Service) Cost(ctx context.Context, clusterID string) (*CostReport, error) {
	var p Pricing
	if err := s.Store.Load("pricing", &p); err != nil || p.CPUHour == 0 {
		p = DefaultPricing
	}
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	pods, err := typed.CoreV1().Pods("").List(ctx, metav1.ListOptions{ResourceVersion: "0"})
	if err != nil {
		return nil, err
	}
	const hours = 730.0
	ns := map[string]*CostLine{}
	wl := map[string]*CostLine{}
	line := func(m map[string]*CostLine, key, n, w string) *CostLine {
		if m[key] == nil {
			m[key] = &CostLine{Namespace: n, Workload: w}
		}
		return m[key]
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		var cpu, mem float64
		for _, c := range pod.Spec.Containers {
			cpu += float64(c.Resources.Requests.Cpu().MilliValue()) / 1000
			mem += float64(c.Resources.Requests.Memory().Value()) / (1 << 30)
		}
		owner := pod.Name
		if o := metav1.GetControllerOf(&pod); o != nil {
			owner = o.Kind + "/" + o.Name
			if o.Kind == "ReplicaSet" {
				if i := strings.LastIndex(o.Name, "-"); i > 0 {
					owner = "Deployment/" + o.Name[:i]
				}
			}
		}
		for _, l := range []*CostLine{line(ns, pod.Namespace, pod.Namespace, ""), line(wl, pod.Namespace+"/"+owner, pod.Namespace, owner)} {
			l.CPUCores += cpu
			l.MemGiB += mem
			l.Monthly += (cpu*p.CPUHour + mem*p.MemGiBHour) * hours
		}
	}
	rep := &CostReport{Currency: p.Currency, Pricing: p}
	if pvcs, err := typed.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{}); err == nil {
		used := map[string]bool{}
		for _, pod := range pods.Items {
			for _, v := range pod.Spec.Volumes {
				if v.PersistentVolumeClaim != nil {
					used[pod.Namespace+"/"+v.PersistentVolumeClaim.ClaimName] = true
				}
			}
		}
		for _, pvc := range pvcs.Items {
			gib := float64(pvc.Spec.Resources.Requests.Storage().Value()) / (1 << 30)
			l := line(ns, pvc.Namespace, pvc.Namespace, "")
			l.DiskGiB += gib
			l.Monthly += gib * p.DiskGiBMonth
			if !used[pvc.Namespace+"/"+pvc.Name] {
				rep.Idle = append(rep.Idle, fmt.Sprintf("PVC %s/%s (%.0f GiB) is not mounted by any pod", pvc.Namespace, pvc.Name, gib))
			}
		}
	}
	if svcs, err := typed.CoreV1().Services("").List(ctx, metav1.ListOptions{}); err == nil {
		for _, svc := range svcs.Items {
			if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
				l := line(ns, svc.Namespace, svc.Namespace, "")
				l.LBs++
				l.Monthly += p.LBMonth
				if len(svc.Spec.Selector) == 0 {
					rep.Idle = append(rep.Idle, fmt.Sprintf("LoadBalancer %s/%s has no selector", svc.Namespace, svc.Name))
				}
			}
		}
	}
	for _, l := range ns {
		rep.Namespaces = append(rep.Namespaces, *l)
		rep.Total += l.Monthly
	}
	for _, l := range wl {
		rep.Workloads = append(rep.Workloads, *l)
	}
	sort.Slice(rep.Namespaces, func(i, j int) bool { return rep.Namespaces[i].Monthly > rep.Namespaces[j].Monthly })
	sort.Slice(rep.Workloads, func(i, j int) bool { return rep.Workloads[i].Monthly > rep.Workloads[j].Monthly })
	if len(rep.Workloads) > 200 {
		rep.Workloads = rep.Workloads[:200]
	}
	return rep, nil
}

// SetPricing saves pricing.
func (s *Service) SetPricing(p Pricing) error { return s.Store.Save("pricing", p) }

var veleroGroup = "velero.io"

// Velero lists backups, restores and schedules.
func (s *Service) Velero(ctx context.Context, clusterID string) (map[string][]map[string]any, error) {
	dyn, err := s.Clusters.Dynamic(clusterID)
	if err != nil {
		return nil, err
	}
	out := map[string][]map[string]any{}
	for _, r := range []string{"backups", "restores", "schedules", "backupstoragelocations"} {
		l, err := dyn.Resource(schema.GroupVersionResource{Group: veleroGroup, Version: "v1", Resource: r}).List(ctx, metav1.ListOptions{})
		if err != nil {
			if r == "backups" {
				return nil, fmt.Errorf("velero not installed: %w", err)
			}
			continue
		}
		for _, it := range l.Items {
			phase, _, _ := unstructured.NestedString(it.Object, "status", "phase")
			spec, _, _ := unstructured.NestedMap(it.Object, "spec")
			out[r] = append(out[r], map[string]any{"name": it.GetName(), "namespace": it.GetNamespace(), "phase": phase, "created": it.GetCreationTimestamp().Time, "spec": spec,
				"errors": nested(it.Object, "status", "errors"), "warnings": nested(it.Object, "status", "warnings"), "completion": nested(it.Object, "status", "completionTimestamp")})
		}
	}
	return out, nil
}

func nested(o map[string]any, path ...string) any {
	v, _, _ := unstructured.NestedFieldNoCopy(o, path...)
	return v
}

// VeleroCreate creates a Backup (namespaces) or a Restore (from backup).
func (s *Service) VeleroCreate(ctx context.Context, clusterID, kind, name string, namespaces []string, fromBackup string) error {
	dyn, err := s.Clusters.Dynamic(clusterID)
	if err != nil {
		return err
	}
	spec := map[string]any{}
	resource := "backups"
	if kind == "restore" {
		resource = "restores"
		spec["backupName"] = fromBackup
		if len(namespaces) > 0 {
			spec["includedNamespaces"] = namespaces
		}
	} else {
		if len(namespaces) > 0 {
			spec["includedNamespaces"] = namespaces
		}
		spec["ttl"] = "720h0m0s"
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "velero.io/v1", "kind": map[string]string{"backups": "Backup", "restores": "Restore"}[resource],
		"metadata": map[string]any{"name": name, "namespace": "velero"}, "spec": spec,
	}}
	_, err = dyn.Resource(schema.GroupVersionResource{Group: veleroGroup, Version: "v1", Resource: resource}).Namespace("velero").Create(ctx, obj, metav1.CreateOptions{})
	return err
}
