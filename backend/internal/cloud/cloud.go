// Package cloud discovers managed Kubernetes clusters and related cloud
// resources using each provider's official CLI and the user's existing cloud
// credentials (AWS SSO/IAM, Azure AD/Managed Identity, gcloud auth, doctl,
// oci, ibmcloud, linode-cli, vultr-cli). Kubeconfigs are imported with the
// provider's exec auth plugin so tokens stay short-lived.
package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/clusters"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/tools"
)

// Service performs cloud discovery.
type Service struct {
	Tools    *tools.Runner
	Clusters *clusters.Manager
}

// ManagedCluster is a cluster discovered in a cloud account.
type ManagedCluster struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Version  string `json:"version,omitempty"`
	Status   string `json:"status,omitempty"`
	Account  string `json:"account,omitempty"` // profile / subscription / project / compartment
	Group    string `json:"group,omitempty"`   // azure resource group
}

// Options carry provider specific scope.
type Options struct {
	Profile      string `json:"profile"`      // aws profile
	Region       string `json:"region"`       // aws region / oci region
	Subscription string `json:"subscription"` // azure
	Project      string `json:"project"`      // gcp
	Compartment  string `json:"compartment"`  // oci
}

func (s *Service) run(ctx context.Context, name string, args []string, extraEnv ...string) (string, error) {
	r, err := s.Tools.Run(ctx, "", name, args, nil, extraEnv...)
	if err != nil {
		return "", err
	}
	return r.Stdout, nil
}

func awsArgs(o Options, args ...string) []string {
	if o.Profile != "" {
		args = append(args, "--profile", o.Profile)
	}
	if o.Region != "" {
		args = append(args, "--region", o.Region)
	}
	return append(args, "--output", "json")
}

// Discover lists clusters for a provider.
func (s *Service) Discover(ctx context.Context, provider string, o Options) ([]ManagedCluster, error) {
	var out []ManagedCluster
	switch provider {
	case "aws":
		raw, err := s.run(ctx, "aws", awsArgs(o, "eks", "list-clusters"))
		if err != nil {
			return nil, err
		}
		var r struct{ Clusters []string }
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		for _, c := range r.Clusters {
			mc := ManagedCluster{Provider: "aws", ID: c, Name: c, Location: o.Region, Account: o.Profile}
			if d, err := s.run(ctx, "aws", awsArgs(o, "eks", "describe-cluster", "--name", c)); err == nil {
				var dr struct {
					Cluster struct{ Version, Status, Arn string }
				}
				if json.Unmarshal([]byte(d), &dr) == nil {
					mc.Version, mc.Status = dr.Cluster.Version, dr.Cluster.Status
				}
			}
			out = append(out, mc)
		}
	case "azure":
		args := []string{"aks", "list", "-o", "json"}
		if o.Subscription != "" {
			args = append(args, "--subscription", o.Subscription)
		}
		raw, err := s.run(ctx, "az", args)
		if err != nil {
			return nil, err
		}
		var r []struct {
			Name, Location, KubernetesVersion, ResourceGroup, ID string
			ProvisioningState                                    string
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		for _, c := range r {
			out = append(out, ManagedCluster{Provider: "azure", ID: c.ID, Name: c.Name, Location: c.Location, Version: c.KubernetesVersion, Status: c.ProvisioningState, Group: c.ResourceGroup, Account: o.Subscription})
		}
	case "gcp":
		args := []string{"container", "clusters", "list", "--format=json"}
		if o.Project != "" {
			args = append(args, "--project", o.Project)
		}
		raw, err := s.run(ctx, "gcloud", args)
		if err != nil {
			return nil, err
		}
		var r []struct {
			Name, Location, CurrentMasterVersion, Status, SelfLink string
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		for _, c := range r {
			out = append(out, ManagedCluster{Provider: "gcp", ID: c.Name, Name: c.Name, Location: c.Location, Version: c.CurrentMasterVersion, Status: c.Status, Account: o.Project})
		}
	case "digitalocean":
		raw, err := s.run(ctx, "doctl", []string{"kubernetes", "cluster", "list", "-o", "json"})
		if err != nil {
			return nil, err
		}
		var r []struct {
			ID, Name, Region, Version string
			Status                    struct{ State string }
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		for _, c := range r {
			out = append(out, ManagedCluster{Provider: "digitalocean", ID: c.ID, Name: c.Name, Location: c.Region, Version: c.Version, Status: c.Status.State})
		}
	case "oracle":
		if o.Compartment == "" {
			return nil, fmt.Errorf("compartment OCID required")
		}
		raw, err := s.run(ctx, "oci", []string{"ce", "cluster", "list", "--compartment-id", o.Compartment, "--all"})
		if err != nil {
			return nil, err
		}
		var r struct {
			Data []struct {
				ID                string `json:"id"`
				Name              string `json:"name"`
				KubernetesVersion string `json:"kubernetes-version"`
				LifecycleState    string `json:"lifecycle-state"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		for _, c := range r.Data {
			out = append(out, ManagedCluster{Provider: "oracle", ID: c.ID, Name: c.Name, Location: o.Region, Version: c.KubernetesVersion, Status: c.LifecycleState, Account: o.Compartment})
		}
	case "ibm":
		raw, err := s.run(ctx, "ibmcloud", []string{"ks", "clusters", "--output", "json"})
		if err != nil {
			return nil, err
		}
		var r []struct {
			ID, Name, Location, MasterKubeVersion, State string
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		for _, c := range r {
			out = append(out, ManagedCluster{Provider: "ibm", ID: c.ID, Name: c.Name, Location: c.Location, Version: c.MasterKubeVersion, Status: c.State})
		}
	case "linode":
		raw, err := s.run(ctx, "linode-cli", []string{"lke", "clusters-list", "--json"})
		if err != nil {
			return nil, err
		}
		var r []struct {
			ID         int    `json:"id"`
			Label      string `json:"label"`
			Region     string `json:"region"`
			K8sVersion string `json:"k8s_version"`
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		for _, c := range r {
			out = append(out, ManagedCluster{Provider: "linode", ID: fmt.Sprint(c.ID), Name: c.Label, Location: c.Region, Version: c.K8sVersion})
		}
	case "vultr":
		raw, err := s.run(ctx, "vultr-cli", []string{"kubernetes", "list", "-o", "json"})
		if err != nil {
			return nil, err
		}
		var r struct {
			VKEClusters []struct {
				ID, Label, Region, Version, Status string
			} `json:"vke_clusters"`
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		for _, c := range r.VKEClusters {
			out = append(out, ManagedCluster{Provider: "vultr", ID: c.ID, Name: c.Label, Location: c.Region, Version: c.Version, Status: c.Status})
		}
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	return out, nil
}

// Import writes a kubeconfig for the cluster and registers it.
func (s *Service) Import(ctx context.Context, c ManagedCluster, o Options) (string, error) {
	tmp, err := os.MkdirTemp("", "kubenova-cloud-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	kc := filepath.Join(tmp, "config")
	env := []string{"KUBECONFIG=" + kc}
	var data []byte
	switch c.Provider {
	case "aws":
		_, err = s.run(ctx, "aws", awsArgs(Options{Profile: o.Profile, Region: c.Location}, "eks", "update-kubeconfig", "--name", c.Name, "--kubeconfig", kc, "--alias", "eks-"+c.Name), env...)
	case "azure":
		args := []string{"aks", "get-credentials", "--name", c.Name, "--resource-group", c.Group, "--file", kc, "--overwrite-existing"}
		if c.Account != "" {
			args = append(args, "--subscription", c.Account)
		}
		if _, err = s.run(ctx, "az", args); err == nil {
			// Convert to Azure AD (Entra ID) exec auth when kubelogin is present.
			_, _ = s.run(ctx, "kubelogin", []string{"convert-kubeconfig", "-l", "azurecli", "--kubeconfig", kc})
		}
	case "gcp":
		args := []string{"container", "clusters", "get-credentials", c.Name, "--location", c.Location}
		if c.Account != "" {
			args = append(args, "--project", c.Account)
		}
		_, err = s.run(ctx, "gcloud", args, env...)
	case "digitalocean":
		var out string
		out, err = s.run(ctx, "doctl", []string{"kubernetes", "cluster", "kubeconfig", "show", c.ID})
		data = []byte(out)
	case "oracle":
		args := []string{"ce", "cluster", "create-kubeconfig", "--cluster-id", c.ID, "--file", kc, "--token-version", "2.0.0"}
		if c.Location != "" {
			args = append(args, "--region", c.Location)
		}
		_, err = s.run(ctx, "oci", args)
	case "ibm":
		_, err = s.run(ctx, "ibmcloud", []string{"ks", "cluster", "config", "--cluster", c.ID}, env...)
	case "linode":
		var out string
		out, err = s.run(ctx, "linode-cli", []string{"lke", "kubeconfig-view", c.ID, "--json"})
		if err == nil {
			var r []struct{ Kubeconfig string }
			if json.Unmarshal([]byte(out), &r) == nil && len(r) > 0 {
				data, err = base64.StdEncoding.DecodeString(r[0].Kubeconfig)
			}
		}
	case "vultr":
		var out string
		out, err = s.run(ctx, "vultr-cli", []string{"kubernetes", "config", c.ID})
		if err == nil {
			out = strings.TrimSpace(out)
			if dec, derr := base64.StdEncoding.DecodeString(out); derr == nil {
				data = dec
			} else {
				data = []byte(out)
			}
		}
	default:
		return "", fmt.Errorf("unknown provider %q", c.Provider)
	}
	if err != nil {
		return "", err
	}
	if data == nil {
		data, err = os.ReadFile(kc)
		if err != nil {
			return "", fmt.Errorf("provider CLI did not produce a kubeconfig: %w", err)
		}
	}
	return s.Clusters.ImportKubeconfig(c.Provider+"-"+c.Name, data)
}

// Login runs the provider's interactive login (AWS SSO, Azure AD, Managed Identity, gcloud).
func (s *Service) Login(ctx context.Context, provider string, o Options, method string) (string, error) {
	switch provider {
	case "aws":
		return s.run(ctx, "aws", []string{"sso", "login", "--profile", o.Profile})
	case "azure":
		if method == "managed-identity" {
			return s.run(ctx, "az", []string{"login", "--identity"})
		}
		return s.run(ctx, "az", []string{"login"})
	case "gcp":
		return s.run(ctx, "gcloud", []string{"auth", "login", "--update-adc"})
	}
	return "", fmt.Errorf("interactive login not supported for %s; configure the CLI credentials", provider)
}

// Profiles lists configured AWS profiles / Azure subscriptions / GCP projects.
func (s *Service) Profiles(ctx context.Context, provider string) ([]string, error) {
	var out string
	var err error
	switch provider {
	case "aws":
		out, err = s.run(ctx, "aws", []string{"configure", "list-profiles"})
		return strings.Fields(out), err
	case "azure":
		out, err = s.run(ctx, "az", []string{"account", "list", "--query", "[].id", "-o", "tsv"})
		return strings.Fields(out), err
	case "gcp":
		out, err = s.run(ctx, "gcloud", []string{"projects", "list", "--format=value(projectId)"})
		return strings.Fields(out), err
	}
	return nil, nil
}

// cloudResources maps provider/resource to the CLI that lists it.
var cloudResources = map[string]map[string][]string{
	"aws": {
		"iam-identity": {"aws", "sts", "get-caller-identity"},
		"elb":          {"aws", "elbv2", "describe-load-balancers"},
		"ebs":          {"aws", "ec2", "describe-volumes", "--filters", "Name=tag-key,Values=kubernetes.io/created-for/pvc/name"},
		"efs":          {"aws", "efs", "describe-file-systems"},
		"route53":      {"aws", "route53", "list-hosted-zones"},
		"cloudwatch":   {"aws", "cloudwatch", "describe-alarms", "--state-value", "ALARM"},
		"loggroups":    {"aws", "logs", "describe-log-groups", "--log-group-name-prefix", "/aws/eks"},
	},
	"azure": {
		"identity":      {"az", "account", "show", "-o", "json"},
		"disks":         {"az", "disk", "list", "-o", "json"},
		"files":         {"az", "storage", "account", "list", "-o", "json"},
		"monitor":       {"az", "monitor", "metrics", "alert", "list", "-o", "json"},
		"identities":    {"az", "identity", "list", "-o", "json"},
		"loadbalancers": {"az", "network", "lb", "list", "-o", "json"},
	},
	"gcp": {
		"logging":           {"gcloud", "logging", "read", "resource.type=k8s_container AND severity>=ERROR", "--limit", "100", "--format=json", "--freshness", "1h"},
		"monitoring":        {"gcloud", "alpha", "monitoring", "policies", "list", "--format=json"},
		"gcr":               {"gcloud", "container", "images", "list", "--format=json"},
		"artifact-registry": {"gcloud", "artifacts", "repositories", "list", "--format=json"},
		"disks":             {"gcloud", "compute", "disks", "list", "--format=json"},
	},
	"digitalocean": {
		"loadbalancers": {"doctl", "compute", "load-balancer", "list", "-o", "json"},
		"volumes":       {"doctl", "compute", "volume", "list", "-o", "json"},
		"registry":      {"doctl", "registry", "repository", "list-v2", "-o", "json"},
	},
}

// Resources lists a provider resource (ELB, EBS, Azure Disks, GCR, ...).
func (s *Service) Resources(ctx context.Context, provider, kind string, o Options) (json.RawMessage, error) {
	cmd, ok := cloudResources[provider][kind]
	if !ok {
		return nil, fmt.Errorf("unsupported %s resource %q", provider, kind)
	}
	args := cmd[1:]
	if provider == "aws" {
		args = awsArgs(o, args...)
	}
	out, err := s.run(ctx, cmd[0], args)
	if err != nil {
		return nil, err
	}
	if !json.Valid([]byte(out)) {
		b, _ := json.Marshal(map[string]string{"raw": out})
		return b, nil
	}
	return json.RawMessage(out), nil
}

// ResourceKinds returns the supported cloud resource kinds per provider.
func ResourceKinds() map[string][]string {
	out := map[string][]string{}
	for p, m := range cloudResources {
		for k := range m {
			out[p] = append(out[p], k)
		}
	}
	return out
}
