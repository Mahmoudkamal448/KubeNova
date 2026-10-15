package resources

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Row is a compact, table friendly projection of an object. Returning rows
// rather than full objects keeps payloads small for 100k+ resources.
type Row struct {
	UID       string            `json:"uid"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Kind      string            `json:"kind"`
	Created   time.Time         `json:"created"`
	Status    string            `json:"status"`
	Health    string            `json:"health"` // ok | warn | error | neutral
	Cells     map[string]string `json:"cells"`
	Labels    map[string]string `json:"labels,omitempty"`
	RV        string            `json:"rv"`
}

func str(obj map[string]any, path ...string) string {
	v, _, _ := unstructured.NestedFieldNoCopy(obj, path...)
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func num(obj map[string]any, path ...string) int64 {
	v, _, _ := unstructured.NestedFieldNoCopy(obj, path...)
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	}
	return 0
}

func slice(obj map[string]any, path ...string) []any {
	v, _, _ := unstructured.NestedFieldNoCopy(obj, path...)
	s, _ := v.([]any)
	return s
}

// Summarize builds the Row for an object, with kind specific columns.
func Summarize(u *unstructured.Unstructured) Row {
	o := u.Object
	r := Row{UID: string(u.GetUID()), Name: u.GetName(), Namespace: u.GetNamespace(), Kind: u.GetKind(),
		Created: u.GetCreationTimestamp().Time, Cells: map[string]string{}, Labels: u.GetLabels(), RV: u.GetResourceVersion(), Health: "neutral"}
	if u.GetDeletionTimestamp() != nil {
		r.Status, r.Health = "Terminating", "warn"
	}
	switch u.GetKind() {
	case "Pod":
		var ready, total, restarts int64
		status := str(o, "status", "phase")
		for _, cs := range slice(o, "status", "containerStatuses") {
			m, _ := cs.(map[string]any)
			total++
			if b, _ := m["ready"].(bool); b {
				ready++
			}
			restarts += num(m, "restartCount")
			if w := str(m, "state", "waiting", "reason"); w != "" {
				status = w
			}
			if t := str(m, "state", "terminated", "reason"); t != "" && status == "Running" {
				status = t
			}
		}
		if r.Status == "" {
			r.Status = status
		}
		r.Cells["ready"] = fmt.Sprintf("%d/%d", ready, total)
		r.Cells["restarts"] = fmt.Sprint(restarts)
		r.Cells["node"] = str(o, "spec", "nodeName")
		r.Cells["ip"] = str(o, "status", "podIP")
		r.Cells["qos"] = str(o, "status", "qosClass")
		switch {
		case r.Status == "Running" && ready == total, r.Status == "Succeeded", r.Status == "Completed":
			r.Health = "ok"
		case r.Status == "Pending", r.Status == "ContainerCreating", r.Status == "Running", r.Status == "Terminating":
			r.Health = "warn"
		default:
			r.Health = "error"
		}
	case "Deployment", "StatefulSet", "ReplicaSet":
		desired := num(o, "spec", "replicas")
		ready := num(o, "status", "readyReplicas")
		r.Cells["ready"] = fmt.Sprintf("%d/%d", ready, desired)
		r.Cells["upToDate"] = fmt.Sprint(num(o, "status", "updatedReplicas"))
		r.Cells["available"] = fmt.Sprint(num(o, "status", "availableReplicas"))
		r.Cells["images"] = images(o)
		r.Status, r.Health = readyHealth(ready, desired)
	case "DaemonSet":
		desired := num(o, "status", "desiredNumberScheduled")
		ready := num(o, "status", "numberReady")
		r.Cells["ready"] = fmt.Sprintf("%d/%d", ready, desired)
		r.Cells["images"] = images(o)
		r.Status, r.Health = readyHealth(ready, desired)
	case "Job":
		comp := num(o, "spec", "completions")
		succ := num(o, "status", "succeeded")
		r.Cells["completions"] = fmt.Sprintf("%d/%d", succ, max(comp, 1))
		r.Status, r.Health = "Running", "warn"
		for _, c := range slice(o, "status", "conditions") {
			m, _ := c.(map[string]any)
			if str(m, "status") == "True" {
				r.Status = str(m, "type")
				if r.Status == "Complete" {
					r.Health = "ok"
				} else if r.Status == "Failed" {
					r.Health = "error"
				}
			}
		}
	case "CronJob":
		r.Cells["schedule"] = str(o, "spec", "schedule")
		r.Cells["suspend"] = str(o, "spec", "suspend")
		r.Cells["active"] = fmt.Sprint(len(slice(o, "status", "active")))
		r.Cells["lastSchedule"] = str(o, "status", "lastScheduleTime")
		r.Health = "ok"
	case "Service":
		r.Cells["type"] = str(o, "spec", "type")
		r.Cells["clusterIP"] = str(o, "spec", "clusterIP")
		var ports []string
		for _, p := range slice(o, "spec", "ports") {
			m, _ := p.(map[string]any)
			ports = append(ports, fmt.Sprintf("%v/%v", m["port"], m["protocol"]))
		}
		r.Cells["ports"] = strings.Join(ports, ",")
		var ext []string
		for _, i := range slice(o, "status", "loadBalancer", "ingress") {
			m, _ := i.(map[string]any)
			ext = append(ext, str(m, "ip")+str(m, "hostname"))
		}
		r.Cells["external"] = strings.Join(ext, ",")
		r.Health = "ok"
	case "Ingress":
		var hosts []string
		for _, rule := range slice(o, "spec", "rules") {
			m, _ := rule.(map[string]any)
			hosts = append(hosts, str(m, "host"))
		}
		r.Cells["hosts"] = strings.Join(hosts, ",")
		r.Cells["class"] = str(o, "spec", "ingressClassName")
		r.Health = "ok"
	case "Node":
		r.Status, r.Health = "NotReady", "error"
		for _, c := range slice(o, "status", "conditions") {
			m, _ := c.(map[string]any)
			if str(m, "type") == "Ready" && str(m, "status") == "True" {
				r.Status, r.Health = "Ready", "ok"
			}
		}
		if b, _, _ := unstructured.NestedBool(o, "spec", "unschedulable"); b {
			r.Status += ",SchedulingDisabled"
			r.Health = "warn"
		}
		var roles []string
		for k := range u.GetLabels() {
			if strings.HasPrefix(k, "node-role.kubernetes.io/") {
				roles = append(roles, strings.TrimPrefix(k, "node-role.kubernetes.io/"))
			}
		}
		r.Cells["roles"] = strings.Join(roles, ",")
		r.Cells["version"] = str(o, "status", "nodeInfo", "kubeletVersion")
		r.Cells["cpu"] = str(o, "status", "capacity", "cpu")
		r.Cells["memory"] = str(o, "status", "capacity", "memory")
		r.Cells["os"] = str(o, "status", "nodeInfo", "osImage")
	case "PersistentVolumeClaim", "PersistentVolume":
		r.Status = str(o, "status", "phase")
		r.Cells["capacity"] = str(o, "status", "capacity", "storage")
		r.Cells["storageClass"] = str(o, "spec", "storageClassName")
		r.Cells["volume"] = str(o, "spec", "volumeName")
		if r.Status == "Bound" {
			r.Health = "ok"
		} else if r.Status == "Pending" {
			r.Health = "warn"
		} else {
			r.Health = "error"
		}
	case "Event":
		r.Status = str(o, "type")
		r.Cells["reason"] = str(o, "reason")
		r.Cells["object"] = str(o, "involvedObject", "kind") + "/" + str(o, "involvedObject", "name")
		r.Cells["message"] = str(o, "message")
		r.Cells["count"] = str(o, "count")
		r.Cells["lastSeen"] = str(o, "lastTimestamp")
		if r.Status == "Warning" {
			r.Health = "warn"
		} else {
			r.Health = "ok"
		}
	case "Namespace":
		r.Status = str(o, "status", "phase")
		r.Health = "ok"
	case "Secret":
		r.Cells["type"] = str(o, "type")
		d, _, _ := unstructured.NestedMap(o, "data")
		r.Cells["keys"] = fmt.Sprint(len(d))
	case "ConfigMap":
		d, _, _ := unstructured.NestedMap(o, "data")
		r.Cells["keys"] = fmt.Sprint(len(d))
	default:
		// Generic: use the Ready condition and common fields of CRDs (Flux,
		// Argo, cert-manager, Crossplane...).
		for _, c := range slice(o, "status", "conditions") {
			m, _ := c.(map[string]any)
			if t := str(m, "type"); t == "Ready" || t == "Available" || t == "Synced" {
				if str(m, "status") == "True" {
					r.Status, r.Health = t, "ok"
				} else {
					r.Status, r.Health = "Not"+t, "error"
					if reason := str(m, "reason"); reason != "" {
						r.Status = reason
					}
				}
				r.Cells["message"] = str(m, "message")
			}
		}
		if hs := str(o, "status", "health", "status"); hs != "" { // Argo CD
			r.Status = hs + "/" + str(o, "status", "sync", "status")
			r.Health = map[string]string{"Healthy": "ok", "Progressing": "warn", "Degraded": "error", "Missing": "error"}[hs]
		}
	}
	if r.Status == "" {
		if p := str(o, "status", "phase"); p != "" {
			r.Status = p
		}
	}
	return r
}

func images(o map[string]any) string {
	var out []string
	for _, c := range slice(o, "spec", "template", "spec", "containers") {
		m, _ := c.(map[string]any)
		out = append(out, str(m, "image"))
	}
	return strings.Join(out, ",")
}

func readyHealth(ready, desired int64) (string, string) {
	switch {
	case desired == 0:
		return "Scaled to 0", "neutral"
	case ready >= desired:
		return "Ready", "ok"
	case ready == 0:
		return "Unavailable", "error"
	}
	return "Progressing", "warn"
}
