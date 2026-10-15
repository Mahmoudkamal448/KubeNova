// Package gitops provides native FluxCD and Argo CD support: applications,
// health, sync status, revisions, history, drift and reconcile/sync actions.
package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/clusters"
)

// App is a normalised GitOps application (Flux Kustomization/HelmRelease or
// Argo CD Application).
type App struct {
	Engine      string        `json:"engine"` // flux | argocd
	Kind        string        `json:"kind"`
	Group       string        `json:"group"`
	Version     string        `json:"version"`
	Resource    string        `json:"resource"`
	Name        string        `json:"name"`
	Namespace   string        `json:"namespace"`
	Health      string        `json:"health"`
	Sync        string        `json:"sync"`
	Revision    string        `json:"revision"`
	Source      string        `json:"source"`
	Path        string        `json:"path,omitempty"`
	Message     string        `json:"message,omitempty"`
	Suspended   bool          `json:"suspended"`
	LastApplied string        `json:"lastApplied,omitempty"`
	Drift       []DriftItem   `json:"drift,omitempty"`
	History     []HistoryItem `json:"history,omitempty"`
	DependsOn   []string      `json:"dependsOn,omitempty"`
	Inventory   []string      `json:"inventory,omitempty"`
}

// DriftItem is a resource out of sync with git.
type DriftItem struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"`
	Health    string `json:"health,omitempty"`
	Message   string `json:"message,omitempty"`
}

// HistoryItem is a past deployment.
type HistoryItem struct {
	Revision   string `json:"revision"`
	DeployedAt string `json:"deployedAt"`
	ID         int64  `json:"id,omitempty"`
}

// Service lists and controls GitOps applications.
type Service struct {
	Clusters *clusters.Manager
}

var fluxTypes = []schema.GroupVersionResource{
	{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"},
	{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"},
	{Group: "helm.toolkit.fluxcd.io", Version: "v2beta2", Resource: "helmreleases"},
	{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"},
	{Group: "source.toolkit.fluxcd.io", Version: "v1beta2", Resource: "ocirepositories"},
	{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "helmrepositories"},
	{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "helmcharts"},
	{Group: "source.toolkit.fluxcd.io", Version: "v1beta2", Resource: "buckets"},
	{Group: "image.toolkit.fluxcd.io", Version: "v1beta2", Resource: "imagepolicies"},
	{Group: "notification.toolkit.fluxcd.io", Version: "v1beta3", Resource: "alerts"},
}

var argoApps = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}
var argoAppSets = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applicationsets"}

func s(o map[string]any, path ...string) string {
	v, _, _ := unstructured.NestedFieldNoCopy(o, path...)
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func sl(o map[string]any, path ...string) []any {
	v, _, _ := unstructured.NestedFieldNoCopy(o, path...)
	r, _ := v.([]any)
	return r
}

// Apps lists every Flux and Argo CD application in the cluster.
func (svc *Service) Apps(ctx context.Context, clusterID string) ([]App, error) {
	dyn, err := svc.Clusters.Dynamic(clusterID)
	if err != nil {
		return nil, err
	}
	var out []App
	seen := map[string]bool{}
	for _, gvr := range fluxTypes {
		if seen[gvr.Group+gvr.Resource] {
			continue
		}
		l, err := dyn.Resource(gvr).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		seen[gvr.Group+gvr.Resource] = true
		for _, it := range l.Items {
			out = append(out, fluxApp(gvr, it))
		}
	}
	for _, gvr := range []schema.GroupVersionResource{argoApps, argoAppSets} {
		l, err := dyn.Resource(gvr).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for _, it := range l.Items {
			out = append(out, argoApp(gvr, it))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Engine != out[j].Engine {
			return out[i].Engine < out[j].Engine
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func fluxApp(gvr schema.GroupVersionResource, it unstructured.Unstructured) App {
	o := it.Object
	a := App{Engine: "flux", Kind: it.GetKind(), Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource, Name: it.GetName(), Namespace: it.GetNamespace(),
		Health: "Unknown", Sync: "Unknown", Path: s(o, "spec", "path")}
	a.Suspended, _, _ = unstructured.NestedBool(o, "spec", "suspend")
	a.Revision = s(o, "status", "lastAppliedRevision")
	if a.Revision == "" {
		a.Revision = s(o, "status", "artifact", "revision")
	}
	a.LastApplied = s(o, "status", "lastHandledReconcileAt")
	switch {
	case s(o, "spec", "sourceRef", "name") != "":
		a.Source = s(o, "spec", "sourceRef", "kind") + "/" + s(o, "spec", "sourceRef", "name")
	case s(o, "spec", "chart", "spec", "chart") != "":
		a.Source = s(o, "spec", "chart", "spec", "sourceRef", "kind") + "/" + s(o, "spec", "chart", "spec", "sourceRef", "name") + " : " + s(o, "spec", "chart", "spec", "chart")
	case s(o, "spec", "url") != "":
		a.Source = s(o, "spec", "url")
	}
	for _, c := range sl(o, "status", "conditions") {
		m, _ := c.(map[string]any)
		switch s(m, "type") {
		case "Ready":
			if s(m, "status") == "True" {
				a.Health, a.Sync = "Healthy", "Synced"
			} else if s(m, "status") == "Unknown" {
				a.Health, a.Sync = "Progressing", "Reconciling"
			} else {
				a.Health, a.Sync = "Degraded", "Failed"
			}
			a.Message = s(m, "message")
			if a.LastApplied == "" {
				a.LastApplied = s(m, "lastTransitionTime")
			}
		case "Drifted":
			if s(m, "status") == "True" {
				a.Sync = "OutOfSync"
				a.Drift = append(a.Drift, DriftItem{Kind: it.GetKind(), Name: it.GetName(), Status: "Drifted", Message: s(m, "message")})
			}
		}
	}
	if a.Suspended {
		a.Sync = "Suspended"
	}
	for _, d := range sl(o, "spec", "dependsOn") {
		m, _ := d.(map[string]any)
		a.DependsOn = append(a.DependsOn, s(m, "name"))
	}
	for _, e := range sl(o, "status", "inventory", "entries") {
		m, _ := e.(map[string]any)
		a.Inventory = append(a.Inventory, s(m, "id"))
	}
	for _, h := range sl(o, "status", "history") { // HelmRelease v2
		m, _ := h.(map[string]any)
		a.History = append(a.History, HistoryItem{Revision: s(m, "chartVersion") + " (" + s(m, "status") + ")", DeployedAt: s(m, "lastDeployed")})
	}
	return a
}

func argoApp(gvr schema.GroupVersionResource, it unstructured.Unstructured) App {
	o := it.Object
	a := App{Engine: "argocd", Kind: it.GetKind(), Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource, Name: it.GetName(), Namespace: it.GetNamespace(),
		Health: s(o, "status", "health", "status"), Sync: s(o, "status", "sync", "status"), Revision: s(o, "status", "sync", "revision"),
		Source: s(o, "spec", "source", "repoURL"), Path: s(o, "spec", "source", "path"), LastApplied: s(o, "status", "reconciledAt")}
	if a.Source == "" {
		if srcs := sl(o, "spec", "sources"); len(srcs) > 0 {
			m, _ := srcs[0].(map[string]any)
			a.Source = s(m, "repoURL")
		}
	}
	if gvr.Resource == "applicationsets" {
		a.Health, a.Sync = "Unknown", "-"
		for _, c := range sl(o, "status", "conditions") {
			m, _ := c.(map[string]any)
			if s(m, "type") == "ErrorOccurred" && s(m, "status") == "True" {
				a.Health, a.Message = "Degraded", s(m, "message")
			}
		}
		return a
	}
	if msg := s(o, "status", "operationState", "message"); msg != "" {
		a.Message = msg
	}
	if ph := s(o, "status", "operationState", "phase"); ph == "Failed" || ph == "Error" {
		a.Sync = "SyncFailed"
	}
	for _, c := range sl(o, "status", "conditions") {
		m, _ := c.(map[string]any)
		a.Message += "\n" + s(m, "type") + ": " + s(m, "message")
	}
	for _, r := range sl(o, "status", "resources") {
		m, _ := r.(map[string]any)
		st := s(m, "status")
		h := s(m, "health", "status")
		if st != "Synced" || (h != "" && h != "Healthy") {
			a.Drift = append(a.Drift, DriftItem{Kind: s(m, "kind"), Name: s(m, "name"), Namespace: s(m, "namespace"), Status: st, Health: h, Message: s(m, "health", "message")})
		}
		a.Inventory = append(a.Inventory, s(m, "namespace")+"_"+s(m, "name")+"_"+s(m, "group")+"_"+s(m, "kind"))
	}
	for _, h := range sl(o, "status", "history") {
		m, _ := h.(map[string]any)
		var id int64
		fmt.Sscan(s(m, "id"), &id)
		a.History = append(a.History, HistoryItem{Revision: s(m, "revision"), DeployedAt: s(m, "deployedAt"), ID: id})
	}
	for i, j := 0, len(a.History)-1; i < j; i, j = i+1, j-1 {
		a.History[i], a.History[j] = a.History[j], a.History[i]
	}
	return a
}

// Action performs reconcile/suspend/resume (Flux) or sync/refresh/rollback (Argo).
func (svc *Service) Action(ctx context.Context, clusterID string, app App, action string, arg string) error {
	dyn, err := svc.Clusters.Dynamic(clusterID)
	if err != nil {
		return err
	}
	gvr := schema.GroupVersionResource{Group: app.Group, Version: app.Version, Resource: app.Resource}
	c := dyn.Resource(gvr).Namespace(app.Namespace)
	var patch map[string]any
	now := time.Now().Format(time.RFC3339Nano)
	switch action {
	case "reconcile":
		patch = map[string]any{"metadata": map[string]any{"annotations": map[string]any{"reconcile.fluxcd.io/requestedAt": now}}}
	case "suspend", "resume":
		patch = map[string]any{"spec": map[string]any{"suspend": action == "suspend"}}
	case "refresh":
		patch = map[string]any{"metadata": map[string]any{"annotations": map[string]any{"argocd.argoproj.io/refresh": "hard"}}}
	case "sync":
		patch = map[string]any{"operation": map[string]any{
			"initiatedBy": map[string]any{"username": "kubenova"},
			"sync":        map[string]any{"revision": arg, "prune": true, "syncStrategy": map[string]any{"hook": map[string]any{}}},
		}}
	case "rollback":
		var id int64
		fmt.Sscan(arg, &id)
		patch = map[string]any{"operation": map[string]any{
			"initiatedBy": map[string]any{"username": "kubenova"},
			"sync":        map[string]any{"revision": findRevision(app, id)},
		}}
	default:
		return fmt.Errorf("unknown action %q", action)
	}
	b, _ := json.Marshal(patch)
	_, err = c.Patch(ctx, app.Name, types.MergePatchType, b, metav1.PatchOptions{FieldManager: "kubenova"})
	return err
}

func findRevision(app App, id int64) string {
	for _, h := range app.History {
		if h.ID == id {
			return h.Revision
		}
	}
	return ""
}

// Get returns a single app.
func (svc *Service) Get(ctx context.Context, clusterID, engine, group, version, resource, ns, name string) (*App, error) {
	dyn, err := svc.Clusters.Dynamic(clusterID)
	if err != nil {
		return nil, err
	}
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
	it, err := dyn.Resource(gvr).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var a App
	if engine == "argocd" {
		a = argoApp(gvr, *it)
	} else {
		a = fluxApp(gvr, *it)
	}
	return &a, nil
}
