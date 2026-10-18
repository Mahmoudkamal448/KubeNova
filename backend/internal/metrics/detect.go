package metrics

import (
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// candidate is a service that looks like an observability backend. Lower
// rank wins when several match.
type candidate struct {
	ep   Endpoint
	rank int
}

func portOf(svc corev1.Service, prefer ...int32) (string, bool) {
	for _, want := range prefer {
		for _, p := range svc.Spec.Ports {
			if p.Port == want {
				return strconv.Itoa(int(p.Port)), true
			}
		}
	}
	for _, p := range svc.Spec.Ports {
		if p.Name == "http" || p.Name == "web" || p.Name == "http-web" || p.Name == "http-metrics" {
			return strconv.Itoa(int(p.Port)), true
		}
	}
	return "", false
}

func portExact(svc corev1.Service, ports ...int32) (string, bool) {
	for _, want := range ports {
		for _, p := range svc.Spec.Ports {
			if p.Port == want {
				return strconv.Itoa(int(p.Port)), true
			}
		}
	}
	return "", false
}

// classify decides whether a service is a known backend, by the names and
// ports used by the common charts and operators (kube-prometheus-stack,
// prometheus, victoria-metrics-k8s-stack/-single/-cluster and the VM
// operator, Grafana's Mimir/Loki/Tempo charts, Rancher monitoring),
// whatever the release name and namespace.
func classify(svc corev1.Service) (string, candidate, bool) {
	n := strings.ToLower(svc.Name)
	app := strings.ToLower(svc.Labels["app.kubernetes.io/name"] + " " + svc.Labels["app"])
	has := func(s string) bool { return strings.Contains(n, s) || strings.Contains(app, s) }
	ep := func(kind, port, path string) Endpoint {
		return Endpoint{Kind: kind, Namespace: svc.Namespace, Service: svc.Name, Port: port, Path: path}
	}
	// Exporters and operators expose metrics but are not query backends.
	for _, skip := range []string{"exporter", "operator", "pushgateway", "kube-state", "webhook", "headless-thanos"} {
		if strings.Contains(n, skip) {
			return "", candidate{}, false
		}
	}
	switch {
	case strings.HasPrefix(n, "vmselect") || has("vmselect"):
		if p, ok := portOf(svc, 8481); ok {
			return VictoriaMetrics, candidate{ep(VictoriaMetrics, p, "/select/0/prometheus"), 1}, true
		}
	case strings.HasPrefix(n, "vmsingle") || has("victoria-metrics-single") || has("vmsingle"):
		if p, ok := portOf(svc, 8429, 8428); ok {
			return VictoriaMetrics, candidate{ep(VictoriaMetrics, p, ""), 0}, true
		}
	case has("vmalertmanager") || (has("alertmanager") && !has("config")):
		if p, ok := portOf(svc, 9093); ok {
			r := 1
			if n == "alertmanager-operated" {
				r = 0
			}
			return Alertmanager, candidate{ep(Alertmanager, p, ""), r}, true
		}
	case has("mimir") && (has("nginx") || has("gateway") || has("query-frontend")):
		if p, ok := portOf(svc, 80, 8080); ok {
			return Mimir, candidate{ep(Mimir, p, "/prometheus"), 0}, true
		}
	case has("thanos") && has("query") && !has("frontend-grpc"):
		if p, ok := portOf(svc, 9090, 10902); ok {
			return Prometheus, candidate{ep(Prometheus, p, ""), 2}, true
		}
	case has("loki") && !has("memberlist") && !has("canary") && !has("headless"):
		if p, ok := portOf(svc, 80, 3100); ok {
			r := 2
			if has("gateway") {
				r = 0
			} else if has("read") || n == "loki" {
				r = 1
			}
			return Loki, candidate{ep(Loki, p, ""), r}, true
		}
	case has("tempo") && !has("memberlist") && !has("headless"):
		if p, ok := portOf(svc, 3200, 3100); ok {
			r := 1
			if has("query-frontend") || n == "tempo" {
				r = 0
			}
			return Tempo, candidate{ep(Tempo, p, ""), r}, true
		}
	case has("grafana") && !has("agent") && !has("alloy"):
		if p, ok := portOf(svc, 80, 3000); ok {
			return Grafana, candidate{ep(Grafana, p, ""), 0}, true
		}
	case has("prometheus"):
		if p, ok := portExact(svc, 9090, 80); ok {
			r := 3
			switch {
			case n == "prometheus-operated":
				r = 0
			case strings.HasSuffix(n, "-prometheus") || n == "prometheus-server" || strings.HasSuffix(n, "prometheus-server"):
				r = 1
			}
			return Prometheus, candidate{ep(Prometheus, p, ""), r}, true
		}
	}
	return "", candidate{}, false
}

// pick returns the best backend of each kind among the services.
func pick(svcs []corev1.Service) map[string]Endpoint {
	best := map[string]candidate{}
	for _, svc := range svcs {
		kind, c, ok := classify(svc)
		if !ok {
			continue
		}
		if cur, seen := best[kind]; !seen || c.rank < cur.rank {
			best[kind] = c
		}
	}
	out := map[string]Endpoint{}
	for k, c := range best {
		out[k] = c.ep
	}
	return out
}
