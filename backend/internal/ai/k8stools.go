package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/gitops"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/helm"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/metrics"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/pods"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/resources"
)

// Services bundles what the Kubernetes tools need.
type Services struct {
	Resources *resources.Service
	Pods      *pods.Service
	Metrics   *metrics.Service
	Helm      *helm.Service
	GitOps    *gitops.Service
}

func prop(t, desc string) map[string]any { return map[string]any{"type": t, "description": desc} }

// KubernetesTools returns the built-in tool set bound to a cluster.
func KubernetesTools(s Services, clusterID, defaultNS string) []Tool {
	ns := func(v string) string {
		if v == "" {
			return defaultNS
		}
		return v
	}
	return []Tool{
		{
			Name:        "list_api_resources",
			Description: "List every resource type the cluster serves, including CRDs. Use to discover custom resources.",
			Properties:  map[string]any{},
			Run: func(ctx context.Context, _ json.RawMessage) (string, error) {
				rs, err := s.Resources.Discover(ctx, clusterID)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for _, r := range rs {
					fmt.Fprintf(&b, "%s\t%s/%s\tnamespaced=%v\n", r.Resource, r.Group, r.Version, r.Namespaced)
				}
				return b.String(), nil
			},
		},
		{
			Name:        "list_resources",
			Description: "List objects of a kind (e.g. pods, deployments, nodes, or any CRD plural) with status columns. Namespace empty means all namespaces.",
			Properties: map[string]any{
				"kind": prop("string", "Resource plural, kind or short name"), "namespace": prop("string", "Namespace, empty for all"),
				"labelSelector": prop("string", "Optional label selector"),
			},
			Required: []string{"kind"},
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct{ Kind, Namespace, LabelSelector string }
				_ = json.Unmarshal(in, &a)
				r, err := s.Resources.Resolve(ctx, clusterID, a.Kind)
				if err != nil {
					return "", err
				}
				l, err := s.Resources.List(ctx, clusterID, r.GVR(), resources.ListOptions{Namespace: a.Namespace, LabelSelector: a.LabelSelector, Limit: 500})
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for i := range l.Items {
					row := resources.Summarize(&l.Items[i])
					cells, _ := json.Marshal(row.Cells)
					fmt.Fprintf(&b, "%s/%s status=%s age=%s %s\n", row.Namespace, row.Name, row.Status, time.Since(row.Created).Round(time.Minute), cells)
				}
				if b.Len() == 0 {
					return "no objects found", nil
				}
				return b.String(), nil
			},
		},
		{
			Name:        "get_resource",
			Description: "Get the full YAML (spec and status) of one object.",
			Properties:  map[string]any{"kind": prop("string", "Resource kind"), "namespace": prop("string", "Namespace"), "name": prop("string", "Object name")},
			Required:    []string{"kind", "name"},
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct{ Kind, Namespace, Name string }
				_ = json.Unmarshal(in, &a)
				r, err := s.Resources.Resolve(ctx, clusterID, a.Kind)
				if err != nil {
					return "", err
				}
				n := ""
				if r.Namespaced {
					n = ns(a.Namespace)
				}
				obj, err := s.Resources.Get(ctx, clusterID, r.GVR(), n, a.Name)
				if err != nil {
					return "", err
				}
				o := obj.DeepCopy()
				o.SetManagedFields(nil)
				if o.GetKind() == "Secret" {
					delete(o.Object, "data")
					delete(o.Object, "stringData")
				}
				return resources.ToYAML(o.Object)
			},
		},
		{
			Name:        "get_events",
			Description: "Get Kubernetes events, optionally for a specific object, in chronological order.",
			Properties:  map[string]any{"namespace": prop("string", "Namespace (empty = all)"), "kind": prop("string", "Involved object kind, e.g. Pod"), "name": prop("string", "Involved object name")},
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct{ Namespace, Kind, Name string }
				_ = json.Unmarshal(in, &a)
				evs, err := s.Resources.Timeline(ctx, clusterID, a.Namespace, a.Kind, a.Name)
				if err != nil {
					return "", err
				}
				if len(evs) > 200 {
					evs = evs[len(evs)-200:]
				}
				var b strings.Builder
				for _, e := range evs {
					fmt.Fprintf(&b, "%s %s %s %s/%s x%d: %s\n", e.Time.Format(time.RFC3339), e.Type, e.Reason, e.Kind, e.Name, e.Count, e.Message)
				}
				if b.Len() == 0 {
					return "no events", nil
				}
				return b.String(), nil
			},
		},
		{
			Name:        "get_pod_logs",
			Description: "Fetch the last lines of a pod's logs. Set previous=true for the crashed previous container instance.",
			Properties: map[string]any{
				"namespace": prop("string", "Namespace"), "pod": prop("string", "Pod name"), "container": prop("string", "Container (empty = all)"),
				"previous": prop("boolean", "Logs of the previous terminated container"), "tail": prop("integer", "Number of lines (default 200)"),
				"filter": prop("string", "Optional regex filter"),
			},
			Required: []string{"pod"},
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct {
					Namespace, Pod, Container, Filter string
					Previous                          bool
					Tail                              int64
				}
				_ = json.Unmarshal(in, &a)
				if a.Tail <= 0 {
					a.Tail = 200
				}
				var b strings.Builder
				err := s.Pods.StreamLogs(ctx, clusterID, pods.LogOptions{Namespace: ns(a.Namespace), Pods: []string{a.Pod}, Container: a.Container, Previous: a.Previous, TailLines: a.Tail, Filter: a.Filter, Regex: a.Filter != ""}, func(l pods.LogLine) {
					fmt.Fprintf(&b, "[%s] %s\n", l.Container, l.Line)
				})
				if err != nil {
					return "", err
				}
				if b.Len() == 0 {
					return "no log lines", nil
				}
				return b.String(), nil
			},
		},
		{
			Name:        "get_usage",
			Description: "Current CPU/memory usage (metrics-server) for pods in a namespace, or for nodes when namespace is '_nodes'.",
			Properties:  map[string]any{"namespace": prop("string", "Namespace, or _nodes")},
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct{ Namespace string }
				_ = json.Unmarshal(in, &a)
				var us []metrics.Usage
				var err error
				if a.Namespace == "_nodes" {
					us, err = s.Metrics.NodeUsage(ctx, clusterID)
				} else {
					us, err = s.Metrics.PodUsage(ctx, clusterID, a.Namespace)
				}
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for _, u := range us {
					fmt.Fprintf(&b, "%s/%s cpu=%dm mem=%dMi\n", u.Namespace, u.Name, u.CPUMilli, u.MemoryByte/(1<<20))
				}
				return b.String(), nil
			},
		},
		{
			Name:        "promql_query",
			Description: "Run an instant PromQL query against the cluster's Prometheus/Mimir/VictoriaMetrics.",
			Properties:  map[string]any{"query": prop("string", "PromQL expression")},
			Required:    []string{"query"},
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct{ Query string }
				_ = json.Unmarshal(in, &a)
				out, err := s.Metrics.PromQuery(ctx, clusterID, a.Query, time.Time{}, time.Time{}, 0)
				return string(out), err
			},
		},
		{
			Name:        "loki_query",
			Description: "Run a LogQL query against Loki over the last hour.",
			Properties:  map[string]any{"query": prop("string", "LogQL expression"), "limit": prop("integer", "Max lines (default 100)")},
			Required:    []string{"query"},
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct {
					Query string
					Limit int
				}
				_ = json.Unmarshal(in, &a)
				if a.Limit <= 0 {
					a.Limit = 100
				}
				out, err := s.Metrics.LokiQuery(ctx, clusterID, a.Query, time.Now().Add(-time.Hour), time.Now(), a.Limit)
				return string(out), err
			},
		},
		{
			Name:        "helm_releases",
			Description: "List Helm releases in the cluster with status and chart versions.",
			Properties:  map[string]any{"namespace": prop("string", "Namespace (empty = all)")},
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct{ Namespace string }
				_ = json.Unmarshal(in, &a)
				rs, err := s.Helm.Releases(ctx, clusterID, a.Namespace)
				if err != nil {
					return "", err
				}
				b, _ := json.Marshal(rs)
				return string(b), nil
			},
		},
		{
			Name:        "gitops_apps",
			Description: "List Flux and Argo CD applications with health, sync status, revision, drift and error messages.",
			Properties:  map[string]any{},
			Run: func(ctx context.Context, _ json.RawMessage) (string, error) {
				apps, err := s.GitOps.Apps(ctx, clusterID)
				if err != nil {
					return "", err
				}
				b, _ := json.Marshal(apps)
				return string(b), nil
			},
		},
		{
			Name:        "apply_yaml",
			Description: "Apply YAML to the cluster with server-side apply. Use dryRun=true first to validate.",
			Properties:  map[string]any{"yaml": prop("string", "Manifest(s) to apply"), "dryRun": prop("boolean", "Validate only")},
			Required:    []string{"yaml"},
			Write:       true,
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct {
					YAML   string `json:"yaml"`
					DryRun bool   `json:"dryRun"`
				}
				_ = json.Unmarshal(in, &a)
				res, err := s.Resources.Apply(ctx, clusterID, a.YAML, resources.ApplyOptions{ServerSide: true, Force: true, DryRun: a.DryRun})
				if err != nil {
					return "", err
				}
				b, _ := json.Marshal(res)
				return string(b), nil
			},
		},
		{
			Name:        "scale_workload",
			Description: "Scale a Deployment/StatefulSet/ReplicaSet.",
			Properties:  map[string]any{"kind": prop("string", "Workload kind"), "namespace": prop("string", "Namespace"), "name": prop("string", "Name"), "replicas": prop("integer", "Replica count")},
			Required:    []string{"kind", "name", "replicas"},
			Write:       true,
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct {
					Kind, Namespace, Name string
					Replicas              int32
				}
				_ = json.Unmarshal(in, &a)
				r, err := s.Resources.Resolve(ctx, clusterID, a.Kind)
				if err != nil {
					return "", err
				}
				return "scaled", s.Resources.Scale(ctx, clusterID, r.GVR(), ns(a.Namespace), a.Name, a.Replicas)
			},
		},
		{
			Name:        "restart_workload",
			Description: "Rollout restart a Deployment/StatefulSet/DaemonSet.",
			Properties:  map[string]any{"kind": prop("string", "Workload kind"), "namespace": prop("string", "Namespace"), "name": prop("string", "Name")},
			Required:    []string{"kind", "name"},
			Write:       true,
			Run: func(ctx context.Context, in json.RawMessage) (string, error) {
				var a struct{ Kind, Namespace, Name string }
				_ = json.Unmarshal(in, &a)
				r, err := s.Resources.Resolve(ctx, clusterID, a.Kind)
				if err != nil {
					return "", err
				}
				return "restarted", s.Resources.Restart(ctx, clusterID, r.GVR(), ns(a.Namespace), a.Name)
			},
		},
	}
}
