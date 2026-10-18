// Package metrics integrates metrics-server and Prometheus compatible backends
// (Prometheus, Mimir, VictoriaMetrics, Thanos) plus Loki, Tempo, Alertmanager
// and Grafana, either through in-cluster service proxies or direct URLs.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/mahmoudkamal448/kubenova/backend/internal/clusters"
	"github.com/mahmoudkamal448/kubenova/backend/internal/store"
)

// Backend kinds.
const (
	Prometheus      = "prometheus"
	Mimir           = "mimir"
	VictoriaMetrics = "victoriametrics"
	Loki            = "loki"
	Tempo           = "tempo"
	Alertmanager    = "alertmanager"
	Grafana         = "grafana"
)

// Endpoint configures how to reach an observability backend for a cluster.
// Either URL (direct) or Namespace/Service/Port (Kubernetes API proxy).
type Endpoint struct {
	Kind      string `json:"kind"`
	URL       string `json:"url,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Service   string `json:"service,omitempty"`
	Port      string `json:"port,omitempty"`
	Path      string `json:"path,omitempty"` // e.g. /prometheus for Mimir
	Token     string `json:"token,omitempty"`
	OrgID     string `json:"orgId,omitempty"` // X-Scope-OrgID for Mimir/Loki/Tempo
}

// Service provides metrics.
type Service struct {
	Clusters *clusters.Manager
	Store    *store.Store
}

func (s *Service) endpoints(clusterID string) map[string]Endpoint {
	m := map[string]Endpoint{}
	_ = s.Store.Load("observability/"+clusterID, &m)
	return m
}

// Endpoints returns configured endpoints for a cluster.
func (s *Service) Endpoints(clusterID string) map[string]Endpoint { return s.endpoints(clusterID) }

// SetEndpoint saves an endpoint.
func (s *Service) SetEndpoint(clusterID string, e Endpoint) error {
	m := s.endpoints(clusterID)
	m[e.Kind] = e
	return s.Store.Save("observability/"+clusterID, m)
}

var wellKnown = map[string][][3]string{
	Prometheus: {
		{"monitoring", "prometheus-operated", "9090"}, {"monitoring", "kube-prometheus-stack-prometheus", "9090"},
		{"monitoring", "prometheus-server", "80"}, {"prometheus", "prometheus-server", "80"},
		{"cattle-monitoring-system", "rancher-monitoring-prometheus", "9090"}, {"observability", "prometheus", "9090"},
	},
	VictoriaMetrics: {{"monitoring", "vmsingle-victoria-metrics-k8s-stack", "8429"}, {"victoria-metrics", "victoria-metrics-single-server", "8428"}},
	Mimir:           {{"mimir", "mimir-nginx", "80"}, {"monitoring", "mimir-query-frontend", "8080"}},
	Loki:            {{"loki", "loki-gateway", "80"}, {"monitoring", "loki", "3100"}, {"loki", "loki", "3100"}},
	Tempo:           {{"tempo", "tempo", "3100"}, {"monitoring", "tempo", "3100"}},
	Alertmanager:    {{"monitoring", "alertmanager-operated", "9093"}, {"cattle-monitoring-system", "rancher-monitoring-alertmanager", "9093"}},
	Grafana:         {{"monitoring", "grafana", "80"}, {"monitoring", "kube-prometheus-stack-grafana", "80"}, {"cattle-monitoring-system", "rancher-monitoring-grafana", "80"}},
}

// AutoDetect probes well known in-cluster services and stores what it finds.
func (s *Service) AutoDetect(ctx context.Context, clusterID string) (map[string]Endpoint, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	found := s.endpoints(clusterID)
	for kind, cands := range wellKnown {
		if _, ok := found[kind]; ok {
			continue
		}
		for _, c := range cands {
			if _, err := typed.CoreV1().Services(c[0]).Get(ctx, c[1], metav1.GetOptions{}); err == nil {
				e := Endpoint{Kind: kind, Namespace: c[0], Service: c[1], Port: c[2]}
				if kind == Mimir {
					e.Path = "/prometheus"
				}
				found[kind] = e
				break
			}
		}
	}
	return found, s.Store.Save("observability/"+clusterID, found)
}

// Do performs an HTTP GET against a backend, through the API server service
// proxy when no direct URL is configured.
func (s *Service) Do(ctx context.Context, clusterID, kind, path string, q url.Values) ([]byte, error) {
	e, ok := s.endpoints(clusterID)[kind]
	if !ok {
		if kind == VictoriaMetrics || kind == Mimir {
			return nil, fmt.Errorf("%s endpoint not configured", kind)
		}
		return nil, fmt.Errorf("%s endpoint not configured (use auto-detect or settings)", kind)
	}
	full := e.Path + path
	if e.URL != "" {
		u := strings.TrimRight(e.URL, "/") + full
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		if e.Token != "" {
			req.Header.Set("Authorization", "Bearer "+e.Token)
		}
		if e.OrgID != "" {
			req.Header.Set("X-Scope-OrgID", e.OrgID)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("%s: %s", resp.Status, string(b))
		}
		return b, err
	}
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	req := typed.CoreV1().Services(e.Namespace).ProxyGet("http", e.Service, e.Port, full, flatten(q))
	return req.DoRaw(ctx)
}

func flatten(q url.Values) map[string]string {
	m := map[string]string{}
	for k, v := range q {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}

// PromQuery runs an instant or range PromQL query on the first available
// Prometheus compatible backend.
func (s *Service) PromQuery(ctx context.Context, clusterID, query string, start, end time.Time, step time.Duration) (json.RawMessage, error) {
	q := url.Values{"query": {query}}
	path := "/api/v1/query"
	if !start.IsZero() {
		path = "/api/v1/query_range"
		q.Set("start", fmt.Sprint(start.Unix()))
		q.Set("end", fmt.Sprint(end.Unix()))
		q.Set("step", fmt.Sprintf("%ds", int(step.Seconds())))
	}
	var lastErr error
	for _, kind := range []string{Prometheus, Mimir, VictoriaMetrics} {
		b, err := s.Do(ctx, clusterID, kind, path, q)
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// LokiQuery runs a LogQL range query.
func (s *Service) LokiQuery(ctx context.Context, clusterID, query string, start, end time.Time, limit int) (json.RawMessage, error) {
	q := url.Values{"query": {query}, "start": {fmt.Sprint(start.UnixNano())}, "end": {fmt.Sprint(end.UnixNano())}, "limit": {fmt.Sprint(limit)}}
	return s.Do(ctx, clusterID, Loki, "/loki/api/v1/query_range", q)
}

// Alerts returns active alerts from Alertmanager.
func (s *Service) Alerts(ctx context.Context, clusterID string) (json.RawMessage, error) {
	return s.Do(ctx, clusterID, Alertmanager, "/api/v2/alerts", url.Values{"active": {"true"}})
}

// Trace fetches a trace from Tempo.
func (s *Service) Trace(ctx context.Context, clusterID, traceID string) (json.RawMessage, error) {
	return s.Do(ctx, clusterID, Tempo, "/api/traces/"+url.PathEscape(traceID), nil)
}

// Usage is CPU/memory usage of an object.
type Usage struct {
	Name       string  `json:"name"`
	Namespace  string  `json:"namespace,omitempty"`
	CPUMilli   int64   `json:"cpuMilli"`
	MemoryByte int64   `json:"memoryBytes"`
	Containers []Usage `json:"containers,omitempty"`
}

var (
	podMetricsGVR  = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}
	nodeMetricsGVR = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "nodes"}
)

func parseQty(v any) int64 {
	s, _ := v.(string)
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	return q.MilliValue()
}

// PodUsage returns metrics-server usage for pods in a namespace.
func (s *Service) PodUsage(ctx context.Context, clusterID, ns string) ([]Usage, error) {
	dyn, err := s.Clusters.Dynamic(clusterID)
	if err != nil {
		return nil, err
	}
	l, err := dyn.Resource(podMetricsGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("metrics-server unavailable: %w", err)
	}
	var out []Usage
	for _, it := range l.Items {
		u := Usage{Name: it.GetName(), Namespace: it.GetNamespace()}
		cs, _ := it.Object["containers"].([]any)
		for _, c := range cs {
			m, _ := c.(map[string]any)
			usage, _ := m["usage"].(map[string]any)
			cu := Usage{Name: fmt.Sprint(m["name"]), CPUMilli: parseQty(usage["cpu"]), MemoryByte: parseQty(usage["memory"]) / 1000}
			u.CPUMilli += cu.CPUMilli
			u.MemoryByte += cu.MemoryByte
			u.Containers = append(u.Containers, cu)
		}
		out = append(out, u)
	}
	return out, nil
}

// NodeUsage returns metrics-server usage for nodes.
func (s *Service) NodeUsage(ctx context.Context, clusterID string) ([]Usage, error) {
	dyn, err := s.Clusters.Dynamic(clusterID)
	if err != nil {
		return nil, err
	}
	l, err := dyn.Resource(nodeMetricsGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("metrics-server unavailable: %w", err)
	}
	var out []Usage
	for _, it := range l.Items {
		usage, _ := it.Object["usage"].(map[string]any)
		out = append(out, Usage{Name: it.GetName(), CPUMilli: parseQty(usage["cpu"]), MemoryByte: parseQty(usage["memory"]) / 1000})
	}
	return out, nil
}

// Overview aggregates cluster and namespace usage vs. capacity.
type Overview struct {
	CPUUsageMilli    int64            `json:"cpuUsageMilli"`
	CPUCapacityMilli int64            `json:"cpuCapacityMilli"`
	CPURequestsMilli int64            `json:"cpuRequestsMilli"`
	MemUsage         int64            `json:"memoryUsageBytes"`
	MemCapacity      int64            `json:"memoryCapacityBytes"`
	MemRequests      int64            `json:"memoryRequestsBytes"`
	Namespaces       map[string]Usage `json:"namespaces"`
	Nodes            []Usage          `json:"nodes"`
	Source           string           `json:"source"`
}

// ClusterOverview computes cluster level metrics.
func (s *Service) ClusterOverview(ctx context.Context, clusterID string) (*Overview, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	o := &Overview{Namespaces: map[string]Usage{}, Source: "metrics-server"}
	nodes, err := typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, n := range nodes.Items {
		o.CPUCapacityMilli += n.Status.Allocatable.Cpu().MilliValue()
		o.MemCapacity += n.Status.Allocatable.Memory().Value()
	}
	pods, err := typed.CoreV1().Pods("").List(ctx, metav1.ListOptions{ResourceVersion: "0"})
	if err == nil {
		for _, p := range pods.Items {
			if p.Status.Phase != "Running" {
				continue
			}
			for _, c := range p.Spec.Containers {
				o.CPURequestsMilli += c.Resources.Requests.Cpu().MilliValue()
				o.MemRequests += c.Resources.Requests.Memory().Value()
			}
		}
	}
	if nu, err := s.NodeUsage(ctx, clusterID); err == nil {
		o.Nodes = nu
		for _, u := range nu {
			o.CPUUsageMilli += u.CPUMilli
			o.MemUsage += u.MemoryByte
		}
	} else {
		o.Source = "unavailable"
	}
	if pu, err := s.PodUsage(ctx, clusterID, ""); err == nil {
		for _, u := range pu {
			n := o.Namespaces[u.Namespace]
			n.Name = u.Namespace
			n.CPUMilli += u.CPUMilli
			n.MemoryByte += u.MemoryByte
			o.Namespaces[u.Namespace] = n
		}
	}
	return o, nil
}

// Standard PromQL used by the built-in dashboards.
var Queries = map[string]string{
	"cluster_cpu":        `sum(rate(container_cpu_usage_seconds_total{container!="",pod!=""}[5m]))`,
	"cluster_memory":     `sum(container_memory_working_set_bytes{container!="",pod!=""})`,
	"cluster_network_rx": `sum(rate(container_network_receive_bytes_total[5m]))`,
	"cluster_network_tx": `sum(rate(container_network_transmit_bytes_total[5m]))`,
	"cluster_fs":         `sum(container_fs_usage_bytes{container!=""})`,
	"namespace_cpu":      `sum by (namespace) (rate(container_cpu_usage_seconds_total{container!="",pod!=""}[5m]))`,
	"namespace_memory":   `sum by (namespace) (container_memory_working_set_bytes{container!="",pod!=""})`,
	"node_cpu":           `1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m]))`,
	"node_memory":        `1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes`,
	"node_fs":            `1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay"} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay"}`,
	"pod_cpu":            `sum by (pod) (rate(container_cpu_usage_seconds_total{namespace="$namespace",pod=~"$pod",container!=""}[5m]))`,
	"pod_memory":         `sum by (pod) (container_memory_working_set_bytes{namespace="$namespace",pod=~"$pod",container!=""})`,
	"pod_network_rx":     `sum by (pod) (rate(container_network_receive_bytes_total{namespace="$namespace",pod=~"$pod"}[5m]))`,
	"pod_network_tx":     `sum by (pod) (rate(container_network_transmit_bytes_total{namespace="$namespace",pod=~"$pod"}[5m]))`,
	"pod_fs":             `sum by (pod) (container_fs_usage_bytes{namespace="$namespace",pod=~"$pod",container!=""})`,
}
