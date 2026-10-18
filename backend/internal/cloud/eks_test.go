package cloud

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/clusters"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/store"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/tools"
)

// fakeAWS mimics the parts of the AWS CLI KubeNova uses: EKS discovery,
// update-kubeconfig (which writes an exec-plugin kubeconfig, like the real
// CLI) and get-token (the exec plugin itself). Every call is logged.
const fakeAWS = `#!/bin/sh
echo "$*" >> "$FAKE_LOG"
case "$*" in
  *"eks list-clusters"*) echo '{"clusters":["prod-eu","staging"]}' ;;
  *"eks describe-cluster"*) echo '{"cluster":{"version":"1.31","status":"ACTIVE","arn":"arn:aws:eks:eu-west-1:123:cluster/x"}}' ;;
  *"eks update-kubeconfig"*)
    while [ $# -gt 0 ]; do case "$1" in --kubeconfig) kc="$2"; shift;; --name) name="$2"; shift;; --alias) alias="$2"; shift;; esac; shift; done
    cat > "$kc" <<KC
apiVersion: v1
kind: Config
clusters:
- name: arn:aws:eks:eu-west-1:123:cluster/$name
  cluster: {server: "$FAKE_SERVER", certificate-authority-data: "$FAKE_CA"}
users:
- name: arn:aws:eks:eu-west-1:123:cluster/$name
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws
      args: [--region, eu-west-1, eks, get-token, --cluster-name, $name, --output, json]
      env: [{name: AWS_PROFILE, value: sso-admin}]
contexts:
- name: $alias
  context: {cluster: "arn:aws:eks:eu-west-1:123:cluster/$name", user: "arn:aws:eks:eu-west-1:123:cluster/$name"}
current-context: $alias
KC
    echo "Added new context $alias to $kc" ;;
  *"eks get-token"*) echo '{"kind":"ExecCredential","apiVersion":"client.authentication.k8s.io/v1beta1","spec":{},"status":{"token":"k8s-aws-v1.fake-token"}}' ;;
  *) echo "unsupported: $*" >&2; exit 1 ;;
esac
`

func TestEKSDiscoverImportConnect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	dir := t.TempDir()
	var authed atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k8s-aws-v1.fake-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authed.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"gitVersion":"v1.31.4-eks-2d5f260"}`))
		case "/api/v1/nodes":
			_, _ = w.Write([]byte(`{"kind":"NodeList","apiVersion":"v1","items":[]}`))
		default:
			_, _ = w.Write([]byte(`{"kind":"List","apiVersion":"v1","items":[]}`))
		}
	}))
	defer srv.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})

	bin := filepath.Join(dir, "bin")
	_ = os.MkdirAll(bin, 0o755)
	if err := os.WriteFile(filepath.Join(bin, "aws"), []byte(fakeAWS), 0o755); err != nil {
		t.Fatal(err)
	}
	logf := filepath.Join(dir, "aws.log")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG", logf)
	t.Setenv("FAKE_SERVER", srv.URL)
	t.Setenv("FAKE_CA", base64.StdEncoding.EncodeToString(ca))
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", filepath.Join(dir, "home"))

	st, _ := store.New(filepath.Join(dir, "data"))
	m, err := clusters.NewManager(st)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Tools: &tools.Runner{}, Clusters: m}
	ctx := context.Background()
	opts := Options{Profile: "sso-admin", Region: "eu-west-1"}

	found, err := s.Discover(ctx, "aws", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found[0].Name != "prod-eu" || found[0].Version != "1.31" || found[0].Status != "ACTIVE" || found[0].Location != "eu-west-1" {
		t.Fatalf("discover: %+v", found)
	}
	if _, err := s.Import(ctx, found[0], opts); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	var id string
	for _, c := range m.List("") {
		if c.Context == "eks-prod-eu" {
			id = c.ID
		}
	}
	if id == "" {
		t.Fatalf("imported cluster not listed: %+v", m.List(""))
	}
	m.Touch(id)
	h, err := m.CheckHealth(ctx, id, true)
	if err != nil {
		t.Fatal(err)
	}
	if h.Version != "v1.31.4-eks-2d5f260" || authed.Load() == 0 {
		t.Fatalf("connect through aws eks get-token failed: %+v (authed=%d)", h, authed.Load())
	}

	log, _ := os.ReadFile(logf)
	for _, want := range []string{"eks list-clusters --profile sso-admin --region eu-west-1", "eks update-kubeconfig --name prod-eu", "eks get-token --cluster-name prod-eu"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("aws was not called with %q; calls:\n%s", want, log)
		}
	}
}

func TestFixPathFindsShellTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not used on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "shellbin")
	_ = os.MkdirAll(bin, 0o755)
	_ = os.WriteFile(filepath.Join(bin, "aws"), []byte("#!/bin/sh\necho ok\n"), 0o755)
	// A login shell that adds a directory the app's own PATH lacks, like
	// Homebrew's shellenv in ~/.zprofile.
	sh := filepath.Join(dir, "fake-shell")
	_ = os.WriteFile(sh, []byte("#!/bin/sh\nPATH=\""+bin+":$PATH\"; export PATH\nshift; eval \"$1\"\n"), 0o755)
	t.Setenv("SHELL", sh)
	t.Setenv("PATH", "/usr/bin:/bin")
	tools.FixPath()
	if _, err := (&tools.Runner{}).Find("aws"); err != nil {
		t.Fatalf("aws not found after FixPath; PATH=%s", os.Getenv("PATH"))
	}
}
