// Package tools locates and runs external CLIs (kubectl, helm, kustomize,
// stern, trivy, kubescape, aws, az, gcloud, doctl...). Bundled binaries in the
// application's tools directory take precedence over $PATH, which supports
// air-gapped installs.
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Known lists every CLI the IDE integrates with.
var Known = []string{"kubectl", "helm", "kustomize", "stern", "k9s", "trivy", "kubescape", "flux", "argocd", "aws", "az", "gcloud", "doctl", "oci", "ibmcloud", "linode-cli", "vultr-cli", "terraform", "ansible-playbook", "git", "velero", "kube-bench"}

// Runner executes tools.
type Runner struct {
	BundleDir string
	Proxy     string
}

// Find returns the path of a tool or an error if it is not installed.
func (r *Runner) Find(name string) (string, error) {
	exe := name
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if r.BundleDir != "" {
		p := filepath.Join(r.BundleDir, exe)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return exec.LookPath(name)
}

// Status reports which tools are installed and their versions.
func (r *Runner) Status(ctx context.Context) map[string]string {
	out := map[string]string{}
	for _, t := range Known {
		p, err := r.Find(t)
		if err != nil {
			out[t] = ""
			continue
		}
		out[t] = p
	}
	return out
}

// Result is the output of a tool invocation.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// Run executes a tool with KUBECONFIG pointed at kubeconfig (if non-empty).
func (r *Runner) Run(ctx context.Context, kubeconfig, name string, args []string, stdin []byte, extraEnv ...string) (*Result, error) {
	p, err := r.Find(name)
	if err != nil {
		return nil, fmt.Errorf("%s is not installed (looked in bundle and PATH)", name)
	}
	cmd := exec.CommandContext(ctx, p, args...)
	cmd.Env = r.Env(kubeconfig, extraEnv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err = cmd.Run()
	res := &Result{Stdout: so.String(), Stderr: se.String()}
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
		return res, fmt.Errorf("%s %s failed: %s", name, strings.Join(args, " "), strings.TrimSpace(se.String()))
	}
	return res, err
}

// Env returns the environment for tool processes.
func (r *Runner) Env(kubeconfig string, extra ...string) []string {
	env := os.Environ()
	if kubeconfig != "" {
		env = append(env, "KUBECONFIG="+kubeconfig)
	}
	if r.BundleDir != "" {
		env = append(env, "PATH="+r.BundleDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	if r.Proxy != "" {
		env = append(env, "HTTPS_PROXY="+r.Proxy, "HTTP_PROXY="+r.Proxy)
	}
	return append(env, extra...)
}
