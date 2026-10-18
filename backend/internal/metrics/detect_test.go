package metrics

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func svc(ns, name string, ports ...int32) corev1.Service {
	s := corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	for _, p := range ports {
		s.Spec.Ports = append(s.Spec.Ports, corev1.ServicePort{Port: p})
	}
	return s
}

func TestPickBackends(t *testing.T) {
	cases := []struct {
		name string
		svcs []corev1.Service
		want map[string]Endpoint
	}{
		{"victoria-metrics-k8s-stack in custom namespace", []corev1.Service{
			svc("observability", "vmsingle-vm-stack", 8429),
			svc("observability", "vmalertmanager-vm-stack", 9093),
			svc("observability", "vm-stack-grafana", 80),
			svc("observability", "vm-stack-prometheus-node-exporter", 9100),
			svc("observability", "vm-stack-kube-state-metrics", 8080),
			svc("observability", "vm-stack-victoria-metrics-operator", 8080),
		}, map[string]Endpoint{
			VictoriaMetrics: {Namespace: "observability", Service: "vmsingle-vm-stack", Port: "8429"},
			Alertmanager:    {Namespace: "observability", Service: "vmalertmanager-vm-stack", Port: "9093"},
			Grafana:         {Namespace: "observability", Service: "vm-stack-grafana", Port: "80"},
		}},
		{"victoria-metrics cluster mode", []corev1.Service{
			svc("vm", "vmselect-prod", 8481), svc("vm", "vminsert-prod", 8480), svc("vm", "vmstorage-prod", 8482, 8400, 8401),
		}, map[string]Endpoint{
			VictoriaMetrics: {Namespace: "vm", Service: "vmselect-prod", Port: "8481", Path: "/select/0/prometheus"},
		}},
		{"victoria-metrics-single chart", []corev1.Service{svc("metrics", "vm-victoria-metrics-single-server", 8428)},
			map[string]Endpoint{VictoriaMetrics: {Namespace: "metrics", Service: "vm-victoria-metrics-single-server", Port: "8428"}}},
		{"kube-prometheus-stack", []corev1.Service{
			svc("mon", "kps-kube-prometheus-stack-prometheus", 9090, 8080),
			svc("mon", "prometheus-operated", 9090),
			svc("mon", "kps-kube-prometheus-stack-alertmanager", 9093, 8080),
			svc("mon", "alertmanager-operated", 9093, 9094),
			svc("mon", "kps-grafana", 80),
			svc("mon", "kps-kube-prometheus-stack-operator", 443),
			svc("kube-system", "kps-kube-prometheus-stack-kubelet", 10250, 10255, 4194),
			svc("mon", "kube-prometheus-stack-grafana", 80),
		}, map[string]Endpoint{
			Prometheus:   {Namespace: "mon", Service: "prometheus-operated", Port: "9090"},
			Alertmanager: {Namespace: "mon", Service: "alertmanager-operated", Port: "9093"},
			Grafana:      {Namespace: "mon", Service: "kps-grafana", Port: "80"},
		}},
		{"loki and tempo", []corev1.Service{
			svc("logs", "loki-gateway", 80), svc("logs", "loki-read", 3100), svc("logs", "loki-memberlist", 7946), svc("logs", "loki-canary", 3500),
			svc("tracing", "tempo", 3200, 3100),
		}, map[string]Endpoint{
			Loki:  {Namespace: "logs", Service: "loki-gateway", Port: "80"},
			Tempo: {Namespace: "tracing", Service: "tempo", Port: "3200"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pick(c.svcs)
			for kind, w := range c.want {
				g := got[kind]
				if g.Namespace != w.Namespace || g.Service != w.Service || g.Port != w.Port || g.Path != w.Path {
					t.Errorf("%s: got %+v, want %+v", kind, g, w)
				}
			}
			for kind := range got {
				if _, ok := c.want[kind]; !ok {
					t.Errorf("unexpected %s: %+v", kind, got[kind])
				}
			}
		})
	}
}
