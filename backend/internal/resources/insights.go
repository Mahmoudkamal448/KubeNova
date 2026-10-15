package resources

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GraphNode is a vertex in the topology / dependency graph.
type GraphNode struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Health    string `json:"health"`
	Status    string `json:"status"`
}

// GraphEdge connects two nodes.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"` // owns | selects | routes | mounts | uses
}

// Graph is a topology graph.
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

var topologyTypes = []schema.GroupVersionResource{
	{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
	{Version: "v1", Resource: "services"},
	{Group: "apps", Version: "v1", Resource: "deployments"},
	{Group: "apps", Version: "v1", Resource: "statefulsets"},
	{Group: "apps", Version: "v1", Resource: "daemonsets"},
	{Group: "apps", Version: "v1", Resource: "replicasets"},
	{Group: "batch", Version: "v1", Resource: "cronjobs"},
	{Group: "batch", Version: "v1", Resource: "jobs"},
	{Version: "v1", Resource: "pods"},
	{Version: "v1", Resource: "configmaps"},
	{Version: "v1", Resource: "secrets"},
	{Version: "v1", Resource: "persistentvolumeclaims"},
	{Version: "v1", Resource: "serviceaccounts"},
	{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"},
}

// Topology builds a live dependency graph for a namespace: ownership chains,
// service selectors, ingress/HTTPRoute backends and pod volume/config usage.
func (s *Service) Topology(ctx context.Context, clusterID, ns string) (*Graph, error) {
	g := &Graph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	nodeID := func(kind, ns, name string) string { return kind + "/" + ns + "/" + name }
	type item struct {
		row Row
		obj map[string]any
		uid string
	}
	var items []item
	byUID := map[string]string{}
	for _, gvr := range topologyTypes {
		l, err := s.List(ctx, clusterID, gvr, ListOptions{Namespace: ns})
		if err != nil {
			continue
		}
		for i := range l.Items {
			u := &l.Items[i]
			r := Summarize(u)
			id := nodeID(u.GetKind(), u.GetNamespace(), u.GetName())
			byUID[string(u.GetUID())] = id
			items = append(items, item{r, u.Object, string(u.GetUID())})
			g.Nodes = append(g.Nodes, GraphNode{ID: id, Kind: u.GetKind(), Name: u.GetName(), Namespace: u.GetNamespace(), Health: r.Health, Status: r.Status})
		}
	}
	exists := map[string]bool{}
	for _, n := range g.Nodes {
		exists[n.ID] = true
	}
	add := func(from, to, t string) {
		if exists[from] && exists[to] {
			g.Edges = append(g.Edges, GraphEdge{From: from, To: to, Type: t})
		}
	}
	for _, it := range items {
		id := nodeID(it.row.Kind, it.row.Namespace, it.row.Name)
		// Owner references.
		for _, o := range slice(it.obj, "metadata", "ownerReferences") {
			m, _ := o.(map[string]any)
			if owner, ok := byUID[str(m, "uid")]; ok {
				add(owner, id, "owns")
			}
		}
		switch it.row.Kind {
		case "Service":
			sel, _, _ := nestedStringMap(it.obj, "spec", "selector")
			if len(sel) == 0 {
				continue
			}
			ls := labels.SelectorFromSet(sel)
			for _, p := range items {
				if p.row.Kind == "Pod" && p.row.Namespace == it.row.Namespace && ls.Matches(labels.Set(p.row.Labels)) {
					add(id, nodeID("Pod", p.row.Namespace, p.row.Name), "selects")
				}
			}
		case "Ingress":
			for _, rule := range slice(it.obj, "spec", "rules") {
				m, _ := rule.(map[string]any)
				for _, p := range slice(m, "http", "paths") {
					pm, _ := p.(map[string]any)
					add(id, nodeID("Service", it.row.Namespace, str(pm, "backend", "service", "name")), "routes")
				}
			}
			if d := str(it.obj, "spec", "defaultBackend", "service", "name"); d != "" {
				add(id, nodeID("Service", it.row.Namespace, d), "routes")
			}
		case "HTTPRoute":
			for _, rule := range slice(it.obj, "spec", "rules") {
				m, _ := rule.(map[string]any)
				for _, b := range slice(m, "backendRefs") {
					bm, _ := b.(map[string]any)
					add(id, nodeID("Service", it.row.Namespace, str(bm, "name")), "routes")
				}
			}
		case "Pod":
			for _, v := range slice(it.obj, "spec", "volumes") {
				m, _ := v.(map[string]any)
				if n := str(m, "configMap", "name"); n != "" {
					add(id, nodeID("ConfigMap", it.row.Namespace, n), "mounts")
				}
				if n := str(m, "secret", "secretName"); n != "" {
					add(id, nodeID("Secret", it.row.Namespace, n), "mounts")
				}
				if n := str(m, "persistentVolumeClaim", "claimName"); n != "" {
					add(id, nodeID("PersistentVolumeClaim", it.row.Namespace, n), "mounts")
				}
			}
			if sa := str(it.obj, "spec", "serviceAccountName"); sa != "" {
				add(id, nodeID("ServiceAccount", it.row.Namespace, sa), "uses")
			}
		}
	}
	// Drop isolated config objects to keep the graph readable.
	connected := map[string]bool{}
	for _, e := range g.Edges {
		connected[e.From], connected[e.To] = true, true
	}
	nodes := []GraphNode{}
	for _, n := range g.Nodes {
		switch n.Kind {
		case "ConfigMap", "Secret", "ServiceAccount":
			if !connected[n.ID] {
				continue
			}
		}
		nodes = append(nodes, n)
	}
	g.Nodes = nodes
	return g, nil
}

func nestedStringMap(obj map[string]any, path ...string) (map[string]string, bool, error) {
	v, _, _ := unstructuredNested(obj, path...)
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false, nil
	}
	out := map[string]string{}
	for k, val := range m {
		out[k] = fmt.Sprint(val)
	}
	return out, true, nil
}

func unstructuredNested(obj map[string]any, path ...string) (any, bool, error) {
	var cur any = obj
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		cur, ok = m[p]
		if !ok {
			return nil, false, nil
		}
	}
	return cur, true, nil
}

// SearchHit is a multi-cluster search result.
type SearchHit struct {
	ClusterID   string `json:"clusterId"`
	ClusterName string `json:"clusterName"`
	Group       string `json:"group"`
	Version     string `json:"version"`
	Resource    string `json:"resource"`
	Row         Row    `json:"row"`
}

// SearchAcross searches resource names (and labels) across many clusters in
// parallel. kinds defaults to common workload types.
func (s *Service) SearchAcross(ctx context.Context, clusterIDs []string, query string, kinds []string) []SearchHit {
	if len(kinds) == 0 {
		kinds = []string{"pods", "deployments", "statefulsets", "daemonsets", "services", "ingresses", "configmaps", "secrets", "nodes", "namespaces"}
	}
	q := strings.ToLower(query)
	var mu sync.Mutex
	hits := []SearchHit{}
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, cid := range clusterIDs {
		cl, err := s.Clusters.Get(cid)
		if err != nil {
			continue
		}
		wg.Add(1)
		go func(cid, cname string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			for _, k := range kinds {
				r, err := s.Resolve(cctx, cid, k)
				if err != nil {
					continue
				}
				l, err := s.List(cctx, cid, r.GVR(), ListOptions{})
				if err != nil {
					continue
				}
				for i := range l.Items {
					u := &l.Items[i]
					match := strings.Contains(strings.ToLower(u.GetName()), q)
					if !match {
						for k, v := range u.GetLabels() {
							if strings.Contains(strings.ToLower(k+"="+v), q) {
								match = true
								break
							}
						}
					}
					if match {
						mu.Lock()
						hits = append(hits, SearchHit{ClusterID: cid, ClusterName: cname, Group: r.Group, Version: r.Version, Resource: r.Resource, Row: Summarize(u)})
						mu.Unlock()
					}
				}
			}
		}(cid, cl.Name)
	}
	wg.Wait()
	sort.Slice(hits, func(i, j int) bool { return hits[i].Row.Name < hits[j].Row.Name })
	if len(hits) > 2000 {
		hits = hits[:2000]
	}
	return hits
}

// TimelineEntry is an event on the resource timeline.
type TimelineEntry struct {
	Time      time.Time `json:"time"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Count     int32     `json:"count"`
	Source    string    `json:"source"`
}

// Timeline returns events for a namespace (or an individual object) ordered
// chronologically; used by the resource timeline and event replay views.
func (s *Service) Timeline(ctx context.Context, clusterID, ns, kind, name string) ([]TimelineEntry, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	var fs []string
	if kind != "" {
		fs = append(fs, "involvedObject.kind="+kind)
	}
	if name != "" {
		fs = append(fs, "involvedObject.name="+name)
	}
	evs, err := typed.CoreV1().Events(ns).List(ctx, metav1.ListOptions{FieldSelector: strings.Join(fs, ",")})
	if err != nil {
		return nil, err
	}
	out := make([]TimelineEntry, 0, len(evs.Items))
	for _, e := range evs.Items {
		out = append(out, TimelineEntry{Time: eventTime(e), Type: e.Type, Reason: e.Reason, Message: e.Message,
			Kind: e.InvolvedObject.Kind, Name: e.InvolvedObject.Name, Namespace: e.Namespace, Count: e.Count, Source: e.Source.Component + e.ReportingController})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

func eventTime(e corev1.Event) time.Time {
	switch {
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	case !e.FirstTimestamp.IsZero():
		return e.FirstTimestamp.Time
	}
	return e.CreationTimestamp.Time
}

// CompareEntry describes how one object differs between two clusters.
type CompareEntry struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	State     string `json:"state"` // same | different | onlyA | onlyB
	Diff      string `json:"diff,omitempty"`
}

// Compare compares objects of a type in a namespace across two clusters.
func (s *Service) Compare(ctx context.Context, a, b, resource, ns string) ([]CompareEntry, error) {
	ra, err := s.Resolve(ctx, a, resource)
	if err != nil {
		return nil, err
	}
	la, err := s.List(ctx, a, ra.GVR(), ListOptions{Namespace: ns})
	if err != nil {
		return nil, err
	}
	rb, err := s.Resolve(ctx, b, resource)
	if err != nil {
		return nil, err
	}
	lb, err := s.List(ctx, b, rb.GVR(), ListOptions{Namespace: ns})
	if err != nil {
		return nil, err
	}
	key := func(ns, n string) string { return ns + "/" + n }
	out := []CompareEntry{}
	mb := map[string]string{}
	for i := range lb.Items {
		y, _ := ToYAML(CleanForExport(&lb.Items[i]).Object)
		mb[key(lb.Items[i].GetNamespace(), lb.Items[i].GetName())] = y
	}
	for i := range la.Items {
		u := &la.Items[i]
		k := key(u.GetNamespace(), u.GetName())
		ya, _ := ToYAML(CleanForExport(u).Object)
		e := CompareEntry{Kind: u.GetKind(), Namespace: u.GetNamespace(), Name: u.GetName()}
		if yb, ok := mb[k]; ok {
			delete(mb, k)
			if d := DiffObjects(ya, yb); d != "" {
				e.State, e.Diff = "different", d
			} else {
				e.State = "same"
			}
		} else {
			e.State = "onlyA"
		}
		out = append(out, e)
	}
	for k := range mb {
		parts := strings.SplitN(k, "/", 2)
		out = append(out, CompareEntry{Kind: ra.Kind, Namespace: parts[0], Name: parts[1], State: "onlyB"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
