// Package resources implements generic, discovery driven access to every
// Kubernetes resource type, including CustomResourceDefinitions, along with
// higher level operations (scale, restart, rollback, clone, diff, apply).
package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	"github.com/mahmoudkamal448/kubenova/backend/internal/clusters"
	"github.com/mahmoudkamal448/kubenova/backend/internal/textdiff"
)

// FieldManager identifies changes performed through the IDE.
const FieldManager = "kubenova"

// APIResource describes a discoverable resource type.
type APIResource struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Resource   string   `json:"resource"`
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs"`
	ShortNames []string `json:"shortNames,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Custom     bool     `json:"custom"`
	Section    string   `json:"section"`
}

// GVR returns the GroupVersionResource for the type.
func (a APIResource) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: a.Group, Version: a.Version, Resource: a.Resource}
}

var builtinGroups = map[string]bool{
	"": true, "apps": true, "batch": true, "autoscaling": true, "policy": true,
	"networking.k8s.io": true, "rbac.authorization.k8s.io": true, "storage.k8s.io": true,
	"scheduling.k8s.io": true, "coordination.k8s.io": true, "node.k8s.io": true,
	"discovery.k8s.io": true, "apiextensions.k8s.io": true, "admissionregistration.k8s.io": true,
	"certificates.k8s.io": true, "events.k8s.io": true, "flowcontrol.apiserver.k8s.io": true,
	"apiregistration.k8s.io": true, "authentication.k8s.io": true, "authorization.k8s.io": true,
	"metrics.k8s.io": true, "resource.k8s.io": true, "storagemigration.k8s.io": true,
	"internal.apiserver.k8s.io": true,
}

// section assigns a navigation section to a resource for the sidebar.
func section(group, resource string) string {
	switch resource {
	case "pods", "deployments", "replicasets", "statefulsets", "daemonsets", "jobs", "cronjobs", "replicationcontrollers", "horizontalpodautoscalers", "poddisruptionbudgets":
		return "Workloads"
	case "services", "endpoints", "endpointslices", "ingresses", "ingressclasses", "networkpolicies":
		return "Network"
	case "configmaps", "secrets", "resourcequotas", "limitranges", "priorityclasses", "runtimeclasses", "leases", "mutatingwebhookconfigurations", "validatingwebhookconfigurations":
		return "Config"
	case "persistentvolumeclaims", "persistentvolumes", "storageclasses", "volumeattachments", "csidrivers", "csinodes", "csistoragecapacities", "volumesnapshots", "volumesnapshotclasses", "volumesnapshotcontents":
		return "Storage"
	case "serviceaccounts", "roles", "rolebindings", "clusterroles", "clusterrolebindings", "podsecuritypolicies", "certificatesigningrequests":
		return "Access Control"
	case "namespaces", "nodes", "events", "componentstatuses", "apiservices", "customresourcedefinitions":
		return "Cluster"
	}
	switch resource {
	case "controllerrevisions", "podtemplates":
		return "Workloads"
	case "flowschemas", "prioritylevelconfigurations", "validatingadmissionpolicies", "validatingadmissionpolicybindings", "mutatingadmissionpolicies", "mutatingadmissionpolicybindings":
		return "Cluster"
	}
	switch {
	case group == "gateway.networking.k8s.io":
		return "Network"
	case strings.HasSuffix(group, "fluxcd.io"), group == "argoproj.io":
		return "GitOps"
	case strings.Contains(group, "istio.io"), strings.Contains(group, "linkerd.io"), strings.Contains(group, "cilium.io"), strings.Contains(group, "projectcalico.org"), strings.Contains(group, "metallb.io"), strings.Contains(group, "traefik"):
		return "Network"
	case strings.Contains(group, "monitoring.coreos.com"):
		return "Monitoring"
	case strings.Contains(group, "kyverno.io"), strings.Contains(group, "gatekeeper.sh"), strings.Contains(group, "aquasecurity.github.io"), strings.Contains(group, "cert-manager.io"):
		return "Security"
	case strings.Contains(group, "longhorn.io"), strings.Contains(group, "ceph.rook.io"), strings.Contains(group, "openebs.io"), strings.Contains(group, "snapshot.storage.k8s.io"):
		return "Storage"
	case strings.Contains(group, "cattle.io"):
		return "Rancher"
	}
	if builtinGroups[group] || strings.HasSuffix(group, ".k8s.io") {
		return "Cluster"
	}
	return "Custom Resources"
}

func hasVerb(verbs []string, v string) bool {
	for _, x := range verbs {
		if x == v {
			return true
		}
	}
	return false
}

// Service performs resource operations against clusters.
type Service struct {
	Clusters *clusters.Manager
}

// Discover returns every resource type served by the cluster (including CRDs).
func (s *Service) Discover(ctx context.Context, clusterID string) ([]APIResource, error) {
	disc, err := s.Clusters.Discovery(clusterID)
	if err != nil {
		return nil, err
	}
	lists, err := disc.ServerPreferredResources()
	if err != nil && len(lists) == 0 {
		return nil, err
	}
	var out []APIResource
	for _, l := range lists {
		gv, perr := schema.ParseGroupVersion(l.GroupVersion)
		if perr != nil {
			continue
		}
		for _, r := range l.APIResources {
			if strings.Contains(r.Name, "/") || !hasVerb(r.Verbs, "list") {
				continue // sub resources and create-only APIs (reviews, bindings)
			}
			out = append(out, APIResource{
				Group: gv.Group, Version: gv.Version, Resource: r.Name, Kind: r.Kind,
				Namespaced: r.Namespaced, Verbs: r.Verbs, ShortNames: r.ShortNames, Categories: r.Categories,
				Custom: !builtinGroups[gv.Group] && !strings.HasSuffix(gv.Group, ".k8s.io"), Section: section(gv.Group, r.Name),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Section != out[j].Section {
			return out[i].Section < out[j].Section
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

// Resolve maps a kind or resource name (pods, Pod, po, deploy...) to a GVR.
func (s *Service) Resolve(ctx context.Context, clusterID, name string) (APIResource, error) {
	all, err := s.Discover(ctx, clusterID)
	if err != nil {
		return APIResource{}, err
	}
	n := strings.ToLower(name)
	group := ""
	if i := strings.Index(n, "."); i > 0 {
		n, group = n[:i], n[i+1:]
	}
	for _, r := range all {
		if group != "" && r.Group != group {
			continue
		}
		if r.Resource == n || strings.ToLower(r.Kind) == n || strings.TrimSuffix(r.Resource, "s") == n {
			return r, nil
		}
		for _, sn := range r.ShortNames {
			if sn == n {
				return r, nil
			}
		}
	}
	return APIResource{}, fmt.Errorf("resource type %q not found", name)
}

func (s *Service) client(clusterID string, gvr schema.GroupVersionResource, ns string) (dynamic.ResourceInterface, error) {
	dyn, err := s.Clusters.Dynamic(clusterID)
	if err != nil {
		return nil, err
	}
	nri := dyn.Resource(gvr)
	if ns != "" {
		return nri.Namespace(ns), nil
	}
	return nri, nil
}

func (s *Service) clientFor(clusterID string, obj *unstructured.Unstructured) (dynamic.ResourceInterface, schema.GroupVersionResource, error) {
	mapper, err := s.Clusters.Mapper(clusterID)
	if err != nil {
		return nil, schema.GroupVersionResource{}, err
	}
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		mapper.Reset()
		mapping, err = mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, schema.GroupVersionResource{}, err
		}
	}
	ns := ""
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns = obj.GetNamespace()
		if ns == "" {
			ns = "default"
			obj.SetNamespace(ns)
		}
	}
	c, err := s.client(clusterID, mapping.Resource, ns)
	return c, mapping.Resource, err
}

// ListOptions controls list queries.
type ListOptions struct {
	Namespace     string `json:"namespace"`
	LabelSelector string `json:"labelSelector"`
	FieldSelector string `json:"fieldSelector"`
	Limit         int64  `json:"limit"`
	Continue      string `json:"continue"`
}

// List lists objects of a type. Results are paginated using Limit/Continue so
// that clusters with 100k+ objects can be lazily loaded.
func (s *Service) List(ctx context.Context, clusterID string, gvr schema.GroupVersionResource, o ListOptions) (*unstructured.UnstructuredList, error) {
	c, err := s.client(clusterID, gvr, o.Namespace)
	if err != nil {
		return nil, err
	}
	return c.List(ctx, metav1.ListOptions{LabelSelector: o.LabelSelector, FieldSelector: o.FieldSelector, Limit: o.Limit, Continue: o.Continue})
}

// Get returns a single object.
func (s *Service) Get(ctx context.Context, clusterID string, gvr schema.GroupVersionResource, ns, name string) (*unstructured.Unstructured, error) {
	c, err := s.client(clusterID, gvr, ns)
	if err != nil {
		return nil, err
	}
	return c.Get(ctx, name, metav1.GetOptions{})
}

// Watch starts a watch for the type.
func (s *Service) Watch(ctx context.Context, clusterID string, gvr schema.GroupVersionResource, o ListOptions, resourceVersion string) (watch.Interface, error) {
	c, err := s.client(clusterID, gvr, o.Namespace)
	if err != nil {
		return nil, err
	}
	return c.Watch(ctx, metav1.ListOptions{LabelSelector: o.LabelSelector, FieldSelector: o.FieldSelector, ResourceVersion: resourceVersion, AllowWatchBookmarks: true})
}

// DecodeYAML parses a multi document YAML or JSON stream.
func DecodeYAML(data string) ([]*unstructured.Unstructured, error) {
	var out []*unstructured.Unstructured
	for _, doc := range splitDocuments(data) {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		j, err := yaml.YAMLToJSON([]byte(doc))
		if err != nil {
			return nil, err
		}
		if string(j) == "null" {
			continue
		}
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON(j); err != nil {
			return nil, err
		}
		if obj.IsList() {
			l, err := obj.ToList()
			if err != nil {
				return nil, err
			}
			for i := range l.Items {
				out = append(out, &l.Items[i])
			}
			continue
		}
		out = append(out, obj)
	}
	return out, nil
}

func splitDocuments(s string) []string {
	var docs []string
	var cur strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimRight(line, " \r") == "---" {
			docs = append(docs, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
	}
	return append(docs, cur.String())
}

// ApplyOptions controls apply behaviour.
type ApplyOptions struct {
	ServerSide bool `json:"serverSide"`
	Force      bool `json:"force"`
	DryRun     bool `json:"dryRun"`
}

// ApplyResult is the per-object result of Apply.
type ApplyResult struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Action    string `json:"action"`
	Error     string `json:"error,omitempty"`
}

// Apply creates or updates every object in the YAML stream. With ServerSide
// it uses Server-Side Apply, otherwise create-or-replace semantics.
func (s *Service) Apply(ctx context.Context, clusterID, data string, o ApplyOptions) ([]ApplyResult, error) {
	objs, err := DecodeYAML(data)
	if err != nil {
		return nil, err
	}
	var results []ApplyResult
	var dry []string
	if o.DryRun {
		dry = []string{metav1.DryRunAll}
	}
	for _, obj := range objs {
		r := ApplyResult{Kind: obj.GetKind(), Name: obj.GetName()}
		c, _, err := s.clientFor(clusterID, obj)
		if err != nil {
			r.Error = err.Error()
			results = append(results, r)
			continue
		}
		r.Namespace = obj.GetNamespace()
		if o.ServerSide {
			obj.SetManagedFields(nil)
			obj.SetResourceVersion("")
			_, err = c.Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{FieldManager: FieldManager, Force: o.Force, DryRun: dry})
			r.Action = "applied (server-side)"
		} else {
			existing, gerr := c.Get(ctx, obj.GetName(), metav1.GetOptions{})
			if gerr != nil {
				if obj.GetName() == "" && obj.GetGenerateName() == "" {
					err = fmt.Errorf("metadata.name is required")
				} else {
					_, err = c.Create(ctx, obj, metav1.CreateOptions{FieldManager: FieldManager, DryRun: dry})
					r.Action = "created"
				}
			} else {
				obj.SetResourceVersion(existing.GetResourceVersion())
				_, err = c.Update(ctx, obj, metav1.UpdateOptions{FieldManager: FieldManager, DryRun: dry})
				r.Action = "configured"
			}
		}
		if err != nil {
			r.Error = err.Error()
		}
		if o.DryRun {
			r.Action += " (dry run)"
		}
		results = append(results, r)
	}
	if !o.DryRun {
		for _, obj := range objs {
			if obj.GetKind() == "CustomResourceDefinition" {
				s.Clusters.InvalidateDiscovery(clusterID)
				break
			}
		}
	}
	return results, nil
}

// Create creates objects from YAML (fails if they already exist).
func (s *Service) Create(ctx context.Context, clusterID, data string) ([]ApplyResult, error) {
	objs, err := DecodeYAML(data)
	if err != nil {
		return nil, err
	}
	var results []ApplyResult
	for _, obj := range objs {
		r := ApplyResult{Kind: obj.GetKind(), Name: obj.GetName(), Action: "created"}
		c, _, err := s.clientFor(clusterID, obj)
		if err == nil {
			r.Namespace = obj.GetNamespace()
			var created *unstructured.Unstructured
			created, err = c.Create(ctx, obj, metav1.CreateOptions{FieldManager: FieldManager})
			if err == nil {
				r.Name = created.GetName()
			}
		}
		if err != nil {
			r.Error = err.Error()
		}
		results = append(results, r)
	}
	return results, nil
}

// Delete deletes an object.
func (s *Service) Delete(ctx context.Context, clusterID string, gvr schema.GroupVersionResource, ns, name, propagation string, grace *int64) error {
	c, err := s.client(clusterID, gvr, ns)
	if err != nil {
		return err
	}
	opts := metav1.DeleteOptions{GracePeriodSeconds: grace}
	if propagation != "" {
		p := metav1.DeletionPropagation(propagation)
		opts.PropagationPolicy = &p
	}
	return c.Delete(ctx, name, opts)
}

// Patch applies a JSON, merge, strategic or apply patch.
func (s *Service) Patch(ctx context.Context, clusterID string, gvr schema.GroupVersionResource, ns, name, patchType string, data []byte) (*unstructured.Unstructured, error) {
	c, err := s.client(clusterID, gvr, ns)
	if err != nil {
		return nil, err
	}
	pt := types.MergePatchType
	switch patchType {
	case "json":
		pt = types.JSONPatchType
	case "strategic":
		pt = types.StrategicMergePatchType
	case "apply":
		pt = types.ApplyPatchType
		force := true
		return c.Patch(ctx, name, pt, data, metav1.PatchOptions{FieldManager: FieldManager, Force: &force})
	}
	return c.Patch(ctx, name, pt, data, metav1.PatchOptions{FieldManager: FieldManager})
}

// Scale sets the replica count via the scale sub resource.
func (s *Service) Scale(ctx context.Context, clusterID string, gvr schema.GroupVersionResource, ns, name string, replicas int32) error {
	c, err := s.client(clusterID, gvr, ns)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	_, err = c.Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{FieldManager: FieldManager}, "scale")
	if err != nil {
		// Some CRDs do not implement /scale; patch the main resource.
		_, err = c.Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{FieldManager: FieldManager})
	}
	return err
}

// Restart performs a rollout restart by stamping the pod template.
func (s *Service) Restart(ctx context.Context, clusterID string, gvr schema.GroupVersionResource, ns, name string) error {
	c, err := s.client(clusterID, gvr, ns)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`, time.Now().Format(time.RFC3339))
	_, err = c.Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{FieldManager: FieldManager})
	if err != nil {
		_, err = c.Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{FieldManager: FieldManager})
	}
	return err
}

// Revision is an entry in a workload's rollout history.
type Revision struct {
	Revision int64     `json:"revision"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	Images   []string  `json:"images"`
	Cause    string    `json:"cause,omitempty"`
}

// History returns the rollout history of a Deployment, StatefulSet or DaemonSet.
func (s *Service) History(ctx context.Context, clusterID, kind, ns, name string) ([]Revision, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	var out []Revision
	switch strings.ToLower(kind) {
	case "deployment", "deployments":
		d, err := typed.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		sel, _ := metav1.LabelSelectorAsSelector(d.Spec.Selector)
		rss, err := typed.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
		if err != nil {
			return nil, err
		}
		for _, rs := range rss.Items {
			if !metav1.IsControlledBy(&rs, d) {
				continue
			}
			var rev int64
			fmt.Sscan(rs.Annotations["deployment.kubernetes.io/revision"], &rev)
			var images []string
			for _, c := range rs.Spec.Template.Spec.Containers {
				images = append(images, c.Image)
			}
			out = append(out, Revision{Revision: rev, Name: rs.Name, Created: rs.CreationTimestamp.Time, Images: images, Cause: rs.Annotations["kubernetes.io/change-cause"]})
		}
	case "statefulset", "statefulsets", "daemonset", "daemonsets":
		crs, err := typed.AppsV1().ControllerRevisions(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, cr := range crs.Items {
			owner := metav1.GetControllerOf(&cr)
			if owner == nil || owner.Name != name || !strings.EqualFold(owner.Kind+"s", strings.TrimSuffix(strings.ToLower(kind), "s")+"s") {
				continue
			}
			out = append(out, Revision{Revision: cr.Revision, Name: cr.Name, Created: cr.CreationTimestamp.Time})
		}
	default:
		return nil, fmt.Errorf("history not supported for %s", kind)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision > out[j].Revision })
	return out, nil
}

// Rollback rolls a workload back to the given revision (0 = previous).
func (s *Service) Rollback(ctx context.Context, clusterID, kind, ns, name string, revision int64) error {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return err
	}
	hist, err := s.History(ctx, clusterID, kind, ns, name)
	if err != nil {
		return err
	}
	if len(hist) < 2 && revision == 0 {
		return fmt.Errorf("no previous revision to roll back to")
	}
	target := Revision{}
	if revision == 0 {
		target = hist[1]
	} else {
		for _, h := range hist {
			if h.Revision == revision {
				target = h
			}
		}
		if target.Name == "" {
			return fmt.Errorf("revision %d not found", revision)
		}
	}
	switch strings.ToLower(kind) {
	case "deployment", "deployments":
		rs, err := typed.AppsV1().ReplicaSets(ns).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		tmpl := rs.Spec.Template.DeepCopy()
		delete(tmpl.Labels, appsv1.DefaultDeploymentUniqueLabelKey)
		patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"template": tmpl}})
		_, err = typed.AppsV1().Deployments(ns).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{FieldManager: FieldManager})
		return err
	default:
		cr, err := typed.AppsV1().ControllerRevisions(ns).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
		if strings.HasPrefix(strings.ToLower(kind), "daemonset") {
			gvr.Resource = "daemonsets"
		}
		_, err = s.Patch(ctx, clusterID, gvr, ns, name, "strategic", cr.Data.Raw)
		return err
	}
}

// CleanForExport strips server populated fields so YAML can be re-applied.
func CleanForExport(obj *unstructured.Unstructured) *unstructured.Unstructured {
	o := obj.DeepCopy()
	unstructured.RemoveNestedField(o.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(o.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(o.Object, "metadata", "uid")
	unstructured.RemoveNestedField(o.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(o.Object, "metadata", "generation")
	unstructured.RemoveNestedField(o.Object, "metadata", "selfLink")
	unstructured.RemoveNestedField(o.Object, "metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")
	unstructured.RemoveNestedField(o.Object, "metadata", "annotations", "deployment.kubernetes.io/revision")
	unstructured.RemoveNestedField(o.Object, "status")
	if len(o.GetAnnotations()) == 0 {
		unstructured.RemoveNestedField(o.Object, "metadata", "annotations")
	}
	return o
}

// ToYAML serialises an object as YAML.
func ToYAML(obj any) (string, error) {
	b, err := yaml.Marshal(obj)
	return string(b), err
}

// Export returns re-applicable YAML for an object.
func (s *Service) Export(ctx context.Context, clusterID string, gvr schema.GroupVersionResource, ns, name string) (string, error) {
	obj, err := s.Get(ctx, clusterID, gvr, ns, name)
	if err != nil {
		return "", err
	}
	return ToYAML(CleanForExport(obj).Object)
}

// Clone copies an object under a new name/namespace, optionally to a different cluster.
func (s *Service) Clone(ctx context.Context, clusterID string, gvr schema.GroupVersionResource, ns, name, targetCluster, targetNS, newName string) (*unstructured.Unstructured, error) {
	obj, err := s.Get(ctx, clusterID, gvr, ns, name)
	if err != nil {
		return nil, err
	}
	c := CleanForExport(obj)
	unstructured.RemoveNestedField(c.Object, "metadata", "ownerReferences")
	if newName != "" {
		c.SetName(newName)
	}
	if targetNS != "" && ns != "" {
		c.SetNamespace(targetNS)
	}
	if targetCluster == "" {
		targetCluster = clusterID
	}
	if gvr.Resource == "services" {
		unstructured.RemoveNestedField(c.Object, "spec", "clusterIP")
		unstructured.RemoveNestedField(c.Object, "spec", "clusterIPs")
	}
	if gvr.Resource == "persistentvolumeclaims" {
		unstructured.RemoveNestedField(c.Object, "spec", "volumeName")
	}
	cl, _, err := s.clientFor(targetCluster, c)
	if err != nil {
		return nil, err
	}
	return cl.Create(ctx, c, metav1.CreateOptions{FieldManager: FieldManager})
}

// DiffResult is the diff of live state vs. a desired manifest.
type DiffResult struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Live      string `json:"live"`
	Desired   string `json:"desired"`
	Diff      string `json:"diff"`
	New       bool   `json:"new"`
}

// Diff computes what would change if the YAML were applied, using a server
// side dry-run so defaulting and admission webhooks are taken into account.
func (s *Service) Diff(ctx context.Context, clusterID, data string) ([]DiffResult, error) {
	objs, err := DecodeYAML(data)
	if err != nil {
		return nil, err
	}
	var out []DiffResult
	for _, obj := range objs {
		c, _, err := s.clientFor(clusterID, obj)
		if err != nil {
			return nil, err
		}
		r := DiffResult{Kind: obj.GetKind(), Name: obj.GetName(), Namespace: obj.GetNamespace()}
		live, gerr := c.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if gerr == nil {
			r.Live, _ = ToYAML(CleanForExport(live).Object)
		} else {
			r.New = true
		}
		obj.SetManagedFields(nil)
		obj.SetResourceVersion("")
		dry, err := c.Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{FieldManager: FieldManager, Force: true, DryRun: []string{metav1.DryRunAll}})
		if err != nil {
			r.Desired, _ = ToYAML(obj.Object)
		} else {
			r.Desired, _ = ToYAML(CleanForExport(dry).Object)
		}
		r.Diff = textdiff.Unified("live", "desired", r.Live, r.Desired, 3)
		out = append(out, r)
	}
	return out, nil
}

// DiffObjects diffs two YAML strings (used by the diff viewer and cluster comparison).
func DiffObjects(a, b string) string { return textdiff.Unified("a", "b", a, b, 3) }
