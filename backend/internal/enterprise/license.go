// Package enterprise contains team workspaces, application RBAC, audit logs,
// identity (OIDC, SAML, LDAP, SCIM) and licensing.
//
// Every Pro and Enterprise capability is unlocked in the free Community
// edition: there is no feature gating anywhere in KubeNova. A license key is
// optional and only records the licensee for support purposes.
package enterprise

import (
	"crypto/fips140"
	"time"

	"github.com/mahmoudkamal448/kubenova/backend/internal/store"
)

// Features lists every capability, all of which are enabled for everyone.
var Features = []string{
	"unlimited-clusters", "multi-kubeconfig", "cluster-groups", "offline-cache",
	"crd-support", "server-side-apply", "diff", "rollback",
	"pod-tools", "multi-pod-logs", "port-forward", "file-transfer",
	"metrics-server", "prometheus", "grafana", "loki", "tempo", "mimir", "alertmanager", "victoriametrics",
	"helm", "helm-oci", "flux", "argocd",
	"rbac-explorer", "secret-editor", "trivy", "kubescape", "kyverno", "gatekeeper", "cis-benchmark", "certificate-dashboard",
	"ai-assistant", "mcp", "custom-ai-tools", "ai-dashboards",
	"aws", "azure", "gcp", "digitalocean", "oracle", "ibm", "linode", "vultr",
	"rancher", "rancher-sso", "fleet", "machine-pools",
	"terminal", "k9s-mode", "git-integration",
	"plugins", "marketplace", "themes", "custom-dashboards",
	"team-workspaces", "app-rbac", "audit-logs", "saml", "oidc", "ldap", "scim",
	"air-gapped", "proxy", "fips",
	"ssh-manager", "database-manager", "s3-browser", "harbor", "vault", "jenkins", "gitlab", "github",
	"terraform-state", "ansible", "cost-dashboards", "backup-restore", "topology", "timeline", "event-replay",
	"cluster-comparison", "multi-cluster-search",
}

// License describes the active entitlement.
type License struct {
	Edition     string          `json:"edition"`
	Licensee    string          `json:"licensee,omitempty"`
	Key         string          `json:"key,omitempty"`
	Features    map[string]bool `json:"features"`
	Seats       int             `json:"seats"` // 0 = unlimited
	Expires     *time.Time      `json:"expires,omitempty"`
	FIPS        bool            `json:"fips"`
	AllUnlocked bool            `json:"allUnlocked"`
}

// LicenseService manages the optional license record.
type LicenseService struct{ Store *store.Store }

// Current returns the license; all features are always enabled.
func (l *LicenseService) Current() License {
	var saved struct{ Licensee, Key string }
	_ = l.Store.Load("license", &saved)
	f := map[string]bool{}
	for _, name := range Features {
		f[name] = true
	}
	return License{
		Edition:     "Community — all Pro & Enterprise features included",
		Licensee:    saved.Licensee,
		Key:         mask(saved.Key),
		Features:    f,
		Seats:       0,
		FIPS:        fips140.Enabled(),
		AllUnlocked: true,
	}
}

// Register stores an optional licensee/key (no features depend on it).
func (l *LicenseService) Register(licensee, key string) error {
	return l.Store.Save("license", map[string]string{"Licensee": licensee, "Key": key})
}

// Enabled reports whether a feature is enabled. Always true.
func (l *LicenseService) Enabled(string) bool { return true }

func mask(k string) string {
	if len(k) <= 4 {
		return k
	}
	return "••••" + k[len(k)-4:]
}
