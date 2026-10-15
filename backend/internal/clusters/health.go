package clusters

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// Health is the result of a cluster health probe.
type Health struct {
	Status         string     `json:"status"` // healthy | degraded | unreachable | unknown
	Version        string     `json:"version,omitempty"`
	Platform       string     `json:"platform,omitempty"`
	Distribution   string     `json:"distribution,omitempty"`
	LatencyMs      int64      `json:"latencyMs"`
	CheckedAt      time.Time  `json:"checkedAt"`
	Error          string     `json:"error,omitempty"`
	Nodes          int        `json:"nodes"`
	NodesReady     int        `json:"nodesReady"`
	Namespaces     int        `json:"namespaces"`
	Pods           int        `json:"pods"`
	PodsRunning    int        `json:"podsRunning"`
	CPUCapacity    int64      `json:"cpuCapacityMilli"`
	MemoryCapacity int64      `json:"memoryCapacityBytes"`
	CertExpiry     *time.Time `json:"certExpiry,omitempty"`
	CertWarning    string     `json:"certWarning,omitempty"`
	Cached         bool       `json:"cached,omitempty"`
}

// CheckHealth probes the API server, measures latency, detects version and
// collects a resource overview. When summary is false only the cheap checks
// (version + node readiness) run.
func (m *Manager) CheckHealth(ctx context.Context, id string, summary bool) (*Health, error) {
	h := &Health{Status: "unknown", CheckedAt: time.Now()}
	defer func() {
		m.mu.Lock()
		m.state.Health[id] = h
		if c, ok := m.clusters[id]; ok {
			c.Health = h
		}
		m.mu.Unlock()
	}()

	if exp, err := m.certExpiry(id); err == nil && exp != nil {
		h.CertExpiry = exp
		left := time.Until(*exp)
		switch {
		case left < 0:
			h.CertWarning = "client certificate expired"
		case left < 7*24*time.Hour:
			h.CertWarning = fmt.Sprintf("client certificate expires in %d days", int(left.Hours()/24))
		case left < 30*24*time.Hour:
			h.CertWarning = fmt.Sprintf("client certificate expires in %d days", int(left.Hours()/24))
		}
	}

	typed, err := m.Typed(id)
	if err != nil {
		h.Status, h.Error = "unreachable", err.Error()
		return h, err
	}
	start := time.Now()
	body, err := typed.Discovery().RESTClient().Get().AbsPath("/livez").Do(ctx).Raw()
	h.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		// Some distributions restrict /livez; fall back to /version.
		start = time.Now()
		if _, verr := typed.Discovery().ServerVersion(); verr != nil {
			h.Status, h.Error = "unreachable", verr.Error()
			return h, verr
		}
		h.LatencyMs = time.Since(start).Milliseconds()
	}
	_ = body
	if v, err := typed.Discovery().ServerVersion(); err == nil {
		h.Version, h.Platform = v.GitVersion, v.Platform
		h.Distribution = detectDistribution(v.GitVersion)
	}
	h.Status = "healthy"

	nodes, err := typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil {
		h.Nodes = len(nodes.Items)
		for _, n := range nodes.Items {
			for _, c := range n.Status.Conditions {
				if c.Type == "Ready" && c.Status == "True" {
					h.NodesReady++
				}
			}
			h.CPUCapacity += n.Status.Capacity.Cpu().MilliValue()
			h.MemoryCapacity += n.Status.Capacity.Memory().Value()
			if h.Distribution == "" {
				h.Distribution = distributionFromNode(n.Labels, n.Spec.ProviderID)
			}
		}
		if h.NodesReady < h.Nodes {
			h.Status = "degraded"
		}
	}
	if summary {
		if ns, err := typed.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); err == nil {
			h.Namespaces = len(ns.Items)
		}
		if pods, err := typed.CoreV1().Pods("").List(ctx, metav1.ListOptions{ResourceVersion: "0"}); err == nil {
			h.Pods = len(pods.Items)
			for _, p := range pods.Items {
				if p.Status.Phase == "Running" || p.Status.Phase == "Succeeded" {
					h.PodsRunning++
				}
			}
		}
	}
	return h, nil
}

func detectDistribution(v string) string {
	switch {
	case contains(v, "eks"):
		return "EKS"
	case contains(v, "gke"):
		return "GKE"
	case contains(v, "rke2"):
		return "RKE2"
	case contains(v, "k3s"):
		return "K3s"
	case contains(v, "aks"):
		return "AKS"
	}
	return ""
}

func distributionFromNode(labels map[string]string, providerID string) string {
	switch {
	case labels["kubernetes.azure.com/cluster"] != "":
		return "AKS"
	case labels["eks.amazonaws.com/nodegroup"] != "" || contains(providerID, "aws://"):
		return "EKS"
	case labels["cloud.google.com/gke-nodepool"] != "":
		return "GKE"
	case labels["doks.digitalocean.com/node-pool"] != "":
		return "DOKS"
	case contains(providerID, "oci://"):
		return "OKE"
	case contains(providerID, "linode://"):
		return "LKE"
	case contains(providerID, "vultr://"):
		return "VKE"
	case contains(providerID, "ibm://"):
		return "IKS"
	case labels["node.kubernetes.io/instance-type"] == "k3s":
		return "K3s"
	case contains(providerID, "kind://"):
		return "kind"
	}
	return ""
}

func contains(s, sub string) bool { return sub != "" && strings.Contains(s, sub) }

// certExpiry returns the expiry of the client certificate used for the cluster.
func (m *Manager) certExpiry(id string) (*time.Time, error) {
	raw, c, err := m.RawConfig(id)
	if err != nil {
		return nil, err
	}
	ctx := raw.Contexts[c.Context]
	if ctx == nil {
		return nil, nil
	}
	ai := raw.AuthInfos[ctx.AuthInfo]
	if ai == nil {
		return nil, nil
	}
	data := ai.ClientCertificateData
	if len(data) == 0 && ai.ClientCertificate != "" {
		data, err = os.ReadFile(ai.ClientCertificate)
		if err != nil {
			return nil, err
		}
	}
	return EarliestExpiry(data)
}

// EarliestExpiry parses PEM data and returns the earliest NotAfter.
func EarliestExpiry(data []byte) (*time.Time, error) {
	var earliest *time.Time
	for len(data) > 0 {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		t := cert.NotAfter
		if earliest == nil || t.Before(*earliest) {
			earliest = &t
		}
	}
	return earliest, nil
}

func applyProxy(cfg *rest.Config, proxy string) {
	u, err := url.Parse(proxy)
	if err != nil {
		return
	}
	cfg.Proxy = http.ProxyURL(u)
}
