// Package security provides the RBAC explorer, secret decoding/editing,
// policy engines (Kyverno, Gatekeeper, Pod Security Admission), image and
// configuration scanning (Trivy, Kubescape, CIS) and certificate expiry.
package security

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/mahmoudkamal448/kubenova/backend/internal/clusters"
	"github.com/mahmoudkamal448/kubenova/backend/internal/tools"
)

// Service provides security features.
type Service struct {
	Clusters *clusters.Manager
	Tools    *tools.Runner
}

// Subject identifies an RBAC subject.
type Subject struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// Grant is a role bound to a subject.
type Grant struct {
	Subject   Subject             `json:"subject"`
	Binding   string              `json:"binding"`
	Role      string              `json:"role"`
	RoleKind  string              `json:"roleKind"`
	Namespace string              `json:"namespace,omitempty"` // empty = cluster wide
	Rules     []rbacv1.PolicyRule `json:"rules"`
	Risky     []string            `json:"risky,omitempty"`
}

// RBACMatrix returns every grant in the cluster, optionally filtered by subject.
func (s *Service) RBACMatrix(ctx context.Context, clusterID, subjectFilter string) ([]Grant, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	crs, err := typed.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	clusterRoles := map[string][]rbacv1.PolicyRule{}
	for _, r := range crs.Items {
		clusterRoles[r.Name] = r.Rules
	}
	roles := map[string][]rbacv1.PolicyRule{}
	if rs, err := typed.RbacV1().Roles("").List(ctx, metav1.ListOptions{}); err == nil {
		for _, r := range rs.Items {
			roles[r.Namespace+"/"+r.Name] = r.Rules
		}
	}
	var out []Grant
	match := func(sub rbacv1.Subject) bool {
		if subjectFilter == "" {
			return true
		}
		return strings.Contains(strings.ToLower(sub.Kind+":"+sub.Namespace+":"+sub.Name), strings.ToLower(subjectFilter))
	}
	crbs, err := typed.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, b := range crbs.Items {
			for _, sub := range b.Subjects {
				if !match(sub) {
					continue
				}
				rules := clusterRoles[b.RoleRef.Name]
				out = append(out, Grant{Subject: Subject{sub.Kind, sub.Name, sub.Namespace}, Binding: b.Name, Role: b.RoleRef.Name, RoleKind: b.RoleRef.Kind, Rules: rules, Risky: risky(rules, true)})
			}
		}
	}
	rbs, err := typed.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, b := range rbs.Items {
			for _, sub := range b.Subjects {
				if !match(sub) {
					continue
				}
				var rules []rbacv1.PolicyRule
				if b.RoleRef.Kind == "ClusterRole" {
					rules = clusterRoles[b.RoleRef.Name]
				} else {
					rules = roles[b.Namespace+"/"+b.RoleRef.Name]
				}
				out = append(out, Grant{Subject: Subject{sub.Kind, sub.Name, sub.Namespace}, Binding: b.Name, Role: b.RoleRef.Name, RoleKind: b.RoleRef.Kind, Namespace: b.Namespace, Rules: rules, Risky: risky(rules, false)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject.Name < out[j].Subject.Name })
	return out, nil
}

func risky(rules []rbacv1.PolicyRule, clusterWide bool) []string {
	var out []string
	has := func(list []string, v string) bool {
		for _, x := range list {
			if x == v || x == "*" {
				return true
			}
		}
		return false
	}
	for _, r := range rules {
		if has(r.Verbs, "*") && has(r.Resources, "*") {
			out = append(out, "full admin (*/*)")
		}
		if has(r.Resources, "secrets") && (has(r.Verbs, "get") || has(r.Verbs, "list")) {
			out = append(out, "can read secrets")
		}
		if has(r.Resources, "pods/exec") && has(r.Verbs, "create") {
			out = append(out, "can exec into pods")
		}
		if has(r.Verbs, "escalate") || has(r.Verbs, "bind") || has(r.Verbs, "impersonate") {
			out = append(out, "privilege escalation verbs")
		}
		if has(r.Resources, "pods") && has(r.Verbs, "create") && clusterWide {
			out = append(out, "can create pods cluster-wide")
		}
	}
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// CanI checks whether the current user (or an impersonated subject) can perform an action.
func (s *Service) CanI(ctx context.Context, clusterID, verb, group, resource, ns, user string, groups []string) (bool, string, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return false, "", err
	}
	attrs := &authv1.ResourceAttributes{Verb: verb, Group: group, Resource: resource, Namespace: ns}
	if user == "" {
		r, err := typed.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attrs}}, metav1.CreateOptions{})
		if err != nil {
			return false, "", err
		}
		return r.Status.Allowed, r.Status.Reason, nil
	}
	r, err := typed.AuthorizationV1().SubjectAccessReviews().Create(ctx, &authv1.SubjectAccessReview{Spec: authv1.SubjectAccessReviewSpec{ResourceAttributes: attrs, User: user, Groups: groups}}, metav1.CreateOptions{})
	if err != nil {
		return false, "", err
	}
	return r.Status.Allowed, r.Status.Reason, nil
}

// AccessMatrix evaluates common verbs on common resources for a subject; a
// ServiceAccount is expressed as system:serviceaccount:<ns>:<name>.
func (s *Service) AccessMatrix(ctx context.Context, clusterID, ns, user string) (map[string]map[string]bool, error) {
	resources := []string{"pods", "pods/exec", "pods/log", "deployments", "services", "secrets", "configmaps", "ingresses", "persistentvolumeclaims", "nodes", "namespaces", "roles", "rolebindings", "clusterroles", "customresourcedefinitions"}
	groups := map[string]string{"deployments": "apps", "ingresses": "networking.k8s.io", "roles": "rbac.authorization.k8s.io", "rolebindings": "rbac.authorization.k8s.io", "clusterroles": "rbac.authorization.k8s.io", "customresourcedefinitions": "apiextensions.k8s.io"}
	verbs := []string{"get", "list", "watch", "create", "update", "patch", "delete"}
	var userGroups []string
	if strings.HasPrefix(user, "system:serviceaccount:") {
		parts := strings.Split(user, ":")
		userGroups = []string{"system:serviceaccounts", "system:serviceaccounts:" + parts[2], "system:authenticated"}
	}
	out := map[string]map[string]bool{}
	for _, r := range resources {
		out[r] = map[string]bool{}
		res, sub := r, ""
		if i := strings.Index(r, "/"); i > 0 {
			res, sub = r[:i], r[i+1:]
		}
		for _, v := range verbs {
			if sub != "" && v != "create" && v != "get" {
				continue
			}
			attrs := res
			ok, _, err := s.canISub(ctx, clusterID, v, groups[r], attrs, sub, ns, user, userGroups)
			if err != nil {
				return nil, err
			}
			out[r][v] = ok
		}
	}
	return out, nil
}

func (s *Service) canISub(ctx context.Context, clusterID, verb, group, resource, sub, ns, user string, groups []string) (bool, string, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return false, "", err
	}
	attrs := &authv1.ResourceAttributes{Verb: verb, Group: group, Resource: resource, Subresource: sub, Namespace: ns}
	if user == "" {
		r, err := typed.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attrs}}, metav1.CreateOptions{})
		if err != nil {
			return false, "", err
		}
		return r.Status.Allowed, r.Status.Reason, nil
	}
	r, err := typed.AuthorizationV1().SubjectAccessReviews().Create(ctx, &authv1.SubjectAccessReview{Spec: authv1.SubjectAccessReviewSpec{ResourceAttributes: attrs, User: user, Groups: groups}}, metav1.CreateOptions{})
	if err != nil {
		return false, "", err
	}
	return r.Status.Allowed, r.Status.Reason, nil
}

// DecodedSecret is a secret with decoded values.
type DecodedSecret struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Type      string            `json:"type"`
	Data      map[string]string `json:"data"`
	Binary    map[string]bool   `json:"binary"`
	Certs     []CertInfo        `json:"certs,omitempty"`
}

// DecodeSecret returns a secret's decoded data.
func (s *Service) DecodeSecret(ctx context.Context, clusterID, ns, name string) (*DecodedSecret, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	sec, err := typed.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	d := &DecodedSecret{Name: name, Namespace: ns, Type: string(sec.Type), Data: map[string]string{}, Binary: map[string]bool{}}
	for k, v := range sec.Data {
		if isText(v) {
			d.Data[k] = string(v)
		} else {
			d.Data[k] = base64.StdEncoding.EncodeToString(v)
			d.Binary[k] = true
		}
		if strings.HasSuffix(k, ".crt") || k == "ca.crt" || k == "tls.crt" {
			if exp, err := clusters.EarliestExpiry(v); err == nil && exp != nil {
				d.Certs = append(d.Certs, CertInfo{Name: name, Namespace: ns, Key: k, NotAfter: *exp, DaysLeft: int(time.Until(*exp).Hours() / 24)})
			}
		}
	}
	return d, nil
}

func isText(b []byte) bool {
	for _, c := range b {
		if c == 0 || (c < 32 && c != '\n' && c != '\r' && c != '\t') {
			return false
		}
	}
	return true
}

// UpdateSecret writes plaintext values back (encoding them).
func (s *Service) UpdateSecret(ctx context.Context, clusterID, ns, name string, data map[string]string, binary map[string]bool) error {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return err
	}
	sec, err := typed.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	sec.Data = map[string][]byte{}
	for k, v := range data {
		if binary[k] {
			b, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return fmt.Errorf("key %s: %w", k, err)
			}
			sec.Data[k] = b
		} else {
			sec.Data[k] = []byte(v)
		}
	}
	sec.StringData = nil
	_, err = typed.CoreV1().Secrets(ns).Update(ctx, sec, metav1.UpdateOptions{FieldManager: "kubenova"})
	return err
}

// CertInfo describes a certificate and when it expires.
type CertInfo struct {
	Source    string    `json:"source"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace,omitempty"`
	Key       string    `json:"key,omitempty"`
	NotAfter  time.Time `json:"notAfter"`
	DaysLeft  int       `json:"daysLeft"`
	Status    string    `json:"status"`
}

// Certificates collects TLS secrets, cert-manager Certificates and the
// kubeconfig client certificate with their expiry.
func (s *Service) Certificates(ctx context.Context, clusterID string) ([]CertInfo, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	var out []CertInfo
	secs, err := typed.CoreV1().Secrets("").List(ctx, metav1.ListOptions{FieldSelector: "type=kubernetes.io/tls"})
	if err == nil {
		for _, sec := range secs.Items {
			if exp, _ := clusters.EarliestExpiry(sec.Data["tls.crt"]); exp != nil {
				out = append(out, CertInfo{Source: "Secret", Name: sec.Name, Namespace: sec.Namespace, Key: "tls.crt", NotAfter: *exp})
			}
		}
	}
	dyn, err := s.Clusters.Dynamic(clusterID)
	if err == nil {
		gvr := schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}
		if l, err := dyn.Resource(gvr).List(ctx, metav1.ListOptions{}); err == nil {
			for _, c := range l.Items {
				na, _, _ := unstructured.NestedString(c.Object, "status", "notAfter")
				t, err := time.Parse(time.RFC3339, na)
				if err != nil {
					continue
				}
				out = append(out, CertInfo{Source: "cert-manager", Name: c.GetName(), Namespace: c.GetNamespace(), NotAfter: t})
			}
		}
	}
	if c, err := s.Clusters.Get(clusterID); err == nil && c.Health != nil && c.Health.CertExpiry != nil {
		out = append(out, CertInfo{Source: "kubeconfig", Name: c.User, NotAfter: *c.Health.CertExpiry})
	}
	for i := range out {
		out[i].DaysLeft = int(time.Until(out[i].NotAfter).Hours() / 24)
		switch {
		case out[i].DaysLeft < 0:
			out[i].Status = "expired"
		case out[i].DaysLeft < 14:
			out[i].Status = "critical"
		case out[i].DaysLeft < 30:
			out[i].Status = "warning"
		default:
			out[i].Status = "ok"
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NotAfter.Before(out[j].NotAfter) })
	return out, nil
}

// PolicyItem is a policy/constraint and its violations.
type PolicyItem struct {
	Engine     string `json:"engine"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Action     string `json:"action"`
	Violations int64  `json:"violations"`
	Ready      bool   `json:"ready"`
	Message    string `json:"message,omitempty"`
}

// Policies lists Kyverno policies (with PolicyReport results), Gatekeeper
// constraints and Pod Security Admission namespace labels.
func (s *Service) Policies(ctx context.Context, clusterID string) ([]PolicyItem, error) {
	dyn, err := s.Clusters.Dynamic(clusterID)
	if err != nil {
		return nil, err
	}
	var out []PolicyItem
	fail := map[string]int64{}
	for _, gvr := range []schema.GroupVersionResource{
		{Group: "wgpolicyk8s.io", Version: "v1alpha2", Resource: "policyreports"},
		{Group: "wgpolicyk8s.io", Version: "v1alpha2", Resource: "clusterpolicyreports"},
	} {
		if l, err := dyn.Resource(gvr).List(ctx, metav1.ListOptions{}); err == nil {
			for _, r := range l.Items {
				res, _, _ := unstructured.NestedSlice(r.Object, "results")
				for _, x := range res {
					m, _ := x.(map[string]any)
					if m["result"] == "fail" {
						fail[fmt.Sprint(m["policy"])]++
					}
				}
			}
		}
	}
	for _, gvr := range []schema.GroupVersionResource{
		{Group: "kyverno.io", Version: "v1", Resource: "clusterpolicies"},
		{Group: "kyverno.io", Version: "v1", Resource: "policies"},
	} {
		if l, err := dyn.Resource(gvr).List(ctx, metav1.ListOptions{}); err == nil {
			for _, p := range l.Items {
				action, _, _ := unstructured.NestedString(p.Object, "spec", "validationFailureAction")
				ready := true
				conds, _, _ := unstructured.NestedSlice(p.Object, "status", "conditions")
				for _, c := range conds {
					m, _ := c.(map[string]any)
					if m["type"] == "Ready" && m["status"] != "True" {
						ready = false
					}
				}
				out = append(out, PolicyItem{Engine: "kyverno", Kind: p.GetKind(), Name: p.GetName(), Namespace: p.GetNamespace(), Action: action, Violations: fail[p.GetName()], Ready: ready})
			}
		}
	}
	// Gatekeeper: each ConstraintTemplate defines a constraint kind in constraints.gatekeeper.sh.
	ctGVR := schema.GroupVersionResource{Group: "templates.gatekeeper.sh", Version: "v1", Resource: "constrainttemplates"}
	if l, err := dyn.Resource(ctGVR).List(ctx, metav1.ListOptions{}); err == nil {
		for _, t := range l.Items {
			kind, _, _ := unstructured.NestedString(t.Object, "spec", "crd", "spec", "names", "kind")
			if kind == "" {
				continue
			}
			cg := schema.GroupVersionResource{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Resource: strings.ToLower(kind)}
			cl, err := dyn.Resource(cg).List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}
			for _, c := range cl.Items {
				action, _, _ := unstructured.NestedString(c.Object, "spec", "enforcementAction")
				v, _, _ := unstructured.NestedInt64(c.Object, "status", "totalViolations")
				out = append(out, PolicyItem{Engine: "gatekeeper", Kind: kind, Name: c.GetName(), Action: action, Violations: v, Ready: true})
			}
		}
	}
	typed, err := s.Clusters.Typed(clusterID)
	if err == nil {
		if nss, err := typed.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); err == nil {
			for _, ns := range nss.Items {
				enf := ns.Labels["pod-security.kubernetes.io/enforce"]
				warn := ns.Labels["pod-security.kubernetes.io/warn"]
				audit := ns.Labels["pod-security.kubernetes.io/audit"]
				if enf == "" && warn == "" && audit == "" {
					enf = "privileged (default)"
				}
				out = append(out, PolicyItem{Engine: "psa", Kind: "Namespace", Name: ns.Name, Action: fmt.Sprintf("enforce=%s warn=%s audit=%s", enf, warn, audit), Ready: true})
			}
		}
	}
	return out, nil
}

// ScanImage runs Trivy against an image and returns its JSON report.
func (s *Service) ScanImage(ctx context.Context, image string) (json.RawMessage, error) {
	res, err := s.Tools.Run(ctx, "", "trivy", []string{"image", "--quiet", "--format", "json", "--scanners", "vuln", image}, nil)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(res.Stdout), nil
}

// ScanCluster runs Trivy (k8s), Kubescape (nsa/mitre/cis) or kube-bench.
func (s *Service) ScanCluster(ctx context.Context, clusterID, scanner, framework string) (json.RawMessage, error) {
	kc, err := s.Clusters.KubeconfigFor(clusterID)
	if err != nil {
		return nil, err
	}
	var res *tools.Result
	switch scanner {
	case "trivy":
		res, err = s.Tools.Run(ctx, kc, "trivy", []string{"k8s", "--report", "summary", "--format", "json", "--quiet"}, nil)
	case "kubescape":
		if framework == "" {
			framework = "nsa"
		}
		res, err = s.Tools.Run(ctx, kc, "kubescape", []string{"scan", "framework", framework, "--format", "json", "--output", "/dev/stdout", "--logger", "error"}, nil)
	case "cis":
		// CIS benchmark via Kubescape's CIS framework (works on any distribution).
		res, err = s.Tools.Run(ctx, kc, "kubescape", []string{"scan", "framework", "cis-v1.23-t1.0.1", "--format", "json", "--output", "/dev/stdout", "--logger", "error"}, nil)
	default:
		return nil, fmt.Errorf("unknown scanner %q", scanner)
	}
	if err != nil {
		return nil, err
	}
	out := strings.TrimSpace(res.Stdout)
	if !json.Valid([]byte(out)) {
		b, _ := json.Marshal(map[string]string{"raw": out})
		return b, nil
	}
	return json.RawMessage(out), nil
}

// Images lists every distinct container image running in the cluster.
func (s *Service) Images(ctx context.Context, clusterID string) (map[string][]string, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	pods, err := typed.CoreV1().Pods("").List(ctx, metav1.ListOptions{ResourceVersion: "0"})
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, p := range pods.Items {
		for _, c := range append(p.Spec.InitContainers, p.Spec.Containers...) {
			out[c.Image] = append(out[c.Image], p.Namespace+"/"+p.Name)
		}
	}
	return out, nil
}
